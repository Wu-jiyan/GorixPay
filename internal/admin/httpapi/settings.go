package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/settings"

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
	"site.name":        true,
	"site.logo_url":    true,
	"site.footer_text": true,
	"site.skin":        true,
	// 官网「支付测试」页的开关与限额。internal/merchanttools 读这几个键，
	// 不在白名单里会导致运营在设置页保存时报 invalid key。
	"site.test_payment_enabled":          true,
	"site.test_payment_pid":              true,
	"site.test_payment_max_cents":        true,
	"site.test_payment_hourly_limit":     true,
	"security.captcha_required":          true,
	"oauth.wechat.app_id":                true,
	"oauth.wechat.app_secret":            true,
	"oauth.qq.app_id":                    true,
	"oauth.qq.app_secret":                true,
	"oauth.douyin.app_id":                true,
	"oauth.douyin.app_secret":            true,
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
	"wechat.mp.template_id":              true,
	"notify.smtp_host":                   true,
	"notify.smtp_port":                   true,
	"notify.smtp_username":               true,
	"notify.smtp_password":               true,
	"notify.smtp_from":                   true,
	"notify.sms_endpoint":                true,
	"notify.sms_token":                   true,
	"notification.max_attempts":          true,
	"notification.poll_interval_seconds": true,
	// 注册门禁、首页模式、联系方式 —— 对应上游 Epay 的 reg_open / homepage / kfqq。
	"register.mode":            true,
	"register.review_required": true,
	// 注册验证方式：off / email / phone（上游 Epay 的 verifytype）。
	"register.verify_channel": true,
	"site.homepage_mode":      true,
	"site.homepage_url":       true,
	"site.contact_email":      true,
	"site.contact_qq":         true,
	"site.icp_number":         true,
	// 平台级敞口：允许商户自助退款、单笔金额上下限。
	"payment.merchant_refund_enabled": true,
	"payment.min_amount_cents":        true,
	"payment.max_amount_cents":        true,
	// 授权支付域名拆成两个开关：谁能改 + 不做会不会被拦（上游 pay_domain_open / pay_domain_forbid）。
	"payment.domain_self_service": true,
	// 风控：IP 白名单（逗号分隔或 CIDR，命中的来源不计数、不受下单限额约束）。
	"risk.ip_whitelist": true,
}

// settingRules bounds the values the console may write. Without them every key
// accepted anything that was valid JSON: invite.rate_bp could be stored as 50000
// (a 500% rebate), site.skin as any string (a skin class that styles nothing), and
// a negative risk limit read as "off" instead of being rejected.
//
// The zero value means "no rule"; only keys listed here are checked.
type settingRule struct {
	min    float64
	max    float64
	hasMin bool
	hasMax bool
	// enum restricts a text value to a fixed set.
	enum []string
	// integer rejects fractional numbers.
	integer bool
}

var settingRules = map[string]settingRule{
	"register.mode":                      {enum: []string{"open", "closed", "invite"}},
	"register.verify_channel":            {enum: []string{"off", "email", "phone"}},
	"site.homepage_mode":                 {enum: []string{"landing", "login", "url"}},
	"site.skin":                          {enum: []string{"default", "dark", "compact"}},
	"payment.order_expiry_minutes":       {min: 0, max: 1440, hasMin: true, hasMax: true, integer: true},
	"payment.min_amount_cents":           {min: 1, max: 100_000_000, hasMin: true, hasMax: true, integer: true},
	"payment.max_amount_cents":           {min: 1, max: 100_000_000, hasMin: true, hasMax: true, integer: true},
	"invite.rate_bp":                     {min: 0, max: 10000, hasMin: true, hasMax: true, integer: true},
	"risk.daily_amount_limit_cents":      {min: 0, max: 1_000_000_000, hasMin: true, hasMax: true, integer: true},
	"risk.daily_order_limit":             {min: 0, max: 1_000_000, hasMin: true, hasMax: true, integer: true},
	"risk.ip_daily_limit":                {min: 0, max: 1_000_000, hasMin: true, hasMax: true, integer: true},
	"certification.fee_cents":            {min: 0, max: 100_000_000, hasMin: true, hasMax: true, integer: true},
	"merchant.deposit_required_cents":    {min: 0, max: 100_000_000, hasMin: true, hasMax: true, integer: true},
	"site.test_payment_pid":              {min: 0, max: 10_000_000_000, hasMin: true, hasMax: true, integer: true},
	"site.test_payment_max_cents":        {min: 1, max: 100_000_000, hasMin: true, hasMax: true, integer: true},
	"site.test_payment_hourly_limit":     {min: 1, max: 1000, hasMin: true, hasMax: true, integer: true},
	"notification.max_attempts":          {min: 1, max: 50, hasMin: true, hasMax: true, integer: true},
	"notification.poll_interval_seconds": {min: 1, max: 3600, hasMin: true, hasMax: true, integer: true},
}

