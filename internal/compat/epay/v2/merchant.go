package v2

import (
	"context"
	"strconv"
	"strings"
	"time"

	financemodel "gorixpay/internal/finance/model"
	ledgermodel "gorixpay/internal/ledger/model"
	paymentmodel "gorixpay/internal/payment/model"

	"gorm.io/gorm"
)

// merchantInfo mirrors the reference Merchant::info response with a V2 envelope:
// the money field is the merchant ledger balance in yuan.
func (service *Service) merchantInfo(req *request) {
	ctx := req.context()
	var total, today, lastday int64
	db := service.deps.DB.WithContext(ctx)
	base := db.Model(&paymentmodel.Order{}).Where("merchant_id = ?", req.merchant.ID)
	if err := base.Count(&total).Error; err != nil {
		service.writeError(req.ctx, -1, "账户信息查询失败")
		return
	}
	now := service.now().UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startOfLastday := startOfToday.Add(-24 * time.Hour)
	paidTodayQuery := func() *gorm.DB {
		return db.Model(&paymentmodel.Order{}).
			Where("merchant_id = ? AND status = ? AND paid_at >= ?", req.merchant.ID, paymentmodel.StatusPaid, startOfToday)
	}
	paidLastdayQuery := func() *gorm.DB {
		return db.Model(&paymentmodel.Order{}).
			Where("merchant_id = ? AND status = ? AND paid_at >= ? AND paid_at < ?", req.merchant.ID, paymentmodel.StatusPaid, startOfLastday, startOfToday)
	}
	if err := paidTodayQuery().Count(&today).Error; err != nil {
		service.writeError(req.ctx, -1, "账户信息查询失败")
		return
	}
	if err := paidLastdayQuery().Count(&lastday).Error; err != nil {
		service.writeError(req.ctx, -1, "账户信息查询失败")
		return
	}
	available, frozen := service.merchantBalance(ctx, req.merchant.ID)
	var todayAmount, lastdayAmount int64
	if err := paidTodayQuery().Select("COALESCE(SUM(amount), 0)").Scan(&todayAmount).Error; err != nil {
		service.writeError(req.ctx, -1, "账户信息查询失败")
		return
	}
	if err := paidLastdayQuery().Select("COALESCE(SUM(amount), 0)").Scan(&lastdayAmount).Error; err != nil {
		service.writeError(req.ctx, -1, "账户信息查询失败")
		return
	}
	payStatus := 0
	if string(req.merchant.Status) == "active" {
		payStatus = 1
	}
	// Field names follow Merchant.php:20 exactly: order_num / order_num_today /
	// order_num_lastday / order_money_today / order_money_lastday / pay_status.
	// The orders* aliases stay for our own console. Upstream runs array_filter on
	// the array, so fields it cannot fill are simply absent rather than empty.
	payload := map[string]any{
		"code":                0,
		"pid":                 strconv.FormatInt(req.pid, 10),
		"status":              string(req.merchant.Status),
		"pay_status":          payStatus,
		"money":               FormatAmount(available),
		"frozen_money":        FormatAmount(frozen),
		"order_num":           total,
		"order_num_today":     today,
		"order_num_lastday":   lastday,
		"order_money_today":   FormatAmount(todayAmount),
		"order_money_lastday": FormatAmount(lastdayAmount),
		"orders":              total,
		"orders_today":        today,
		"orders_lastday":      lastday,
	}
	// Merchant.php:20 also publishes the settlement destination; array_filter
	// removes it while the merchant has not configured one.
	if req.merchant.SettleType != 0 {
		payload["settle_type"] = req.merchant.SettleType
	}
	if account := stringValue(req.merchant.SettleAccount); account != "" {
		payload["settle_account"] = account
	}
	if req.merchant.DisplayName != "" {
		payload["settle_name"] = req.merchant.DisplayName
	}
	service.writeJSON(req.ctx, payload)
}

