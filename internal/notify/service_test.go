package notify

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/identity/password"
)

var codePattern = regexp.MustCompile(`验证码：(\d{6})`)

func codeFromMail(t *testing.T, body string) string {
	t.Helper()
	matches := codePattern.FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatalf("no code in message %q", body)
	}
	return matches[1]
}

func TestSendVerifyCodeRateLimits(t *testing.T) {
	service, _, sender, source := newFixture(t, "notify-rate")
	ctx := context.Background()
	const target = "13800000001"

	first, err := service.SendVerifyCode(ctx, SceneResetPassword, target)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if first.Scene != SceneResetPassword || !first.ExpiresAt.After(source.at) {
		t.Fatalf("sent = %+v", first)
	}
	if len(sender.sms) != 1 || sender.sms[0].To != target || !strings.Contains(sender.sms[0].Body, "验证码") {
		t.Fatalf("sms = %+v", sender.sms)
	}

	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, target); !errors.Is(err, ErrCodeTooSoon) {
		t.Fatalf("second send inside 60s: err = %v, want ErrCodeTooSoon", err)
	}
	// A different scene does not reset the per-target window.
	if _, err := service.SendVerifyCode(ctx, SceneBindPhone, target); !errors.Is(err, ErrCodeTooSoon) {
		t.Fatalf("other scene inside 60s: err = %v", err)
	}
	source.Advance(61 * time.Second)
	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, target); err != nil {
		t.Fatalf("send after the window: %v", err)
	}
	for index := 2; index < dailyCodeLimit; index++ {
		source.Advance(61 * time.Second)
		if _, err := service.SendVerifyCode(ctx, SceneResetPassword, target); err != nil {
			t.Fatalf("send %d: %v", index+1, err)
		}
	}
	source.Advance(61 * time.Second)
	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, target); !errors.Is(err, ErrCodeDailyLimit) {
		t.Fatalf("11th send: err = %v, want ErrCodeDailyLimit", err)
	}
	source.Advance(24 * time.Hour)
	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, target); err != nil {
		t.Fatalf("send on the next day: %v", err)
	}
	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, "not a target"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid target err = %v", err)
	}
	if _, err := service.SendVerifyCode(ctx, "unknown_scene", target); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown scene err = %v", err)
	}
}

func TestVerifyCodeLifecycle(t *testing.T) {
	service, _, sender, source := newFixture(t, "notify-code")
	ctx := context.Background()
	const target = "user@example.com"

	if _, err := service.SendVerifyCode(ctx, SceneBindPhone, target); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(sender.mail) != 1 || sender.mail[0].To != target {
		t.Fatalf("mail = %+v", sender.mail)
	}
	code := codeFromMail(t, sender.mail[0].Body)

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	for attempt := 1; attempt <= maxCodeAttempts; attempt++ {
		if err := service.VerifyCode(ctx, SceneBindPhone, target, wrong); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("attempt %d: err = %v, want ErrCodeInvalid", attempt, err)
		}
	}
	// The fifth wrong guess burns the code, so even the right one is gone.
	if err := service.VerifyCode(ctx, SceneBindPhone, target, code); !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("burned code err = %v, want ErrCodeNotFound", err)
	}

	source.Advance(61 * time.Second)
	if _, err := service.SendVerifyCode(ctx, SceneBindPhone, target); err != nil {
		t.Fatalf("resend: %v", err)
	}
	fresh := codeFromMail(t, sender.mail[len(sender.mail)-1].Body)
	if err := service.VerifyCode(ctx, SceneBindPhone, target, fresh); err != nil {
		t.Fatalf("verify fresh code: %v", err)
	}
	if err := service.VerifyCode(ctx, SceneBindPhone, target, fresh); !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("reused code err = %v, want ErrCodeNotFound", err)
	}

	source.Advance(61 * time.Second)
	if _, err := service.SendVerifyCode(ctx, SceneBindPhone, target); err != nil {
		t.Fatalf("send for expiry: %v", err)
	}
	expiring := codeFromMail(t, sender.mail[len(sender.mail)-1].Body)
	source.Advance(codeTTL + time.Second)
	if err := service.VerifyCode(ctx, SceneBindPhone, target, expiring); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("expired code err = %v, want ErrCodeExpired", err)
	}
}

