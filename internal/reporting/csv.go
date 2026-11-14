// Package reporting renders CSV exports, platform statistics and the merchant
// ledger statement. Every export shares one row cap and one encoding: UTF-8 with
// a BOM so Excel on Windows opens the file without mojibake.
package reporting

import (
	"bytes"
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
// open for minutes and blow up memory, so the query asks for one extra row and
// reports truncation when it comes back.
var maxExportRows = 100000

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
	var items []paymentmodel.Order
	if err := orderQuery(handler.db.WithContext(ctx.Request.Context()), filter).
		Order("created_at DESC, id DESC").Limit(maxExportRows + 1).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	items, truncated := trimExport(items)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.TradeNo, item.OutTradeNo, item.MerchantID, item.PaymentType, string(item.Kind), item.Name,
			yuan(item.Amount), item.Currency, string(item.Status), textPointer(item.Buyer), textPointer(item.ClientIP),
			textPointer(item.ChannelTradeNo), timePointer(item.PaidAt), timeValue(item.ExpiresAt), timeValue(item.CreatedAt),
		})
	}
	writeExport(ctx, "orders", orderHeader, rows, truncated)
}

func (handler *Handler) exportMerchantOrders(ctx *gin.Context) {
	filter, ok := orderFilterFromQuery(ctx, identityMerchantID(ctx))
	if !ok {
		return
	}
	var items []paymentmodel.Order
	if err := orderQuery(handler.db.WithContext(ctx.Request.Context()), filter).
		Order("created_at DESC, id DESC").Limit(maxExportRows + 1).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	items, truncated := trimExport(items)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.TradeNo, item.OutTradeNo, item.PaymentType, string(item.Kind), item.Name,
			yuan(item.Amount), item.Currency, string(item.Status), textPointer(item.Buyer), textPointer(item.ClientIP),
			textPointer(item.ChannelTradeNo), timePointer(item.PaidAt), timeValue(item.ExpiresAt), timeValue(item.CreatedAt),
		})
	}
	writeExport(ctx, "orders", merchantOrderHeader, rows, truncated)
}

func (handler *Handler) exportRefunds(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && !financeStatusValid(status) {
		invalidRequest(ctx)
		return
	}
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
	var items []financemodel.Refund
	if err := query.Order("created_at DESC, id DESC").Limit(maxExportRows + 1).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	items, truncated := trimExport(items)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.RefundNo, item.MerchantID, item.PaymentOrderID, item.OutRefundNo, yuan(item.Amount),
			item.Currency, item.Reason, string(item.Status), textPointer(item.RejectionReason),
			textPointer(item.ProcessedBy), timePointer(item.ProcessedAt), timeValue(item.CreatedAt),
		})
	}
	writeExport(ctx, "refunds", refundHeader, rows, truncated)
}

func (handler *Handler) exportSettlements(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && !financeStatusValid(status) {
		invalidRequest(ctx)
		return
	}
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
	var items []financemodel.Settlement
	if err := query.Order("created_at DESC, id DESC").Limit(maxExportRows + 1).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	items, truncated := trimExport(items)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.SettlementNo, item.MerchantID, item.OutSettlementNo, yuan(item.Amount), item.Currency,
			item.Destination, string(item.Status), textPointer(item.RejectionReason),
			textPointer(item.ProcessedBy), timePointer(item.ProcessedAt), timeValue(item.CreatedAt),
		})
	}
	writeExport(ctx, "settlements", settlementHeader, rows, truncated)
}

func (handler *Handler) exportTransfers(ctx *gin.Context) {
	query := handler.db.WithContext(ctx.Request.Context()).Model(&transfer.Order{})
	if merchantID := strings.TrimSpace(ctx.Query("merchant_id")); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if status := strings.TrimSpace(ctx.Query("status")); status != "" {
		if !transfer.ValidStatus(transfer.Status(status)) {
			invalidRequest(ctx)
			return
		}
		query = query.Where("status = ?", status)
	}
	var items []transfer.Order
	if err := query.Order("created_at DESC, id DESC").Limit(maxExportRows + 1).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	items, truncated := trimExport(items)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.TransferNo, item.MerchantID, item.OutTransferNo, yuan(item.Amount), item.Currency,
			item.AccountName, item.AccountNo, item.BankName, item.BranchName, string(item.Status),
			textPointer(item.RejectionReason), timePointer(item.ProcessedAt), timeValue(item.CreatedAt),
		})
	}
	writeExport(ctx, "transfers", transferHeader, rows, truncated)
}

