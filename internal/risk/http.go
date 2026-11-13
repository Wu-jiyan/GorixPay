package risk

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

type blacklistPayload struct {
	EntryType string `json:"entry_type"`
	Value     string `json:"value"`
	Remark    string `json:"remark"`
}

// RegisterAdmin mounts the blacklist and risk event endpoints on an
// authenticated administrator group (/api/v1/admin).
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/blacklist", service.listBlacklist)
	group.POST("/blacklist", service.addBlacklist)
	group.DELETE("/blacklist/:id", service.removeBlacklist)
	group.GET("/risk-events", service.listAllEvents)
}

// RegisterMerchant mounts the merchant risk event endpoint on an authenticated
// merchant group (/api/v1/merchant).
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/risk-events", service.listMyEvents)
}

func (service *Service) listBlacklist(ctx *gin.Context) {
	entries, err := service.ListBlacklist(ctx.Request.Context(), ctx.Query("entry_type"))
	if err != nil {
		if errors.Is(err, ErrInvalidRequest) {
			invalidRequest(ctx, "invalid entry type")
			return
		}
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": entries})
}

func (service *Service) addBlacklist(ctx *gin.Context) {
	var payload blacklistPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	entry, err := service.AddBlacklist(ctx.Request.Context(), payload.EntryType, payload.Value, payload.Remark)
	if err != nil {
		switch {
		case errors.Is(err, ErrDuplicate):
			ctx.JSON(http.StatusConflict, gin.H{"code": "duplicate", "message": "blacklist entry already exists"})
		default:
			invalidRequest(ctx, "invalid request")
		}
		return
	}
	ctx.JSON(http.StatusCreated, entry)
}

func (service *Service) removeBlacklist(ctx *gin.Context) {
	if err := service.RemoveBlacklist(ctx.Request.Context(), ctx.Param("id")); err != nil {
		if errors.Is(err, ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "blacklist entry not found"})
			return
		}
		internalError(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (service *Service) listAllEvents(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	events, total, err := service.ListEvents(ctx.Request.Context(), Filter{
		MerchantID: strings.TrimSpace(ctx.Query("merchant_id")),
		RuleCode:   strings.TrimSpace(ctx.Query("rule_code")),
	}, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": events, "total": total, "page": page, "page_size": size})
}

func (service *Service) listMyEvents(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	events, total, err := service.ListEvents(ctx.Request.Context(), Filter{MerchantID: merchantID}, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": events, "total": total, "page": page, "page_size": size})
}

func pagination(ctx *gin.Context) (int, int, bool) {
	page := 1
	size := 20
	if raw := strings.TrimSpace(ctx.DefaultQuery("page", "1")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			invalidRequest(ctx, "invalid page")
			return 0, 0, false
		}
		page = parsed
	}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page_size", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			invalidRequest(ctx, "invalid page size")
			return 0, 0, false
		}
		size = parsed
	}
	return page, size, true
}

func invalidRequest(ctx *gin.Context, message string) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": message})
}

func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
