package payment

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	epayv2 "gorixpay/internal/compat/epay/v2"
	financeservice "gorixpay/internal/finance/service"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	"gorixpay/internal/merchant/credential"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// settingSystemKey keeps the platform wide V1 secret that authenticates the
// internal api.php modes (act=order with sign=md5(SYS_KEY.trade_no.SYS_KEY) and
// act=refundapi). It is the counterpart of $conf['syskey'] in
// Epay/includes/common.php:68 and is never returned by any endpoint.
const settingSystemKey = "epay.v1.system_key"

// APIHandler implements the official epay api.php surface: query, order, orders,
// refund, refundapi and settle, plus the V2 api.php?s=class/method dispatcher.
type APIHandler struct {
	db          *gorm.DB
	credentials *credential.Service
	orders      *paymentservice.Service
	finance     *financeservice.Service
	now         func() time.Time
}

// RegisterAPI mounts GET and POST /api.php.
func RegisterAPI(router *gin.Engine, db *gorm.DB, credentials *credential.Service, orders *paymentservice.Service, finance *financeservice.Service) {
	handler := &APIHandler{db: db, credentials: credentials, orders: orders, finance: finance, now: time.Now}
	router.GET("/api.php", handler.handle)
	router.POST("/api.php", handler.handle)
}

func (handler *APIHandler) handle(ctx *gin.Context) {
	if err := ctx.Request.ParseForm(); err != nil {
		writeAPIReply(ctx, gin.H{"code": -4, "msg": "参数解析失败"})
		return
	}
	if strings.TrimSpace(ctx.Query("s")) != "" || strings.TrimSpace(ctx.PostForm("s")) != "" {
		if epayv2.HandleLegacyQuery(ctx) {
			return
		}
		writeAPIReply(ctx, gin.H{"code": -5, "msg": "No Act!"})
		return
	}
	act := strings.TrimSpace(ctx.Query("act"))
	if act == "" {
		act = strings.TrimSpace(ctx.PostForm("act"))
	}
	switch act {
	case "query":
		handler.query(ctx)
	case "order":
		handler.order(ctx)
	case "orders":
		handler.listOrders(ctx)
	case "refund":
		handler.refund(ctx)
	case "refundapi":
		handler.refundAPI(ctx)
	case "settle":
		handler.settle(ctx)
	default:
		writeAPIReply(ctx, gin.H{"code": -5, "msg": "No Act!"})
	}
}

func (handler *APIHandler) query(ctx *gin.Context) {
	merchant, ok := handler.authenticate(ctx)
	if !ok {
		return
	}
	var total, today, lastday int64
	db := handler.db.WithContext(ctx.Request.Context())
	if err := db.Model(&paymentmodel.Order{}).Where("merchant_id = ?", merchant.ID).Count(&total).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "查询商户信息失败"})
		return
	}
	now := handler.now().UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startOfLastday := startOfToday.Add(-24 * time.Hour)
	if err := db.Model(&paymentmodel.Order{}).Where("merchant_id = ? AND status = ? AND paid_at >= ?", merchant.ID, paymentmodel.StatusPaid, startOfToday).Count(&today).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "查询商户信息失败"})
		return
	}
	if err := db.Model(&paymentmodel.Order{}).Where("merchant_id = ? AND status = ? AND paid_at >= ? AND paid_at < ?", merchant.ID, paymentmodel.StatusPaid, startOfLastday, startOfToday).Count(&lastday).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "查询商户信息失败"})
		return
	}
	active := 0
	if merchant.Status == identitymodel.StatusActive {
		active = 1
	}
	available, _ := handler.merchantBalance(ctx, merchant.ID)
	// Epay/api.php:28 answers exactly these keys. type is the merchant's
	// settlement method id and account the payout account; this platform keeps
	// no per-merchant settlement method yet, so both answer their empty value
	// while username is the login name of the merchant.
	writeAPIReply(ctx, gin.H{
		"code":           1,
		"pid":            merchantPID(merchant),
		"key":            ctx.Request.Form.Get("key"),
		"active":         active,
		"money":          formatCentsYuan(available),
		"type":           merchant.SettleType,
		"account":        legacyString(merchant.SettleAccount),
		"username":       merchantUsername(merchant),
		"orders":         total,
		"orders_today":   today,
		"orders_lastday": lastday,
	})
}

