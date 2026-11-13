package v2

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	channelrouter "gorixpay/internal/channel/router"
	epayrequest "gorixpay/internal/compat/epay/request"
	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/google/uuid"

	"gorixpay/internal/transfer"
)

var outTradeNoPattern = regexp.MustCompile(`^[a-zA-Z0-9._\-|]+$`)

// paySubmit creates an order and redirects the browser to the cashier.
func (service *Service) paySubmit(req *request) {
	amount, apiErr := requireMoney(req)
	if apiErr != nil {
		service.writeError(req.ctx, apiErr.code, apiErr.msg)
		return
	}
	command, _, apiErr := service.createCommand(req, amount, false)
	if apiErr != nil {
		service.writeError(req.ctx, apiErr.code, apiErr.msg)
		return
	}
	order, _, err := service.deps.Orders.Create(req.context(), command)
	if err != nil {
		service.writeOrderError(req, command.OutTradeNo, err)
		return
	}
	req.ctx.Redirect(302, service.cashierURL(order.TradeNo))
}

// payCreate creates an order and immediately starts the payment attempt.
func (service *Service) payCreate(req *request) {
	amount, apiErr := requireMoney(req)
	if apiErr != nil {
		service.writeError(req.ctx, apiErr.code, apiErr.msg)
		return
	}
	command, options, apiErr := service.createCommand(req, amount, true)
	if apiErr != nil {
		service.writeError(req.ctx, apiErr.code, apiErr.msg)
		return
	}
	order, _, err := service.deps.Orders.Create(req.context(), command)
	if err != nil {
		service.writeOrderError(req, command.OutTradeNo, err)
		return
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionJump, URL: service.cashierURL(order.TradeNo)}
	if service.deps.Starter != nil {
		result, startErr := startPayment(service.deps.Starter, req, order.TradeNo, options)
		switch {
		case startErr == nil:
			action = result.Action
		case errors.Is(startErr, channelrouter.ErrNoAvailableChannel):
			service.writeError(req.ctx, -1, "没有可用的支付通道")
			return
		default:
			service.writeError(req.ctx, -1, "支付通道请求失败")
			return
		}
	}
	payType, payInfo := epayAction(action)
	service.writeJSON(req.ctx, map[string]any{
		"code":     0,
		"trade_no": order.TradeNo,
		"pay_type": payType,
		"pay_info": payInfo,
	})
}

// payQuery returns one order owned by the calling merchant. Every failure here is
// thrown by Pay.php:535-557, so nothing on this path is signed.
func (service *Service) payQuery(req *request) {
	order, apiErr := service.findOrder(req)
	if apiErr != nil {
		service.writeError(req.ctx, apiErr.code, apiErr.msg)
		return
	}
	payload := map[string]any{
		"code":         0,
		"trade_no":     order.TradeNo,
		"out_trade_no": order.OutTradeNo,
		"type":         order.PaymentType,
		"pid":          strconv.FormatInt(req.pid, 10),
		"name":         order.Name,
		"money":        FormatAmount(order.Amount),
		"status":       epayOrderStatus(order.Status),
		"addtime":      order.CreatedAt.Format("2006-01-02 15:04:05"),
		"endtime":      formatOptionalTime(order.PaidAt),
		"param":        stringValue(order.Attach),
		"buyer":        "",
		"clientip":     stringValue(order.ClientIP),
	}
	if order.ChannelTradeNo != nil {
		payload["api_trade_no"] = *order.ChannelTradeNo
	}
	service.writeJSON(req.ctx, payload)
}

// payClose closes a pending order. An empty selector is thrown by Pay.php:634
// (unsigned), while Order::close reports "order missing"/"not closable" as a
// returned array, which the V2 envelope signs.
func (service *Service) payClose(req *request) {
	if req.value("trade_no") == "" && req.value("out_trade_no") == "" {
		service.writeError(req.ctx, -1, "订单号不能为空")
		return
	}
	order, _, err := service.deps.Orders.Close(req.context(), req.merchant.ID, req.value("trade_no"), req.value("out_trade_no"))
	if err != nil {
		message := "订单号不存在"
		if errors.Is(err, paymentservice.ErrOrderNotClosable) {
			message = "当前订单状态不允许关闭"
		}
		service.writeResultError(req.ctx, -1, message)
		return
	}
	service.writeJSON(req.ctx, map[string]any{"code": 0, "msg": "订单关闭成功", "trade_no": order.TradeNo})
}