func (handler *Handler) exportMerchants(ctx *gin.Context) {
	query := handler.db.WithContext(ctx.Request.Context()).Model(&identitymodel.Merchant{})
	if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("display_name LIKE ? OR email LIKE ? OR phone LIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(ctx.Query("status")); status != "" {
		if status != string(identitymodel.StatusActive) && status != string(identitymodel.StatusDisabled) {
			invalidRequest(ctx)
			return
		}
		query = query.Where("status = ?", status)
	}
	var items []identitymodel.Merchant
	if err := query.Order("created_at DESC, id DESC").Limit(maxExportRows + 1).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	items, truncated := trimExport(items)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.ID, intPointer(item.LegacyUID), item.DisplayName, textPointer(item.Email),
			textPointer(item.Phone), string(item.Status), timeValue(item.CreatedAt),
		})
	}
	writeExport(ctx, "merchants", merchantHeader, rows, truncated)
}

func (handler *Handler) exportLedger(ctx *gin.Context) {
	filter, ok := ledgerFilterFromQuery(ctx)
	if !ok {
		return
	}
	items, truncated, err := loadLedgerRows(handler.db.WithContext(ctx.Request.Context()), filter, maxExportRows)
	if err != nil {
		internalError(ctx)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			timeValue(item.CreatedAt), item.EntryID, item.AccountID, item.OwnerType, item.OwnerID,
			item.Purpose, item.Currency, item.Direction, item.Bucket, yuan(item.Amount),
			yuan(item.BalanceAfter), item.ReferenceType, item.ReferenceID, item.Description,
		})
	}
	writeExport(ctx, "ledger", ledgerHeader, rows, truncated)
}

func (handler *Handler) exportMerchantRecords(ctx *gin.Context) {
	merchantID := identityMerchantID(ctx)
	filter, ok := recordFilterFromQuery(ctx, merchantID)
	if !ok {
		return
	}
	items, truncated, err := loadLedgerRows(handler.db.WithContext(ctx.Request.Context()), filter, maxExportRows)
	if err != nil {
		internalError(ctx)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			timeValue(item.CreatedAt), item.ReferenceType, item.Direction,
			yuan(item.Amount), yuan(item.BalanceAfter), item.ReferenceID,
		})
	}
	writeExport(ctx, "records", recordHeader, rows, truncated)
}

// exportBuyers dumps the same aggregation the buyers statistics endpoint shows,
// without pagination but still bounded by the shared row cap.
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
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.Buyer, strconv.FormatInt(item.OrderCount, 10), yuan(item.PaidAmount),
			timePointer(item.FirstPaidAt), timePointer(item.LastPaidAt),
		})
	}
	writeExport(ctx, "buyers", buyerHeader, rows, truncated)
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

func loadLedgerRows(db *gorm.DB, filter ledgerFilter, limit int) ([]ledgerRow, bool, error) {
	var items []ledgerRow
	if err := applyLedgerFilter(ledgerBase(db), filter).
		Select(ledgerColumns).
		Order("e.created_at DESC, e.sequence DESC").
		Limit(limit + 1).Find(&items).Error; err != nil {
		return nil, false, err
	}
	items, truncated := trimExport(items)
	return items, truncated, nil
}

func writeExport(ctx *gin.Context, baseName string, header []string, rows [][]string, truncated bool) {
	name := baseName
	if truncated {
		name += "-truncated"
	}
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().UTC().Format("20060102-150405"))
	var buffer bytes.Buffer
	buffer.WriteString("\uFEFF")
	writer := csv.NewWriter(&buffer)
	if truncated {
		// The notice keeps the full column width so strict CSV readers (Go,
		// Python csv, PostgreSQL COPY) do not reject the ragged first row.
		notice := make([]string, len(header))
		notice[0] = fmt.Sprintf(truncationNotice, maxExportRows)
		if err := writer.Write(neutralizeRow(notice)); err != nil {
			internalError(ctx)
			return
		}
	}
	if err := writer.Write(neutralizeRow(header)); err != nil {
		internalError(ctx)
		return
	}
	for index := range rows {
		rows[index] = neutralizeRow(rows[index])
	}
	if err := writer.WriteAll(rows); err != nil {
		internalError(ctx)
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		internalError(ctx)
		return
	}
	ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	ctx.Header("X-Export-Truncated", strconv.FormatBool(truncated))
	ctx.Header("X-Export-Row-Limit", strconv.Itoa(maxExportRows))
	ctx.Header("X-Export-Rows", strconv.Itoa(len(rows)))
	ctx.Data(http.StatusOK, "text/csv; charset=utf-8", buffer.Bytes())
}

func trimExport[T any](items []T) ([]T, bool) {
	if len(items) <= maxExportRows {
		return items, false
	}
	return items[:maxExportRows], true
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