func (handler *APIHandler) order(ctx *gin.Context) {
	// Epay/api.php:56-59: a request that carries sign and trade_no but no
	// pid/key is the platform internal mode, authenticated with
	// md5(SYS_KEY.trade_no.SYS_KEY) instead of a merchant credential.
	if _, signed := ctx.Request.Form["sign"]; signed && strings.TrimSpace(ctx.Request.Form.Get("trade_no")) != "" {
		handler.orderBySystemSign(ctx)
		return
	}
	merchant, ok := handler.authenticate(ctx)
	if !ok {
		return
	}
	tradeNo := strings.TrimSpace(ctx.Request.Form.Get("trade_no"))
	outTradeNo := strings.TrimSpace(ctx.Request.Form.Get("out_trade_no"))
	if tradeNo == "" && outTradeNo == "" {
		writeAPIReply(ctx, gin.H{"code": -4, "msg": "订单号不能为空"})
		return
	}
	order, err := handler.orders.Find(ctx.Request.Context(), merchant.ID, tradeNo, outTradeNo)
	if err != nil {
		if errors.Is(err, paymentservice.ErrInvalidQuery) {
			writeAPIReply(ctx, gin.H{"code": -4, "msg": "订单号不能为空"})
			return
		}
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "订单号不存在"})
		return
	}
	handler.writeOrder(ctx, order)
}

// orderBySystemSign answers the platform internal order lookup of
// Epay/api.php:56-59. The trade_no is not scoped to a merchant because the
// caller proved possession of the platform key instead of a merchant key.
func (handler *APIHandler) orderBySystemSign(ctx *gin.Context) {
	tradeNo := strings.TrimSpace(ctx.Request.Form.Get("trade_no"))
	signature := strings.TrimSpace(ctx.Request.Form.Get("sign"))
	systemKey, err := handler.systemKey(ctx)
	if err != nil || !verifySystemSign(signature, systemKey+tradeNo+systemKey) {
		writeAPIReply(ctx, gin.H{"code": -3, "msg": "verify sign failed"})
		return
	}
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "订单号不存在"})
		return
	}
	handler.writeOrder(ctx, order)
}

// writeOrder answers act=order with the field set of Epay/api.php:80. money,
// status and endtime keep the upstream value shapes; payurl replays the URL of
// the last successful channel attempt, which is what
// Epay/includes/lib/Payment.php:662-665 stores in the payurl column.
func (handler *APIHandler) writeOrder(ctx *gin.Context, order paymentmodel.Order) {
	writeAPIReply(ctx, gin.H{
		"code":          1,
		"msg":           "succ",
		"trade_no":      order.TradeNo,
		"out_trade_no":  order.OutTradeNo,
		"api_trade_no":  legacyString(order.ChannelTradeNo),
		"bill_trade_no": "",
		"type":          order.PaymentType,
		"pid":           handler.orderPID(ctx, order),
		"addtime":       order.CreatedAt.Format("2006-01-02 15:04:05"),
		"endtime":       legacyOptionalTime(order.PaidAt),
		"name":          order.Name,
		"money":         formatCentsYuan(order.Amount),
		"param":         legacyString(order.Attach),
		"buyer":         "",
		"status":        legacyOrderStatus(order.Status),
		"payurl":        handler.orderPayURL(ctx, order.ID),
	})
}

// orderPID reports the legacy uid of the merchant that owns the order, which
// is the pid upstream echoes in every order payload (Epay/api.php:80).
func (handler *APIHandler) orderPID(ctx *gin.Context, order paymentmodel.Order) int64 {
	var merchant identitymodel.Merchant
	if err := handler.db.WithContext(ctx.Request.Context()).Where("id = ?", order.MerchantID).First(&merchant).Error; err != nil {
		return 0
	}
	return merchantPID(merchant)
}

// orderPayURL replays the URL of the last succeeded channel attempt for the
// qrcode and jump flows so act=order keeps returning the same payurl a client
// would have received when creating the order.
func (handler *APIHandler) orderPayURL(ctx *gin.Context, orderID string) string {
	var attempt paymentmodel.Attempt
	if err := handler.db.WithContext(ctx.Request.Context()).
		Where("order_id = ? AND status = ?", orderID, paymentmodel.AttemptSucceeded).
		Order("attempt_no DESC").First(&attempt).Error; err != nil {
		return ""
	}
	if strings.TrimSpace(attempt.ActionPayload) == "" {
		return ""
	}
	var action pluginpayment.Action
	if err := json.Unmarshal([]byte(attempt.ActionPayload), &action); err != nil {
		return ""
	}
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		return action.URL
	default:
		return ""
	}
}