// payRefund creates a merchant refund request. out_refund_no makes it idempotent.
func (service *Service) payRefund(req *request) {
	if service.deps.Finance == nil {
		service.writeError(req.ctx, -1, "管理员未开启商户后台自助退款")
		return
	}
	// Pay.php:565-566 validates the optional amount before the selector and throws,
	// so a malformed amount is never signed.
	requested := req.value("money")
	if requested != "" {
		if _, err := ParseAmount(requested); err != nil {
			service.writeError(req.ctx, -1, "金额输入错误")
			return
		}
	}
	tradeNo := req.value("trade_no")
	outTradeNo := req.value("out_trade_no")
	if tradeNo == "" && outTradeNo == "" {
		service.writeError(req.ctx, -1, "订单号不能为空")
		return
	}
	order, err := service.deps.Orders.Find(req.context(), req.merchant.ID, tradeNo, outTradeNo)
	if err != nil {
		// Pay.php:570-573 throws when the out_trade_no selector misses (unsigned);
		// a missing trade_no falls through to Order::refund, which returns an array
		// (signed, Order.php:71-72).
		if tradeNo == "" {
			service.writeError(req.ctx, -1, "当前订单不存在！")
		} else {
			service.writeResultError(req.ctx, -1, "当前订单不存在！")
		}
		return
	}
	amount := order.Amount
	if requested != "" {
		amount, _ = ParseAmount(requested)
	}
	outRefundNo := req.value("out_refund_no")
	if len(outRefundNo) < 5 {
		outRefundNo = generateBusinessNo("RF", service.now())
	}
	refund, _, err := service.deps.Finance.CreateRefund(req.context(), financeservice.CreateRefundRequest{
		MerchantID:     req.merchant.ID,
		PaymentOrderID: order.ID,
		OutRefundNo:    outRefundNo,
		Amount:         amount,
		Reason:         "merchant api refund",
	})
	if err != nil {
		service.writeRefundError(req, err)
		return
	}
	service.writeJSON(req.ctx, map[string]any{
		"code":          0,
		"msg":           "退款成功！退款金额¥" + FormatAmount(refund.Amount),
		"refund_no":     refund.RefundNo,
		"out_refund_no": refund.OutRefundNo,
		"trade_no":      order.TradeNo,
		"out_trade_no":  order.OutTradeNo,
		"money":         FormatAmount(refund.Amount),
		"status":        epayFinanceStatus(refund.Status),
	})
}

// payRefundQuery returns a refund by refund_no or out_refund_no. Every failure is
// thrown by Pay.php:597-620, so nothing on this path is signed.
func (service *Service) payRefundQuery(req *request) {
	if service.deps.Finance == nil {
		service.writeError(req.ctx, -1, "管理员未开启商户后台自助退款")
		return
	}
	refundNo := req.value("refund_no")
	outRefundNo := req.value("out_refund_no")
	if refundNo == "" && outRefundNo == "" {
		service.writeError(req.ctx, -1, "商户退款单号不能为空")
		return
	}
	keyword := refundNo
	if keyword == "" {
		keyword = outRefundNo
	}
	refunds, _, err := service.deps.Finance.Refunds(req.context(), financeservice.Filter{MerchantID: req.merchant.ID, Keyword: keyword}, financeservice.Page{Number: 1, Size: 20})
	if err != nil {
		service.writeError(req.ctx, -1, "退款记录查询失败")
		return
	}
	var refund financemodel.Refund
	found := false
	for _, item := range refunds {
		if (refundNo != "" && item.RefundNo == refundNo) || (outRefundNo != "" && item.OutRefundNo == outRefundNo) {
			refund = item
			found = true
			break
		}
	}
	if !found {
		service.writeError(req.ctx, -1, "退款记录不存在")
		return
	}
	order, orderErr := service.orderByID(req, refund.PaymentOrderID)
	payload := map[string]any{
		"code":          0,
		"refund_no":     refund.RefundNo,
		"out_refund_no": refund.OutRefundNo,
		"trade_no":      "",
		"out_trade_no":  "",
		"uid":           req.pid,
		"money":         FormatAmount(refund.Amount),
		// reducemoney is what upstream subtracted from the merchant balance; the
		// ledger refund debits exactly the refunded amount.
		"reducemoney": FormatAmount(refund.Amount),
		"status":      epayFinanceStatus(refund.Status),
		"addtime":     refund.CreatedAt.Format("2006-01-02 15:04:05"),
		"endtime":     formatOptionalTime(refund.ProcessedAt),
	}
	if orderErr == nil {
		payload["trade_no"] = order.TradeNo
		payload["out_trade_no"] = order.OutTradeNo
	}
	service.writeJSON(req.ctx, payload)
}