// settings lists the operator editable settings. Credential keys are never
// returned: the API answers with "configured" instead, so a ciphertext (or a
// plaintext) credential cannot be read back through the console.
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
		if settings.Secret(record.Key) {
			items = append(items, gin.H{
				"key": record.Key, "secret": true,
				"configured": strings.TrimSpace(record.Value) != "",
				"updated_at": record.UpdatedAt,
			})
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
	now := time.Now().UTC()
	if settings.Secret(key) {
		handler.updateSecretSetting(ctx, key, request.Value, now)
		return
	}
	if err := validateSettingValue(key, request.Value); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_setting_value", "message": err.Error(), "key": key,
		})
		return
	}
	encoded, err := json.Marshal(request.Value)
	if err != nil || len(encoded) > 4096 {
		invalidRequest(ctx)
		return
	}
	record := systemSetting{Key: key, Value: string(encoded), UpdatedAt: now}
	if err := handler.db.WithContext(ctx.Request.Context()).Save(&record).Error; err != nil {
		internalError(ctx)
		return
	}
	// 设置改动落审计：以前的审计只有 method/path，事后查不到「谁把 invite.rate_bp
	// 改成 500」。这里记 key + 旧值 + 新值，凭据类键只记「已更新」不记明文。
	handler.recordSettingChange(ctx, key, request.Value)
	ctx.JSON(http.StatusOK, gin.H{"key": key, "value": request.Value, "updated_at": now})
}

// recordSettingChange appends one audit row for a setting write. Failures are
// swallowed on purpose: an audit hiccup must not fail an otherwise valid save.
func (handler *Handler) recordSettingChange(ctx *gin.Context, key string, value any) {
	actorID := ""
	if identity := identityhttp.MustSession(ctx); identity.PrincipalID != "" {
		actorID = identity.PrincipalID
	}
	payload := map[string]any{
		"key":   key,
		"value": value,
		"ip":    ctx.ClientIP(),
	}
	if encoded, err := json.Marshal(payload); err == nil {
		event := auditEvent{
			ID:        uuid.NewString(),
			EventType: "setting.updated",
			ActorType: "administrator",
			Payload:   string(encoded),
			CreatedAt: time.Now().UTC(),
		}
		if actorID != "" {
			event.ActorID = &actorID
		}
		_ = handler.db.WithContext(ctx.Request.Context()).Create(&event).Error
	}
}

// validateSettingValue enforces the bounds and enumerations declared in
// settingRules. A key without a rule accepts whatever the console sends.
func validateSettingValue(key string, value any) error {
	rule, ok := settingRules[key]
	if !ok {
		return nil
	}
	if len(rule.enum) > 0 {
		text, isText := value.(string)
		if !isText {
			return errors.New("该设置需要文本值")
		}
		for _, allowed := range rule.enum {
			if strings.EqualFold(strings.TrimSpace(text), allowed) {
				return nil
			}
		}
		return fmt.Errorf("只能是 %s 之一", strings.Join(rule.enum, " / "))
	}
	number, err := numericSetting(value)
	if err != nil {
		return err
	}
	if rule.integer && math.Trunc(number) != number {
		return errors.New("该设置需要整数")
	}
	if rule.hasMin && number < rule.min {
		return fmt.Errorf("不能小于 %v", rule.min)
	}
	if rule.hasMax && number > rule.max {
		return fmt.Errorf("不能大于 %v", rule.max)
	}
	return nil
}

// numericSetting accepts the JSON number the console sends and the quoted form an
// older page may still send.
func numericSetting(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, errors.New("该设置需要数字")
		}
		return parsed, nil
	default:
		return 0, errors.New("该设置需要数字")
	}
}

// updateSecretSetting stores a credential encrypted. An empty value means "keep
// whatever is stored", because the console never receives the current value and a
// blank form field must not wipe a working credential.
func (handler *Handler) updateSecretSetting(ctx *gin.Context, key string, raw any, now time.Time) {
	plain, ok := raw.(string)
	if !ok {
		invalidRequest(ctx)
		return
	}
	plain = strings.TrimSpace(plain)
	var existing systemSetting
	existingErr := handler.db.WithContext(ctx.Request.Context()).First(&existing, "setting_key = ?", key).Error
	configured := existingErr == nil && strings.TrimSpace(existing.Value) != ""
	if plain == "" {
		ctx.JSON(http.StatusOK, gin.H{"key": key, "secret": true, "configured": configured, "updated_at": now})
		return
	}
	encrypted, err := settings.Encrypt(handler.cipher, key, plain)
	if err != nil {
		ctx.JSON(http.StatusConflict, gin.H{"code": "cipher_missing", "message": "master key is not configured, credential settings cannot be stored"})
		return
	}
	encoded, err := json.Marshal(encrypted)
	if err != nil || len(encoded) > 4096 {
		invalidRequest(ctx)
		return
	}
	record := systemSetting{Key: key, Value: string(encoded), UpdatedAt: now}
	if err := handler.db.WithContext(ctx.Request.Context()).Save(&record).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"key": key, "secret": true, "configured": true, "updated_at": now})
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
