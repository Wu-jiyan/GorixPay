package notifyprefs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

type fakeMailer struct {
	calls int
	to    []string
	err   error
}

func (mailer *fakeMailer) Send(_ context.Context, to, _, _ string) error {
	mailer.calls++
	mailer.to = append(mailer.to, to)
	return mailer.err
}

type fakeSMS struct {
	calls int
	phone []string
	err   error
}

func (sender *fakeSMS) Send(_ context.Context, phone, _ string) error {
	sender.calls++
	sender.phone = append(sender.phone, phone)
	return sender.err
}

type fakeWeChat struct {
	calls      int
	openID     string
	templateID string
	url        string
	data       map[string]string
	err        error
}

func (sender *fakeWeChat) SendWeChatTemplate(_ context.Context, openID, templateID, url string, data map[string]string) error {
	sender.calls++
	sender.openID = openID
	sender.templateID = templateID
	sender.url = url
	sender.data = data
	return sender.err
}

type fixture struct {
	service *Service
	db      *gorm.DB
	mail    *fakeMailer
	sms     *fakeSMS
	wechat  *fakeWeChat
	logs    *bytes.Buffer
	logger  *slog.Logger
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Prefs{}, &systemSetting{}, &identitymodel.Merchant{}, &identitymodel.Session{}, &oauthBinding{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	email := "merchant@example.com"
	merchant := identitymodel.Merchant{ID: "merchant-1", Email: &email, DisplayName: "商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	phone := "13800000000"
	withPhone := identitymodel.Merchant{ID: "merchant-2", Phone: &phone, DisplayName: "手机商户", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	for _, record := range []identitymodel.Merchant{merchant, withPhone} {
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	mail := &fakeMailer{}
	sms := &fakeSMS{}
	wechat := &fakeWeChat{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	service := New(db, logger).WithSenders(mail, sms).WithWeChat(wechat)
	return &fixture{service: service, db: db, mail: mail, sms: sms, wechat: wechat, logs: logs, logger: logger}
}

// bindWeChat stores the merchant's 公众号 openid the way the oauth module does.
func bindWeChat(t *testing.T, db *gorm.DB, merchantID, openID string) {
	t.Helper()
	record := oauthBinding{ID: "binding-" + merchantID, PrincipalType: string(identitymodel.PrincipalMerchant), PrincipalID: merchantID, Provider: weChatProvider, OpenID: openID}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("bind openid: %v", err)
	}
}

func setTemplateID(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	if err := db.Create(&systemSetting{Key: WeChatTemplateSettingKey, Value: value}).Error; err != nil {
		t.Fatalf("seed template id: %v", err)
	}
}

func TestPrefsDefaultsAndValidation(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-defaults")
	ctx := context.Background()
	prefs, err := fixture.service.Prefs(ctx, "merchant-1")
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.NotifyPayment || !prefs.NotifySettlement || !prefs.NotifyComplaint || prefs.NotifyChannel != ChannelMail {
		t.Fatalf("defaults=%+v", prefs)
	}
	off := false
	updated, err := fixture.service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyPayment: &off})
	if err != nil || updated.NotifyPayment || !updated.NotifySettlement {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	channel := "sms"
	if _, err := fixture.service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyChannel: &channel}); err != nil {
		t.Fatal(err)
	}
	bad := "pigeon"
	if _, err := fixture.service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyChannel: &bad}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid channel err=%v", err)
	}
	stored, err := fixture.service.Prefs(ctx, "merchant-1")
	if err != nil || stored.NotifyChannel != ChannelSMS || stored.NotifyPayment {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestNotifyRespectsSceneSwitches(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-switches")
	ctx := context.Background()
	off := false
	if _, err := fixture.service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyPayment: &off}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
		t.Fatal(err)
	}
	if fixture.mail.calls != 0 || fixture.sms.calls != 0 {
		t.Fatalf("disabled scene delivered: mail=%d sms=%d", fixture.mail.calls, fixture.sms.calls)
	}
	if err := fixture.service.Notify(ctx, "merchant-1", "settlement", "结算完成", "金额 100.00"); err != nil {
		t.Fatal(err)
	}
	if fixture.mail.calls != 1 || fixture.mail.to[0] != "merchant@example.com" {
		t.Fatalf("mail calls=%d to=%v", fixture.mail.calls, fixture.mail.to)
	}
	if err := fixture.service.Notify(ctx, "merchant-1", "unknown", "主题", "正文"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown scene err=%v", err)
	}
	if err := fixture.service.Notify(ctx, "  ", "payment", "主题", "正文"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty merchant err=%v", err)
	}
	if err := fixture.service.Notify(ctx, "missing", "payment", "主题", "正文"); err != nil {
		t.Fatalf("missing merchant err=%v", err)
	}
}

