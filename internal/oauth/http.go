package oauth

import (
	"errors"
	"net/http"
	"strings"

	identityauth "gorixpay/internal/identity/auth"
	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	service   *Service
	publicURL string
}

// secureCookies follows the public scheme, like the password login does: behind a
// TLS terminator the inbound hop is plain HTTP.
func (handler *Handler) secureCookies() bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(handler.publicURL)), "https://")
}

// Register mounts the OAuth and QR login routes on the authenticated /api/v1
// group. It returns the service so the caller can reuse it (for example to run
// periodic cleanup of expired tickets).
func Register(group *gin.RouterGroup, db *gorm.DB, sessions *session.Service, cipher *secret.Cipher, publicURL string) *Service {
	if group == nil || db == nil || sessions == nil {
		return nil
	}
	service := New(db, sessions, cipher, publicURL)
	register(group, service, sessions)
	return service
}

// RegisterWithProviders is Register plus explicit provider injection. Tests use
// it to swap the open platform endpoints for a fake, and a deployment can use it
// to terminate OAuth at its own gateway.
func RegisterWithProviders(group *gin.RouterGroup, db *gorm.DB, sessions *session.Service, cipher *secret.Cipher, publicURL string, providers ...Provider) *Service {
	if group == nil || db == nil || sessions == nil {
		return nil
	}
	service := New(db, sessions, cipher, publicURL).WithProviders(providers...)
	register(group, service, sessions)
	return service
}

func register(group *gin.RouterGroup, service *Service, sessions *session.Service) {
	handler := &Handler{service: service, publicURL: service.publicURL}
	authenticate := identityhttp.Authenticate(sessions)

	public := group.Group("/auth")
	public.GET("/oauth/:provider/authorize", handler.authorize)
	public.GET("/oauth/:provider/callback", handler.callback)
	public.POST("/oauth/bind", handler.bind)
	public.POST("/qr/ticket", handler.qrTicket)
	public.GET("/qr/poll", handler.qrPoll)
	public.POST("/qr/scan", handler.qrScan)

	merchant := group.Group("/merchant", authenticate, requireMerchant)
	merchant.GET("/oauth/bindings", handler.listBindings)
	merchant.DELETE("/oauth/bindings/:provider", handler.deleteBinding)
	merchant.POST("/qr/confirm", handler.qrConfirm)
}

func requireMerchant(ctx *gin.Context) {
	if identityhttp.MustSession(ctx).PrincipalType != identitymodel.PrincipalMerchant {
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "merchant access required"})
		return
	}
	ctx.Next()
}

func (handler *Handler) authorize(ctx *gin.Context) {
	providerName := strings.TrimSpace(ctx.Param("provider"))
	state, err := handler.service.CreateState(ctx.Request.Context(), providerName, ctx.Query("redirect"))
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	provider, err := handler.service.provider(ctx.Request.Context(), providerName)
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.Redirect(http.StatusFound, provider.AuthorizeURL(state))
}

func (handler *Handler) callback(ctx *gin.Context) {
	result, err := handler.service.Callback(ctx.Request.Context(),
		strings.TrimSpace(ctx.Param("provider")), ctx.Query("code"), ctx.Query("state"), metadataFrom(ctx))
	if err != nil {
		var notBound *NotBoundError
		if errors.As(err, &notBound) {
			ctx.JSON(http.StatusConflict, gin.H{
				"code":        "not_bound",
				"message":     "this account is not bound yet",
				"bind_ticket": notBound.BindTicket,
			})
			return
		}
		writeOAuthError(ctx, err)
		return
	}
	// The SPA keeps no token in JavaScript: the browser session is the cookie, so
	// a third party login must set it here.
	identityhttp.WriteSessionCookie(ctx, result.Token, result.ExpiresAt, handler.secureCookies())
	ctx.JSON(http.StatusOK, result)
}

