package channelops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

var testNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func channelopsFixture(t *testing.T, name string) (*Service, *gorm.DB, *secret.Cipher) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PayType{}, &PayRoll{}, &WeChatMPConfig{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return New(db, cipher, func() time.Time { return testNow }), db, cipher
}

func TestPayTypeCatalogueAndPublicFilter(t *testing.T) {
	service, db, _ := channelopsFixture(t, "channelops-paytypes")
	ctx := context.Background()
	for _, request := range []PayTypeRequest{
		{Code: "wxpay", Name: "微信支付", Sort: 2, Icon: "https://cdn.example/wx.svg"},
		{Code: "alipay", Name: "支付宝", Sort: 1},
		{Code: "qqpay", Name: "QQ 钱包", Sort: 3, Status: StatusDisabled},
	} {
		if _, err := service.CreatePayType(ctx, request); err != nil {
			t.Fatalf("create %s: %v", request.Code, err)
		}
	}
	public, err := service.PublicPayTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 2 || public[0].Code != "alipay" || public[1].Code != "wxpay" {
		t.Fatalf("public=%+v", public)
	}
	all, err := service.PayTypes(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("all=%d err=%v", len(all), err)
	}
	if all[2].Code != "qqpay" {
		t.Fatalf("disabled entry missing from admin list: %+v", all)
	}

	if _, err := service.CreatePayType(ctx, PayTypeRequest{Code: "alipay", Name: "重复"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	for name, request := range map[string]PayTypeRequest{
		"short-code":   {Code: "a", Name: "n"},
		"bad-code":     {Code: "1bad", Name: "n"},
		"space-code":   {Code: "bad code", Name: "n"},
		"missing-name": {Code: "name-less"},
		"bad-status":   {Code: "statusless", Name: "n", Status: "paused"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreatePayType(ctx, request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err=%v", err)
			}
		})
	}

	alipay := public[0]
	updated, err := service.UpdatePayType(ctx, alipay.ID, PayTypeRequest{Name: "支付宝（新）", Sort: 9})
	if err != nil || updated.Name != "支付宝（新）" || updated.Sort != 9 || updated.Code != "alipay" {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	if _, err := service.UpdatePayType(ctx, alipay.ID, PayTypeRequest{Name: "x", Code: "wxpay"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("update conflict err=%v", err)
	}
	if _, err := service.UpdatePayType(ctx, "missing", PayTypeRequest{Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err=%v", err)
	}
	disabled, err := service.SetPayTypeStatus(ctx, alipay.ID, StatusDisabled)
	if err != nil || disabled.Status != StatusDisabled {
		t.Fatalf("status=%+v err=%v", disabled, err)
	}
	if _, err := service.SetPayTypeStatus(ctx, alipay.ID, "paused"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad status err=%v", err)
	}
	public, err = service.PublicPayTypes(ctx)
	if err != nil || len(public) != 1 || public[0].Code != "wxpay" {
		t.Fatalf("public after disable=%+v err=%v", public, err)
	}

	if err := service.DeletePayType(ctx, all[2].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.DeletePayType(ctx, all[2].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice err=%v", err)
	}

	// A payment method referenced by a channel cannot be removed.
	if err := db.Exec("CREATE TABLE payment_channels (id VARCHAR(36) PRIMARY KEY, payment_type VARCHAR(32))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO payment_channels (id, payment_type) VALUES (?, ?)", "channel-1", "wxpay").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DeletePayType(ctx, all[1].ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete in-use err=%v", err)
	}
}

func TestPayRollUniquenessAndBatchGeneration(t *testing.T) {
	service, _, _ := channelopsFixture(t, "channelops-rolls")
	ctx := context.Background()
	today := DayStart(testNow)

	roll, err := service.CreatePayRoll(ctx, PayRollRequest{ChannelID: "channel-1", Day: today, Weight: 5})
	if err != nil || roll.Weight != 5 || roll.Status != StatusActive {
		t.Fatalf("create roll=%+v err=%v", roll, err)
	}
	if _, err := service.CreatePayRoll(ctx, PayRollRequest{ChannelID: "channel-1", Day: today}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate roll err=%v", err)
	}
	if _, err := service.CreatePayRoll(ctx, PayRollRequest{ChannelID: "channel-1", Day: today, Weight: -1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative weight err=%v", err)
	}

	created, err := service.GeneratePayRolls(ctx, GenerateRequest{
		ChannelIDs: []string{"channel-1", "channel-2", "channel-2"},
		From:       today, To: today.AddDate(0, 0, 2),
	})
	if err != nil || created != 5 {
		t.Fatalf("generate created=%d err=%v", created, err)
	}
	again, err := service.GeneratePayRolls(ctx, GenerateRequest{
		ChannelIDs: []string{"channel-1", "channel-2"}, From: today, To: today.AddDate(0, 0, 2),
	})
	if err != nil || again != 0 {
		t.Fatalf("regenerate created=%d err=%v", again, err)
	}
	if _, err := service.GeneratePayRolls(ctx, GenerateRequest{ChannelIDs: []string{"c"}, From: today.AddDate(0, 0, 3), To: today}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("reversed range err=%v", err)
	}
	if _, err := service.GeneratePayRolls(ctx, GenerateRequest{From: today, To: today}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty channel list err=%v", err)
	}

	rows, total, err := service.PayRolls(ctx, PayRollFilter{ChannelID: "channel-1"}, Page{Number: 1, Size: 10})
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("list total=%d rows=%d err=%v", total, len(rows), err)
	}
	if !rows[0].RollDate.Equal(today.AddDate(0, 0, 2)) {
		t.Fatalf("rolls are not newest first: %+v", rows)
	}
	page, total, err := service.PayRolls(ctx, PayRollFilter{From: &today, To: &today}, Page{Number: 1, Size: 1})
	if err != nil || total != 2 || len(page) != 1 {
		t.Fatalf("date filter total=%d rows=%d err=%v", total, len(page), err)
	}

	weight := 9
	status := StatusDisabled
	patched, err := service.UpdatePayRoll(ctx, rows[0].ID, PayRollUpdate{Weight: &weight, Status: &status})
	if err != nil || patched.Weight != 9 || patched.Status != StatusDisabled {
		t.Fatalf("patch=%+v err=%v", patched, err)
	}
	if _, err := service.UpdatePayRoll(ctx, rows[0].ID, PayRollUpdate{Weight: intPointer(-1)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad weight err=%v", err)
	}
	if _, err := service.UpdatePayRoll(ctx, "missing", PayRollUpdate{Weight: &weight}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("patch missing err=%v", err)
	}
	if err := service.DeletePayRoll(ctx, rows[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.DeletePayRoll(ctx, rows[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice err=%v", err)
	}
}

func TestWeChatConfigStoresCiphertextOnly(t *testing.T) {
	service, db, _ := channelopsFixture(t, "channelops-wechat")
	ctx := context.Background()
	view, err := service.CreateWeChatConfig(ctx, WeChatMPRequest{
		MerchantID: "merchant-1", AppID: "wx-app-1", AppSecret: "s3cret-value",
		Token: "callback-token", AESKey: "aes-key-value",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !view.SecretConfigured || !view.AESKeyConfigured || view.Status != StatusActive {
		t.Fatalf("view=%+v", view)
	}
	var stored WeChatMPConfig
	if err := db.First(&stored, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.AppSecretEncrypted == "" || strings.Contains(stored.AppSecretEncrypted, "s3cret-value") {
		t.Fatalf("secret not encrypted: %q", stored.AppSecretEncrypted)
	}
	if stored.AESKeyEncrypted == nil || strings.Contains(*stored.AESKeyEncrypted, "aes-key-value") {
		t.Fatalf("aes key not encrypted: %v", stored.AESKeyEncrypted)
	}
	if !strings.HasPrefix(stored.AppSecretEncrypted, "v1.") {
		t.Fatalf("ciphertext has no version prefix: %q", stored.AppSecretEncrypted)
	}
	appSecret, aesKey, err := service.WeChatSecrets(ctx, view.ID)
	if err != nil || appSecret != "s3cret-value" || aesKey != "aes-key-value" {
		t.Fatalf("secrets=%q/%q err=%v", appSecret, aesKey, err)
	}

	// Editing the token keeps both ciphertexts untouched.
	patched, err := service.UpdateWeChatConfig(ctx, view.ID, WeChatMPRequest{AppID: "wx-app-1", Token: "new-token"})
	if err != nil || patched.Token == nil || *patched.Token != "new-token" {
		t.Fatalf("patch=%+v err=%v", patched, err)
	}
	var kept WeChatMPConfig
	if err := db.First(&kept, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if kept.AppSecretEncrypted != stored.AppSecretEncrypted {
		t.Fatalf("secret changed without a new value")
	}
	if kept.AESKeyEncrypted == nil || *kept.AESKeyEncrypted != *stored.AESKeyEncrypted {
		t.Fatal("aes key changed without a new value")
	}

	// Rotating the secret re-encrypts it.
	rotated, err := service.UpdateWeChatConfig(ctx, view.ID, WeChatMPRequest{AppID: "wx-app-1", AppSecret: "rotated-secret"})
	if err != nil || !rotated.SecretConfigured {
		t.Fatalf("rotate=%+v err=%v", rotated, err)
	}
	appSecret, _, err = service.WeChatSecrets(ctx, view.ID)
	if err != nil || appSecret != "rotated-secret" {
		t.Fatalf("rotated secret=%q err=%v", appSecret, err)
	}

	views, total, err := service.WeChatConfigs(ctx, "merchant-1", Page{Number: 1, Size: 10})
	if err != nil || total != 1 || len(views) != 1 {
		t.Fatalf("list total=%d views=%d err=%v", total, len(views), err)
	}
	if _, other, err := service.WeChatConfigs(ctx, "merchant-2", Page{Number: 1, Size: 10}); err != nil || other != 0 {
		t.Fatalf("other merchant total=%d err=%v", other, err)
	}

	if _, err := service.CreateWeChatConfig(ctx, WeChatMPRequest{AppID: "wx-app-2"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing secret err=%v", err)
	}
	if _, err := service.UpdateWeChatConfig(ctx, "missing", WeChatMPRequest{AppID: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("patch missing err=%v", err)
	}
	if err := service.DeleteWeChatConfig(ctx, view.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.DeleteWeChatConfig(ctx, view.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice err=%v", err)
	}
	if _, _, err := service.WeChatSecrets(ctx, view.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("secrets after delete err=%v", err)
	}
}

func TestRollWeightLooksUpTheDay(t *testing.T) {
	service, _, _ := channelopsFixture(t, "channelops-rollweight")
	ctx := context.Background()
	today := DayStart(testNow)
	if _, err := service.CreatePayRoll(ctx, PayRollRequest{ChannelID: "channel-1", Day: today, Weight: 4}); err != nil {
		t.Fatal(err)
	}
	if weight, ok := service.RollWeight(ctx, "channel-1", testNow); !ok || weight != 4 {
		t.Fatalf("today weight=%d ok=%v", weight, ok)
	}
	if weight, ok := service.RollWeight(ctx, "channel-1", testNow.Add(11*time.Hour)); !ok || weight != 4 {
		t.Fatalf("late in day weight=%d ok=%v", weight, ok)
	}
	if _, ok := service.RollWeight(ctx, "channel-1", testNow.AddDate(0, 0, 1)); ok {
		t.Fatal("tomorrow should not have a roll")
	}
	if _, ok := service.RollWeight(ctx, "channel-2", testNow); ok {
		t.Fatal("unknown channel should not have a roll")
	}
	if _, ok := service.RollWeight(ctx, "", testNow); ok {
		t.Fatal("empty channel should not have a roll")
	}
	disabled := StatusDisabled
	if _, err := service.UpdatePayRoll(ctx, mustOnlyRoll(t, service, "channel-1").ID, PayRollUpdate{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.RollWeight(ctx, "channel-1", testNow); ok {
		t.Fatal("disabled roll should fall through")
	}
}

func mustOnlyRoll(t *testing.T, service *Service, channelID string) PayRoll {
	t.Helper()
	rows, total, err := service.PayRolls(context.Background(), PayRollFilter{ChannelID: channelID}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 {
		t.Fatalf("rolls total=%d err=%v", total, err)
	}
	return rows[0]
}

func intPointer(value int) *int { return &value }
