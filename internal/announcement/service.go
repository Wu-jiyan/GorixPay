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
)

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
	keys := []string{settingSiteName, settingSiteLogo, settingSiteFooter, settingSiteSkin}
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
