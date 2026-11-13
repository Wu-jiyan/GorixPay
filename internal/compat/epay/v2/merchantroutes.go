package v2

import (
	"net/http"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

// RegisterMerchantRoutes mounts the V2 key management endpoints on an already
// authenticated merchant group (/api/v1/merchant).
func RegisterMerchantRoutes(group *gin.RouterGroup, deps Deps) {
	if group == nil {
		return
	}
	service := New(deps)
	group.GET("/credentials/v2", service.merchantCredential)
	group.POST("/credentials/v2/rotate", service.rotateMerchantCredential)
}

func (service *Service) merchantCredential(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	resolved, err := service.deps.Credentials.ResolveRSA(ctx.Request.Context(), identity.PrincipalID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, map[string]any{"code": "credential_not_found", "message": "v2 credential not found"})
		return
	}
	platformPEM, platformErr := service.platformPublicKey()
	if platformErr != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]any{"code": "internal_error", "message": "platform key unavailable"})
		return
	}
	ctx.JSON(http.StatusOK, map[string]any{
		"key_id":              resolved.Credential.KeyID,
		"public_key":          resolved.PublicKey,
		"created_at":          resolved.Credential.CreatedAt,
		"platform_public_key": platformPEM,
	})
}

func (service *Service) rotateMerchantCredential(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	created, privatePEM, err := service.deps.Credentials.RotateRSA(ctx.Request.Context(), identity.PrincipalID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]any{"code": "internal_error", "message": "credential rotation failed"})
		return
	}
	platformPEM, platformErr := service.platformPublicKey()
	if platformErr != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]any{"code": "internal_error", "message": "platform key unavailable"})
		return
	}
	publicKey := ""
	if created.PublicKey != nil {
		publicKey = *created.PublicKey
	}
	ctx.JSON(http.StatusCreated, map[string]any{
		"key_id":              created.KeyID,
		"private_key":         privatePEM,
		"public_key":          publicKey,
		"platform_public_key": platformPEM,
	})
}

func (service *Service) platformPublicKey() (string, error) {
	signer, err := service.loadSigner()
	if err != nil {
		return "", err
	}
	return signer.PublicKeyPEM(), nil
}
