package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	identityauth "gorixpay/internal/identity/auth"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/secret"
	"gorixpay/internal/settings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrInvalidRequest    = errors.New("invalid oauth request")
	ErrInvalidState      = errors.New("oauth state is invalid")
	ErrStateExpired      = errors.New("oauth state expired")
	ErrInvalidBindTicket = errors.New("bind ticket is invalid")
	ErrBindTicketExpired = errors.New("bind ticket expired")
	ErrAlreadyBound      = errors.New("oauth identity is already bound")
	ErrNotFound          = errors.New("record not found")
	ErrExpired           = errors.New("ticket expired")
	ErrPrincipalRequired = errors.New("principal is required")
)

const (
	statusPending   = "pending"
	statusScanned   = "scanned"
	statusConfirmed = "confirmed"
	statusExpired   = "expired"

	stateTTL        = 10 * time.Minute
	bindTicketTTL   = 10 * time.Minute
	qrTicketTTL     = 5 * time.Minute
	retention       = 24 * time.Hour
	adminSession    = 12 * time.Hour
	merchantSession = 7 * 24 * time.Hour
)

type Binding struct {
	ID            string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	PrincipalType string    `gorm:"type:varchar(20);not null" json:"principal_type"`
	PrincipalID   string    `gorm:"type:varchar(36);not null" json:"principal_id"`
	Provider      string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_oauth_provider_open" json:"provider"`
	OpenID        string    `gorm:"type:varchar(128);not null;uniqueIndex:idx_oauth_provider_open" json:"open_id"`
	UnionID       *string   `gorm:"type:varchar(128)" json:"union_id"`
	Nickname      *string   `gorm:"type:varchar(64)" json:"nickname"`
	Avatar        *string   `gorm:"type:varchar(255)" json:"avatar"`
	CreatedAt     time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt     time.Time `gorm:"not null" json:"updated_at"`
}

func (Binding) TableName() string { return "oauth_bindings" }

