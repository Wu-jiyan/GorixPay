// Package reporting renders CSV exports, platform statistics and the merchant
// ledger statement. Exports stream page by page so a large dump never holds the
// whole result in memory, and every row is written as one complete CSV record.
package reporting

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	financemodel "gorixpay/internal/finance/model"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/transfer"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// maxExportRows bounds every export. A full-table dump would hold the request
// open for minutes, so the caller only ever sees this many rows.
var maxExportRows = 100000

// exportPageSize is how many rows one streaming page pulls from the database.
// Paging keeps the SQLite connection from being parked in one long query.
const exportPageSize = 500

const truncationNotice = "结果超过导出上限 %d 行，已截断，请缩小时间范围或增加筛选条件"

var (
	orderHeader = []string{
		"trade_no", "out_trade_no", "merchant_id", "payment_type", "kind", "name",
		"amount_yuan", "currency", "status", "buyer", "client_ip", "channel_trade_no",
		"paid_at", "expires_at", "created_at",
	}
	merchantOrderHeader = []string{
		"trade_no", "out_trade_no", "payment_type", "kind", "name", "amount_yuan",
		"currency", "status", "buyer", "client_ip", "channel_trade_no", "paid_at",
		"expires_at", "created_at",
	}
	refundHeader = []string{
		"refund_no", "merchant_id", "payment_order_id", "out_refund_no", "amount_yuan",
		"currency", "reason", "status", "rejection_reason", "processed_by", "processed_at", "created_at",
	}
	settlementHeader = []string{
		"settlement_no", "merchant_id", "out_settlement_no", "amount_yuan", "currency",
		"destination", "status", "rejection_reason", "processed_by", "processed_at", "created_at",
	}
	transferHeader = []string{
		"transfer_no", "merchant_id", "out_transfer_no", "amount_yuan", "currency",
		"account_name", "account_no", "bank_name", "branch_name", "status",
		"rejection_reason", "processed_at", "created_at",
	}
	merchantHeader = []string{
		"id", "legacy_uid", "display_name", "email", "phone", "status", "created_at",
	}
	ledgerHeader = []string{
		"created_at", "entry_id", "account_id", "owner_type", "owner_id", "purpose",
		"currency", "direction", "bucket", "amount_yuan", "balance_after_yuan",
		"reference_type", "reference_id", "description",
	}
	recordHeader = []string{
		"created_at", "type", "direction", "amount_yuan", "balance_after_yuan", "reference",
	}
	buyerHeader = []string{
		"buyer", "order_count", "paid_amount", "first_paid_at", "last_paid_at",
	}
)

func (handler *Handler) exportOrders(ctx *gin.Context) {
	filter, ok := orderFilterFromQuery(ctx, "")
	if !ok {
		return
	}
	streamQueryExport(ctx, "orders", orderHeader,
		func() *gorm.DB { return orderQuery(handler.db.WithContext(ctx.Request.Context()), filter) },
		"created_at DESC, id DESC",
		func(item paymentmodel.Order) []string {
			return []string{
				item.TradeNo, item.OutTradeNo, item.MerchantID, item.PaymentType, string(item.Kind), item.Name,
				yuan(item.Amount), item.Currency, string(item.Status), textPointer(item.Buyer), textPointer(item.ClientIP),
				textPointer(item.ChannelTradeNo), timePointer(item.PaidAt), timeValue(item.ExpiresAt), timeValue(item.CreatedAt),
			}
		})
}

func (handler *Handler) exportMerchantOrders(ctx *gin.Context) {
	filter, ok := orderFilterFromQuery(ctx, identityMerchantID(ctx))
	if !ok {
		return
	}
	streamQueryExport(ctx, "orders", merchantOrderHeader,
		func() *gorm.DB { return orderQuery(handler.db.WithContext(ctx.Request.Context()), filter) },
		"created_at DESC, id DESC",
		func(item paymentmodel.Order) []string {
			return []string{
				item.TradeNo, item.OutTradeNo, item.PaymentType, string(item.Kind), item.Name,
				yuan(item.Amount), item.Currency, string(item.Status), textPointer(item.Buyer), textPointer(item.ClientIP),
				textPointer(item.ChannelTradeNo), timePointer(item.PaidAt), timeValue(item.ExpiresAt), timeValue(item.CreatedAt),
			}
		})
}

