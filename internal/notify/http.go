package notify

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Option tweaks the service Register builds.
type Option func(*Config)

// WithSender injects the production Sender (SMS/mail gateway). Without it the
// development log sender is used and reset tokens are written to the log.
func WithSender(sender Sender) Option {
	return func(config *Config) { config.Sender = sender }
}

// WithClock swaps the clock; tests use it to move past the rate limits.
func WithClock(now func() time.Time) Option {
	return func(config *Config) { config.Now = now }
}

// WithLogger sets the log sink (stdout by default through slog-free fmt).
func WithLogger(logf func(format string, arguments ...any)) Option {
	return func(config *Config) { config.Logf = logf }
}

// WithTokenLogging forces reset tokens in or out of the log. By default they are
// logged only when no real Sender is configured (development).
func WithTokenLogging(enabled bool) Option {
	return func(config *Config) { config.LogTokens = &enabled }
}

// Register mounts the M8 routes on the caller's /api/v1 group:
//
//	public:   POST /auth/merchant/password/forgot, /auth/merchant/password/reset, /auth/code
//	merchant: POST /merchant/password/change              (session required)
//	admin:    /admin/notification-channels...             (administrator required)
//
// The /auth sub-group deliberately has no session middleware: a merchant who lost
// the password cannot authenticate yet. Every other route carries the same
// middlewares the rest of the application uses.
//
// cipher may be nil when the deployment runs without a master key: the code and
// password routes keep working and the channel routes answer 503, which matches
// the "no key, no encrypted feature" rule the rest of app.go follows.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions *session.Service, cipher *secret.Cipher, options ...Option) {
	if group == nil || db == nil || sessions == nil {
		return
	}
	config := Config{DB: db, Cipher: cipher}
	for _, option := range options {
		option(&config)
	}
	service, err := NewService(config)
	if err != nil {
		// Unreachable after the nil checks above; returning keeps an unauthenticated
		// or unencrypted surface from being mounted by accident.
		return
	}
	handler := &handler{service: service}

	public := group.Group("/auth")
	public.POST("/merchant/password/forgot", handler.forgotPassword)
	public.POST("/merchant/password/reset", handler.resetPassword)
	public.POST("/code", handler.sendCode)

	merchant := group.Group("/merchant", identityhttp.Authenticate(sessions))
	merchant.POST("/password/change", handler.changePassword)

	admin := group.Group("/admin", identityhttp.Authenticate(sessions), identityhttp.RequireAdministrator())
	admin.GET("/notification-channels", handler.listChannels)
	admin.POST("/notification-channels", handler.createChannel)
	admin.PUT("/notification-channels/:id", handler.updateChannel)
	admin.DELETE("/notification-channels/:id", handler.deleteChannel)
	admin.POST("/notification-channels/:id/test", handler.testChannel)
}

type handler struct {
	service *Service
}

type sendCodePayload struct {
	Scene  string `json:"scene"`
	Target string `json:"target"`
}

func (handler *handler) sendCode(ctx *gin.Context) {
	var payload sendCodePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	sent, err := handler.service.SendVerifyCode(ctx.Request.Context(), payload.Scene, payload.Target)
	if err != nil {
		switch {
		case errors.Is(err, ErrCodeTooSoon):
			ctx.Header("Retry-After", strconv.Itoa(int(codeInterval.Seconds())))
			tooManyRequests(ctx, "验证码发送过于频繁，请稍后再试")
		case errors.Is(err, ErrCodeDailyLimit):
			ctx.Header("Retry-After", strconv.Itoa(nextDaySeconds(handler.service.now())))
			tooManyRequests(ctx, "今日验证码发送次数已用完")
		default:
			respondError(ctx, err)
		}
		return
	}
	// The code itself is never echoed: it only leaves through the Sender.
	ctx.JSON(http.StatusAccepted, gin.H{
		"status":     "sent",
		"scene":      sent.Scene,
		"expires_in": int(codeTTL.Seconds()),
	})
}

type forgotPayload struct {
	Identifier string `json:"identifier"`
}

