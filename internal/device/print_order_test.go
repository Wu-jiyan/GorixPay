package device

import (
	"context"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func printFixture(t *testing.T) (*Service, *gorm.DB, paymentmodel.Order) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Merchant{}, &paymentmodel.Order{}, &Printer{}, &PrintJob{}, &VoiceDevice{}, &Broadcast{}); err != nil {
		t.Fatal(err)
	}
	service := New(db, time.Now)
	// CreatePrinter validates that the owning merchant exists.
	if err := db.Create(&identitymodel.Merchant{
		ID: "merchant-1", DisplayName: "打印商户", Status: identitymodel.StatusActive,
	}).Error; err != nil {
		t.Fatal(err)
	}
	paidAt := time.Now().UTC()
	order := paymentmodel.Order{
		ID: "order-print-1", TradeNo: "T-PRINT-1", OutTradeNo: "OUT-PRINT-1",
		MerchantID: "merchant-1", PaymentType: "alipay", Name: "打印测试", Amount: 1250,
		Currency: "CNY", Status: paymentmodel.StatusPaid, NotifyURL: "https://m.example/n",
		PaidAt: &paidAt, ExpiresAt: paidAt.Add(time.Hour), CreatedAt: paidAt, UpdatedAt: paidAt,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	return service, db, order
}

// TestPrintOrderQueuesReceiptForOwnPrinter pins the merchant flow: the receipt is
// queued on the merchant's own active printer, carries the order reference, and
// the text holds the fields a thermal receipt needs.
func TestPrintOrderQueuesReceiptForOwnPrinter(t *testing.T) {
	service, db, order := printFixture(t)
	printer, _, err := service.CreatePrinter(context.Background(), PrinterInput{
		MerchantID: order.MerchantID, Name: "前台打印机", Status: string(StatusActive),
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.PrintOrder(context.Background(), order.MerchantID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.PrinterID != printer.ID {
		t.Fatalf("printer=%q want %q", job.PrinterID, printer.ID)
	}
	if job.OrderID == nil || *job.OrderID != order.ID {
		t.Fatalf("order id=%v", job.OrderID)
	}
	for _, want := range []string{order.TradeNo, order.OutTradeNo, "12.50", "alipay"} {
		if !strings.Contains(job.Content, want) {
			t.Fatalf("receipt=%q 缺少 %q", job.Content, want)
		}
	}
	var stored PrintJob
	if err := db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != JobPending {
		t.Fatalf("status=%q want pending", stored.Status)
	}
}

// TestPrintOrderGuards keeps the three refusals honest: someone else's order, an
// unpaid order, and a merchant without a printer each answer with a typed error
// instead of a queued job nobody asked for.
func TestPrintOrderGuards(t *testing.T) {
	service, _, order := printFixture(t)
	ctx := context.Background()

	if _, err := service.PrintOrder(ctx, "merchant-1", order.ID); err != ErrNoPrinter {
		t.Fatalf("no printer err=%v", err)
	}
	if _, err := service.PrintOrder(ctx, "other-merchant", order.ID); err != ErrNotFound {
		t.Fatalf("foreign order err=%v", err)
	}
	if _, _, err := service.CreatePrinter(ctx, PrinterInput{
		MerchantID: order.MerchantID, Name: "前台", Status: string(StatusActive),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PrintOrder(ctx, "merchant-1", "missing-order"); err != ErrNotFound {
		t.Fatalf("missing order err=%v", err)
	}
}