// transferSubmit creates a merchant transfer (payout) request. The parameter
// validation mirrors Transfer.php:17-40, which throws (unsigned) for every reject
// before Transfer::add runs; add() then reports business failures as a returned
// array, which is signed.
func (service *Service) transferSubmit(req *request) {
	if service.deps.Transfers == nil {
		service.writeError(req.ctx, -1, "管理员未开启代付功能")
		return
	}
	if req.value("type") == "" {
		service.writeError(req.ctx, -1, "代付方式(type)不能为空")
		return
	}
	accountNo := req.value("account")
	if accountNo == "" {
		service.writeError(req.ctx, -1, "收款人账号(account)不能为空")
		return
	}
	rawMoney := req.value("money")
	if rawMoney == "" {
		service.writeError(req.ctx, -1, "转账金额(money)不能为空")
		return
	}
	amount, err := ParseAmount(rawMoney)
	if err != nil {
		service.writeError(req.ctx, -1, "转账金额输入不规范")
		return
	}
	outTransferNo := firstNonEmpty(req.value("out_biz_no"), req.value("out_transfer_no"))
	if outTransferNo == "" {
		outTransferNo = generateBusinessNo("TF", service.now())
	}
	if !outTradeNoPattern.MatchString(outTransferNo) {
		service.writeError(req.ctx, -1, "交易号输入不规范")
		return
	}
	currency := req.value("currency")
	if currency == "" {
		currency = "CNY"
	}
	order, _, err := service.deps.Transfers.Create(req.context(), transfer.Request{
		MerchantID:    req.merchant.ID,
		OutTransferNo: outTransferNo,
		Amount:        amount,
		Currency:      currency,
		AccountName:   req.value("name"),
		AccountNo:     accountNo,
		BankName:      firstNonEmpty(req.value("bank"), req.value("bank_name"), req.value("type")),
		BranchName:    firstNonEmpty(req.value("branch"), req.value("branch_name")),
	})
	if err != nil {
		service.writeTransferError(req, err)
		return
	}
	service.writeJSON(req.ctx, map[string]any{
		"code":            0,
		"msg":             "代付申请已提交",
		"transfer_no":     order.TransferNo,
		"out_transfer_no": order.OutTransferNo,
		"status":          string(order.Status),
		"money":           FormatAmount(order.Amount),
	})
}

// transferQuery returns one transfer owned by the calling merchant. Every failure
// is thrown by Transfer.php:42-92, so nothing on this path is signed.
func (service *Service) transferQuery(req *request) {
	if service.deps.Transfers == nil {
		service.writeError(req.ctx, -1, "管理员未开启代付功能")
		return
	}
	bizNo, outBizNo := transferSelectors(req)
	if bizNo == "" && outBizNo == "" {
		service.writeError(req.ctx, -1, "转账交易号不能为空")
		return
	}
	order, err := service.deps.Transfers.Find(req.context(), req.merchant.ID, bizNo, outBizNo)
	if err != nil {
		service.writeError(req.ctx, -1, "转账交易号不存在")
		return
	}
	service.writeJSON(req.ctx, transferPayload(order))
}

