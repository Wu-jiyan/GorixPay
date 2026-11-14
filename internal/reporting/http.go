package reporting

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultDailyDays = 14
	maxDailyDays     = 90
	defaultPageSize  = 20
	maxPageSize      = 100
)

// Page is the shared pagination input for the reporting endpoints.
type Page struct {
	Number int
	Size   int
}

// FeeResolver returns a merchant's fee rate in basis points. It is wired to the
// merchant group service; a missing rate means "no fee", never an error.
type FeeResolver func(ctx context.Context, merchantID string) (int, bool)

type Handler struct {
	db   *gorm.DB
	now  func() time.Time
	fees FeeResolver
}

// Register mounts the reporting routes. The caller passes the authenticated
// /api/v1 group, the session middleware (Bearer or session cookie) and the fee
// resolver used for the merchant fee estimate.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, feeResolver FeeResolver) {
	if group == nil || db == nil || sessions == nil {
		return
	}
	handler := &Handler{db: db, now: time.Now, fees: feeResolver}
	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	merchant := group.Group("/merchant", sessions, requireMerchant)

	admin.GET("/export/orders.csv", handler.exportOrders)
	admin.GET("/export/refunds.csv", handler.exportRefunds)
	admin.GET("/export/settlements.csv", handler.exportSettlements)
	admin.GET("/export/transfers.csv", handler.exportTransfers)
	admin.GET("/export/merchants.csv", handler.exportMerchants)
	admin.GET("/export/ledger.csv", handler.exportLedger)
	admin.GET("/export/buyers.csv", handler.exportBuyers)
	admin.GET("/stats/buyers", handler.statsBuyers)
	admin.GET("/stats/merchants", handler.statsMerchants)

	merchant.GET("/export/orders.csv", handler.exportMerchantOrders)
	merchant.GET("/export/records.csv", handler.exportMerchantRecords)
	merchant.GET("/stats/daily", handler.statsMerchantDaily)
	merchant.GET("/records", handler.merchantRecords)
}

func requireMerchant(ctx *gin.Context) {
	if identityhttp.MustSession(ctx).PrincipalType != identitymodel.PrincipalMerchant {
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "merchant access required"})
		return
	}
	ctx.Next()
}

// identityMerchantID is the only source of the merchant identity on these
// routes; a merchant_id query parameter is ignored on purpose.
func identityMerchantID(ctx *gin.Context) string {
	return identityhttp.MustSession(ctx).PrincipalID
}

func (handler *Handler) statsBuyers(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	filter, ok := buyerFilterFromQuery(ctx)
	if !ok {
		return
	}
	items, total, err := loadBuyerStats(handler.db.WithContext(ctx.Request.Context()), filter, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) statsMerchants(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	filter, ok := merchantFilterFromQuery(ctx)
	if !ok {
		return
	}
	rateOverride, ok := rateBpFromQuery(ctx)
	if !ok {
		return
	}
	items, total, err := loadMerchantStats(ctx.Request.Context(), handler.db.WithContext(ctx.Request.Context()), filter, handler.fees, rateOverride, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	payload := gin.H{"items": items, "total": total, "page": page, "page_size": size}
	if rateOverride != nil {
		payload["rate_bp"] = *rateOverride
	}
	ctx.JSON(http.StatusOK, payload)
}

func (handler *Handler) statsMerchantDaily(ctx *gin.Context) {
	days, ok := daysFromQuery(ctx)
	if !ok {
		return
	}
	items, err := loadMerchantDaily(handler.db.WithContext(ctx.Request.Context()), identityMerchantID(ctx), days, handler.now())
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "days": days})
}

