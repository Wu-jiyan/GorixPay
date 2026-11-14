package device

import (
	"context"
	"errors"
	"testing"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func TestVoiceHeartbeatAndAckAreIdempotent(t *testing.T) {
	env := newDeviceEnv(t, "device-voice")
	device, secret, err := env.service.CreateVoiceDevice(context.Background(), VoiceDeviceInput{MerchantID: &env.merchant1.ID, Name: "前台音箱"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.VoiceTest(context.Background(), device.ID, "支付宝到账 1.00 元"); err != nil {
		t.Fatal(err)
	}
	broadcasts, err := env.service.VoiceHeartbeat(context.Background(), device.DeviceNo, secret)
	if err != nil || len(broadcasts) != 1 || broadcasts[0].Text != "支付宝到账 1.00 元" {
		t.Fatalf("heartbeat broadcasts=%+v err=%v", broadcasts, err)
	}
	if broadcasts[0].Status != BroadcastDelivered || broadcasts[0].DeliveredAt == nil {
		t.Fatalf("broadcast not marked delivered: %+v", broadcasts[0])
	}
	again, err := env.service.VoiceHeartbeat(context.Background(), device.DeviceNo, secret)
	if err != nil || len(again) != 0 {
		t.Fatalf("second heartbeat broadcasts=%+v err=%v", again, err)
	}
	if _, err := env.service.VoiceHeartbeat(context.Background(), device.DeviceNo, "wrong-secret"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong secret err=%v", err)
	}

	extra, err := env.service.VoiceTest(context.Background(), device.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	acked, err := env.service.VoiceAck(context.Background(), device.DeviceNo, secret, []string{extra.ID})
	if err != nil || acked != 1 {
		t.Fatalf("ack=%d err=%v", acked, err)
	}
	replay, err := env.service.VoiceAck(context.Background(), device.DeviceNo, secret, []string{extra.ID})
	if err != nil || replay != 0 {
		t.Fatalf("replay ack=%d err=%v", replay, err)
	}
	var deviceRow VoiceDevice
	if err := env.db.First(&deviceRow, "id = ?", device.ID).Error; err != nil || deviceRow.LastSeenAt == nil {
		t.Fatalf("last seen not refreshed: %+v err=%v", deviceRow, err)
	}
}

func TestPrinterPollRetriesUntilAck(t *testing.T) {
	env := newDeviceEnv(t, "device-printer")
	printer, secret, err := env.service.CreatePrinter(context.Background(), PrinterInput{MerchantID: env.merchant1.ID, Name: "收银台打印机"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := env.service.PrinterTest(context.Background(), printer.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := env.service.PrinterPoll(context.Background(), printer.ID, secret)
	if err != nil || len(first) != 1 || first[0].ID != job.ID || first[0].Attempts != 1 {
		t.Fatalf("first poll=%+v err=%v", first, err)
	}
	second, err := env.service.PrinterPoll(context.Background(), printer.ID, secret)
	if err != nil || len(second) != 1 || second[0].Attempts != 2 {
		t.Fatalf("second poll=%+v err=%v", second, err)
	}
	third, err := env.service.PrinterPoll(context.Background(), printer.ID, secret)
	if err != nil || len(third) != 1 || third[0].Attempts != 3 {
		t.Fatalf("third poll=%+v err=%v", third, err)
	}
	exhausted, err := env.service.PrinterPoll(context.Background(), printer.ID, secret)
	if err != nil || len(exhausted) != 0 {
		t.Fatalf("exhausted poll=%+v err=%v", exhausted, err)
	}
	acked, err := env.service.PrinterAck(context.Background(), printer.ID, secret, []string{job.ID})
	if err != nil {
		t.Fatal(err)
	}
	if acked != 1 {
		t.Fatalf("acked=%d", acked)
	}
	var stored PrintJob
	if err := env.db.First(&stored, "id = ?", job.ID).Error; err != nil || stored.Status != JobPrinted || stored.PrintedAt == nil {
		t.Fatalf("job=%+v err=%v", stored, err)
	}
	if _, err := env.service.PrinterPoll(context.Background(), printer.ID, "bad-secret"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong secret err=%v", err)
	}
}

func TestPaymentHookEnqueuesOncePerDeviceAndPrinter(t *testing.T) {
	env := newDeviceEnv(t, "device-hook")
	global, globalSecret, err := env.service.CreateVoiceDevice(context.Background(), VoiceDeviceInput{Name: "全站音箱"})
	if err != nil || globalSecret == "" {
		t.Fatalf("global device err=%v", err)
	}
	mine, _, err := env.service.CreateVoiceDevice(context.Background(), VoiceDeviceInput{MerchantID: &env.merchant1.ID, Name: "商户音箱"})
	if err != nil {
		t.Fatal(err)
	}
	printer, _, err := env.service.CreatePrinter(context.Background(), PrinterInput{MerchantID: env.merchant1.ID, Name: "商户打印机"})
	if err != nil {
		t.Fatal(err)
	}
	otherPrinter, _, err := env.service.CreatePrinter(context.Background(), PrinterInput{MerchantID: env.merchant2.ID, Name: "别家打印机"})
	if err != nil {
		t.Fatal(err)
	}
	order := paymentmodel.Order{
		ID: "order-1", TradeNo: "T20261114000001", MerchantID: env.merchant1.ID, OutTradeNo: "OUT-1",
		PaymentType: "wxpay", Name: "测试订单", Amount: 1234, Currency: "CNY",
		Status: paymentmodel.StatusPaid, Kind: paymentmodel.KindSale, NotifyURL: "https://merchant.example/notify",
		CreatedAt: env.now, UpdatedAt: env.now,
	}
	if err := env.db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	hook := env.service.PaymentHook()
	for attempt := 0; attempt < 2; attempt++ {
		if err := env.db.Transaction(func(tx *gorm.DB) error {
			return hook.OnPayment(context.Background(), tx, order)
		}); err != nil {
			t.Fatal(err)
		}
	}
	var broadcasts []Broadcast
	if err := env.db.Order("device_id ASC").Find(&broadcasts).Error; err != nil {
		t.Fatal(err)
	}
	if len(broadcasts) != 2 {
		t.Fatalf("broadcasts=%d %+v", len(broadcasts), broadcasts)
	}
	for _, broadcast := range broadcasts {
		if broadcast.Text != "微信到账 12.34 元" || broadcast.AmountCents != 1234 || broadcast.OrderID == nil {
			t.Fatalf("broadcast=%+v", broadcast)
		}
		if broadcast.DeviceID != global.ID && broadcast.DeviceID != mine.ID {
			t.Fatalf("unexpected device %s", broadcast.DeviceID)
		}
	}
	var jobs []PrintJob
	if err := env.db.Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].PrinterID != printer.ID {
		t.Fatalf("jobs=%+v other=%s", jobs, otherPrinter.ID)
	}
	if jobs[0].Content == "" || jobs[0].OrderID == nil {
		t.Fatalf("receipt content empty: %+v", jobs[0])
	}

	recharge := order
	recharge.ID = "order-2"
	recharge.Kind = paymentmodel.KindRecharge
	if err := env.db.Transaction(func(tx *gorm.DB) error {
		return hook.OnPayment(context.Background(), tx, recharge)
	}); err != nil {
		t.Fatal(err)
	}
	var afterBroadcasts, afterJobs int64
	env.db.Model(&Broadcast{}).Count(&afterBroadcasts)
	env.db.Model(&PrintJob{}).Count(&afterJobs)
	if afterBroadcasts != 2 || afterJobs != 1 {
		t.Fatalf("recharge enqueued work: broadcasts=%d jobs=%d", afterBroadcasts, afterJobs)
	}
}

func TestCleanupDryRunAndDelete(t *testing.T) {
	env := newDeviceEnv(t, "device-cleanup")
	old := env.now.Add(-45 * 24 * time.Hour)
	recent := env.now.Add(-1 * time.Hour)
	insertAudit := func(id string, at time.Time) {
		if err := env.db.Exec("INSERT INTO audit_events (id, event_type, actor_type, payload, created_at) VALUES (?, ?, ?, ?, ?)",
			id, "test.event", "administrator", "{}", at).Error; err != nil {
			t.Fatal(err)
		}
	}
	insertOutbox := func(id string, at time.Time) {
		if err := env.db.Exec("INSERT INTO outbox_messages (id, topic, aggregate_id, idempotency_key, payload, status, attempts, available_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, "test.topic", "agg-1", "idem-"+id, "{}", "pending", 0, at, at, at).Error; err != nil {
			t.Fatal(err)
		}
	}
	insertAudit("audit-old", old)
	insertAudit("audit-recent", recent)
	insertOutbox("outbox-old", old)
	insertOutbox("outbox-recent", recent)

	before := env.now.Add(-40 * 24 * time.Hour)
	dry, err := env.service.Cleanup(context.Background(), []string{"audit_events", "outbox_messages"}, before, true)
	if err != nil || len(dry) != 2 {
		t.Fatalf("dry=%+v err=%v", dry, err)
	}
	if dry[0].WouldDelete != 1 || dry[0].Deleted != 0 || dry[1].WouldDelete != 1 || dry[1].Deleted != 0 {
		t.Fatalf("dry items=%+v", dry)
	}
	var audits int64
	env.db.Table("audit_events").Count(&audits)
	if audits != 2 {
		t.Fatalf("dry run deleted rows: %d", audits)
	}

	real, err := env.service.Cleanup(context.Background(), []string{"audit_events", "outbox_messages"}, before, false)
	if err != nil || len(real) != 2 || real[0].Deleted != 1 || real[1].Deleted != 1 {
		t.Fatalf("real=%+v err=%v", real, err)
	}
	env.db.Table("audit_events").Count(&audits)
	if audits != 1 {
		t.Fatalf("audit rows after delete=%d", audits)
	}
	var outbox int64
	env.db.Table("outbox_messages").Count(&outbox)
	if outbox != 1 {
		t.Fatalf("outbox rows after delete=%d", outbox)
	}

	if _, err := env.service.Cleanup(context.Background(), []string{"audit_events"}, env.now.Add(-10*24*time.Hour), true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("fresh cutoff err=%v", err)
	}
	if _, err := env.service.Cleanup(context.Background(), []string{"users"}, before, true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown target err=%v", err)
	}
	if _, err := env.service.Cleanup(context.Background(), nil, before, true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty targets err=%v", err)
	}
}

func TestCreateTestOrderRequiresPaymentWiring(t *testing.T) {
	env := newDeviceEnv(t, "device-testorder")
	if _, err := env.service.CreateTestOrder(context.Background(), "channel-1", env.merchant1.ID, "alipay", 100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

type deviceEnv struct {
	db        *gorm.DB
	service   *Service
	merchant1 identitymodel.Merchant
	merchant2 identitymodel.Merchant
	now       time.Time
}

func newDeviceEnv(t *testing.T, name string) *deviceEnv {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&VoiceDevice{}, &Broadcast{}, &Printer{}, &PrintJob{},
		&identitymodel.Merchant{}, &paymentmodel.Order{}, &channelrouter.Channel{},
	); err != nil {
		t.Fatal(err)
	}
	// Cleanup touches platform tables that have no Go model of their own.
	for _, statement := range []string{
		"CREATE TABLE IF NOT EXISTS audit_events (id VARCHAR(36) PRIMARY KEY, event_type VARCHAR(100) NOT NULL, actor_type VARCHAR(50) NOT NULL, actor_id VARCHAR(36), payload TEXT NOT NULL, created_at TIMESTAMP NOT NULL)",
		"CREATE TABLE IF NOT EXISTS outbox_messages (id VARCHAR(36) PRIMARY KEY, topic VARCHAR(100) NOT NULL, aggregate_id VARCHAR(36) NOT NULL, idempotency_key VARCHAR(191) NOT NULL UNIQUE, payload TEXT NOT NULL, status VARCHAR(20) NOT NULL, attempts INTEGER NOT NULL, available_at TIMESTAMP NOT NULL, lease_until TIMESTAMP, last_error VARCHAR(500), created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	merchant1 := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant one", Status: identitymodel.StatusActive}
	merchant2 := identitymodel.Merchant{ID: "merchant-2", DisplayName: "merchant two", Status: identitymodel.StatusActive}
	for _, merchant := range []identitymodel.Merchant{merchant1, merchant2} {
		if err := db.Create(&merchant).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	return &deviceEnv{
		db: db, service: New(db, func() time.Time { return now }),
		merchant1: merchant1, merchant2: merchant2, now: now,
	}
}
