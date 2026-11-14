package merchantgroup

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Register mounts the administrator merchant-group endpoints.
//
// group may be either the API root (/api/v1 — the same argument shape
// adminhttp.Register takes) or an already authenticated administrator group
// (/api/v1/admin — the shape certification.RegisterAdmin takes). The routes are
// always mounted under /admin and always carry the session and administrator
// middlewares, so a caller that passes an unprotected group still gets a closed
// door. sessions must be identityhttp.Authenticate(sessionsService); a nil value
// registers nothing rather than exposing an unauthenticated admin surface.
//
// The per-merchant assignment lives here as well (PATCH /admin/merchants/:id/group):
// it shares the group service, and the route sits next to the existing
// /admin/merchants/:id/status without colliding with it (same ":id" parameter).
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc) {
	if group == nil || db == nil || sessions == nil {
		return
	}
	service := New(db, nil)
	admin := group
	if !strings.HasSuffix(strings.TrimRight(group.BasePath(), "/"), "/admin") {
		admin = group.Group("/admin")
	}
	// Use() on an existing group only affects the routes registered below it.
	admin.Use(sessions, identityhttp.RequireAdministrator())

	admin.GET("/groups", service.list)
	admin.POST("/groups", service.create)
	admin.PUT("/groups/:id", service.update)
	admin.PATCH("/groups/:id/status", service.setStatus)
	admin.DELETE("/groups/:id", service.remove)
	admin.PATCH("/merchants/:id/group", service.assignMerchant)
}

type groupPayload struct {
	Name       string   `json:"name"`
	RateBp     int      `json:"rate_bp"`
	PayTypes   []string `json:"pay_types"`
	SettleMode string   `json:"settle_mode"`
}

func (payload groupPayload) input() Input {
	return Input{Name: payload.Name, RateBp: payload.RateBp, PayTypes: payload.PayTypes, SettleMode: payload.SettleMode}
}

type statusPayload struct {
	Status string `json:"status"`
}

// assignmentPayload keeps "group_id": null (move the merchant out of every group)
// distinguishable from a missing field, which is a malformed request rather than
// a silent unassignment.
type assignmentPayload struct {
	GroupID json.RawMessage `json:"group_id"`
}

func (payload assignmentPayload) target() (groupID *string, ok bool) {
	raw := strings.TrimSpace(string(payload.GroupID))
	switch {
	case raw == "":
		return nil, false
	case raw == "null":
		return nil, true
	}
	var value string
	if err := json.Unmarshal(payload.GroupID, &value); err != nil {
		return nil, false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true
	}
	return &value, true
}

func (service *Service) list(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	views, total, err := service.List(ctx.Request.Context(),
		Filter{Status: ctx.Query("status"), Keyword: ctx.Query("keyword")},
		Page{Number: page, Size: size})
	if err != nil {
		respondError(ctx, err)
		return
	}
	items := make([]gin.H, 0, len(views))
	for _, view := range views {
		items = append(items, groupResponse(view))
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (service *Service) create(ctx *gin.Context) {
	var payload groupPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	view, err := service.Create(ctx.Request.Context(), payload.input())
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, groupResponse(view))
}

func (service *Service) update(ctx *gin.Context) {
	var payload groupPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	view, err := service.Update(ctx.Request.Context(), ctx.Param("id"), payload.input())
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, groupResponse(view))
}

func (service *Service) setStatus(ctx *gin.Context) {
	var payload statusPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	view, err := service.SetStatus(ctx.Request.Context(), ctx.Param("id"), payload.Status)
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, groupResponse(view))
}

func (service *Service) remove(ctx *gin.Context) {
	if err := service.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		respondError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (service *Service) assignMerchant(ctx *gin.Context) {
	var payload assignmentPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	groupID, ok := payload.target()
	if !ok {
		invalidRequest(ctx, "group_id is required (use null to remove the merchant from its group)")
		return
	}
	assignment, err := service.AssignMerchant(ctx.Request.Context(), ctx.Param("id"), groupID)
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"merchant_id": assignment.MerchantID,
		"group_id":    assignment.GroupID,
		"settle_mode": assignment.SettleMode,
	})
}

func groupResponse(view GroupView) gin.H {
	return gin.H{
		"id":             view.ID,
		"name":           view.Name,
		"rate_bp":        view.RateBp,
		"pay_types":      payTypeResponse(view.PayTypes),
		"settle_mode":    view.SettleMode,
		"status":         view.Status,
		"merchant_count": view.MerchantCount,
		"created_at":     view.CreatedAt,
		"updated_at":     view.UpdatedAt,
	}
}

func payTypeResponse(types PayTypes) []string {
	if len(types) == 0 {
		return []string{}
	}
	return []string(types)
}

func respondError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalidRequest(ctx, "invalid request")
	case errors.Is(err, ErrDuplicateName):
		ctx.JSON(http.StatusConflict, gin.H{"code": "duplicate_name", "message": "group name already exists"})
	case errors.Is(err, ErrGroupInUse):
		ctx.JSON(http.StatusConflict, gin.H{"code": "group_in_use", "message": "group still has merchants"})
	case errors.Is(err, ErrGroupDisabled):
		ctx.JSON(http.StatusConflict, gin.H{"code": "group_disabled", "message": "group is disabled"})
	case errors.Is(err, ErrMerchantNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "merchant not found"})
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "merchant group not found"})
	default:
		internalError(ctx)
	}
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
