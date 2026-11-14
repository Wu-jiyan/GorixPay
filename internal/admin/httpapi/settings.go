package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type systemSetting struct {
	Key       string    `gorm:"column:setting_key;primaryKey" json:"key"`
	Value     string    `gorm:"column:setting_value;type:text;not null" json:"-"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (systemSetting) TableName() string {
	return "system_settings"
}

type auditEvent struct {
	ID        string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	EventType string    `gorm:"type:varchar(100);not null" json:"event_type"`
	ActorType string    `gorm:"type:varchar(50);not null" json:"actor_type"`
	ActorID   *string   `gorm:"type:varchar(36)" json:"actor_id"`
	Payload   string    `gorm:"type:text;not null" json:"payload"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

func (auditEvent) TableName() string {
	return "audit_events"
}

var allowedSettings = map[string]bool{
	"site.name":                          true,
	"site.logo_url":                      true,
	"site.footer_text":                   true,
	"security.captcha_required":          true,
	"payment.order_expiry_minutes":       true,
	"payment.domain_whitelist_required":  true,
	"certification.required":             true,
	"certification.fee_cents":            true,
	"invite.enabled":                     true,
	"invite.rate_bp":                     true,
	"risk.daily_amount_limit_cents":      true,
	"risk.daily_order_limit":             true,
	"risk.ip_daily_limit":                true,
	"merchant.deposit_required_cents":    true,
	"notification.max_attempts":          true,
	"notification.poll_interval_seconds": true,
}

func (handler *Handler) settings(ctx *gin.Context) {
	var records []systemSetting
	if err := handler.db.WithContext(ctx.Request.Context()).Order("setting_key ASC").Find(&records).Error; err != nil {
		internalError(ctx)
		return
	}
	items := make([]gin.H, 0, len(records))
	for _, record := range records {
		if !allowedSettings[record.Key] {
			continue
		}
		var value any
		if json.Unmarshal([]byte(record.Value), &value) != nil {
			value = record.Value
		}
		items = append(items, gin.H{"key": record.Key, "value": value, "updated_at": record.UpdatedAt})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) updateSetting(ctx *gin.Context) {
	key := strings.TrimSpace(ctx.Param("key"))
	if !allowedSettings[key] {
		invalidRequest(ctx)
		return
	}
	var request struct {
		Value any `json:"value"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		invalidRequest(ctx)
		return
	}
	encoded, err := json.Marshal(request.Value)
	if err != nil || len(encoded) > 4096 {
		invalidRequest(ctx)
		return
	}
	now := time.Now().UTC()
	record := systemSetting{Key: key, Value: string(encoded), UpdatedAt: now}
	if err := handler.db.WithContext(ctx.Request.Context()).Save(&record).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"key": key, "value": request.Value, "updated_at": now})
}

func (handler *Handler) auditEvents(ctx *gin.Context) {
	page, size := pagination(ctx)
	query := handler.db.WithContext(ctx.Request.Context()).Model(&auditEvent{})
	if eventType := strings.TrimSpace(ctx.Query("event_type")); eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}
	if actorID := strings.TrimSpace(ctx.Query("actor_id")); actorID != "" {
		query = query.Where("actor_id = ?", actorID)
	}
	if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("event_type LIKE ? OR actor_id LIKE ? OR payload LIKE ?", like, like, like)
	}
	var total int64
	query.Count(&total)
	var items []auditEvent
	if err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) audit(ctx *gin.Context) {
	ctx.Next()
	if ctx.Request.Method == http.MethodGet || ctx.Request.Method == http.MethodHead || ctx.Request.Method == http.MethodOptions {
		return
	}
	identity := identityhttp.MustSession(ctx)
	payload, err := json.Marshal(map[string]any{
		"method":      ctx.Request.Method,
		"path":        ctx.FullPath(),
		"status":      ctx.Writer.Status(),
		"resource_id": ctx.Param("id"),
		"request_id":  ctx.GetHeader("X-Request-ID"),
	})
	if err != nil {
		return
	}
	actorID := identity.PrincipalID
	event := auditEvent{
		ID: uuid.NewString(), EventType: "admin.http." + strings.ToLower(ctx.Request.Method),
		ActorType: string(identity.PrincipalType), ActorID: &actorID, Payload: string(payload), CreatedAt: time.Now().UTC(),
	}
	_ = handler.db.WithContext(ctx.Request.Context()).Create(&event).Error
}