// transferProof returns the payout proof placeholder that the reference
// implementation also returns until the channel settles the transfer.
//
// Transfer.php:106-108 looks the out_biz_no value up in the biz_no column, so a
// proof request that only carries out_biz_no matches nothing unless the value is
// itself a biz_no. Reproduced deliberately: Epay/tests/contracts manifest marks
// this as known_quirk out_biz_no_query_uses_biz_no_column.
func (service *Service) transferProof(req *request) {
	if service.deps.Transfers == nil {
		service.writeError(req.ctx, -1, "管理员未开启代付功能")
		return
	}
	bizNo, outBizNo := transferSelectors(req)
	if bizNo == "" && outBizNo == "" {
		service.writeError(req.ctx, -1, "转账交易号不能为空")
		return
	}
	keyword := bizNo
	if keyword == "" {
		keyword = outBizNo
	}
	order, err := service.deps.Transfers.Find(req.context(), req.merchant.ID, keyword, "")
	if err != nil {
		service.writeError(req.ctx, -1, "当前转账订单不存在")
		return
	}
	payload := transferPayload(order)
	payload["code"] = 0
	payload["msg"] = "代付凭证待生成"
	payload["proof_url"] = ""
	service.writeJSON(req.ctx, payload)
}

// transferBalance returns the payout-able balance of the merchant.
func (service *Service) transferBalance(req *request) {
	if service.deps.Transfers == nil {
		service.writeError(req.ctx, -1, "管理员未开启代付功能")
		return
	}
	balance, err := service.deps.Transfers.Balance(req.context(), req.merchant.ID)
	if err != nil {
		service.writeError(req.ctx, -1, "余额查询失败")
		return
	}
	service.writeJSON(req.ctx, map[string]any{
		"code":            0,
		"available_money": FormatAmount(balance.Available),
		"frozen_money":    FormatAmount(balance.Frozen),
		"transfer_rate":   "0",
	})
}

