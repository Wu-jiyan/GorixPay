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
			"display_name": result.Merchant.DisplayName,
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