func TestSendVerifyCodeBurnsCodeWhenDeliveryFails(t *testing.T) {
	service, db, sender, _ := newFixture(t, "notify-delivery")
	ctx := context.Background()
	sender.fail = errDeliveryStub

	if _, err := service.SendVerifyCode(ctx, SceneResetPassword, "13800000009"); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("delivery failure err = %v", err)
	}
	var pending int64
	if err := db.Model(&VerifyCode{}).Where("consumed_at IS NULL").Count(&pending).Error; err != nil {
		t.Fatalf("count codes: %v", err)
	}
	if pending != 0 {
		t.Fatalf("undelivered code is still usable: %d rows", pending)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	service, db, sender, source := newFixture(t, "notify-reset")
	ctx := context.Background()
	seedMerchant(t, db, merchantSeed{ID: "merchant-1", Email: "Shop@Example.com", Password: "old-password"})
	seedMerchant(t, db, merchantSeed{ID: "merchant-2", Email: "other@example.com", Password: "other-password"})
	seedSession(t, db, "session-1", "merchant-1", source.at)
	seedSession(t, db, "session-2", "merchant-1", source.at)
	seedSession(t, db, "session-3", "merchant-2", source.at)

	// Unknown identifiers are accepted silently: no enumeration.
	if issued, err := service.RequestPasswordReset(ctx, "nobody@example.com"); err != nil || issued {
		t.Fatalf("unknown identifier = (%v,%v), want (false,nil)", issued, err)
	}
	issued, err := service.RequestPasswordReset(ctx, "shop@example.com")
	if err != nil || !issued {
		t.Fatalf("request reset = (%v,%v)", issued, err)
	}
	if len(sender.mail) != 1 || sender.mail[0].To != "shop@example.com" {
		t.Fatalf("reset mail = %+v", sender.mail)
	}
	var resetCount int64
	if err := db.Model(&PasswordReset{}).Where("principal_id = ?", "merchant-1").Count(&resetCount).Error; err != nil {
		t.Fatalf("count resets: %v", err)
	}
	if resetCount != 1 {
		t.Fatalf("password_resets rows = %d, want 1", resetCount)
	}

	ticket, err := service.issueResetToken(ctx, "merchant-1")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if err := service.ResetPassword(ctx, "Shop@Example.com", strings.ToUpper(ticket.Token), "brand-new-password"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("token is case sensitive on purpose, err = %v", err)
	}
	if err := service.ResetPassword(ctx, "shop@example.com", ticket.Token, "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("short password err = %v", err)
	}
	if err := service.ResetPassword(ctx, "shop@example.com", ticket.Token, "brand-new-password"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// One-time: the same token cannot be replayed.
	if err := service.ResetPassword(ctx, "shop@example.com", ticket.Token, "another-password"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("replayed token err = %v, want ErrInvalidToken", err)
	}
	// Every session of the merchant is revoked; other merchants are untouched.
	if live := liveSessions(t, db, "merchant-1"); live != 0 {
		t.Fatalf("merchant-1 live sessions = %d, want 0", live)
	}
	if live := liveSessions(t, db, "merchant-2"); live != 1 {
		t.Fatalf("merchant-2 live sessions = %d, want 1", live)
	}

	hash := storedPasswordHash(t, db, "merchant-1")
	if matched, _, _ := password.Verify("old-password", hash); matched {
		t.Fatal("old password still works after the reset")
	}
	if matched, _, _ := password.Verify("brand-new-password", hash); !matched {
		t.Fatal("new password does not work after the reset")
	}

	// Expired tokens are rejected.
	expired, err := service.issueResetToken(ctx, "merchant-1")
	if err != nil {
		t.Fatalf("issue expiring token: %v", err)
	}
	source.Advance(resetTTL + time.Minute)
	if err := service.ResetPassword(ctx, "shop@example.com", expired.Token, "yet-another-password"); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired token err = %v, want ErrTokenExpired", err)
	}
}

func TestChangePassword(t *testing.T) {
	service, db, _, _ := newFixture(t, "notify-change")
	ctx := context.Background()
	seedMerchant(t, db, merchantSeed{ID: "merchant-1", Email: "shop@example.com", Password: "old-password"})

	if err := service.ChangePassword(ctx, "merchant-1", "wrong-password", "new-password-1"); !errors.Is(err, ErrInvalidOldPassword) {
		t.Fatalf("wrong old password err = %v", err)
	}
	if err := service.ChangePassword(ctx, "merchant-1", "old-password", "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("short new password err = %v", err)
	}
	if err := service.ChangePassword(ctx, "merchant-1", "old-password", "new-password-1"); err != nil {
		t.Fatalf("change: %v", err)
	}
	hash := storedPasswordHash(t, db, "merchant-1")
	if matched, _, _ := password.Verify("old-password", hash); matched {
		t.Fatal("old password still works")
	}
	if matched, _, _ := password.Verify("new-password-1", hash); !matched {
		t.Fatal("new password does not work")
	}
	if err := service.ChangePassword(ctx, "missing", "old-password", "new-password-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown merchant err = %v", err)
	}
}