func (handler *APIHandler) listOrders(ctx *gin.Context) {
	merchant, ok := handler.authenticate(ctx)
	if !ok {
		return
	}
	limit, err := parseLimit(ctx.Request.Form.Get("limit"))
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -4, "msg": "分页参数不正确"})
		return
	}
	offset := 0
	if raw := strings.TrimSpace(ctx.Request.Form.Get("offset")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			writeAPIReply(ctx, gin.H{"code": -4, "msg": "分页参数不正确"})
			return
		}
		offset = parsed
	}
	query := handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{}).Where("merchant_id = ?", merchant.ID)
	if raw := strings.TrimSpace(ctx.Request.Form.Get("status")); raw != "" {
		status, convErr := strconv.Atoi(raw)
		if convErr != nil {
			writeAPIReply(ctx, gin.H{"code": -4, "msg": "订单状态参数不正确"})
			return
		}
		switch status {
		case 0:
			query = query.Where("status IN ?", []paymentmodel.Status{paymentmodel.StatusPending, paymentmodel.StatusProcessing})
		case 1:
			query = query.Where("status = ?", paymentmodel.StatusPaid)
		case 2:
			query = query.Where("status IN ?", []paymentmodel.Status{paymentmodel.StatusClosed, paymentmodel.StatusFailed})
		default:
			writeAPIReply(ctx, gin.H{"code": -4, "msg": "订单状态参数不正确"})
			return
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "查询订单记录失败！"})
		return
	}
	var orders []paymentmodel.Order
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "查询订单记录失败！"})
		return
	}
	data := make([]gin.H, 0, len(orders))
	for _, order := range orders {
		data = append(data, gin.H{
			"trade_no":     order.TradeNo,
			"out_trade_no": order.OutTradeNo,
			"type":         order.PaymentType,
			"pid":          merchantPID(merchant),
			"addtime":      order.CreatedAt.Format("2006-01-02 15:04:05"),
			"endtime":      legacyOptionalTime(order.PaidAt),
			"name":         order.Name,
			"money":        formatCentsYuan(order.Amount),
			"param":        legacyString(order.Attach),
			"buyer":        "",
			"status":       legacyOrderStatus(order.Status),
		})
	}
	writeAPIReply(ctx, gin.H{"code": 1, "msg": "查询订单记录成功！", "count": total, "data": data})
}

func (handler *APIHandler) refund(ctx *gin.Context) {
	merchant, ok := handler.authenticate(ctx)
	if !ok {
		return
	}
	if handler.finance == nil {
		writeAPIReply(ctx, gin.H{"code": -2, "msg": "未开启商户后台自助退款"})
		return
	}
	tradeNo := strings.TrimSpace(ctx.Request.Form.Get("trade_no"))
	outTradeNo := strings.TrimSpace(ctx.Request.Form.Get("out_trade_no"))
	if tradeNo == "" && outTradeNo == "" {
		writeAPIReply(ctx, gin.H{"code": -4, "msg": "订单号不能为空"})
		return
	}
	amount, err := epayv2.ParseAmount(strings.TrimSpace(ctx.Request.Form.Get("money")))
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "金额输入错误"})
		return
	}
	order, err := handler.orders.Find(ctx.Request.Context(), merchant.ID, tradeNo, outTradeNo)
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "当前订单不存在！"})
		return
	}
	outRefundNo := strings.TrimSpace(ctx.Request.Form.Get("out_refund_no"))
	if len(outRefundNo) <= 5 {
		outRefundNo = businessNo("RF", handler.now())
	}
	refund, _, err := handler.finance.CreateRefund(ctx.Request.Context(), financeservice.CreateRefundRequest{
		MerchantID:     merchant.ID,
		PaymentOrderID: order.ID,
		OutRefundNo:    outRefundNo,
		Amount:         amount,
		Reason:         "merchant api refund",
	})
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "退款失败：" + refundErrorMessage(err)})
		return
	}
	writeAPIReply(ctx, gin.H{
		"code":          0,
		"msg":           "退款成功！退款金额¥" + formatCentsYuan(refund.Amount),
		"refund_no":     refund.RefundNo,
		"out_refund_no": refund.OutRefundNo,
		"trade_no":      order.TradeNo,
		"out_trade_no":  order.OutTradeNo,
		// uid and reducemoney close the upstream field set of
		// Epay/includes/lib/Order.php:134. The ledger debits exactly the refunded
		// amount, so reducemoney equals the refund money here.
		"uid":         merchantPID(merchant),
		"money":       formatCentsYuan(refund.Amount),
		"reducemoney": formatCentsYuan(refund.Amount),
	})
}

