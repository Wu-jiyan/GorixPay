package channelops

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"gorixpay/internal/platform/secret"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

// MaxGenerateDays caps one batch of roll generation at a year.
const MaxGenerateDays = 366

// MaxGenerateChannels caps how many channels one batch may touch.
const MaxGenerateChannels = 200

// Service implements the payment method catalogue, the rotation table and the
// WeChat MP credentials.
type Service struct {
	db     *gorm.DB
	cipher *secret.Cipher
	now    func() time.Time
}

func New(db *gorm.DB, cipher *secret.Cipher, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, cipher: cipher, now: now}
}

// PayTypeRequest creates or replaces a payment method entry.
type PayTypeRequest struct {
	Code   string
	Name   string
	Icon   string
	Sort   int
	Status Status
}

// PublicPayTypes lists the enabled catalogue for the public site.
func (service *Service) PublicPayTypes(ctx context.Context) ([]PayType, error) {
	var items []PayType
	err := service.db.WithContext(ctx).
		Where("status = ?", StatusActive).
		Order("sort ASC, code ASC").Limit(200).Find(&items).Error
	return items, err
}

// PayTypes lists the whole catalogue for the administrator.
func (service *Service) PayTypes(ctx context.Context) ([]PayType, error) {
	var items []PayType
	err := service.db.WithContext(ctx).Order("sort ASC, code ASC").Limit(500).Find(&items).Error
	return items, err
}

// CreatePayType adds one catalogue entry. Codes are unique and lower case.
func (service *Service) CreatePayType(ctx context.Context, request PayTypeRequest) (PayType, error) {
	request = normalizePayType(request)
	if err := validatePayType(request); err != nil {
		return PayType{}, err
	}
	var existing int64
	if err := service.db.WithContext(ctx).Model(&PayType{}).Where("code = ?", request.Code).Count(&existing).Error; err != nil {
		return PayType{}, err
	}
	if existing > 0 {
		return PayType{}, ErrDuplicate
	}
	now := service.now().UTC()
	entry := PayType{
		ID: uuid.NewString(), Code: request.Code, Name: request.Name,
		Icon: optional(request.Icon), Sort: request.Sort, Status: request.Status,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&entry).Error; err != nil {
		return PayType{}, err
	}
	return entry, nil
}

// UpdatePayType replaces the editable fields of one entry. The code stays
// immutable, because channels reference it.
func (service *Service) UpdatePayType(ctx context.Context, id string, request PayTypeRequest) (PayType, error) {
	var entry PayType
	if err := service.db.WithContext(ctx).First(&entry, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PayType{}, ErrNotFound
		}
		return PayType{}, err
	}
	request = normalizePayType(request)
	if request.Code == "" {
		request.Code = entry.Code
	}
	if err := validatePayType(request); err != nil {
		return PayType{}, err
	}
	if request.Code != entry.Code {
		var conflict int64
		if err := service.db.WithContext(ctx).Model(&PayType{}).
			Where("code = ? AND id <> ?", request.Code, entry.ID).Count(&conflict).Error; err != nil {
			return PayType{}, err
		}
		if conflict > 0 {
			return PayType{}, ErrDuplicate
		}
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&PayType{}).Where("id = ?", entry.ID).Updates(map[string]any{
		"code": request.Code, "name": request.Name, "icon": optional(request.Icon),
		"sort": request.Sort, "status": request.Status, "updated_at": now,
	}).Error; err != nil {
		return PayType{}, err
	}
	entry.Code, entry.Name, entry.Icon, entry.Sort, entry.Status, entry.UpdatedAt =
		request.Code, request.Name, optional(request.Icon), request.Sort, request.Status, now
	return entry, nil
}