// transferPayload publishes the Transfer.php:60-92 field names: biz_no,
// out_biz_no, status (1 success / 2 failed / 0 pending), amount, cost_money,
// paydate and remark. The transfer_no/out_transfer_no/money aliases stay for our
// own console.
func transferPayload(order transfer.Order) map[string]any {
	money := FormatAmount(order.Amount)
	paydate := ""
	if order.ProcessedAt != nil {
		paydate = order.ProcessedAt.Format("2006-01-02 15:04:05")
	}
	payload := map[string]any{
		"code":            0,
		"biz_no":          order.TransferNo,
		"out_biz_no":      order.OutTransferNo,
		"status":          epayTransferStatus(order.Status),
		"amount":          money,
		"cost_money":      money,
		"paydate":         paydate,
		"remark":          "",
		"transfer_no":     order.TransferNo,
		"out_transfer_no": order.OutTransferNo,
		"status_text":     string(order.Status),
		"money":           money,
		"account_name":    order.AccountName,
		"account_no":      order.AccountNo,
		"bank_name":       order.BankName,
		"addtime":         order.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	if order.ProcessedAt != nil {
		payload["endtime"] = order.ProcessedAt.Format("2006-01-02 15:04:05")
	}
	if order.RejectionReason != nil {
		payload["errmsg"] = *order.RejectionReason
	}
	return payload
}

// epayTransferStatus maps our payout states onto the reference 0/1/2 codes.
func epayTransferStatus(status transfer.Status) int {
	switch status {
	case transfer.StatusApproved:
		return 1
	case transfer.StatusRejected:
		return 2
	default:
		return 0
	}
}

// transferSelectors reads the payout selectors the way Transfer.php:51-59 does:
// biz_no wins over out_biz_no, and transfer_no/out_transfer_no are accepted as
// aliases because the reference table calls the same columns biz_no/out_biz_no.
func transferSelectors(req *request) (string, string) {
	bizNo := firstNonEmpty(req.value("biz_no"), req.value("transfer_no"))
	outBizNo := firstNonEmpty(req.value("out_biz_no"), req.value("out_transfer_no"))
	return bizNo, outBizNo
}

func (service *Service) findOrder(req *request) (paymentmodel.Order, *apiError) {
	tradeNo := req.value("trade_no")
	outTradeNo := req.value("out_trade_no")
	if tradeNo == "" && outTradeNo == "" {
		return paymentmodel.Order{}, errorf(-1, "订单号不能为空")
	}
	order, err := service.deps.Orders.Find(req.context(), req.merchant.ID, tradeNo, outTradeNo)
	if err != nil {
		return paymentmodel.Order{}, errorf(-1, "订单号不存在")
	}
	return order, nil
}

func (service *Service) orderByID(req *request, id string) (paymentmodel.Order, error) {
	var order paymentmodel.Order
	err := service.deps.DB.WithContext(req.context()).Where("id = ? AND merchant_id = ?", id, req.merchant.ID).First(&order).Error
	return order, err
}

// startPayment forwards the request hints when the configured starter supports
// them; the orchestrator does, test doubles may not.
func startPayment(starter PaymentStarter, req *request, tradeNo string, options paymentorchestrator.StartOptions) (paymentorchestrator.Result, error) {
	if optionStarter, ok := starter.(optionStarter); ok {
		return optionStarter.StartWithOptions(req.context(), tradeNo, options)
	}
	return starter.Start(req.context(), tradeNo)
}

// createCommand validates the order parameters the way Pay.php:313-321 does. Every
// reject there is an echojsonmsg call with the default code, so the answer is -1 and
// unsigned — never -4 and never signed.
func (service *Service) createCommand(req *request, amount int64, requireClientIP bool) (paymentservice.CreateCommand, paymentorchestrator.StartOptions, *apiError) {
	options := epayrequest.Options(func(name string) string { return req.values[name] })
	outTradeNo := req.value("out_trade_no")
	if outTradeNo == "" {
		return paymentservice.CreateCommand{}, options, errorf(-1, "订单号(out_trade_no)不能为空")
	}
	if !outTradeNoPattern.MatchString(outTradeNo) {
		return paymentservice.CreateCommand{}, options, errorf(-1, "订单号(out_trade_no)格式不正确")
	}
	notifyURL := req.value("notify_url")
	if notifyURL == "" {
		return paymentservice.CreateCommand{}, options, errorf(-1, "通知地址(notify_url)不能为空")
	}
	name := req.value("name")
	if name == "" {
		return paymentservice.CreateCommand{}, options, errorf(-1, "商品名称(name)不能为空")
	}
	paymentType := req.value("type")
	if paymentType == "" && options.Method == "scan" {
		// Pay.php:307-310 derives the wallet from the auth_code prefix.
		paymentType = epayrequest.ScanPayType(options.AuthCode)
		if paymentType == "" {
			return paymentservice.CreateCommand{}, options, errorf(-1, "未知的付款码类型")
		}
	}
	if paymentType == "" {
		if requireClientIP {
			return paymentservice.CreateCommand{}, options, errorf(-1, "支付方式(type)不能为空")
		}
		paymentType = "cashier"
	}
	if options.Method == "jsapi" && options.SubOpenID == "" {
		return paymentservice.CreateCommand{}, options, errorf(-1, "jsapi支付时参数(sub_openid)不能为空")
	}
	if options.Method == "scan" && options.AuthCode == "" {
		return paymentservice.CreateCommand{}, options, errorf(-1, "付款码支付时授权码(auth_code)不能为空")
	}
	clientIP := req.value("clientip")
	if requireClientIP && net.ParseIP(clientIP) == nil {
		return paymentservice.CreateCommand{}, options, errorf(-1, "用户IP地址不合法")
	}
	return paymentservice.CreateCommand{
		MerchantID:  req.merchant.ID,
		OutTradeNo:  outTradeNo,
		PaymentType: paymentType,
		Name:        name,
		Amount:      amount,
		NotifyURL:   notifyURL,
		ReturnURL:   req.value("return_url"),
		ClientIP:    clientIP,
		Buyer:       options.SubOpenID,
		Attach:      req.value("param"),
		// Orders created through API_INIT are version 1 upstream
		// (Epay/includes/lib/api/Pay.php:171) and are notified with the RSA
		// payload (Epay/includes/functions.php:503).
		Version: 1,
	}, options, nil
}

// requireMoney mirrors Pay.php:316-320: a missing amount is "金额(money)不能为空"
// and a malformed one is "金额不合法", both thrown with the default -1.
func requireMoney(req *request) (int64, *apiError) {
	raw := req.value("money")
	if raw == "" {
		return 0, errorf(-1, "金额(money)不能为空")
	}
	amount, err := ParseAmount(raw)
	if err != nil {
		return 0, errorf(-1, "金额不合法")
	}
	return amount, nil
}

func (service *Service) writeOrderError(req *request, outTradeNo string, err error) {
	var rejection paymentservice.Rejection
	switch {
	case errors.As(err, &rejection):
		service.writeError(req.ctx, -1, rejection.Reason)
	case errors.Is(err, paymentservice.ErrIdempotencyConflict):
		service.writeError(req.ctx, -1, fmt.Sprintf("该订单(%s)支付参数有变化，请更换订单号重新发起支付", outTradeNo))
	case errors.Is(err, paymentservice.ErrInvalidCommand):
		service.writeError(req.ctx, -1, "提交参数不完整或格式不正确")
	default:
		service.writeError(req.ctx, -1, "创建订单失败，请返回重试！")
	}
}

// writeRefundError reports an Order::refund failure. Order.php:65-134 returns an
// array for every business reject, so the V2 envelope signs the answer.
func (service *Service) writeRefundError(req *request, err error) {
	switch {
	case errors.Is(err, financeservice.ErrRefundAmountExceeded):
		service.writeResultError(req.ctx, -1, "退款金额超过订单可退金额")
	case errors.Is(err, financeservice.ErrPaymentNotPaid):
		service.writeResultError(req.ctx, -1, "订单未支付，无法退款")
	case errors.Is(err, financeservice.ErrNotFound):
		service.writeResultError(req.ctx, -1, "订单号不存在")
	case errors.Is(err, financeservice.ErrIdempotencyConflict):
		service.writeResultError(req.ctx, -1, "退款单号已存在且参数不一致")
	case errors.Is(err, financeservice.ErrInvalidRequest):
		service.writeResultError(req.ctx, -1, "退款参数不合法")
	default:
		service.writeResultError(req.ctx, -1, "退款失败，请稍后重试")
	}
}

// writeTransferError reports a Transfer::add failure. add() (Transfer.php:36-121)
// returns an array, so the answer is signed.
func (service *Service) writeTransferError(req *request, err error) {
	switch {
	case errors.Is(err, transfer.ErrInsufficientBalance):
		service.writeResultError(req.ctx, -1, "可用余额不足")
	case errors.Is(err, transfer.ErrIdempotencyConflict):
		service.writeResultError(req.ctx, -1, "代付单号已存在且参数不一致")
	case errors.Is(err, transfer.ErrInvalidRequest):
		service.writeResultError(req.ctx, -4, "代付参数不完整或格式不正确")
	default:
		service.writeResultError(req.ctx, -1, "代付申请失败，请稍后重试")
	}
}

func (service *Service) cashierURL(tradeNo string) string {
	return service.siteURL + "/cashier.php?trade_no=" + tradeNo
}

func epayAction(action pluginpayment.Action) (string, any) {
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		return string(action.Type), action.URL
	default:
		return string(action.Type), action.Data
	}
}

func epayOrderStatus(status paymentmodel.Status) int {
	switch status {
	case paymentmodel.StatusPaid:
		return 1
	case paymentmodel.StatusClosed, paymentmodel.StatusFailed:
		return 2
	default:
		return 0
	}
}

func epayFinanceStatus(status financemodel.Status) int {
	switch status {
	case financemodel.StatusApproved:
		return 1
	case financemodel.StatusRejected:
		return 2
	default:
		return 0
	}
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func generateBusinessNo(prefix string, now time.Time) string {
	return fmt.Sprintf("%s%s%s", prefix, now.Format("20060102150405"), strings.ReplaceAll(uuid.NewString(), "-", "")[:5])
}