// refundAPI implements act=refundapi (Epay/api.php:147-160): the platform
// internal refund that authenticates with key=md5(trade_no.SYS_KEY.trade_no)
// instead of a merchant pid and key.
func (handler *APIHandler) refundAPI(ctx *gin.Context) {
	if handler.finance == nil {
		writeAPIReply(ctx, gin.H{"code": -2, "msg": "未开启商户后台自助退款"})
		return
	}
	tradeNo := strings.TrimSpace(ctx.Request.Form.Get("trade_no"))
	amount, err := epayv2.ParseAmount(strings.TrimSpace(ctx.Request.Form.Get("money")))
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "金额输入错误"})
		return
	}
	systemKey, keyErr := handler.systemKey(ctx)
	if keyErr != nil || !verifySystemSign(ctx.Request.Form.Get("key"), tradeNo+systemKey+tradeNo) {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "密钥错误"})
		return
	}
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "当前订单不存在！"})
		return
	}
	refund, _, err := handler.finance.CreateRefund(ctx.Request.Context(), financeservice.CreateRefundRequest{
		MerchantID:     order.MerchantID,
		PaymentOrderID: order.ID,
		OutRefundNo:    businessNo("RF", handler.now()),
		Amount:         amount,
		Reason:         "internal api refund",
	})
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "退款失败：" + refundErrorMessage(err)})
		return
	}
	writeAPIReply(ctx, gin.H{
		"code": 0,
		// The upstream internal refund reports the full width ¥ (Epay/api.php:157).
		"msg":           "退款成功！退款金额￥" + formatCentsYuan(refund.Amount),
		"refund_no":     refund.RefundNo,
		"out_refund_no": refund.OutRefundNo,
		"trade_no":      order.TradeNo,
		"out_trade_no":  order.OutTradeNo,
		"uid":           handler.orderPID(ctx, order),
		"money":         formatCentsYuan(refund.Amount),
		"reducemoney":   formatCentsYuan(refund.Amount),
	})
}

// systemKey returns the platform wide V1 key used by the internal api.php
// modes, the counterpart of $conf['syskey'] in Epay/includes/common.php:68. It
// is generated on first use, stored in system_settings and never returned by an
// endpoint, so only a caller with database access can use the internal modes.
func (handler *APIHandler) systemKey(ctx *gin.Context) (string, error) {
	db := handler.db.WithContext(ctx.Request.Context())
	var setting epayv2.PlatformSetting
	err := db.Where("setting_key = ?", settingSystemKey).First(&setting).Error
	if err == nil && strings.TrimSpace(setting.Value) != "" {
		return strings.TrimSpace(setting.Value), nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	record := epayv2.PlatformSetting{Key: settingSystemKey, Value: hex.EncodeToString(buffer), UpdatedAt: handler.now().UTC()}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "setting_key"}}, DoNothing: true}).Create(&record).Error; err != nil {
		return "", err
	}
	if err := db.Where("setting_key = ?", settingSystemKey).First(&setting).Error; err != nil {
		return "", err
	}
	return strings.TrimSpace(setting.Value), nil
}

// verifySystemSign compares the lowercase hex md5 of body with signature in
// constant time, the way the upstream syskey comparisons do.
func verifySystemSign(signature, body string) bool {
	sum := md5.Sum([]byte(body))
	expected := hex.EncodeToString(sum[:])
	return len(expected) == len(signature) && subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

func (handler *APIHandler) settle(ctx *gin.Context) {
	merchant, ok := handler.authenticate(ctx)
	if !ok {
		return
	}
	if handler.finance == nil {
		writeAPIReply(ctx, gin.H{"code": -2, "msg": "未开启结算功能"})
		return
	}
	limit, err := parseLimit(ctx.Request.Form.Get("limit"))
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -4, "msg": "分页参数不正确"})
		return
	}
	offset := 0
	if raw := strings.TrimSpace(ctx.Request.Form.Get("offset")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			writeAPIReply(ctx, gin.H{"code": -4, "msg": "分页参数不正确"})
			return
		}
		offset = parsed
	}
	page := offset/limit + 1
	settlements, _, err := handler.finance.Settlements(ctx.Request.Context(), financeservice.Filter{MerchantID: merchant.ID}, financeservice.Page{Number: page, Size: limit})
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -1, "msg": "查询结算记录失败！"})
		return
	}
	data := make([]gin.H, 0, len(settlements))
	for _, settlement := range settlements {
		data = append(data, gin.H{
			"settlement_no":     settlement.SettlementNo,
			"out_settlement_no": settlement.OutSettlementNo,
			"money":             formatCentsYuan(settlement.Amount),
			"status":            string(settlement.Status),
			"addtime":           settlement.CreatedAt.Format("2006-01-02 15:04:05"),
			"endtime":           legacyOptionalTime(settlement.ProcessedAt),
		})
	}
	writeAPIReply(ctx, gin.H{"code": 1, "msg": "查询结算记录成功！", "data": data})
}