func TestNotifySMSFallsBackToMail(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-sms")
	ctx := context.Background()
	channel := "sms"
	prefs, err := fixture.service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyChannel: &channel})
	if err != nil || prefs.NotifyChannel != ChannelSMS {
		t.Fatalf("prefs=%+v err=%v", prefs, err)
	}
	// merchant-1 has no phone, so the sms channel falls back to mail.
	if err := fixture.service.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
		t.Fatal(err)
	}
	if fixture.sms.calls != 0 || fixture.mail.calls != 1 {
		t.Fatalf("sms=%d mail=%d", fixture.sms.calls, fixture.mail.calls)
	}
	// merchant-2 has a phone; an unconfigured gateway must not fail the call.
	if _, err := fixture.service.UpdatePrefs(ctx, "merchant-2", UpdateInput{NotifyChannel: &channel}); err != nil {
		t.Fatal(err)
	}
	fixture.sms.err = ErrChannelNotConfigured
	if err := fixture.service.Notify(ctx, "merchant-2", "payment", "支付成功", "订单 T2"); err != nil {
		t.Fatalf("unconfigured sms err=%v", err)
	}
	if fixture.sms.calls != 1 {
		t.Fatalf("sms calls=%d", fixture.sms.calls)
	}
}

func TestNotifyWeChatChannel(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-wechat")
	ctx := context.Background()
	channel := ChannelWeChat
	if _, err := fixture.service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyChannel: &channel}); err != nil {
		t.Fatalf("wechat channel rejected: %v", err)
	}
	setTemplateID(t, fixture.db, `"tmpl-1"`)
	bindWeChat(t, fixture.db, "merchant-1", "open-1")

	if err := fixture.service.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if fixture.wechat.calls != 1 {
		t.Fatalf("wechat calls = %d, want 1", fixture.wechat.calls)
	}
	if fixture.wechat.openID != "open-1" || fixture.wechat.templateID != "tmpl-1" || fixture.wechat.url != "" {
		t.Fatalf("wechat call = %+v", fixture.wechat)
	}
	if fixture.wechat.data["first"] != "支付成功" || fixture.wechat.data["keyword1"] != "订单 T1" {
		t.Fatalf("template data = %v", fixture.wechat.data)
	}
	if fixture.mail.calls != 0 || fixture.sms.calls != 0 {
		t.Fatalf("fallback delivered: mail=%d sms=%d", fixture.mail.calls, fixture.sms.calls)
	}
}

func TestNotifyWeChatFallsBackToMail(t *testing.T) {
	ctx := context.Background()
	channel := ChannelWeChat

	cases := []struct {
		name   string
		setup  func(t *testing.T, fixture *fixture) *Service
		reason string
	}{
		{
			name:   "no openid",
			reason: "no_openid",
			setup: func(t *testing.T, fixture *fixture) *Service {
				setTemplateID(t, fixture.db, "tmpl-1")
				return fixture.service
			},
		},
		{
			name:   "no template",
			reason: "template_not_configured",
			setup: func(t *testing.T, fixture *fixture) *Service {
				bindWeChat(t, fixture.db, "merchant-1", "open-1")
				return fixture.service
			},
		},
		{
			name:   "sender not wired",
			reason: "sender_not_wired",
			setup: func(t *testing.T, fixture *fixture) *Service {
				bindWeChat(t, fixture.db, "merchant-1", "open-1")
				setTemplateID(t, fixture.db, "tmpl-1")
				return New(fixture.db, fixture.logger).WithSenders(fixture.mail, fixture.sms)
			},
		},
		{
			name:   "delivery failed",
			reason: "delivery_failed",
			setup: func(t *testing.T, fixture *fixture) *Service {
				bindWeChat(t, fixture.db, "merchant-1", "open-1")
				setTemplateID(t, fixture.db, "tmpl-1")
				fixture.wechat.err = errors.New("errcode=40003 invalid openid")
				return fixture.service
			},
		},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFixture(t, "notifyprefs-wechat-fallback-"+strconv.Itoa(index))
			service := testCase.setup(t, fixture)
			if _, err := service.UpdatePrefs(ctx, "merchant-1", UpdateInput{NotifyChannel: &channel}); err != nil {
				t.Fatalf("channel: %v", err)
			}
			if err := service.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
				t.Fatalf("notify: %v", err)
			}
			if fixture.mail.calls != 1 || fixture.mail.to[0] != "merchant@example.com" {
				t.Fatalf("fallback mail = %d %v", fixture.mail.calls, fixture.mail.to)
			}
			logs := fixture.logs.String()
			if !strings.Contains(logs, "wechat channel unavailable") || !strings.Contains(logs, testCase.reason) {
				t.Fatalf("log = %s", logs)
			}
		})
	}
}

func TestPrefsRejectsUnknownChannel(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-wechat-validate")
	bad := "fax"
	if _, err := fixture.service.UpdatePrefs(context.Background(), "merchant-1", UpdateInput{NotifyChannel: &bad}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown channel err = %v", err)
	}
}

func TestNotifySwallowsDeliveryFailures(t *testing.T) {
	fixture := newFixture(t, "notifyprefs-failures")
	ctx := context.Background()
	fixture.mail.err = errors.New("smtp down")
	if err := fixture.service.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
		t.Fatalf("delivery failure must not bubble: %v", err)
	}
	fixture.mail.err = ErrChannelNotConfigured
	if err := fixture.service.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
		t.Fatalf("unconfigured mail err=%v", err)
	}
	bare := New(fixture.db, nil)
	if err := bare.Notify(ctx, "merchant-1", "payment", "支付成功", "订单 T1"); err != nil {
		t.Fatalf("default senders err=%v", err)
	}
}
