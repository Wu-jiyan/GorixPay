package invite

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

// RegisterMerchant mounts the merchant invite endpoints on an authenticated
// merchant group (/api/v1/merchant).
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/invite", service.stats)
	group.GET("/invite/records", service.recordsView)
}

// RegisterAdmin mounts the invite rebate listing on an authenticated
// administrator group (/api/v1/admin).
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/invites", service.adminRecords)
}

func (service *Service) stats(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	stats, err := service.Stats(ctx.Request.Context(), merchantID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "merchant not found"})
			return
		}
		invalidRequest(ctx, "invalid request")
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"enabled":       stats.Enabled,
		"rate_bp":       stats.RateBP,
		"invite_code":   stats.InviteCode,
		"invite_url":    stats.InviteURL,
		"invited_count": stats.InvitedCount,
		"today_income":  stats.TodayIncome,
		"total_income":  stats.TotalIncome,
	})
}

func (service *Service) recordsView(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	views, total, err := service.Records(ctx.Request.Context(), merchantID, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": recordResponses(views), "total": total, "page": page, "page_size": size})
}

func (service *Service) adminRecords(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	views, total, err := service.AdminRecords(ctx.Request.Context(), ctx.Query("inviter_id"), Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": recordResponses(views), "total": total, "page": page, "page_size": size})
}

func recordResponses(views []RecordView) []gin.H {
	items := make([]gin.H, 0, len(views))
	for _, view := range views {
		items = append(items, gin.H{
			"id":            view.Rebate.ID,
			"inviter_id":    view.Rebate.InviterID,
			"invitee_id":    view.Rebate.InviteeID,
			"invitee_name":  view.InviteeName,
			"order_id":      view.Rebate.OrderID,
			"trade_no":      view.Rebate.TradeNo,
			"order_amount":  view.Rebate.OrderAmount,
			"rebate_amount": view.Rebate.RebateAmount,
			"rate_bp":       view.Rebate.RateBP,
			"created_at":    view.Rebate.CreatedAt.Format(time.RFC3339),
		})
	}
	return items
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
