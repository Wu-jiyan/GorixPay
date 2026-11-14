package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"gorixpay/internal/identity/auth"
	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"

	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "gorixpay_session"
	principalKey      = "identity_principal"
)

type Handler struct {
	auth     *auth.Service
	sessions *session.Service
}

func Register(group *gin.RouterGroup, authService *auth.Service, sessions *session.Service) {
	handler := &Handler{auth: authService, sessions: sessions}
	group.POST("/auth/merchant/login", handler.loginMerchant)
	group.POST("/auth/administrator/login", handler.loginAdministrator)
	group.POST("/auth/register", handler.registerMerchant)
	protected := group.Group("/auth", Authenticate(sessions))
	protected.GET("/me", handler.me)
	protected.POST("/logout", handler.logout)
}

func RequireAdministrator() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity := MustSession(ctx)
		if identity.PrincipalType != model.PrincipalAdministrator {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "administrator access required"})
			return
		}
		ctx.Next()
	}
}

type loginRequest struct {
	Identifier string `json:"identifier" binding:"required"`
	Password   string `json:"password" binding:"required"`
	// Optional anti-bot and second-factor fields. The security package decides
	// whether they are required.
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
	TOTPCode    string `json:"totp_code"`
}

type registerMerchantRequest struct {
	Identifier string `json:"identifier" binding:"required"`
	Password   string `json:"password" binding:"required,min=4"`
	Invite     string `json:"invite"`
	// InviteCode is a registration invite code issued by the operator, separate
	// from the inviter code above.
	InviteCode string `json:"invite_code"`
}

func (handler *Handler) registerMerchant(ctx *gin.Context) {
	var request registerMerchantRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
		return
	}
	result, err := handler.auth.RegisterMerchantWithCodes(ctx.Request.Context(), request.Identifier, request.Password, request.Invite, request.InviteCode, auth.LoginMetadata{
		IPAddress: ctx.ClientIP(),
		UserAgent: ctx.Request.UserAgent(),
	})
	if err != nil {
		if errors.Is(err, auth.ErrMerchantExists) {
			ctx.JSON(http.StatusConflict, gin.H{"code": "merchant_exists", "message": "merchant already exists"})
			return
		}
		if errors.Is(err, auth.ErrInviteCodeInvalid) {
			ctx.JSON(http.StatusBadRequest, gin.H{"code": "invite_code_invalid", "message": "invite code is invalid"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    result.Token,
		Path:     "/",
		Expires:  result.Session.ExpiresAt,
		HttpOnly: true,
		Secure:   ctx.Request.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	ctx.JSON(http.StatusCreated, gin.H{
		"token":      result.Token,
		"expires_at": result.Session.ExpiresAt,
		"merchant": gin.H{
			"id":           result.Merchant.ID,
			"legacy_uid":   result.Merchant.LegacyUID,
			"display_name": result.Merchant.DisplayName,
			"invite_code":  auth.InviteCodeOf(result.Merchant),
		},
	})
}

func (handler *Handler) loginMerchant(ctx *gin.Context) {
	var request loginRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
		return
	}
	result, err := handler.auth.LoginMerchant(ctx.Request.Context(), request.Identifier, request.Password, auth.LoginMetadata{
		IPAddress: ctx.ClientIP(),
		UserAgent: ctx.Request.UserAgent(),
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			ctx.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_credentials", "message": "invalid credentials"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    result.Token,
		Path:     "/",
		Expires:  result.Session.ExpiresAt,
		HttpOnly: true,
		Secure:   ctx.Request.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	ctx.JSON(http.StatusOK, gin.H{
		"token":      result.Token,
		"expires_at": result.Session.ExpiresAt,
		"merchant": gin.H{
			"id":           result.Merchant.ID,
			"legacy_uid":   result.Merchant.LegacyUID,
			"display_name": result.Merchant.DisplayName,
			"invite_code":  auth.InviteCodeOf(result.Merchant),
		},
	})
}

func (handler *Handler) loginAdministrator(ctx *gin.Context) {
	var request loginRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
		return
	}
	result, err := handler.auth.LoginAdministrator(ctx.Request.Context(), request.Identifier, request.Password, auth.LoginMetadata{
		IPAddress: ctx.ClientIP(), UserAgent: ctx.Request.UserAgent(),
		CaptchaID: request.CaptchaID, CaptchaCode: request.CaptchaCode, TOTPCode: request.TOTPCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCaptchaRequired):
			ctx.JSON(http.StatusBadRequest, gin.H{"code": "captcha_required", "message": "captcha required"})
		case errors.Is(err, auth.ErrCaptchaInvalid):
			ctx.JSON(http.StatusBadRequest, gin.H{"code": "captcha_invalid", "message": "captcha invalid"})
		case errors.Is(err, auth.ErrTOTPRequired):
			ctx.JSON(http.StatusUnauthorized, gin.H{"code": "totp_required", "message": "totp code required"})
		case errors.Is(err, auth.ErrTOTPInvalid):
			ctx.JSON(http.StatusUnauthorized, gin.H{"code": "totp_invalid", "message": "totp code invalid"})
		case errors.Is(err, auth.ErrInvalidCredentials):
			ctx.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_credentials", "message": "invalid credentials"})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		}
		return
	}
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name: sessionCookieName, Value: result.Token, Path: "/", Expires: result.Session.ExpiresAt,
		HttpOnly: true, Secure: ctx.Request.TLS != nil, SameSite: http.SameSiteLaxMode,
	})
	ctx.JSON(http.StatusOK, gin.H{
		"token": result.Token, "expires_at": result.Session.ExpiresAt,
		"administrator": gin.H{"id": result.Administrator.ID, "username": result.Administrator.Username, "display_name": result.Administrator.DisplayName},
	})
}

func Authenticate(sessions *session.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := bearerToken(ctx.GetHeader("Authorization"))
		if token == "" {
			if cookie, err := ctx.Cookie(sessionCookieName); err == nil {
				token = cookie
			}
		}
		identity, err := sessions.Authenticate(ctx.Request.Context(), token)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "unauthorized", "message": "authentication required"})
			return
		}
		ctx.Set(principalKey, identity)
		ctx.Next()
	}
}

func MustSession(ctx *gin.Context) model.Session {
	return ctx.MustGet(principalKey).(model.Session)
}

func (handler *Handler) me(ctx *gin.Context) {
	identity := MustSession(ctx)
	ctx.JSON(http.StatusOK, gin.H{
		"principal_type": identity.PrincipalType,
		"principal_id":   identity.PrincipalID,
		"session_id":     identity.ID,
		"expires_at":     identity.ExpiresAt,
	})
}

func (handler *Handler) logout(ctx *gin.Context) {
	identity := MustSession(ctx)
	if err := handler.sessions.Revoke(ctx.Request.Context(), identity.ID); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   ctx.Request.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	ctx.Status(http.StatusNoContent)
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
