package invitecode

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

// RegisterAdmin mounts the invite code CRUD on the already authenticated
// administrator group.
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	handler := &Handler{service: service}
	group.GET("/invite-codes", handler.list)
	group.POST("/invite-codes", handler.create)
	group.PUT("/invite-codes/:id", handler.update)
	group.DELETE("/invite-codes/:id", handler.remove)
}

func (handler *Handler) list(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	items, total, err := handler.service.List(ctx.Request.Context(), Filter{
		Status: ctx.Query("status"), Keyword: ctx.Query("keyword"),
	}, Page{Number: page, Size: size})
	if err != nil {
		writeInviteError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) create(ctx *gin.Context) {
	var request struct {
		Code            string     `json:"code"`
		OwnerMerchantID *string    `json:"owner_merchant_id"`
		MaxUses         int        `json:"max_uses"`
		ExpiresAt       *time.Time `json:"expires_at"`
		Status          Status     `json:"status"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidRequest(ctx)
		return
	}
	created, err := handler.service.Create(ctx.Request.Context(), CreateInput{
		Code: request.Code, OwnerMerchantID: request.OwnerMerchantID,
		MaxUses: request.MaxUses, ExpiresAt: request.ExpiresAt, Status: request.Status,
	})
	if err != nil {
		writeInviteError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, created)
}

func (handler *Handler) update(ctx *gin.Context) {
	var request struct {
		OwnerMerchantID *string    `json:"owner_merchant_id"`
		MaxUses         *int       `json:"max_uses"`
		ExpiresAt       *time.Time `json:"expires_at"`
		ClearExpiry     bool       `json:"clear_expiry"`
		Status          Status     `json:"status"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidRequest(ctx)
		return
	}
	updated, err := handler.service.Update(ctx.Request.Context(), ctx.Param("id"), UpdateInput{
		OwnerMerchantID: request.OwnerMerchantID, MaxUses: request.MaxUses,
		ExpiresAt: request.ExpiresAt, ClearExpiry: request.ClearExpiry, Status: request.Status,
	})
	if err != nil {
		writeInviteError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, updated)
}

func (handler *Handler) remove(ctx *gin.Context) {
	if err := handler.service.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeInviteError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func pagination(ctx *gin.Context) (int, int, bool) {
	page, err := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		invalidRequest(ctx)
		return 0, 0, false
	}
	size, err := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if err != nil || size < 1 || size > 100 {
		invalidRequest(ctx)
		return 0, 0, false
	}
	return page, size, true
}

func writeInviteError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalidRequest(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "invite code not found"})
	case errors.Is(err, ErrDuplicate):
		ctx.JSON(http.StatusConflict, gin.H{"code": "code_exists", "message": "invite code already exists"})
	case errors.Is(err, ErrInvalid):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invite_code_invalid", "message": "invite code is invalid"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