// authenticate implements the plaintext pid/key check used by api.php. The MD5
// credential is compared in constant time; RSA-only merchants are rejected.
func (handler *APIHandler) authenticate(ctx *gin.Context) (identitymodel.Merchant, bool) {
	pidText := strings.TrimSpace(ctx.Request.Form.Get("pid"))
	pid, err := strconv.ParseInt(pidText, 10, 64)
	if err != nil || pid <= 0 {
		writeAPIReply(ctx, gin.H{"code": -3, "msg": "商户ID不存在"})
		return identitymodel.Merchant{}, false
	}
	var merchant identitymodel.Merchant
	if err := handler.db.WithContext(ctx.Request.Context()).Where("legacy_uid = ?", pid).First(&merchant).Error; err != nil {
		writeAPIReply(ctx, gin.H{"code": -3, "msg": "商户ID不存在"})
		return identitymodel.Merchant{}, false
	}
	resolved, err := handler.credentials.ResolveActive(ctx.Request.Context(), merchant.ID, identitymodel.CredentialMD5)
	if err != nil {
		writeAPIReply(ctx, gin.H{"code": -3, "msg": "该商户只能使用RSA签名类型"})
		return identitymodel.Merchant{}, false
	}
	key := ctx.Request.Form.Get("key")
	if len(key) != len(resolved.Secret) || subtle.ConstantTimeCompare([]byte(key), []byte(resolved.Secret)) != 1 {
		writeAPIReply(ctx, gin.H{"code": -3, "msg": "商户密钥错误"})
		return identitymodel.Merchant{}, false
	}
	return merchant, true
}

func (handler *APIHandler) merchantBalance(ctx *gin.Context, merchantID string) (int64, int64) {
	var account ledgermodel.Account
	err := handler.db.WithContext(ctx.Request.Context()).
		Where("owner_type = ? AND owner_id = ? AND purpose = ? AND currency = ?", "merchant", merchantID, "balance", "CNY").
		First(&account).Error
	if err != nil {
		return 0, 0
	}
	var balance ledgermodel.Balance
	if err := handler.db.WithContext(ctx.Request.Context()).First(&balance, "account_id = ?", account.ID).Error; err != nil {
		return 0, 0
	}
	return balance.Available, balance.Frozen
}

func writeAPIReply(ctx *gin.Context, payload gin.H) {
	ctx.JSON(http.StatusOK, payload)
}

func parseLimit(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 10, nil
	}
	limit, err := strconv.Atoi(trimmed)
	if err != nil || limit <= 0 {
		return 0, errors.New("invalid limit")
	}
	if limit > 50 {
		limit = 50
	}
	return limit, nil
}

func merchantPID(merchant identitymodel.Merchant) int64 {
	if merchant.LegacyUID == nil {
		return 0
	}
	return *merchant.LegacyUID
}

// merchantUsername reports the login name upstream echoes as username in
// act=query (Epay/api.php:28). Merchants register with an email or a phone
// number, which is what sign-in uses, so that identifier wins over the display
// name.
func merchantUsername(merchant identitymodel.Merchant) string {
	if merchant.Email != nil && strings.TrimSpace(*merchant.Email) != "" {
		return strings.TrimSpace(*merchant.Email)
	}
	if merchant.Phone != nil && strings.TrimSpace(*merchant.Phone) != "" {
		return strings.TrimSpace(*merchant.Phone)
	}
	return merchant.DisplayName
}

func formatCentsYuan(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func legacyOrderStatus(status paymentmodel.Status) int {
	switch status {
	case paymentmodel.StatusPaid:
		return 1
	case paymentmodel.StatusClosed, paymentmodel.StatusFailed:
		return 2
	default:
		return 0
	}
}

func legacyOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func legacyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func refundErrorMessage(err error) string {
	switch {
	case errors.Is(err, financeservice.ErrRefundAmountExceeded):
		return "退款金额超过订单可退金额"
	case errors.Is(err, financeservice.ErrPaymentNotPaid):
		return "订单未支付"
	case errors.Is(err, financeservice.ErrNotFound):
		return "当前订单不存在！"
	case errors.Is(err, financeservice.ErrIdempotencyConflict):
		return "退款单号已存在且参数不一致"
	case errors.Is(err, financeservice.ErrInvalidRequest):
		return "退款参数不合法"
	default:
		return "系统繁忙，请稍后重试"
	}
}

func businessNo(prefix string, now time.Time) string {
	return fmt.Sprintf("%s%s%s", prefix, now.Format("20060102150405"), strings.ReplaceAll(uuid.NewString(), "-", "")[:5])
}