// merchantRecords renders one page of the calling merchant's own ledger
// statement. The account filter is pinned to owner_type=merchant, purpose=balance
// and the session merchant id.
func (handler *Handler) merchantRecords(ctx *gin.Context) {
	merchantID := identityMerchantID(ctx)
	filter, ok := recordFilterFromQuery(ctx, merchantID)
	if !ok {
		return
	}
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	db := handler.db.WithContext(ctx.Request.Context())
	total := int64(0)
	if err := applyLedgerFilter(ledgerBase(db), filter).Count(&total).Error; err != nil {
		internalError(ctx)
		return
	}
	var rows []ledgerRow
	if err := applyLedgerFilter(ledgerBase(db), filter).
		Select(ledgerColumns).
		Order("e.created_at DESC, e.sequence DESC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		internalError(ctx)
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, gin.H{
			"created_at":    row.CreatedAt.UTC(),
			"type":          row.ReferenceType,
			"direction":     row.Direction,
			"amount":        row.Amount,
			"balance_after": row.BalanceAfter,
			"reference":     row.ReferenceID,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

type orderFilter struct {
	MerchantID  string
	Status      paymentmodel.Status
	Kind        paymentmodel.Kind
	PaymentType string
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

func orderFilterFromQuery(ctx *gin.Context, forcedMerchantID string) (orderFilter, bool) {
	filter := orderFilter{
		MerchantID:  forcedMerchantID,
		PaymentType: strings.TrimSpace(ctx.Query("payment_type")),
		Keyword:     strings.TrimSpace(ctx.Query("keyword")),
	}
	if filter.MerchantID == "" {
		filter.MerchantID = strings.TrimSpace(ctx.Query("merchant_id"))
	}
	if raw := strings.TrimSpace(ctx.Query("status")); raw != "" {
		status := paymentmodel.Status(raw)
		if !validOrderStatus(status) {
			invalidRequest(ctx)
			return orderFilter{}, false
		}
		filter.Status = status
	}
	if raw := strings.TrimSpace(ctx.Query("kind")); raw != "" {
		kind := paymentmodel.Kind(raw)
		if kind != paymentmodel.KindSale && kind != paymentmodel.KindRecharge && kind != paymentmodel.KindDeposit {
			invalidRequest(ctx)
			return orderFilter{}, false
		}
		filter.Kind = kind
	}
	from, to, ok := timeRangeFromQuery(ctx)
	if !ok {
		return orderFilter{}, false
	}
	filter.CreatedFrom = from
	filter.CreatedTo = to
	return filter, true
}

func buyerFilterFromQuery(ctx *gin.Context) (buyerFilter, bool) {
	filter := buyerFilter{
		MerchantID: strings.TrimSpace(ctx.Query("merchant_id")),
		Keyword:    strings.TrimSpace(ctx.Query("keyword")),
	}
	from, to, ok := timeRangeFromQuery(ctx)
	if !ok {
		return buyerFilter{}, false
	}
	filter.CreatedFrom = from
	filter.CreatedTo = to
	return filter, true
}

func merchantFilterFromQuery(ctx *gin.Context) (merchantFilter, bool) {
	filter := merchantFilter{Keyword: strings.TrimSpace(ctx.Query("keyword"))}
	from, to, ok := timeRangeFromQuery(ctx)
	if !ok {
		return merchantFilter{}, false
	}
	filter.CreatedFrom = from
	filter.CreatedTo = to
	return filter, true
}

func recordFilterFromQuery(ctx *gin.Context, merchantID string) (ledgerFilter, bool) {
	filter := ledgerFilter{
		MerchantID:    merchantID,
		Direction:     strings.TrimSpace(ctx.Query("direction")),
		ReferenceType: strings.TrimSpace(ctx.Query("type")),
	}
	if filter.Direction != "" && filter.Direction != "debit" && filter.Direction != "credit" {
		invalidRequest(ctx)
		return ledgerFilter{}, false
	}
	from, to, ok := timeRangeFromQuery(ctx)
	if !ok {
		return ledgerFilter{}, false
	}
	filter.CreatedFrom = from
	filter.CreatedTo = to
	return filter, true
}

func ledgerFilterFromQuery(ctx *gin.Context) (ledgerFilter, bool) {
	filter := ledgerFilter{
		AccountID:     strings.TrimSpace(ctx.Query("account_id")),
		OwnerType:     strings.TrimSpace(ctx.Query("owner_type")),
		OwnerID:       strings.TrimSpace(ctx.Query("owner_id")),
		Purpose:       strings.TrimSpace(ctx.Query("purpose")),
		Currency:      strings.TrimSpace(ctx.Query("currency")),
		Direction:     strings.TrimSpace(ctx.Query("direction")),
		ReferenceType: strings.TrimSpace(ctx.Query("reference_type")),
		ReferenceID:   strings.TrimSpace(ctx.Query("reference_id")),
	}
	if filter.Direction != "" && filter.Direction != "debit" && filter.Direction != "credit" {
		invalidRequest(ctx)
		return ledgerFilter{}, false
	}
	from, to, ok := timeRangeFromQuery(ctx)
	if !ok {
		return ledgerFilter{}, false
	}
	filter.CreatedFrom = from
	filter.CreatedTo = to
	return filter, true
}

func timeRangeFromQuery(ctx *gin.Context) (*time.Time, *time.Time, bool) {
	var from, to *time.Time
	if raw := strings.TrimSpace(ctx.Query("created_from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			invalidRequest(ctx)
			return nil, nil, false
		}
		utc := parsed.UTC()
		from = &utc
	}
	if raw := strings.TrimSpace(ctx.Query("created_to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			invalidRequest(ctx)
			return nil, nil, false
		}
		utc := parsed.UTC()
		to = &utc
	}
	if from != nil && to != nil && !from.Before(*to) {
		invalidRequest(ctx)
		return nil, nil, false
	}
	return from, to, true
}

func daysFromQuery(ctx *gin.Context) (int, bool) {
	raw := strings.TrimSpace(ctx.Query("days"))
	if raw == "" {
		return defaultDailyDays, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > maxDailyDays {
		invalidRequest(ctx)
		return 0, false
	}
	return parsed, true
}

// rateBpFromQuery reads the optional fee override. Nil means "no override", so
// the per-merchant resolver decides; a present value wins for every row.
func rateBpFromQuery(ctx *gin.Context) (*int, bool) {
	raw := strings.TrimSpace(ctx.Query("rate_bp"))
	if raw == "" {
		return nil, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 || parsed > 10000 {
		invalidRequest(ctx)
		return nil, false
	}
	return &parsed, true
}

func pagination(ctx *gin.Context) (int, int, bool) {
	page, err := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		invalidRequest(ctx)
		return 0, 0, false
	}
	size, err := strconv.Atoi(ctx.DefaultQuery("page_size", strconv.Itoa(defaultPageSize)))
	if err != nil || size < 1 || size > maxPageSize {
		invalidRequest(ctx)
		return 0, 0, false
	}
	return page, size, true
}

func validOrderStatus(status paymentmodel.Status) bool {
	switch status {
	case paymentmodel.StatusPending, paymentmodel.StatusProcessing, paymentmodel.StatusPaid,
		paymentmodel.StatusClosed, paymentmodel.StatusFailed:
		return true
	default:
		return false
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