func (handler *Handler) bind(ctx *gin.Context) {
	var request struct {
		BindTicket string `json:"bind_ticket"`
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidRequest(ctx)
		return
	}
	result, err := handler.service.Bind(ctx.Request.Context(), request.BindTicket, request.Identifier, request.Password, metadataFrom(ctx))
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (handler *Handler) listBindings(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	items, err := handler.service.ListBindings(ctx.Request.Context(), string(principal.PrincipalType), principal.PrincipalID)
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	responses := make([]gin.H, 0, len(items))
	for _, item := range items {
		responses = append(responses, gin.H{
			"provider": item.Provider, "open_id": item.OpenID, "nickname": item.Nickname,
			"avatar": item.Avatar, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": responses})
}

func (handler *Handler) deleteBinding(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	if err := handler.service.DeleteBinding(ctx.Request.Context(), string(principal.PrincipalType), principal.PrincipalID, strings.TrimSpace(ctx.Param("provider"))); err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) qrTicket(ctx *gin.Context) {
	ticket, err := handler.service.CreateQRTicket(ctx.Request.Context())
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, ticket)
}

func (handler *Handler) qrPoll(ctx *gin.Context) {
	result, err := handler.service.PollQRTicket(ctx.Request.Context(), ctx.Query("ticket"), metadataFrom(ctx))
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	// A confirmed scan hands the browser its session here: the SPA never stores a
	// token, so a token in the JSON alone would leave the user logged out.
	if result.Status == statusConfirmed && result.Token != "" && result.ExpiresAt != nil {
		identityhttp.WriteSessionCookie(ctx, result.Token, *result.ExpiresAt, handler.secureCookies())
	}
	ctx.JSON(http.StatusOK, result)
}

func (handler *Handler) qrScan(ctx *gin.Context) {
	ticket := strings.TrimSpace(ctx.Query("ticket"))
	if ticket == "" {
		var request struct {
			Ticket string `json:"ticket"`
		}
		_ = ctx.ShouldBindJSON(&request)
		ticket = strings.TrimSpace(request.Ticket)
	}
	status, err := handler.service.ScanQRTicket(ctx.Request.Context(), ticket)
	if err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": status})
}

func (handler *Handler) qrConfirm(ctx *gin.Context) {
	var request struct {
		Ticket string `json:"ticket"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidRequest(ctx)
		return
	}
	principal := identityhttp.MustSession(ctx)
	if err := handler.service.ConfirmQRTicket(ctx.Request.Context(), request.Ticket, string(principal.PrincipalType), principal.PrincipalID); err != nil {
		writeOAuthError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": statusConfirmed})
}

func metadataFrom(ctx *gin.Context) ClientMetadata {
	return ClientMetadata{IPAddress: ctx.ClientIP(), UserAgent: ctx.Request.UserAgent()}
}

func writeOAuthError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrProviderUnknown):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "provider_not_found", "message": "unknown oauth provider"})
	case errors.Is(err, ErrNotConfigured):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "provider_not_configured", "message": "oauth provider is not configured"})
	case errors.Is(err, ErrInvalidState):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_state", "message": "oauth state is invalid or already used"})
	case errors.Is(err, ErrStateExpired):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "state_expired", "message": "oauth state expired"})
	case errors.Is(err, ErrExchangeFailed):
		ctx.JSON(http.StatusBadGateway, gin.H{"code": "provider_exchange_failed", "message": "oauth provider rejected the exchange"})
	case errors.Is(err, identityauth.ErrInvalidCredentials):
		ctx.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_credentials", "message": "invalid credentials"})
	case errors.Is(err, ErrInvalidBindTicket):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "bind_ticket_invalid", "message": "bind ticket is invalid"})
	case errors.Is(err, ErrBindTicketExpired):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "bind_ticket_expired", "message": "bind ticket expired"})
	case errors.Is(err, ErrAlreadyBound):
		ctx.JSON(http.StatusConflict, gin.H{"code": "already_bound", "message": "this identity is bound to another account"})
	case errors.Is(err, ErrExpired):
		ctx.JSON(http.StatusGone, gin.H{"code": "ticket_expired", "message": "qr ticket expired"})
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "record not found"})
	default:
		if errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrPrincipalRequired) {
			invalidRequest(ctx)
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