func TestChannelsEncryptAndMaskConfig(t *testing.T) {
	service, db, sender, _ := newFixture(t, "notify-channel")
	ctx := context.Background()

	view, err := service.CreateChannel(ctx, ChannelInput{
		Kind:     KindSMS,
		Provider: "aliyun-sms",
		Config:   map[string]string{"access_key": "AK-secret-value", "sign_name": "GorixPay"},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if view.Status != StatusActive || len(view.ConfigKeys) != 2 {
		t.Fatalf("view = %+v", view)
	}

	var stored Channel
	if err := db.First(&stored, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("reread channel: %v", err)
	}
	if strings.Contains(stored.ConfigEncrypted, "AK-secret-value") || strings.Contains(stored.ConfigEncrypted, "access_key") {
		t.Fatalf("config stored in plaintext: %q", stored.ConfigEncrypted)
	}
	plain, version, err := service.cipher.Decrypt(stored.ConfigEncrypted, []byte(channelContextPrefix+view.ID))
	if err != nil || version != "v1" {
		t.Fatalf("decrypt = (%q,%q,%v)", plain, version, err)
	}
	if !strings.Contains(string(plain), "AK-secret-value") {
		t.Fatalf("decrypted config = %s", plain)
	}
	// The AAD binds the ciphertext to the row: another id cannot open it.
	if _, _, err := service.cipher.Decrypt(stored.ConfigEncrypted, []byte(channelContextPrefix+"other-id")); err == nil {
		t.Fatal("ciphertext opened with a foreign AAD")
	}

	// Update without config keeps the stored secret.
	updated, err := service.UpdateChannel(ctx, view.ID, ChannelInput{Kind: KindSMS, Provider: "aliyun-sms", Status: StatusDisabled})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Status != StatusDisabled || len(updated.ConfigKeys) != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	var afterUpdate Channel
	if err := db.First(&afterUpdate, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("reread: %v", err)
	}
	if afterUpdate.ConfigEncrypted != stored.ConfigEncrypted {
		t.Fatal("config changed although the request omitted it")
	}

	// Explicit config replaces the stored secret.
	if _, err := service.UpdateChannel(ctx, view.ID, ChannelInput{
		Kind: KindSMS, Provider: "aliyun-sms", Status: StatusActive,
		Config: map[string]string{"access_key": "AK-2"}, ConfigSet: true,
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	var replaced Channel
	if err := db.First(&replaced, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("reread: %v", err)
	}
	if replaced.ConfigEncrypted == stored.ConfigEncrypted {
		t.Fatal("config was not re-encrypted")
	}

	// The test endpoint sends through the matching Sender method.
	if _, err := service.TestChannel(ctx, view.ID); err != nil {
		t.Fatalf("test channel: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1", sender.calls)
	}

	// A different master key cannot read the row.
	other, err := NewService(Config{
		DB: db, Cipher: newTestCipher(t, 0x7f), Sender: sender, Now: service.now,
	})
	if err != nil {
		t.Fatalf("second service: %v", err)
	}
	if _, _, err := other.ListChannels(ctx, ChannelFilter{}, Page{}); err == nil {
		t.Fatal("a channel encrypted with another key was readable")
	}
}

func TestChannelValidationDeleteAndList(t *testing.T) {
	service, _, _, _ := newFixture(t, "notify-channel-admin")
	ctx := context.Background()

	cases := []ChannelInput{
		{Kind: "push", Provider: "x"},
		{Kind: KindSMS, Provider: ""},
		{Kind: KindSMS, Provider: "x", Status: "paused"},
		{Kind: KindMail, Provider: "smtp", Config: map[string]string{"": "v"}},
	}
	for index, input := range cases {
		if _, err := service.CreateChannel(ctx, input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("case %d: err = %v, want ErrInvalidRequest", index, err)
		}
	}

	mail, err := service.CreateChannel(ctx, ChannelInput{Kind: KindMail, Provider: "smtp", Config: map[string]string{"host": "smtp.example", "password": "secret"}})
	if err != nil {
		t.Fatalf("create mail: %v", err)
	}
	sms, err := service.CreateChannel(ctx, ChannelInput{Kind: KindSMS, Provider: "aliyun-sms", Status: StatusDisabled})
	if err != nil {
		t.Fatalf("create sms: %v", err)
	}

	views, total, err := service.ListChannels(ctx, ChannelFilter{}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 || len(views) != 2 {
		t.Fatalf("list: total=%d views=%d err=%v", total, len(views), err)
	}
	views, total, err = service.ListChannels(ctx, ChannelFilter{Kind: "sms"}, Page{})
	if err != nil || total != 1 || views[0].ID != sms.ID {
		t.Fatalf("kind filter: total=%d err=%v", total, err)
	}
	if _, _, err := service.ListChannels(ctx, ChannelFilter{Status: "paused"}, Page{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid status filter err = %v", err)
	}

	if err := service.DeleteChannel(ctx, mail.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.DeleteChannel(ctx, mail.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
	if _, err := service.TestChannel(ctx, mail.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("test deleted channel err = %v", err)
	}
	if _, err := service.UpdateChannel(ctx, "missing", ChannelInput{Kind: KindSMS, Provider: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err = %v", err)
	}
}
