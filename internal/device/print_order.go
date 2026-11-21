package device

import (
	"context"
	"errors"
	"fmt"
	"strings"

	paymentmodel "gorixpay/internal/payment/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrNoPrinter means the merchant has no active printer to send the receipt to.
var ErrNoPrinter = errors.New("merchant has no active printer")

// ErrOrderNotPrintable means the order is not something a receipt should exist
// for yet (only paid orders print).
var ErrOrderNotPrintable = errors.New("order is not printable")

// PrintOrder queues a receipt for one of the merchant's own orders.
//
// Upstream Epay offers the same button on the merchant order list
// (user/ajax2.php printOrder). The receipt goes to the merchant's first active
// printer, which can be any agent that speaks the existing printer poll protocol
// (POST /api/v1/device/printer/poll with printer_no + secret) — nothing here
// depends on a specific cloud-printer vendor.
func (service *Service) PrintOrder(ctx context.Context, merchantID, orderID string) (PrintJob, error) {
	if strings.TrimSpace(merchantID) == "" {
		return PrintJob{}, ErrNotFound
	}
	var order paymentmodel.Order
	if err := service.db.WithContext(ctx).First(&order, "id = ? AND merchant_id = ?", orderID, merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PrintJob{}, ErrNotFound
		}
		return PrintJob{}, err
	}
	if order.Status != paymentmodel.StatusPaid {
		return PrintJob{}, ErrOrderNotPrintable
	}
	var printer Printer
	err := service.db.WithContext(ctx).
		Where("merchant_id = ? AND status = ?", merchantID, StatusActive).
		Order("created_at ASC").First(&printer).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PrintJob{}, ErrNoPrinter
		}
		return PrintJob{}, err
	}
	orderIDCopy := order.ID
	job := PrintJob{
		ID: uuid.NewString(), PrinterID: printer.ID, OrderID: &orderIDCopy,
		Content: receiptText(order), Status: JobPending, CreatedAt: service.now().UTC(),
	}
	if err := service.db.WithContext(ctx).Create(&job).Error; err != nil {
		return PrintJob{}, err
	}
	return job, nil
}

// receiptText renders the plain-text receipt a 58mm thermal printer prints.
func receiptText(order paymentmodel.Order) string {
	paidAt := "-"
	if order.PaidAt != nil {
		paidAt = order.PaidAt.UTC().Format("2006-01-02 15:04:05")
	}
	var builder strings.Builder
	builder.WriteString("        GorixPay 收款凭证\n")
	builder.WriteString("--------------------------------\n")
	fmt.Fprintf(&builder, "订单号  : %s\n", order.TradeNo)
	fmt.Fprintf(&builder, "商户单号: %s\n", order.OutTradeNo)
	fmt.Fprintf(&builder, "商品    : %s\n", order.Name)
	fmt.Fprintf(&builder, "金额    : ￥%d.%02d\n", order.Amount/100, order.Amount%100)
	fmt.Fprintf(&builder, "支付方式: %s\n", order.PaymentType)
	fmt.Fprintf(&builder, "支付时间: %s\n", paidAt)
	builder.WriteString("--------------------------------\n")
	builder.WriteString("        谢谢惠顾\n")
	return builder.String()
}
