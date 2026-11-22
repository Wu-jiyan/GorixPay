package announcement

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultSiteName   = "GorixPay"
	settingSiteName   = "site.name"
	settingSiteLogo   = "site.logo_url"
	settingSiteFooter = "site.footer_text"
	settingSiteSkin   = "site.skin"
	// settingCaptchaRequired is owned by internal/security; the public site payload
	// only mirrors it so the login pages know whether to show a captcha box.
	settingCaptchaRequired = "security.captcha_required"
	// 注册门禁 / 首页模式 / 联系方式，对应上游 Epay 的 reg_open、homepage、kfqq 等。
	settingRegisterMode = "register.mode"
	settingHomepageMode = "site.homepage_mode"
	settingHomepageURL  = "site.homepage_url"
	settingContactEmail = "site.contact_email"
	settingContactQQ    = "site.contact_qq"
	settingICPNumber    = "site.icp_number"
)

// firstNonEmpty returns the first non-empty argument.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

type Page struct {
	Number int
	Size   int
}

type Site struct {
	Name         string   `json:"name"`
	LogoURL      string   `json:"logo_url"`
	FooterText   string   `json:"footer_text"`
	Skin         string   `json:"skin"`
	Announcement any      `json:"announcement"`
	PaymentTypes []string `json:"payment_types"`
	// CaptchaRequired mirrors security.captcha_required so the login pages only show
	// a captcha box when the platform actually asks for one.
	CaptchaRequired bool `json:"captcha_required"`
	// RegisterMode is the registration gate: open / closed / invite. The register
	// page renders from it instead of always showing the form.
	RegisterMode string `json:"register_mode"`
	// HomepageMode says what "/" should do: landing / login / url (上游 Epay 的
	// homepage 设置)。
	HomepageMode string `json:"homepage_mode"`
	HomepageURL  string `json:"homepage_url"`
	// Contact details the footer and the payment page can show.
	ContactEmail string `json:"contact_email"`
	ContactQQ    string `json:"contact_qq"`
	ICPNumber    string `json:"icp_number"`
}

type systemSetting struct {
	Key   string `gorm:"column:setting_key;primaryKey"`
	Value string `gorm:"column:setting_value;type:text;not null"`
}

