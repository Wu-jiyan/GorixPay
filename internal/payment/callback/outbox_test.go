package callback

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/outbox"
)

func TestCompleteWritesMerchantNotificationOutboxAtomically(t *testing.T) {
	db, order := callbackDatabase(t, "callback-outbox")
	if err := db.AutoMigrate(&outbox.Message{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service := New(db, func() time.Time { return now })
	if _, applied, err := service.Complete(context.Background(), Completion{
		OrderID: order.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: order.Amount, Digest: "digest-outbox",
	}); err != nil || !applied {
		t.Fatalf("complete: applied=%v err=%v", applied, err)
	}
	var message outbox.Message
	if err := db.First(&message, "aggregate_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if message.Topic != "merchant.payment.notify" || message.Status != outbox.StatusPending ||
		message.IdempotencyKey != "merchant-payment-notify:"+order.ID || !strings.Contains(message.Payload, order.TradeNo) {
		t.Fatalf("message=%+v", message)
	}
	if _, applied, err := service.Complete(context.Background(), Completion{
		OrderID: order.ID, ChannelID: "channel-1", ChannelTradeNo: "CHANNEL-1", Amount: order.Amount, Digest: "digest-outbox",
	}); err != nil || applied {
		t.Fatalf("duplicate: applied=%v err=%v", applied, err)
	}
	var count int64
	if err := db.Model(&outbox.Message{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("outbox count=%d err=%v", count, err)
	}
}