type QRLoginSession struct {
	ID            string    `gorm:"type:varchar(36);primaryKey"`
	Ticket        string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	PrincipalType *string   `gorm:"type:varchar(20)"`
	PrincipalID   *string   `gorm:"type:varchar(36)"`
	Status        string    `gorm:"type:varchar(20);not null"`
	ExpiresAt     time.Time `gorm:"not null"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

func (QRLoginSession) TableName() string { return "qr_login_sessions" }

type OAuthState struct {
	ID           string    `gorm:"type:varchar(36);primaryKey"`
	StateHash    string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	Provider     string    `gorm:"type:varchar(20);not null"`
	RedirectPath string    `gorm:"type:varchar(255);not null"`
	ExpiresAt    time.Time `gorm:"not null"`
	ConsumedAt   *time.Time
	CreatedAt    time.Time `gorm:"not null"`
}

func (OAuthState) TableName() string { return "oauth_states" }

type systemSetting struct {
	Key   string `gorm:"column:setting_key;primaryKey"`
	Value string `gorm:"column:setting_value;type:text;not null"`
}

func (systemSetting) TableName() string { return "system_settings" }

// ClientMetadata carries the request context used when a session is created.
type ClientMetadata struct {
	IPAddress string
	UserAgent string
}

// LoginResult matches the shape of the existing login endpoints.
type LoginResult struct {
	Token         string    `json:"token"`
	PrincipalType string    `json:"principal_type"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// NotBoundError is returned when the provider identity has no local binding yet.
type NotBoundError struct {
	BindTicket string
}

func (err *NotBoundError) Error() string { return "oauth identity is not bound to an account" }

type QRTicket struct {
	Ticket    string    `json:"ticket"`
	SVG       string    `json:"qr_svg"`
	ExpiresAt time.Time `json:"expires_at"`
}

type QRPoll struct {
	Status    string     `json:"status"`
	Token     string     `json:"token,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type Service struct {
	db        *gorm.DB
	sessions  *session.Service
	cipher    *secret.Cipher
	publicURL string
	now       func() time.Time
	injected  map[string]Provider
}

func New(db *gorm.DB, sessions *session.Service, cipher *secret.Cipher, publicURL string) *Service {
	return &Service{
		db:        db,
		sessions:  sessions,
		cipher:    cipher,
		publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"),
		now:       time.Now,
		injected:  map[string]Provider{},
	}
}

// WithProviders overrides provider construction, which is how tests inject a
// fake provider without touching the open platform endpoints.
func (service *Service) WithProviders(providers ...Provider) *Service {
	copied := *service
	copied.injected = make(map[string]Provider, len(providers))
	for _, provider := range providers {
		if provider != nil {
			copied.injected[provider.Name()] = provider
		}
	}
	return &copied
}

func knownProvider(name string) bool {
	switch name {
	case providerWechat, providerQQ, providerDouyin:
		return true
	default:
		return false
	}
}

func (service *Service) provider(ctx context.Context, name string) (Provider, error) {
	if injected, ok := service.injected[name]; ok {
		return injected, nil
	}
	if !knownProvider(name) {
		return nil, ErrProviderUnknown
	}
	if service.publicURL == "" {
		return nil, ErrNotConfigured
	}
	appID, appSecret, err := service.credentials(ctx, name)
	if err != nil {
		return nil, err
	}
	redirect := service.publicURL + "/api/v1/auth/oauth/" + name + "/callback"
	switch name {
	case providerWechat:
		return newWechatProvider(appID, appSecret, redirect), nil
	case providerQQ:
		return newQQProvider(appID, appSecret, redirect), nil
	case providerDouyin:
		return newDouyinProvider(appID, appSecret, redirect), nil
	}
	return nil, ErrProviderUnknown
}

func (service *Service) credentials(ctx context.Context, name string) (string, string, error) {
	appIDKey := fmt.Sprintf("oauth.%s.app_id", name)
	secretKey := fmt.Sprintf("oauth.%s.app_secret", name)
	var records []systemSetting
	if err := service.db.WithContext(ctx).Where("setting_key IN ?", []string{appIDKey, secretKey}).Find(&records).Error; err != nil {
		return "", "", err
	}
	values := make(map[string]string, len(records))
	for _, record := range records {
		values[record.Key] = record.Value
	}
	appID := settings.Text(values[appIDKey])
	appSecret, err := settings.Decrypt(service.cipher, secretKey, values[secretKey])
	if err != nil && service.cipher != nil {
		// Legacy ciphertexts were produced with the provider name as the AEAD
		// context. Accept them so a hand provisioned deployment keeps working.
		if plain, _, legacyErr := service.cipher.Decrypt(settings.Text(values[secretKey]), []byte("oauth:"+name)); legacyErr == nil {
			appSecret, err = strings.TrimSpace(string(plain)), nil
		}
	}
	if appID == "" || appSecret == "" || err != nil {
		return "", "", ErrNotConfigured
	}
	return appID, appSecret, nil
}

// CreateState issues a one-time state token bound to the provider and to the
// post-login redirect path.
func (service *Service) CreateState(ctx context.Context, providerName, redirectPath string) (string, error) {
	if _, err := service.provider(ctx, providerName); err != nil {
		return "", err
	}
	path, err := validateRedirectPath(redirectPath)
	if err != nil {
		return "", err
	}
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	now := service.now().UTC()
	state := OAuthState{
		ID: uuid.NewString(), StateHash: hashToken(token), Provider: providerName,
		RedirectPath: path, ExpiresAt: now.Add(stateTTL), CreatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&state).Error; err != nil {
		return "", err
	}
	return token, nil
}

func (service *Service) ConsumeState(ctx context.Context, providerName, token string) (OAuthState, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return OAuthState{}, ErrInvalidState
	}
	var state OAuthState
	if err := service.db.WithContext(ctx).Where("state_hash = ?", hashToken(token)).First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OAuthState{}, ErrInvalidState
		}
		return OAuthState{}, err
	}
	if state.Provider != providerName || state.ConsumedAt != nil {
		return OAuthState{}, ErrInvalidState
	}
	now := service.now().UTC()
	if !state.ExpiresAt.After(now) {
		return OAuthState{}, ErrStateExpired
	}
	result := service.db.WithContext(ctx).Model(&OAuthState{}).
		Where("id = ? AND consumed_at IS NULL", state.ID).
		Update("consumed_at", now)
	if result.Error != nil {
		return OAuthState{}, result.Error
	}
	if result.RowsAffected != 1 {
		return OAuthState{}, ErrInvalidState
	}
	state.ConsumedAt = &now
	return state, nil
}

// Callback exchanges the authorization code and either signs the bound
// principal in or hands back a bind ticket for an unbound identity.
func (service *Service) Callback(ctx context.Context, providerName, code, stateToken string, meta ClientMetadata) (LoginResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return LoginResult{}, ErrInvalidRequest
	}
	provider, err := service.provider(ctx, providerName)
	if err != nil {
		return LoginResult{}, err
	}
	if _, err := service.ConsumeState(ctx, providerName, stateToken); err != nil {
		return LoginResult{}, err
	}
	identity, err := provider.Exchange(ctx, code)
	if err != nil {
		return LoginResult{}, err
	}
	var binding Binding
	err = service.db.WithContext(ctx).Where("provider = ? AND open_id = ?", providerName, identity.OpenID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ticket, ticketErr := service.issueBindTicket(providerName, identity)
		if ticketErr != nil {
			return LoginResult{}, ticketErr
		}
		return LoginResult{}, &NotBoundError{BindTicket: ticket}
	}
	if err != nil {
		return LoginResult{}, err
	}
	principalType := identitymodel.PrincipalType(binding.PrincipalType)
	token, created, err := service.sessions.Create(ctx, principalType, binding.PrincipalID, sessionTTL(principalType), meta.IPAddress, meta.UserAgent)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, PrincipalType: string(principalType), ExpiresAt: created.ExpiresAt}, nil
}

// Bind verifies the account password with the existing auth service, then links
// the provider identity to that merchant.
func (service *Service) Bind(ctx context.Context, bindTicket, identifier, password string, meta ClientMetadata) (LoginResult, error) {
	payload, err := service.parseBindTicket(bindTicket)
	if err != nil {
		return LoginResult{}, err
	}
	authService := identityauth.NewService(service.db, service.sessions)
	login, err := authService.LoginMerchant(ctx, identifier, password, identityauth.LoginMetadata{
		IPAddress: meta.IPAddress, UserAgent: meta.UserAgent,
	})
	if err != nil {
		return LoginResult{}, err
	}
	now := service.now().UTC()
	var existing Binding
	err = service.db.WithContext(ctx).Where("provider = ? AND open_id = ?", payload.Provider, payload.OpenID).First(&existing).Error
	switch {
	case err == nil:
		if existing.PrincipalType != string(identitymodel.PrincipalMerchant) || existing.PrincipalID != login.Merchant.ID {
			return LoginResult{}, ErrAlreadyBound
		}
		if err := service.db.WithContext(ctx).Model(&Binding{}).Where("id = ?", existing.ID).
			Updates(map[string]any{
				"union_id": optional(payload.UnionID), "nickname": optional(payload.Nickname),
				"avatar": optional(payload.Avatar), "updated_at": now,
			}).Error; err != nil {
			return LoginResult{}, err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		binding := Binding{
			ID: uuid.NewString(), PrincipalType: string(identitymodel.PrincipalMerchant), PrincipalID: login.Merchant.ID,
			Provider: payload.Provider, OpenID: payload.OpenID,
			UnionID: optional(payload.UnionID), Nickname: optional(payload.Nickname), Avatar: optional(payload.Avatar),
			CreatedAt: now, UpdatedAt: now,
		}
		if err := service.db.WithContext(ctx).Create(&binding).Error; err != nil {
			return LoginResult{}, ErrAlreadyBound
		}
	default:
		return LoginResult{}, err
	}
	return LoginResult{
		Token: login.Token, PrincipalType: string(identitymodel.PrincipalMerchant), ExpiresAt: login.Session.ExpiresAt,
	}, nil
}

func (service *Service) ListBindings(ctx context.Context, principalType, principalID string) ([]Binding, error) {
	if strings.TrimSpace(principalID) == "" {
		return nil, ErrPrincipalRequired
	}
	var items []Binding
	if err := service.db.WithContext(ctx).
		Where("principal_type = ? AND principal_id = ?", principalType, principalID).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (service *Service) DeleteBinding(ctx context.Context, principalType, principalID, providerName string) error {
	if strings.TrimSpace(principalID) == "" {
		return ErrPrincipalRequired
	}
	result := service.db.WithContext(ctx).
		Where("principal_type = ? AND principal_id = ? AND provider = ?", principalType, principalID, providerName).
		Delete(&Binding{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (service *Service) CreateQRTicket(ctx context.Context) (QRTicket, error) {
	ticket, err := randomToken(32)
	if err != nil {
		return QRTicket{}, err
	}
	now := service.now().UTC()
	expires := now.Add(qrTicketTTL)
	row := QRLoginSession{
		ID: uuid.NewString(), Ticket: ticket, Status: statusPending,
		ExpiresAt: expires, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&row).Error; err != nil {
		return QRTicket{}, err
	}
	return QRTicket{Ticket: ticket, SVG: renderQRSVG(service.qrContent(ticket)), ExpiresAt: expires}, nil
}

func (service *Service) qrContent(ticket string) string {
	base := service.publicURL
	if base == "" {
		base = "http://localhost"
	}
	return base + "/qr-login?ticket=" + ticket
}

// PollQRTicket reports the ticket state and, exactly once, the session token of
// the confirmed principal. Consuming the ticket on the first successful poll is
// what stops a leaked ticket from minting a second session later.
func (service *Service) PollQRTicket(ctx context.Context, ticket string, meta ClientMetadata) (QRPoll, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return QRPoll{}, ErrInvalidRequest
	}
	var row QRLoginSession
	if err := service.db.WithContext(ctx).Where("ticket = ?", ticket).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return QRPoll{}, ErrNotFound
		}
		return QRPoll{}, err
	}
	now := service.now().UTC()
	if !row.ExpiresAt.After(now) {
		_ = service.db.WithContext(ctx).Model(&QRLoginSession{}).
			Where("id = ? AND status IN ?", row.ID, []string{statusPending, statusScanned}).
			Update("status", statusExpired).Error
		return QRPoll{Status: statusExpired}, nil
	}
	if row.Status != statusConfirmed || row.PrincipalType == nil || row.PrincipalID == nil {
		return QRPoll{Status: row.Status}, nil
	}
	// Clearing the principal marks the token as issued while keeping the ticket
	// in the confirmed state, so a second poll reports confirmed without a token.
	result := service.db.WithContext(ctx).Model(&QRLoginSession{}).
		Where("id = ? AND status = ? AND principal_id IS NOT NULL", row.ID, statusConfirmed).
		Updates(map[string]any{"principal_type": nil, "principal_id": nil, "updated_at": now})
	if result.Error != nil {
		return QRPoll{}, result.Error
	}
	if result.RowsAffected != 1 {
		return QRPoll{Status: statusConfirmed}, nil
	}
	principalType := identitymodel.PrincipalType(*row.PrincipalType)
	token, created, err := service.sessions.Create(ctx, principalType, *row.PrincipalID, sessionTTL(principalType), meta.IPAddress, meta.UserAgent)
	if err != nil {
		return QRPoll{}, err
	}
	return QRPoll{Status: statusConfirmed, Token: token, ExpiresAt: &created.ExpiresAt}, nil
}

// ScanQRTicket moves a pending ticket to scanned, which the phone page calls
// after decoding the QR content.
func (service *Service) ScanQRTicket(ctx context.Context, ticket string) (string, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return "", ErrInvalidRequest
	}
	var row QRLoginSession
	if err := service.db.WithContext(ctx).Where("ticket = ?", ticket).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	now := service.now().UTC()
	if !row.ExpiresAt.After(now) {
		_ = service.db.WithContext(ctx).Model(&QRLoginSession{}).Where("id = ?", row.ID).Update("status", statusExpired).Error
		return statusExpired, nil
	}
	if row.Status == statusPending {
		if err := service.db.WithContext(ctx).Model(&QRLoginSession{}).
			Where("id = ? AND status = ?", row.ID, statusPending).
			Updates(map[string]any{"status": statusScanned, "updated_at": now}).Error; err != nil {
			return "", err
		}
		return statusScanned, nil
	}
	return row.Status, nil
}

// ConfirmQRTicket binds the signed-in principal to the ticket, after which the
// browser poll picks up a fresh session token.
func (service *Service) ConfirmQRTicket(ctx context.Context, ticket, principalType, principalID string) error {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" || strings.TrimSpace(principalID) == "" {
		return ErrPrincipalRequired
	}
	var row QRLoginSession
	if err := service.db.WithContext(ctx).Where("ticket = ?", ticket).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	now := service.now().UTC()
	if !row.ExpiresAt.After(now) {
		return ErrExpired
	}
	if row.Status == statusConfirmed {
		return nil
	}
	result := service.db.WithContext(ctx).Model(&QRLoginSession{}).
		Where("id = ? AND status IN ?", row.ID, []string{statusPending, statusScanned}).
		Updates(map[string]any{
			"status": statusConfirmed, "principal_type": principalType,
			"principal_id": principalID, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrExpired
	}
	return nil
}

// PurgeExpired deletes tickets and states older than the retention window.
func (service *Service) PurgeExpired(ctx context.Context) (int64, error) {
	cutoff := service.now().UTC().Add(-retention)
	sessions := service.db.WithContext(ctx).Where("expires_at < ?", cutoff).Delete(&QRLoginSession{})
	if sessions.Error != nil {
		return 0, sessions.Error
	}
	states := service.db.WithContext(ctx).Where("expires_at < ?", cutoff).Delete(&OAuthState{})
	if states.Error != nil {
		return sessions.RowsAffected, states.Error
	}
	return sessions.RowsAffected + states.RowsAffected, nil
}

type bindPayload struct {
	Provider  string    `json:"provider"`
	OpenID    string    `json:"open_id"`
	UnionID   string    `json:"union_id"`
	Nickname  string    `json:"nickname"`
	Avatar    string    `json:"avatar"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (service *Service) issueBindTicket(providerName string, identity Identity) (string, error) {
	if service.cipher == nil {
		return "", ErrNotConfigured
	}
	payload := bindPayload{
		Provider: providerName, OpenID: identity.OpenID, UnionID: identity.UnionID,
		Nickname: identity.Nickname, Avatar: identity.Avatar,
		ExpiresAt: service.now().UTC().Add(bindTicketTTL),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return service.cipher.Encrypt(encoded, []byte("oauth_bind"))
}

func (service *Service) parseBindTicket(ticket string) (bindPayload, error) {
	if service.cipher == nil {
		return bindPayload{}, ErrNotConfigured
	}
	plain, _, err := service.cipher.Decrypt(strings.TrimSpace(ticket), []byte("oauth_bind"))
	if err != nil {
		return bindPayload{}, ErrInvalidBindTicket
	}
	var payload bindPayload
	if err := json.Unmarshal(plain, &payload); err != nil || payload.Provider == "" || payload.OpenID == "" {
		return bindPayload{}, ErrInvalidBindTicket
	}
	if !payload.ExpiresAt.After(service.now().UTC()) {
		return bindPayload{}, ErrBindTicketExpired
	}
	return payload, nil
}

func sessionTTL(principalType identitymodel.PrincipalType) time.Duration {
	if principalType == identitymodel.PrincipalAdministrator {
		return adminSession
	}
	return merchantSession
}

func validateRedirectPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "/merchant", nil
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "\\") || len(path) > 255 {
		return "", ErrInvalidRequest
	}
	return path, nil
}

func optional(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