// SetPayTypeStatus flips one entry on or off.
func (service *Service) SetPayTypeStatus(ctx context.Context, id string, status Status) (PayType, error) {
	if !validStatus(status) {
		return PayType{}, ErrInvalidRequest
	}
	var entry PayType
	if err := service.db.WithContext(ctx).First(&entry, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PayType{}, ErrNotFound
		}
		return PayType{}, err
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&PayType{}).Where("id = ?", entry.ID).
		Updates(map[string]any{"status": status, "updated_at": now}).Error; err != nil {
		return PayType{}, err
	}
	entry.Status, entry.UpdatedAt = status, now
	return entry, nil
}

// DeletePayType removes an entry that no channel uses.
func (service *Service) DeletePayType(ctx context.Context, id string) error {
	var entry PayType
	if err := service.db.WithContext(ctx).First(&entry, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if service.db.Migrator().HasTable("payment_channels") {
		var used int64
		if err := service.db.WithContext(ctx).Table("payment_channels").
			Where("payment_type = ?", entry.Code).Count(&used).Error; err != nil {
			return err
		}
		if used > 0 {
			return ErrInUse
		}
	}
	result := service.db.WithContext(ctx).Delete(&PayType{}, "id = ?", entry.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// PayRollFilter narrows a rotation listing.
type PayRollFilter struct {
	ChannelID string
	From      *time.Time
	To        *time.Time
	Status    Status
}

// PayRollRequest creates one rotation row.
type PayRollRequest struct {
	ChannelID string
	Day       time.Time
	Weight    int
	Status    Status
}

// PayRollUpdate patches weight and status; nil fields stay untouched.
type PayRollUpdate struct {
	Weight *int
	Status *Status
}

// PayRolls lists rotation rows, newest day first.
func (service *Service) PayRolls(ctx context.Context, filter PayRollFilter, page Page) ([]PayRoll, int64, error) {
	if filter.Status != "" && !validStatus(filter.Status) {
		return nil, 0, ErrInvalidRequest
	}
	query := service.db.WithContext(ctx).Model(&PayRoll{})
	if channelID := strings.TrimSpace(filter.ChannelID); channelID != "" {
		query = query.Where("channel_id = ?", channelID)
	}
	if filter.From != nil {
		query = query.Where("roll_date >= ?", DayStart(*filter.From))
	}
	if filter.To != nil {
		query = query.Where("roll_date <= ?", DayStart(*filter.To))
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []PayRoll
	if err := query.Order("roll_date DESC, channel_id ASC").
		Offset(page.Offset()).Limit(page.Limit()).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CreatePayRoll inserts one rotation row. The (channel_id, roll_date) pair is
// unique, so a duplicate returns ErrDuplicate.
func (service *Service) CreatePayRoll(ctx context.Context, request PayRollRequest) (PayRoll, error) {
	request.ChannelID = strings.TrimSpace(request.ChannelID)
	if request.Status == "" {
		request.Status = StatusActive
	}
	if request.Weight == 0 {
		request.Weight = 1
	}
	if err := validateRoll(request); err != nil {
		return PayRoll{}, err
	}
	day := DayStart(request.Day)
	var existing int64
	if err := service.db.WithContext(ctx).Model(&PayRoll{}).
		Where("channel_id = ? AND roll_date = ?", request.ChannelID, day).Count(&existing).Error; err != nil {
		return PayRoll{}, err
	}
	if existing > 0 {
		return PayRoll{}, ErrDuplicate
	}
	now := service.now().UTC()
	roll := PayRoll{
		ID: uuid.NewString(), ChannelID: request.ChannelID, RollDate: day,
		Weight: request.Weight, Status: request.Status, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&roll).Error; err != nil {
		return PayRoll{}, ErrDuplicate
	}
	return roll, nil
}

// UpdatePayRoll patches one rotation row.
func (service *Service) UpdatePayRoll(ctx context.Context, id string, update PayRollUpdate) (PayRoll, error) {
	var roll PayRoll
	if err := service.db.WithContext(ctx).First(&roll, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PayRoll{}, ErrNotFound
		}
		return PayRoll{}, err
	}
	weight := roll.Weight
	if update.Weight != nil {
		weight = *update.Weight
	}
	status := roll.Status
	if update.Status != nil {
		status = *update.Status
	}
	if weight < 0 || weight > 10000 || !validStatus(status) {
		return PayRoll{}, ErrInvalidRequest
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&PayRoll{}).Where("id = ?", roll.ID).
		Updates(map[string]any{"weight": weight, "status": status, "updated_at": now}).Error; err != nil {
		return PayRoll{}, err
	}
	roll.Weight, roll.Status, roll.UpdatedAt = weight, status, now
	return roll, nil
}

// DeletePayRoll removes one rotation row.
func (service *Service) DeletePayRoll(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Delete(&PayRoll{}, "id = ?", strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// GenerateRequest asks for rotation rows for a channel set across a day range.
type GenerateRequest struct {
	ChannelIDs []string
	From       time.Time
	To         time.Time
}

// GeneratePayRolls creates the missing (channel, day) rows and leaves existing
// ones untouched. It returns how many rows were created.
func (service *Service) GeneratePayRolls(ctx context.Context, request GenerateRequest) (int, error) {
	channels := make([]string, 0, len(request.ChannelIDs))
	seen := make(map[string]struct{}, len(request.ChannelIDs))
	for _, raw := range request.ChannelIDs {
		channelID := strings.TrimSpace(raw)
		if channelID == "" || len(channelID) > 36 {
			return 0, ErrInvalidRequest
		}
		if _, exists := seen[channelID]; exists {
			continue
		}
		seen[channelID] = struct{}{}
		channels = append(channels, channelID)
	}
	if len(channels) == 0 || len(channels) > MaxGenerateChannels {
		return 0, ErrInvalidRequest
	}
	from, to := DayStart(request.From), DayStart(request.To)
	if from.After(to) {
		return 0, ErrInvalidRequest
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if days < 1 || days > MaxGenerateDays {
		return 0, ErrInvalidRequest
	}
	var existing []PayRoll
	if err := service.db.WithContext(ctx).
		Where("channel_id IN ? AND roll_date >= ? AND roll_date <= ?", channels, from, to).
		Find(&existing).Error; err != nil {
		return 0, err
	}
	present := make(map[string]struct{}, len(existing))
	for _, roll := range existing {
		present[roll.ChannelID+"|"+roll.RollDate.Format("2006-01-02")] = struct{}{}
	}
	now := service.now().UTC()
	created := 0
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, channelID := range channels {
			for offset := 0; offset < days; offset++ {
				day := from.AddDate(0, 0, offset)
				key := channelID + "|" + day.Format("2006-01-02")
				if _, exists := present[key]; exists {
					continue
				}
				roll := PayRoll{
					ID: uuid.NewString(), ChannelID: channelID, RollDate: day,
					Weight: 1, Status: StatusActive, CreatedAt: now, UpdatedAt: now,
				}
				if err := tx.Create(&roll).Error; err != nil {
					// A concurrent batch may have inserted the same pair.
					var count int64
					if lookupErr := tx.Model(&PayRoll{}).
						Where("channel_id = ? AND roll_date = ?", channelID, day).Count(&count).Error; lookupErr == nil && count > 0 {
						continue
					}
					return err
				}
				created++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return created, nil
}

// RollWeight reports the rotation weight of a channel on one day. A missing or
// disabled row returns (0, false), which means "keep the configured weight".
func (service *Service) RollWeight(ctx context.Context, channelID string, day time.Time) (int, bool) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return 0, false
	}
	var roll PayRoll
	err := service.db.WithContext(ctx).
		Where("channel_id = ? AND roll_date = ? AND status = ?", channelID, DayStart(day), StatusActive).
		First(&roll).Error
	if err != nil {
		return 0, false
	}
	return roll.Weight, true
}

// WeChatMPRequest creates or patches a 公众号 config. Empty AppSecret and AESKey
// keep the stored ciphertext on update.
type WeChatMPRequest struct {
	MerchantID string
	AppID      string
	AppSecret  string
	Token      string
	AESKey     string
	Status     Status
}

// WeChatConfigs lists configs for one merchant (empty = all).
func (service *Service) WeChatConfigs(ctx context.Context, merchantID string, page Page) ([]WeChatMPView, int64, error) {
	query := service.db.WithContext(ctx).Model(&WeChatMPConfig{})
	if scoped := strings.TrimSpace(merchantID); scoped != "" {
		query = query.Where("merchant_id = ?", scoped)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var configs []WeChatMPConfig
	if err := query.Order("created_at DESC").Offset(page.Offset()).Limit(page.Limit()).Find(&configs).Error; err != nil {
		return nil, 0, err
	}
	views := make([]WeChatMPView, 0, len(configs))
	for _, config := range configs {
		views = append(views, config.View())
	}
	return views, total, nil
}

// CreateWeChatConfig encrypts both secrets and stores the config.
func (service *Service) CreateWeChatConfig(ctx context.Context, request WeChatMPRequest) (WeChatMPView, error) {
	request = normalizeWeChat(request)
	if request.Status == "" {
		request.Status = StatusActive
	}
	if err := validateWeChat(request, true); err != nil {
		return WeChatMPView{}, err
	}
	if service.cipher == nil {
		return WeChatMPView{}, ErrInvalidRequest
	}
	now := service.now().UTC()
	config := WeChatMPConfig{
		ID: uuid.NewString(), MerchantID: optional(request.MerchantID), AppID: request.AppID,
		Token: optional(request.Token), Status: request.Status, CreatedAt: now, UpdatedAt: now,
	}
	ciphertext, err := service.encryptSecret(config.ID, request.AppSecret)
	if err != nil {
		return WeChatMPView{}, err
	}
	config.AppSecretEncrypted = ciphertext
	if request.AESKey != "" {
		keyCiphertext, err := service.encryptSecret(config.ID, request.AESKey)
		if err != nil {
			return WeChatMPView{}, err
		}
		config.AESKeyEncrypted = &keyCiphertext
	}
	if err := service.db.WithContext(ctx).Create(&config).Error; err != nil {
		return WeChatMPView{}, err
	}
	return config.View(), nil
}

// UpdateWeChatConfig patches a config. An empty AppSecret or AESKey keeps the
// stored value, so the console can edit the token without re-entering secrets.
func (service *Service) UpdateWeChatConfig(ctx context.Context, id string, request WeChatMPRequest) (WeChatMPView, error) {
	var config WeChatMPConfig
	if err := service.db.WithContext(ctx).First(&config, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return WeChatMPView{}, ErrNotFound
		}
		return WeChatMPView{}, err
	}
	request = normalizeWeChat(request)
	request.Status = defaultStatus(request.Status, config.Status)
	if err := validateWeChat(request, false); err != nil {
		return WeChatMPView{}, err
	}
	// An empty merchant_id keeps the current owner, so a console that only edits
	// the token cannot accidentally detach a config from its merchant.
	merchantID := config.MerchantID
	if request.MerchantID != "" {
		merchantID = optional(request.MerchantID)
	}
	updates := map[string]any{
		"merchant_id": merchantID,
		"app_id":      request.AppID,
		"token":       optional(request.Token),
		"status":      request.Status,
		"updated_at":  service.now().UTC(),
	}
	if request.AppSecret != "" {
		if service.cipher == nil {
			return WeChatMPView{}, ErrInvalidRequest
		}
		ciphertext, err := service.encryptSecret(config.ID, request.AppSecret)
		if err != nil {
			return WeChatMPView{}, err
		}
		updates["app_secret_encrypted"] = ciphertext
		config.AppSecretEncrypted = ciphertext
	}
	if request.AESKey != "" {
		if service.cipher == nil {
			return WeChatMPView{}, ErrInvalidRequest
		}
		keyCiphertext, err := service.encryptSecret(config.ID, request.AESKey)
		if err != nil {
			return WeChatMPView{}, err
		}
		updates["aes_key_encrypted"] = keyCiphertext
		config.AESKeyEncrypted = &keyCiphertext
	}
	if err := service.db.WithContext(ctx).Model(&WeChatMPConfig{}).Where("id = ?", config.ID).Updates(updates).Error; err != nil {
		return WeChatMPView{}, err
	}
	config.MerchantID, config.AppID = merchantID, request.AppID
	config.Token, config.Status = optional(request.Token), request.Status
	config.UpdatedAt = updates["updated_at"].(time.Time)
	return config.View(), nil
}

// DeleteWeChatConfig removes one config.
func (service *Service) DeleteWeChatConfig(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Delete(&WeChatMPConfig{}, "id = ?", strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// WeChatSecrets decrypts the stored secret and AES key of one config. The
// notification handler uses it; the API never exposes the values.
func (service *Service) WeChatSecrets(ctx context.Context, id string) (string, string, error) {
	var config WeChatMPConfig
	if err := service.db.WithContext(ctx).First(&config, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if service.cipher == nil {
		return "", "", ErrInvalidRequest
	}
	appSecret, err := service.decryptSecret(config.ID, config.AppSecretEncrypted)
	if err != nil {
		return "", "", err
	}
	aesKey := ""
	if config.AESKeyEncrypted != nil && *config.AESKeyEncrypted != "" {
		aesKey, err = service.decryptSecret(config.ID, *config.AESKeyEncrypted)
		if err != nil {
			return "", "", err
		}
	}
	return appSecret, aesKey, nil
}

func (service *Service) encryptSecret(configID, plain string) (string, error) {
	return service.cipher.Encrypt([]byte(plain), []byte("wechat-mp:"+configID))
}

func (service *Service) decryptSecret(configID, ciphertext string) (string, error) {
	plain, _, err := service.cipher.Decrypt(ciphertext, []byte("wechat-mp:"+configID))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func normalizePayType(request PayTypeRequest) PayTypeRequest {
	request.Code = strings.ToLower(strings.TrimSpace(request.Code))
	request.Name = strings.TrimSpace(request.Name)
	request.Icon = strings.TrimSpace(request.Icon)
	if request.Status == "" {
		request.Status = StatusActive
	}
	return request
}

func validatePayType(request PayTypeRequest) error {
	if !codePattern.MatchString(request.Code) || request.Name == "" || len(request.Name) > 64 ||
		len(request.Icon) > 255 || !validStatus(request.Status) {
		return ErrInvalidRequest
	}
	return nil
}

func validateRoll(request PayRollRequest) error {
	if request.ChannelID == "" || len(request.ChannelID) > 36 || request.Day.IsZero() ||
		request.Weight < 0 || request.Weight > 10000 || !validStatus(request.Status) {
		return ErrInvalidRequest
	}
	return nil
}

func normalizeWeChat(request WeChatMPRequest) WeChatMPRequest {
	request.MerchantID = strings.TrimSpace(request.MerchantID)
	request.AppID = strings.TrimSpace(request.AppID)
	request.AppSecret = strings.TrimSpace(request.AppSecret)
	request.Token = strings.TrimSpace(request.Token)
	request.AESKey = strings.TrimSpace(request.AESKey)
	return request
}

func validateWeChat(request WeChatMPRequest, requireSecret bool) error {
	if request.AppID == "" || len(request.AppID) > 64 || len(request.MerchantID) > 36 ||
		len(request.Token) > 64 || len(request.AESKey) > 256 || !validStatus(request.Status) {
		return ErrInvalidRequest
	}
	if requireSecret && (request.AppSecret == "" || len(request.AppSecret) > 256) {
		return ErrInvalidRequest
	}
	if len(request.AppSecret) > 256 {
		return ErrInvalidRequest
	}
	return nil
}

func defaultStatus(current, fallback Status) Status {
	if current == "" {
		return fallback
	}
	return current
}

func optional(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