func (systemSetting) TableName() string {
	return "system_settings"
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

func (service *Service) List(ctx context.Context, page Page) ([]Announcement, int64, error) {
	query := service.db.WithContext(ctx).Model(&Announcement{})
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Announcement
	if err := query.Order("created_at DESC, id DESC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (service *Service) ListPublished(ctx context.Context) ([]Announcement, error) {
	var items []Announcement
	if err := service.db.WithContext(ctx).
		Where("status = ?", StatusPublished).
		Order("published_at DESC, created_at DESC").
		Limit(publishedListCap).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (service *Service) LatestPublished(ctx context.Context) (*Announcement, error) {
	var item Announcement
	if err := service.db.WithContext(ctx).
		Where("status = ?", StatusPublished).
		Order("published_at DESC, created_at DESC").First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (service *Service) Get(ctx context.Context, id string) (Announcement, error) {
	var item Announcement
	if err := service.db.WithContext(ctx).First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Announcement{}, ErrNotFound
		}
		return Announcement{}, err
	}
	return item, nil
}

func (service *Service) Create(ctx context.Context, input CreateInput) (Announcement, error) {
	title, content, status, err := validate(input.Title, input.Content, input.Status)
	if err != nil {
		return Announcement{}, err
	}
	now := service.now().UTC()
	created := Announcement{ID: uuid.NewString(), Title: title, Content: content, Status: status, CreatedAt: now, UpdatedAt: now}
	if status == StatusPublished {
		created.PublishedAt = &now
	}
	if err := service.db.WithContext(ctx).Create(&created).Error; err != nil {
		return Announcement{}, err
	}
	return created, nil
}

func (service *Service) Update(ctx context.Context, id string, input UpdateInput) (Announcement, error) {
	existing, err := service.Get(ctx, id)
	if err != nil {
		return Announcement{}, err
	}
	title, content, status, err := validate(input.Title, input.Content, input.Status)
	if err != nil {
		return Announcement{}, err
	}
	now := service.now().UTC()
	updates := map[string]any{"title": title, "content": content, "status": status, "updated_at": now}
	switch {
	case status == StatusPublished && existing.PublishedAt == nil:
		updates["published_at"] = now
	case status == StatusDraft:
		updates["published_at"] = nil
	}
	if err := service.db.WithContext(ctx).Model(&Announcement{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return Announcement{}, err
	}
	return service.Get(ctx, id)
}

func (service *Service) Delete(ctx context.Context, id string) error {
	result := service.db.WithContext(ctx).Where("id = ?", id).Delete(&Announcement{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Site reads the public site identity from system_settings and appends the
// latest published announcement. Missing settings fall back to defaults so the
// public landing page always renders.
func (service *Service) Site(ctx context.Context) (Site, error) {
	settings, err := service.settings(ctx)
	if err != nil {
		return Site{}, err
	}
	site := Site{
		Name:         defaultSiteName,
		Skin:         "default",
		LogoURL:      "",
		PaymentTypes: service.paymentTypes(ctx),
	}
	if value := decodeSetting(settings[settingSiteName]); value != "" {
		site.Name = value
	}
	if value := decodeSetting(settings[settingSiteLogo]); value != "" {
		site.LogoURL = value
	}
	if value := decodeSetting(settings[settingSiteFooter]); value != "" {
		site.FooterText = value
	}
	if value := decodeSetting(settings[settingSiteSkin]); value != "" {
		site.Skin = value
	}
	// security.captcha_required is stored as a JSON boolean, so it needs its own
	// decoder: decodeSetting only understands JSON strings and would answer "" here.
	site.CaptchaRequired = decodeBoolSetting(settings["security.captcha_required"])
	// 注册门禁与首页模式：公开站点信息要带上，注册页和首页据此改变行为。
	site.RegisterMode = firstNonEmpty(decodeSetting(settings[settingRegisterMode]), "open")
	site.HomepageMode = firstNonEmpty(decodeSetting(settings[settingHomepageMode]), "landing")
	site.HomepageURL = decodeSetting(settings[settingHomepageURL])
	site.ContactEmail = decodeSetting(settings[settingContactEmail])
	site.ContactQQ = decodeSetting(settings[settingContactQQ])
	site.ICPNumber = decodeSetting(settings[settingICPNumber])
	latest, err := service.LatestPublished(ctx)
	if err != nil {
		return Site{}, err
	}
	if latest != nil {
		site.Announcement = latest
	}
	return site, nil
}

// paymentTypes reads the enabled catalogue from pay_types (000023) and falls
// back to the built-in list when the table is missing or empty, so the landing
// page always renders something.
func (service *Service) paymentTypes(ctx context.Context) []string {
	fallback := []string{"alipay", "wxpay", "qqpay"}
	var codes []string
	err := service.db.WithContext(ctx).Table("pay_types").
		Where("status = ?", "active").Order("sort ASC, code ASC").Limit(50).
		Pluck("code", &codes).Error
	if err != nil || len(codes) == 0 {
		return fallback
	}
	return codes
}

func (service *Service) settings(ctx context.Context) (map[string]string, error) {
	keys := []string{
		settingSiteName, settingSiteLogo, settingSiteFooter, settingSiteSkin, settingCaptchaRequired,
		settingRegisterMode, settingHomepageMode, settingHomepageURL,
		settingContactEmail, settingContactQQ, settingICPNumber,
	}
	var records []systemSetting
	if err := service.db.WithContext(ctx).Where("setting_key IN ?", keys).Find(&records).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(records))
	for _, record := range records {
		values[record.Key] = record.Value
	}
	return values, nil
}

func decodeSetting(raw string) string {
	if raw == "" {
		return ""
	}
	var decoded string
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return strings.TrimSpace(decoded)
	}
	return strings.TrimSpace(raw)
}

// decodeBoolSetting reads a JSON boolean setting. The admin console stores
// booleans as real JSON booleans, but an older row could hold the string "true",
// so both shapes are accepted.
func decodeBoolSetting(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	var flag bool
	if err := json.Unmarshal([]byte(trimmed), &flag); err == nil {
		return flag
	}
	var text string
	if err := json.Unmarshal([]byte(trimmed), &text); err == nil {
		return strings.EqualFold(strings.TrimSpace(text), "true")
	}
	return strings.EqualFold(trimmed, "true")
}

func validate(title, content string, status Status) (string, string, Status, error) {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if status == "" {
		status = StatusDraft
	}
	if title == "" || len([]rune(title)) > maxTitleLength || content == "" || len(content) > maxContentLength || !ValidStatus(status) {
		return "", "", "", ErrInvalidRequest
	}
	return title, content, status, nil
}
