package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxDisplayNameLength = 128

type ProfileHandler struct {
	db *gorm.DB
}

// RegisterProfile mounts the merchant self-service endpoints. The group is
// already authenticated and merchant-scoped, so handlers only trust the session
// principal for the merchant identity.
func RegisterProfile(group *gin.RouterGroup, db *gorm.DB) {
	handler := &ProfileHandler{db: db}
	group.GET("/profile", handler.profile)
	group.PUT("/profile", handler.updateProfile)
	group.GET("/dashboard", handler.dashboard)
}

func (handler *ProfileHandler) profile(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	merchant, err := handler.merchant(ctx, identity.PrincipalID)
	if err != nil {
		profileError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, profileResponse(merchant))
}

func (handler *ProfileHandler) updateProfile(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	var request struct {
		DisplayName string `json:"display_name"`
		// Settlement destination, optional: pre_user.settle_id/account.
		SettleType    *int    `json:"settle_type"`
		SettleAccount *string `json:"settle_account"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		profileInvalidRequest(ctx)
		return
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > maxDisplayNameLength {
		profileInvalidRequest(ctx)
		return
	}
	updates := map[string]any{"display_name": displayName, "updated_at": time.Now().UTC()}
	if request.SettleType != nil {
		if *request.SettleType < 0 || *request.SettleType > 2 {
			// 0 = 未设置, 1 = 支付宝, 2 = 银行卡 (pre_user.settle_id)
			profileInvalidRequest(ctx)
			return
		}
		updates["settle_type"] = *request.SettleType
	}
	if request.SettleAccount != nil {
		account := strings.TrimSpace(*request.SettleAccount)
		if utf8.RuneCountInString(account) > 128 {
			profileInvalidRequest(ctx)
			return
		}
		if account == "" {
			updates["settle_account"] = nil
		} else {
			updates["settle_account"] = account
		}
	}
	result := handler.db.WithContext(ctx.Request.Context()).Model(&identitymodel.Merchant{}).
		Where("id = ?", identity.PrincipalID).
		Updates(updates)
	if result.Error != nil {
		profileInternalError(ctx)
		return
	}
	merchant, err := handler.merchant(ctx, identity.PrincipalID)
	if err != nil {
		profileError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, profileResponse(merchant))
}

func (handler *ProfileHandler) merchant(ctx *gin.Context, merchantID string) (identitymodel.Merchant, error) {
	var merchant identitymodel.Merchant
	if err := handler.db.WithContext(ctx.Request.Context()).First(&merchant, "id = ?", merchantID).Error; err != nil {
		return identitymodel.Merchant{}, err
	}
	return merchant, nil
}

func profileResponse(merchant identitymodel.Merchant) gin.H {
	return gin.H{
		"id":             merchant.ID,
		"legacy_uid":     merchant.LegacyUID,
		"display_name":   merchant.DisplayName,
		"email":          merchant.Email,
		"phone":          merchant.Phone,
		"status":         merchant.Status,
		"settle_type":    merchant.SettleType,
		"settle_account": merchant.SettleAccount,
		"created_at":     merchant.CreatedAt,
	}
}

func profileError(ctx *gin.Context, err error) {
	if err == gorm.ErrRecordNotFound {
		profileNotFound(ctx)
		return
	}
	profileInternalError(ctx)
}

func profileInvalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func profileNotFound(ctx *gin.Context) {
	ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "merchant not found"})
}

func profileInternalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