// merchantOrders lists orders with pagination (limit is capped at 50).
func (service *Service) merchantOrders(req *request) {
	limit, apiErr := parseLimit(req)
	if apiErr != nil {
		service.writeError(req.ctx, apiErr.code, apiErr.msg)
		return
	}
	offset, err := strconv.Atoi(firstNonEmpty(req.value("offset"), "0"))
	if err != nil || offset < 0 {
		service.writeError(req.ctx, -4, "分页参数不正确")
		return
	}
	query := service.deps.DB.WithContext(req.context()).Model(&paymentmodel.Order{}).Where("merchant_id = ?", req.merchant.ID)
	// Merchant.php:31-38 detects the presence of status with isset($queryArr['status'])
	// (the signed POST payload) but then reads the *value* from $_GET. A status sent
	// only in the POST body therefore filters on 0 ("unpaid"), exactly like the
	// reference implementation. Pinned by TestV2MerchantOrdersStatusQuirk.
	if _, present := req.values["status"]; present {
		switch phpIntval(req.ctx.Query("status")) {
		case 0:
			query = query.Where("status IN ?", []paymentmodel.Status{paymentmodel.StatusPending, paymentmodel.StatusProcessing})
		case 1:
			query = query.Where("status = ?", paymentmodel.StatusPaid)
		case 2:
			query = query.Where("status IN ?", []paymentmodel.Status{paymentmodel.StatusClosed, paymentmodel.StatusFailed})
		default:
			// intval() never fails upstream, so an unmapped status simply matches
			// nothing instead of returning an error.
			query = query.Where("1 = 0")
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		service.writeError(req.ctx, -1, "查询订单记录失败！")
		return
	}
	var orders []paymentmodel.Order
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error; err != nil {
		service.writeError(req.ctx, -1, "查询订单记录失败！")
		return
	}
	data := make([]map[string]any, 0, len(orders))
	refunded := service.refundedAmounts(req.context(), orders)
	for _, order := range orders {
		item := map[string]any{
			"trade_no":     order.TradeNo,
			"out_trade_no": order.OutTradeNo,
			"type":         order.PaymentType,
			"pid":          strconv.FormatInt(req.pid, 10),
			"addtime":      order.CreatedAt.Format("2006-01-02 15:04:05"),
			"endtime":      formatOptionalTime(order.PaidAt),
			"name":         order.Name,
			"money":        FormatAmount(order.Amount),
			"param":        stringValue(order.Attach),
			"buyer":        "",
			"clientip":     stringValue(order.ClientIP),
			"status":       epayOrderStatus(order.Status),
			"refundmoney":  FormatAmount(refunded[order.ID]),
		}
		if order.ChannelTradeNo != nil {
			item["api_trade_no"] = *order.ChannelTradeNo
		}
		data = append(data, item)
	}
	service.writeJSON(req.ctx, map[string]any{"code": 0, "count": total, "data": data})
}

// refundedAmounts sums the approved refunds per order for the current page, so
// merchant/orders can publish the refundmoney field Merchant.php:47 returns.
func (service *Service) refundedAmounts(ctx context.Context, orders []paymentmodel.Order) map[string]int64 {
	result := make(map[string]int64, len(orders))
	if len(orders) == 0 {
		return result
	}
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
	}
	type row struct {
		PaymentOrderID string
		Total          int64
	}
	var rows []row
	if err := service.deps.DB.WithContext(ctx).
		Model(&financemodel.Refund{}).
		Select("payment_order_id, COALESCE(SUM(amount), 0) AS total").
		Where("payment_order_id IN ? AND status = ?", ids, financemodel.StatusApproved).
		Group("payment_order_id").
		Scan(&rows).Error; err != nil {
		return result
	}
	for _, item := range rows {
		result[item.PaymentOrderID] = item.Total
	}
	return result
}

func parseLimit(req *request) (int, *apiError) {
	raw := req.value("limit")
	if raw == "" {
		return 10, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, errorf(-4, "分页参数不正确")
	}
	if limit > 50 {
		limit = 50
	}
	return limit, nil
}

// phpIntval reproduces the lenient PHP intval() cast the reference implementation
// relies on: leading digits are parsed, everything after them is ignored, and a
// value without leading digits reads as 0.
func phpIntval(text string) int {
	trimmed := strings.TrimSpace(text)
	end := 0
	if end < len(trimmed) && (trimmed[end] == '+' || trimmed[end] == '-') {
		end++
	}
	start := end
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	if end == start {
		return 0
	}
	value, err := strconv.Atoi(trimmed[:end])
	if err != nil {
		return 0
	}
	return value
}

// merchantBalance reads the merchant ledger balance account. A merchant without
// a ledger account yet simply reports a zero balance.
func (service *Service) merchantBalance(ctx context.Context, merchantID string) (int64, int64) {
	var account ledgermodel.Account
	err := service.deps.DB.WithContext(ctx).
		Where("owner_type = ? AND owner_id = ? AND purpose = ? AND currency = ?", "merchant", merchantID, "balance", "CNY").
		First(&account).Error
	if err != nil {
		return 0, 0
	}
	var balance ledgermodel.Balance
	if err := service.deps.DB.WithContext(ctx).First(&balance, "account_id = ?", account.ID).Error; err != nil {
		return 0, 0
	}
	return balance.Available, balance.Frozen
}
