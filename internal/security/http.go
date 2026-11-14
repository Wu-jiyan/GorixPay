package security

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

const (
	httpDefaultPageSize = 20
	httpMaxPageSize     = 100
)

type Handler struct {
	service *Service
}

// Register mounts the captcha endpoint and the administrator security routes.
// group is the authenticated /api/v1 group and sessions is the session
// middleware, which already accepts both the Bearer token and the session
// cookie.
func Register(group *gin.RouterGroup, sessions gin.HandlerFunc, service *Service) {
	if group == nil || service == nil || sessions == nil {
		return
	}
	handler := &Handler{service: service}
	public := group.Group("/auth")
	public.GET("/captcha", handler.captcha)

	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.GET("/security/totp", handler.totpStatus)
	admin.POST("/security/totp/setup", handler.totpSetup)
	admin.POST("/security/totp/enable", handler.totpEnable)
	admin.POST("/security/totp/disable", handler.totpDisable)
	admin.GET("/security/login-attempts", handler.loginAttempts)
}

func (handler *Handler) captcha(ctx *gin.Context) {
	generated, err := handler.service.CreateCaptcha(ctx.Request.Context())
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("X-Captcha-Id", generated.ID)
	ctx.JSON(http.StatusOK, gin.H{"id": generated.ID, "image_svg": generated.SVG})
}

func (handler *Handler) totpStatus(ctx *gin.Context) {
	status, err := handler.service.TOTPStatus(ctx.Request.Context(), identityhttp.MustSession(ctx).PrincipalID)
	if err != nil {
		securityError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, status)
}

func (handler *Handler) totpSetup(ctx *gin.Context) {
	administratorID := identityhttp.MustSession(ctx).PrincipalID
	account, err := handler.service.AdministratorName(ctx.Request.Context(), administratorID)
	if err != nil {
		securityError(ctx, err)
		return
	}
	secret, otpauthURL, err := handler.service.SetupTOTP(ctx.Request.Context(), administratorID, account)
	if err != nil {
		securityError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"secret": secret, "otpauth_url": otpauthURL})
}

func (handler *Handler) totpEnable(ctx *gin.Context) {
	code, ok := codeFromBody(ctx)
	if !ok {
		return
	}
	status, err := handler.service.EnableTOTP(ctx.Request.Context(), identityhttp.MustSession(ctx).PrincipalID, code)
	if err != nil {
		securityError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, status)
}

func (handler *Handler) totpDisable(ctx *gin.Context) {
	code, ok := codeFromBody(ctx)
	if !ok {
		return
	}
	if err := handler.service.DisableTOTP(ctx.Request.Context(), identityhttp.MustSession(ctx).PrincipalID, code); err != nil {
		securityError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"enabled": false})
}

func (handler *Handler) loginAttempts(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	filter := LoginAttemptFilter{Identifier: strings.TrimSpace(ctx.Query("identifier"))}
	if raw := strings.TrimSpace(ctx.Query("success")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			invalidRequest(ctx)
			return
		}
		filter.Success = &parsed
	}
	items, total, err := handler.service.ListLoginAttempts(ctx.Request.Context(), filter, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func codeFromBody(ctx *gin.Context) (string, bool) {
	var request struct {
		Code string `json:"code"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidRequest(ctx)
		return "", false
	}
	code := strings.TrimSpace(request.Code)
	if code == "" {
		invalidRequest(ctx)
		return "", false
	}
	return code, true
}

func pagination(ctx *gin.Context) (int, int, bool) {
	page, err := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		invalidRequest(ctx)
		return 0, 0, false
	}
	size, err := strconv.Atoi(ctx.DefaultQuery("page_size", strconv.Itoa(httpDefaultPageSize)))
	if err != nil || size < 1 || size > httpMaxPageSize {
		invalidRequest(ctx)
		return 0, 0, false
	}
	return page, size, true
}

func securityError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalidRequest(ctx)
	case errors.Is(err, ErrTOTPInvalid):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "totp_invalid", "message": "totp code invalid"})
	case errors.Is(err, ErrTOTPReused):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "totp_reused", "message": "totp code already used, wait for the next one"})
	case errors.Is(err, ErrTOTPNotSetup):
		ctx.JSON(http.StatusConflict, gin.H{"code": "totp_not_setup", "message": "totp is not set up"})
	case errors.Is(err, ErrTOTPAlreadyEnabled):
		ctx.JSON(http.StatusConflict, gin.H{"code": "totp_already_enabled", "message": "totp is already enabled"})
	case errors.Is(err, ErrCipherMissing):
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "master key is not configured"})
	default:
		internalError(ctx)
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