func (handler *handler) forgotPassword(ctx *gin.Context) {
	var payload forgotPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	if _, err := handler.service.RequestPasswordReset(ctx.Request.Context(), payload.Identifier); err != nil {
		respondError(ctx, err)
		return
	}
	// 202 for known and unknown identifiers alike: the answer must not reveal
	// whether a merchant exists.
	ctx.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

type resetPayload struct {
	Identifier string `json:"identifier"`
	Token      string `json:"token"`
	Password   string `json:"password"`
}

func (handler *handler) resetPassword(ctx *gin.Context) {
	var payload resetPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	if err := handler.service.ResetPassword(ctx.Request.Context(), payload.Identifier, payload.Token, payload.Password); err != nil {
		respondError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

type changePasswordPayload struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (handler *handler) changePassword(ctx *gin.Context) {
	identity := identityhttp.MustSession(ctx)
	if identity.PrincipalType != identitymodel.PrincipalMerchant {
		ctx.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "merchant access required"})
		return
	}
	var payload changePasswordPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	if err := handler.service.ChangePassword(ctx.Request.Context(), identity.PrincipalID, payload.OldPassword, payload.NewPassword); err != nil {
		respondError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

type channelPayload struct {
	Kind     string            `json:"kind"`
	Provider string            `json:"provider"`
	Config   map[string]string `json:"config"`
	Status   string            `json:"status"`
}

// input keeps "config omitted" (nil map, secret preserved) apart from an explicit
// replacement, so a status flip cannot wipe credentials.
func (payload channelPayload) input() ChannelInput {
	return ChannelInput{
		Kind:      ChannelKind(payload.Kind),
		Provider:  payload.Provider,
		Config:    payload.Config,
		ConfigSet: payload.Config != nil,
		Status:    ChannelStatus(payload.Status),
	}
}

func (handler *handler) listChannels(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	views, total, err := handler.service.ListChannels(ctx.Request.Context(),
		ChannelFilter{Kind: ctx.Query("kind"), Status: ctx.Query("status")},
		Page{Number: page, Size: size})
	if err != nil {
		respondError(ctx, err)
		return
	}
	items := make([]gin.H, 0, len(views))
	for _, view := range views {
		items = append(items, channelResponse(view))
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *handler) createChannel(ctx *gin.Context) {
	var payload channelPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	view, err := handler.service.CreateChannel(ctx.Request.Context(), payload.input())
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, channelResponse(view))
}

func (handler *handler) updateChannel(ctx *gin.Context) {
	var payload channelPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	view, err := handler.service.UpdateChannel(ctx.Request.Context(), ctx.Param("id"), payload.input())
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, channelResponse(view))
}

func (handler *handler) deleteChannel(ctx *gin.Context) {
	if err := handler.service.DeleteChannel(ctx.Request.Context(), ctx.Param("id")); err != nil {
		respondError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *handler) testChannel(ctx *gin.Context) {
	view, err := handler.service.TestChannel(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		respondError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "channel": channelResponse(view)})
}

// channelResponse never contains config values: only the configured field names.
func channelResponse(view ChannelView) gin.H {
	keys := view.ConfigKeys
	if keys == nil {
		keys = []string{}
	}
	return gin.H{
		"id":          view.ID,
		"kind":        view.Kind,
		"provider":    view.Provider,
		"status":      view.Status,
		"config_keys": keys,
		"created_at":  view.CreatedAt,
		"updated_at":  view.UpdatedAt,
	}
}

func respondError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalidRequest(ctx, "invalid request")
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "notification channel not found"})
	case errors.Is(err, ErrInvalidToken), errors.Is(err, ErrTokenExpired), errors.Is(err, ErrTokenConsumed):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_token", "message": "重置令牌无效或已过期"})
	case errors.Is(err, ErrCodeInvalid), errors.Is(err, ErrCodeExpired),
		errors.Is(err, ErrCodeConsumed), errors.Is(err, ErrCodeExhausted), errors.Is(err, ErrCodeNotFound):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_code", "message": "验证码无效或已过期"})
	case errors.Is(err, ErrInvalidPassword):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_password", "message": "密码长度至少 8 位"})
	case errors.Is(err, ErrInvalidOldPassword):
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_old_password", "message": "当前密码不正确"})
	case errors.Is(err, ErrDeliveryFailed):
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "delivery_failed", "message": "消息发送失败"})
	case errors.Is(err, ErrCipherUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "cipher_unavailable", "message": "未配置主密钥，通知通道不可用"})
	default:
		internalError(ctx)
	}
}

func nextDaySeconds(now time.Time) int {
	next := startOfDay(now).Add(24 * time.Hour)
	seconds := int(next.Sub(now.UTC()).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
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

func tooManyRequests(ctx *gin.Context, message string) {
	ctx.JSON(http.StatusTooManyRequests, gin.H{"code": "rate_limited", "message": message})
}

func invalidRequest(ctx *gin.Context, message string) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": message})
}

func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