func (handler *Handler) exportRefunds(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && !financeStatusValid(status) {
		invalidRequest(ctx)
		return
	}
	base := func() *gorm.DB {
		query := handler.db.WithContext(ctx.Request.Context()).Model(&financemodel.Refund{})
		if merchantID := strings.TrimSpace(ctx.Query("merchant_id")); merchantID != "" {
			query = query.Where("merchant_id = ?", merchantID)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
			like := "%" + keyword + "%"
			query = query.Where("refund_no LIKE ? OR out_refund_no LIKE ?", like, like)
		}
		return query
	}
	streamQueryExport(ctx, "refunds", refundHeader, base, "created_at DESC, id DESC",
		func(item financemodel.Refund) []string {
			return []string{
				item.RefundNo, item.MerchantID, item.PaymentOrderID, item.OutRefundNo, yuan(item.Amount),
				item.Currency, item.Reason, string(item.Status), textPointer(item.RejectionReason),
				textPointer(item.ProcessedBy), timePointer(item.ProcessedAt), timeValue(item.CreatedAt),
			}
		})
}

func (handler *Handler) exportSettlements(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && !financeStatusValid(status) {
		invalidRequest(ctx)
		return
	}
	base := func() *gorm.DB {
		query := handler.db.WithContext(ctx.Request.Context()).Model(&financemodel.Settlement{})
		if merchantID := strings.TrimSpace(ctx.Query("merchant_id")); merchantID != "" {
			query = query.Where("merchant_id = ?", merchantID)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
			like := "%" + keyword + "%"
			query = query.Where("settlement_no LIKE ? OR out_settlement_no LIKE ?", like, like)
		}
		return query
	}
	streamQueryExport(ctx, "settlements", settlementHeader, base, "created_at DESC, id DESC",
		func(item financemodel.Settlement) []string {
			return []string{
				item.SettlementNo, item.MerchantID, item.OutSettlementNo, yuan(item.Amount), item.Currency,
				item.Destination, string(item.Status), textPointer(item.RejectionReason),
				textPointer(item.ProcessedBy), timePointer(item.ProcessedAt), timeValue(item.CreatedAt),
			}
		})
}

