package notifyprefs

import (
	"net/http"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

// RegisterMerchant mounts the preference endpoints on the already authenticated
// merchant group.
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	handler := &Handler{service: service}
	group.GET("/notification-prefs", handler.getPrefs)
	group.PUT("/notification-prefs", handler.updatePrefs)
}

func (handler *Handler) getPrefs(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	prefs, err := handler.service.Prefs(ctx.Request.Context(), merchantID)
	if err != nil {
		writeNotifyError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, prefsResponse(prefs))
}

func (handler *Handler) updatePrefs(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	var input UpdateInput
	if err := ctx.ShouldBindJSON(&input); err != nil {
		invalidRequest(ctx)
		return
	}
	prefs, err := handler.service.UpdatePrefs(ctx.Request.Context(), merchantID, input)
	if err != nil {
		writeNotifyError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, prefsResponse(prefs))
}

func prefsResponse(prefs Prefs) gin.H {
	return gin.H{
		"notify_payment":    prefs.NotifyPayment,
		"notify_settlement": prefs.NotifySettlement,
		"notify_complaint":  prefs.NotifyComplaint,
		"notify_channel":    prefs.NotifyChannel,
		"updated_at":        prefs.UpdatedAt,
	}
}

func writeNotifyError(ctx *gin.Context) {
	if ctx.Request.Method == http.MethodPut {
		invalidRequest(ctx)
		return
	}
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
