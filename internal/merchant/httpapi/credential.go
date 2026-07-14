package httpapi

import (
	"net/http"

	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/merchant/credential"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	credentials *credential.Service
}

func Register(group *gin.RouterGroup, sessions *session.Service, credentials *credential.Service) *gin.RouterGroup {
	handler := &Handler{credentials: credentials}
	protected := group.Group("", identityhttp.Authenticate(sessions), requireMerchant)
	protected.GET("/credentials", handler.list)
	protected.POST("/credentials/md5/rotate", handler.rotateMD5)
	return protected
}

func requireMerchant(ctx *gin.Context) {
	if identityhttp.MustSession(ctx).PrincipalType != model.PrincipalMerchant {
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "merchant access required"})
		return
	}
	ctx.Next()
}

func (handler *Handler) list(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	var credentials []model.MerchantCredential
	if err := handler.credentials.List(ctx.Request.Context(), identity.PrincipalID, &credentials); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	items := make([]gin.H, 0, len(credentials))
	for _, item := range credentials {
		items = append(items, gin.H{
			"id":              item.ID,
			"credential_type": item.CredentialType,
			"key_id":          item.KeyID,
			"status":          item.Status,
			"created_at":      item.CreatedAt,
			"revoked_at":      item.RevokedAt,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) rotateMD5(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	created, plain, err := handler.credentials.RotateMD5(ctx.Request.Context(), identity.PrincipalID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{
		"id":              created.ID,
		"credential_type": created.CredentialType,
		"key_id":          created.KeyID,
		"secret":          plain,
		"created_at":      created.CreatedAt,
	})
}