func (handler *Handler) exportTransfers(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && !transfer.ValidStatus(transfer.Status(status)) {
		invalidRequest(ctx)
		return
	}
	base := func() *gorm.DB {
		query := handler.db.WithContext(ctx.Request.Context()).Model(&transfer.Order{})
		if merchantID := strings.TrimSpace(ctx.Query("merchant_id")); merchantID != "" {
			query = query.Where("merchant_id = ?", merchantID)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		return query
	}
	streamQueryExport(ctx, "transfers", transferHeader, base, "created_at DESC, id DESC",
		func(item transfer.Order) []string {
			return []string{
				item.TransferNo, item.MerchantID, item.OutTransferNo, yuan(item.Amount), item.Currency,
				item.AccountName, item.AccountNo, item.BankName, item.BranchName, string(item.Status),
				textPointer(item.RejectionReason), timePointer(item.ProcessedAt), timeValue(item.CreatedAt),
			}
		})
}

func (handler *Handler) exportMerchants(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && status != string(identitymodel.StatusActive) && status != string(identitymodel.StatusDisabled) {
		invalidRequest(ctx)
		return
	}
	base := func() *gorm.DB {
		query := handler.db.WithContext(ctx.Request.Context()).Model(&identitymodel.Merchant{})
		if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
			like := "%" + keyword + "%"
			query = query.Where("display_name LIKE ? OR email LIKE ? OR phone LIKE ?", like, like, like)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		return query
	}
	streamQueryExport(ctx, "merchants", merchantHeader, base, "created_at DESC, id DESC",
		func(item identitymodel.Merchant) []string {
			return []string{
				item.ID, intPointer(item.LegacyUID), item.DisplayName, textPointer(item.Email),
				textPointer(item.Phone), string(item.Status), timeValue(item.CreatedAt),
			}
		})
}

func (handler *Handler) exportLedger(ctx *gin.Context) {
	filter, ok := ledgerFilterFromQuery(ctx)
	if !ok {
		return
	}
	pages := func() *gorm.DB {
		return applyLedgerFilter(ledgerBase(handler.db.WithContext(ctx.Request.Context())), filter).Select(ledgerColumns)
	}
	count := func() *gorm.DB {
		return applyLedgerFilter(ledgerBase(handler.db.WithContext(ctx.Request.Context())), filter)
	}
	streamExport(ctx, "ledger", ledgerHeader, count, pages, "e.created_at DESC, e.sequence DESC",
		func(item ledgerRow) []string {
			return []string{
				timeValue(item.CreatedAt), item.EntryID, item.AccountID, item.OwnerType, item.OwnerID,
				item.Purpose, item.Currency, item.Direction, item.Bucket, yuan(item.Amount),
				yuan(item.BalanceAfter), item.ReferenceType, item.ReferenceID, item.Description,
			}
		})
}

func (handler *Handler) exportMerchantRecords(ctx *gin.Context) {
	merchantID := identityMerchantID(ctx)
	filter, ok := recordFilterFromQuery(ctx, merchantID)
	if !ok {
		return
	}
	pages := func() *gorm.DB {
		return applyLedgerFilter(ledgerBase(handler.db.WithContext(ctx.Request.Context())), filter).Select(ledgerColumns)
	}
	count := func() *gorm.DB {
		return applyLedgerFilter(ledgerBase(handler.db.WithContext(ctx.Request.Context())), filter)
	}
	streamExport(ctx, "records", recordHeader, count, pages, "e.created_at DESC, e.sequence DESC",
		func(item ledgerRow) []string {
			return []string{
				timeValue(item.CreatedAt), item.ReferenceType, item.Direction,
				yuan(item.Amount), yuan(item.BalanceAfter), item.ReferenceID,
			}
		})
}

// exportBuyers dumps the same aggregation the buyers statistics endpoint shows.
// The result set is already materialised by the grouping query, so it streams
// from memory but still respects the shared row cap.
func (handler *Handler) exportBuyers(ctx *gin.Context) {
	filter, ok := buyerFilterFromQuery(ctx)
	if !ok {
		return
	}
	items, truncated, err := loadBuyerExport(handler.db.WithContext(ctx.Request.Context()), filter, maxExportRows)
	if err != nil {
		internalError(ctx)
		return
	}
	total := int64(len(items))
	if truncated {
		total = int64(maxExportRows) + 1
	}
	writeStreamedExport(ctx, "buyers", buyerHeader, total, func(yield func([]string) bool) error {
		for _, item := range items {
			if !yield([]string{
				item.Buyer, strconv.FormatInt(item.OrderCount, 10), yuan(item.PaidAmount),
				timePointer(item.FirstPaidAt), timePointer(item.LastPaidAt),
			}) {
				break
			}
		}
		return nil
	})
}

type ledgerFilter struct {
	MerchantID    string
	AccountID     string
	OwnerType     string
	OwnerID       string
	Purpose       string
	Currency      string
	Direction     string
	ReferenceType string
	ReferenceID   string
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

type ledgerRow struct {
	EntryID       string    `gorm:"column:entry_id"`
	AccountID     string    `gorm:"column:account_id"`
	OwnerType     string    `gorm:"column:owner_type"`
	OwnerID       string    `gorm:"column:owner_id"`
	Purpose       string    `gorm:"column:purpose"`
	Currency      string    `gorm:"column:currency"`
	Direction     string    `gorm:"column:direction"`
	Bucket        string    `gorm:"column:bucket"`
	Amount        int64     `gorm:"column:amount"`
	BalanceAfter  int64     `gorm:"column:balance_after"`
	ReferenceType string    `gorm:"column:reference_type"`
	ReferenceID   string    `gorm:"column:reference_id"`
	Description   string    `gorm:"column:description"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

const ledgerColumns = "e.id AS entry_id, e.account_id AS account_id, a.owner_type AS owner_type, " +
	"a.owner_id AS owner_id, a.purpose AS purpose, a.currency AS currency, e.direction AS direction, " +
	"e.bucket AS bucket, e.amount AS amount, e.balance_after AS balance_after, t.reference_type AS reference_type, " +
	"t.reference_id AS reference_id, t.description AS description, e.created_at AS created_at"

// ledgerBase joins entries with their transaction and account so both the
// administrator export and the merchant statement share one query shape.
func ledgerBase(db *gorm.DB) *gorm.DB {
	return db.Table("ledger_entries AS e").
		Joins("JOIN ledger_transactions AS t ON t.id = e.transaction_id").
		Joins("JOIN ledger_accounts AS a ON a.id = e.account_id")
}

func applyLedgerFilter(query *gorm.DB, filter ledgerFilter) *gorm.DB {
	if filter.MerchantID != "" {
		query = query.Where("a.owner_type = ? AND a.owner_id = ? AND a.purpose = ?", "merchant", filter.MerchantID, "balance")
	}
	if filter.AccountID != "" {
		query = query.Where("e.account_id = ?", filter.AccountID)
	}
	if filter.OwnerType != "" {
		query = query.Where("a.owner_type = ?", filter.OwnerType)
	}
	if filter.OwnerID != "" {
		query = query.Where("a.owner_id = ?", filter.OwnerID)
	}
	if filter.Purpose != "" {
		query = query.Where("a.purpose = ?", filter.Purpose)
	}
	if filter.Currency != "" {
		query = query.Where("a.currency = ?", strings.ToUpper(filter.Currency))
	}
	if filter.Direction != "" {
		query = query.Where("e.direction = ?", filter.Direction)
	}
	if filter.ReferenceType != "" {
		query = query.Where("t.reference_type = ?", filter.ReferenceType)
	}
	if filter.ReferenceID != "" {
		query = query.Where("t.reference_id = ?", filter.ReferenceID)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("e.created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("e.created_at < ?", *filter.CreatedTo)
	}
	return query
}

// streamQueryExport counts the filtered rows and then streams them page by page.
// The count happens before the response body starts, because once the first byte
// is written the status code and the truncation headers can no longer change.
func streamQueryExport[T any](ctx *gin.Context, baseName string, header []string, base func() *gorm.DB, order string, build func(T) []string) {
	streamExport(ctx, baseName, header, base, base, order, build)
}

func streamExport[T any](ctx *gin.Context, baseName string, header []string, count func() *gorm.DB, pages func() *gorm.DB, order string, build func(T) []string) {
	var total int64
	if err := count().Count(&total).Error; err != nil {
		internalError(ctx)
		return
	}
	writeStreamedExport(ctx, baseName, header, total, func(yield func([]string) bool) error {
		written := 0
		offset := 0
		for written < maxExportRows {
			limit := exportPageSize
			if remaining := maxExportRows - written; remaining < limit {
				limit = remaining
			}
			var items []T
			if err := pages().Order(order).Offset(offset).Limit(limit).Find(&items).Error; err != nil {
				return err
			}
			if len(items) == 0 {
				return nil
			}
			for _, item := range items {
				if !yield(build(item)) {
					return nil
				}
			}
			written += len(items)
			offset += len(items)
			if len(items) < limit {
				return nil
			}
		}
		return nil
	})
}

// writeStreamedExport writes the BOM, the optional truncation notice, the header
// and then each emitted record straight to the response writer. A failing query
// can no longer change the status code, so it appends one complete notice row
// and records the error for the access log instead of leaving a partial line.
func writeStreamedExport(ctx *gin.Context, baseName string, header []string, total int64, emit func(yield func([]string) bool) error) {
	truncated := total > int64(maxExportRows)
	visible := total
	if visible > int64(maxExportRows) {
		visible = int64(maxExportRows)
	}
	name := baseName
	if truncated {
		name += "-truncated"
	}
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().UTC().Format("20060102-150405"))
	ctx.Header("Content-Type", "text/csv; charset=utf-8")
	ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	ctx.Header("X-Export-Truncated", strconv.FormatBool(truncated))
	ctx.Header("X-Export-Row-Limit", strconv.Itoa(maxExportRows))
	ctx.Header("X-Export-Rows", strconv.FormatInt(visible, 10))
	ctx.Writer.WriteHeader(http.StatusOK)

	buffered := bufio.NewWriterSize(ctx.Writer, 32*1024)
	if _, err := buffered.WriteString("\uFEFF"); err != nil {
		ctx.Error(err)
		return
	}
	writer := csv.NewWriter(buffered)
	write := func(record []string) bool {
		if err := writer.Write(neutralizeRow(record)); err != nil {
			ctx.Error(err)
			return false
		}
		return true
	}
	flush := func() {
		writer.Flush()
		_ = buffered.Flush()
	}
	if truncated {
		notice := make([]string, len(header))
		notice[0] = fmt.Sprintf(truncationNotice, maxExportRows)
		if !write(notice) {
			flush()
			return
		}
	}
	if !write(header) {
		flush()
		return
	}
	emitErr := emit(write)
	flush()
	if emitErr != nil {
		ctx.Error(emitErr)
		notice := make([]string, len(header))
		notice[0] = fmt.Sprintf("导出中断：%v", emitErr)
		_ = writer.Write(neutralizeRow(notice))
		flush()
		return
	}
	if err := writer.Error(); err != nil {
		ctx.Error(err)
	}
}

func trimExport[T any](items []T) ([]T, bool) {
	if len(items) <= maxExportRows {
		return items, false
	}
	return items[:maxExportRows], true
}

// writeExport streams an already materialised row set. It stays for the merchant
// exports whose rows are built in memory, and keeps the exact headers, BOM and
// truncation behaviour of the query based path.
func writeExport(ctx *gin.Context, baseName string, header []string, rows [][]string, truncated bool) {
	total := int64(len(rows))
	if truncated {
		total = int64(maxExportRows) + 1
	}
	writeStreamedExport(ctx, baseName, header, total, func(yield func([]string) bool) error {
		for _, row := range rows {
			if !yield(row) {
				break
			}
		}
		return nil
	})
}

// neutralizeRow defuses spreadsheet formula injection. A merchant controlled
// field like =HYPERLINK(...) or +cmd|... would otherwise execute when the
// administrator opens the export in Excel or WPS.
func neutralizeRow(row []string) []string {
	for index, value := range row {
		row[index] = neutralizeCell(value)
	}
	return row
}

func neutralizeCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

func orderQuery(db *gorm.DB, filter orderFilter) *gorm.DB {
	query := db.Model(&paymentmodel.Order{})
	if filter.MerchantID != "" {
		query = query.Where("merchant_id = ?", filter.MerchantID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.PaymentType != "" {
		query = query.Where("payment_type = ?", filter.PaymentType)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("trade_no LIKE ? OR out_trade_no LIKE ? OR name LIKE ?", like, like, like)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at < ?", *filter.CreatedTo)
	}
	return query
}

func financeStatusValid(status string) bool {
	return status == string(financemodel.StatusPending) ||
		status == string(financemodel.StatusApproved) ||
		status == string(financemodel.StatusRejected)
}

func yuan(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func timeValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func timePointer(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func textPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intPointer(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
