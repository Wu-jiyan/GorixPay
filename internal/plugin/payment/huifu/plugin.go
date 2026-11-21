// Package huifu implements 汇付斗拱 (Huifu Douggong) aggregated payment: a signed
// JSON request to /v3/trade/payment/jspay returning a QR code or JSAPI pay info,
// and a form notification whose resp_data is verified as an exact string.
package huifu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"
)

const (
	// maxCallbackBytes bounds a notification body before it is parsed.
	maxCallbackBytes = 1 << 20
	// ackPrefix is the only acknowledgement Douggong accepts; anything else makes
	// it retry the notification forever.
	ackPrefix = "RECV_ORD_ID_"
	// transStatSuccess marks a successful transaction in resp_data.
	transStatSuccess = "S"
	// defaultWXProductID is the WeChat product id Huifu expects for native pay.
	defaultWXProductID = "01001"
	// defaultClientIP is used when the order carries no client IP.
	defaultClientIP = "127.0.0.1"
	// defaultGoodsDesc is used when the order has no description.
	defaultGoodsDesc = "订单支付"
	// defaultProjectTitle is the title 汇付 shows on its hosted cashier.
	defaultProjectTitle = "在线支付"
)

// Transaction families, derived from the channel payment type.
const (
	familyAlipay = "alipay"
	familyWX     = "wx"
	familyBank   = "bank"
)

type Plugin struct {
	client *http.Client
	now    func() time.Time
}

// New builds the plugin. A nil client gets the shared default with a timeout.
func New(client *http.Client) *Plugin {
	return &Plugin{client: pluginpayment.OrDefaultClient(client), now: time.Now}
}

func (*Plugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{
		Code:         "huifu",
		Name:         "汇付斗拱",
		Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment, pluginpayment.CapabilityRefund},
	}
}

func (*Plugin) ConfigFields() []pluginpayment.ConfigField {
	return []pluginpayment.ConfigField{
		{Key: "sys_id", Label: "系统号 (sys_id)", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "product_id", Label: "产品号 (product_id)", Type: pluginpayment.ConfigFieldText, Required: true},
		{Key: "private_key", Label: "商户私钥 (PKCS8 PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "huifu_public_key", Label: "汇付公钥 (PEM)", Type: pluginpayment.ConfigFieldTextarea, Required: true, Secret: true},
		{Key: "huifu_id", Label: "子商户号 (huifu_id)", Type: pluginpayment.ConfigFieldText},
		{Key: "project_id", Label: "托管项目号 (project_id)", Type: pluginpayment.ConfigFieldText},
		{Key: "project_title", Label: "托管收银台标题", Type: pluginpayment.ConfigFieldText},
		{Key: "mode", Label: "接入方式 (native / hosting)", Type: pluginpayment.ConfigFieldText},
	}
}

func (plugin *Plugin) CreatePayment(ctx context.Context, request pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	invoice, err := newClient(request.Configuration, plugin.client)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if request.TradeNo == "" || request.Amount <= 0 {
		return pluginpayment.Action{}, fmt.Errorf("%w: trade number and amount", pluginpayment.ErrInvalidConfiguration)
	}
	if request.NotifyURL == "" {
		return pluginpayment.Action{}, fmt.Errorf("%w: notify_url", pluginpayment.ErrInvalidConfiguration)
	}
	// 托管 (hosted cashier) channels do not take the jspay route: 汇付 only turns
	// on the trade types used by the direct product, so a channel that was sold
	// as 托管 must go through /v2/trade/hosting/payment/preorder and hand the
	// buyer a jump_url.
	if hostingMode(request.Configuration) {
		return plugin.createHosting(ctx, request, invoice)
	}
	tradeType, family, jsapi := selectTradeType(request.PaymentType, request.Method)
	description := strings.TrimSpace(request.Description)
	if description == "" {
		description = defaultGoodsDesc
	}
	clientIP := strings.TrimSpace(request.ClientIP)
	if clientIP == "" {
		clientIP = defaultClientIP
	}
	data := map[string]json.RawMessage{
		"req_date":        rawValue(reqDate(request.TradeNo, plugin.now())),
		"req_seq_id":      rawValue(request.TradeNo),
		"huifu_id":        rawValue(pluginpayment.ConfigValue(request.Configuration, "huifu_id", invoice.sysID)),
		"trade_type":      rawValue(tradeType),
		"trans_amt":       rawValue(pluginpayment.FormatAmount(request.Amount)),
		"goods_desc":      rawValue(description),
		"notify_url":      rawValue(request.NotifyURL),
		"risk_check_data": rawValue(jsonObject(map[string]string{"ip_addr": clientIP})),
	}
	productData, err := productPayload(family, jsapi, description, clientIP, request, request.Configuration)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	for name, value := range productData {
		data[name] = rawValue(value)
	}
	response, err := invoice.post(ctx, jsPayPath, data)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if err := businessError(response); err != nil {
		return pluginpayment.Action{}, err
	}
	return buildAction(tradeType, response)
}

// selectTradeType maps the channel payment type and request method onto a Huifu
// trade_type, keeping the family so the product payload can be built.
func selectTradeType(paymentType, method string) (string, string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(paymentType))
	jsapi := strings.Contains(strings.ToLower(strings.TrimSpace(method)), "jsapi") ||
		strings.Contains(normalized, "jsapi")
	family := familyAlipay
	switch {
	case strings.Contains(normalized, "wx"), strings.Contains(normalized, "wechat"):
		family = familyWX
	case strings.Contains(normalized, "bank"), strings.Contains(normalized, "union"), strings.Contains(normalized, "card"):
		family = familyBank
	}
	prefix := "A"
	switch family {
	case familyWX:
		prefix = "T"
	case familyBank:
		prefix = "U"
	}
	suffix := "NATIVE"
	if jsapi {
		suffix = "JSAPI"
	}
	return prefix + "_" + suffix, family, jsapi
}

// productPayload renders the family-specific nested data Huifu expects.
func productPayload(family string, jsapi bool, description, clientIP string, request pluginpayment.CreateRequest, config map[string]string) (map[string]string, error) {
	openID := resolveOpenID(request, config)
	switch family {
	case familyWX:
		fields := map[string]string{"spbill_create_ip": clientIP}
		if !jsapi {
			fields["product_id"] = defaultWXProductID
			return map[string]string{"wx_data": jsonObject(fields)}, nil
		}
		if openID == "" {
			return nil, fmt.Errorf("%w: sub_openid", pluginpayment.ErrInvalidConfiguration)
		}
		fields["sub_openid"] = openID
		fields["device_info"] = "4"
		if subAppID := strings.TrimSpace(firstNonEmpty(config["sub_appid"], request.Extra["sub_appid"])); subAppID != "" {
			fields["sub_appid"] = subAppID
		}
		return map[string]string{"wx_data": jsonObject(fields)}, nil
	case familyBank:
		fields := map[string]string{"customer_ip": clientIP}
		if !jsapi {
			if qrCode := strings.TrimSpace(request.Extra["qr_code"]); qrCode != "" {
				fields["qr_code"] = qrCode
			}
			return map[string]string{"unionpay_data": jsonObject(fields)}, nil
		}
		if openID == "" {
			return nil, fmt.Errorf("%w: sub_openid", pluginpayment.ErrInvalidConfiguration)
		}
		fields["user_id"] = openID
		return map[string]string{"unionpay_data": jsonObject(fields)}, nil
	default:
		fields := map[string]string{"subject": description}
		if jsapi {
			if openID == "" {
				return nil, fmt.Errorf("%w: sub_openid", pluginpayment.ErrInvalidConfiguration)
			}
			fields["buyer_id"] = openID
		}
		return map[string]string{"alipay_data": jsonObject(fields)}, nil
	}
}

// buildAction turns the verified response into a cashier action: native orders
// return a QR code, JSAPI orders return the pay info blob.
func buildAction(tradeType string, data map[string]json.RawMessage) (pluginpayment.Action, error) {
	if strings.HasSuffix(tradeType, "_JSAPI") {
		payInfo := rawString(data, "pay_info")
		if payInfo == "" {
			return pluginpayment.Action{}, fmt.Errorf("huifu returned no pay_info for %s", tradeType)
		}
		action := pluginpayment.Action{Type: pluginpayment.ActionJSAPI, Data: payInfo}
		if err := action.Validate(); err != nil {
			return pluginpayment.Action{}, err
		}
		return action, nil
	}
	qrCode := strings.TrimSpace(rawString(data, "qr_code"))
	if qrCode == "" {
		return pluginpayment.Action{}, fmt.Errorf("huifu returned no qr_code for %s", tradeType)
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: qrCode}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// hostingMode reports whether the channel runs through 汇付's hosted cashier.
// Epay fills project_id only for 托管 channels, so its presence is the signal;
// an explicit mode=hosting / mode=native always wins over that heuristic.
func hostingMode(config map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(config["mode"])) {
	case "hosting", "host":
		return true
	case "native", "direct":
		return false
	}
	return strings.TrimSpace(config["project_id"]) != ""
}

// createHosting places a 托管 pre-order: 汇付 renders its own cashier and answers
// a jump_url the buyer is redirected to. This is the only route available on
// products sold as 托管H5/PC, where the direct trade types are not enabled.
func (plugin *Plugin) createHosting(ctx context.Context, request pluginpayment.CreateRequest, client *Client) (pluginpayment.Action, error) {
	projectID := strings.TrimSpace(request.Configuration["project_id"])
	if projectID == "" {
		return pluginpayment.Action{}, fmt.Errorf("%w: project_id", pluginpayment.ErrInvalidConfiguration)
	}
	description := strings.TrimSpace(request.Description)
	if description == "" {
		description = defaultGoodsDesc
	}
	requestType := "M"
	device := hostingDevice(request)
	if device == "pc" {
		requestType = "P"
	}
	hostingData := jsonObject(map[string]string{
		"project_title": pluginpayment.ConfigValue(request.Configuration, "project_title", defaultProjectTitle),
		"project_id":    projectID,
		"callback_url":  request.ReturnURL,
		"request_type":  requestType,
	})
	data := map[string]json.RawMessage{
		"req_date":       rawValue(reqDate(request.TradeNo, plugin.now())),
		"req_seq_id":     rawValue(request.TradeNo),
		"huifu_id":       rawValue(pluginpayment.ConfigValue(request.Configuration, "huifu_id", client.sysID)),
		"trans_amt":      rawValue(pluginpayment.FormatAmount(request.Amount)),
		"goods_desc":     rawValue(description),
		"pre_order_type": rawValue("1"),
		"hosting_data":   rawValue(hostingData),
		"notify_url":     rawValue(request.NotifyURL),
	}
	// Empty means "let 汇付 render its own cashier", which is what we want whenever
	// the pinned trade type cannot run in this browser (see hostingTransType).
	if tradeType := hostingTransType(request.PaymentType, requestType, device); tradeType != "" {
		data["trans_type"] = rawValue(tradeType)
	}
	response, err := client.post(ctx, hostingPath, data)
	if err != nil {
		return pluginpayment.Action{}, err
	}
	if err := businessError(response); err != nil {
		return pluginpayment.Action{}, err
	}
	jumpURL := strings.TrimSpace(rawString(response, "jump_url"))
	if jumpURL == "" {
		return pluginpayment.Action{}, fmt.Errorf("huifu hosting response carries no jump_url")
	}
	// 汇付的托管收银台是 H5 页面（hostingh5）。上游 Epay 在 PC 浏览器上不会把买家
	// 直接跳到这个地址——那样汇付会回「当前订单方式不支持」——而是把它当二维码渲染，
	// 让买家拿手机扫。手机上才直接跳过去，所以这里按设备决定动作类型。
	if deviceIsMobile(device) {
		action := pluginpayment.Action{Type: pluginpayment.ActionJump, URL: jumpURL}
		if err := action.Validate(); err != nil {
			return pluginpayment.Action{}, err
		}
		return action, nil
	}
	action := pluginpayment.Action{Type: pluginpayment.ActionQRCode, URL: jumpURL}
	if err := action.Validate(); err != nil {
		return pluginpayment.Action{}, err
	}
	return action, nil
}

// hostingTransType picks the trans_type to pin on a hosted pre-order, or "" to
// let 汇付 show its own cashier.
//
// The pinned type only works in the browser it belongs to: T_JSAPI is 微信公众
// 号支付 (needs the WeChat in-app browser), A_JSAPI needs the Alipay app. Sending
// either from a plain Safari/Chrome makes 汇付's page answer「当前订单支付方式不
// 支持」— which is what upstream Epay trips over too. On a desktop browser the
// only 微信 type a 托管 product accepts is T_JSAPI (there is no T_NATIVE), so a
// desktop 微信 order also has to fall back to the cashier.
//
// Pinning is therefore reserved for the cases that can actually run:
//   - 微信 in-app browser → T_JSAPI
//   - 支付宝 in-app browser → A_JSAPI
//   - desktop → A_NATIVE / U_NATIVE, which render a scannable QR
//
// Everything else passes no trans_type, and 汇付 renders the cashier with the
// methods the project and the browser support.
func hostingTransType(paymentType, requestType, device string) string {
	family := paymentFamily(paymentType)
	switch {
	case device == "wechat" && family == familyWX:
		return "T_JSAPI"
	case device == "alipay" && family == familyAlipay:
		return "A_JSAPI"
	case requestType == "P" && family == familyAlipay:
		return "A_NATIVE"
	case requestType == "P" && family == familyBank:
		return "U_NATIVE"
	default:
		return ""
	}
}

// paymentFamily reuses the family detection of the direct flow, without the
// method hint (hosting has no JSAPI/scan distinction of its own).
func paymentFamily(paymentType string) string {
	_, family, _ := selectTradeType(paymentType, "")
	return family
}

// hostingDevice normalises how the buyer is paying. The request hints carry the
// device the protocol layer detected (Epay's device=mobile/wechat/alipay/app/pc);
// anything missing or unrecognised counts as a desktop browser, which is the safe
// default because the hosted cashier is an H5 page.
//
// The in-app browsers keep their own identity: only they can run a JSAPI trade
// type, so "wechat" and "alipay" must not be flattened into "mobile".
func hostingDevice(request pluginpayment.CreateRequest) string {
	device := strings.ToLower(strings.TrimSpace(request.Extra["device"]))
	if device == "" {
		device = strings.ToLower(strings.TrimSpace(request.Method))
	}
	switch device {
	case "wechat", "weixin", "micromessenger":
		return "wechat"
	case "alipay", "alipayclient":
		return "alipay"
	case "mobile", "wap", "app", "jump", "h5", "jsapi", "qq":
		return "mobile"
	default:
		return "pc"
	}
}

// deviceIsMobile reports whether a hosted page may simply be opened, as opposed
// to being rendered as a QR code for the buyer to scan.
func deviceIsMobile(device string) bool {
	return device == "mobile" || device == "wechat" || device == "alipay"
}

// VerifyCallback authenticates the notification: Huifu signs the exact resp_data
// string, so it must never be re-serialized before verification.
func (plugin *Plugin) VerifyCallback(_ context.Context, r *http.Request, config map[string]string) (pluginpayment.CallbackResult, error) {
	if r == nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing request", pluginpayment.ErrCallbackRejected)
	}
	resolved, err := pluginpayment.RequireConfig(config, "huifu_public_key")
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if r.ContentLength > maxCallbackBytes {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: notification too large", pluginpayment.ErrCallbackRejected)
	}
	if err := r.ParseForm(); err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid notification body", pluginpayment.ErrCallbackRejected)
	}
	respData := r.PostForm.Get("resp_data")
	signature := r.PostForm.Get("sign")
	if strings.TrimSpace(respData) == "" || strings.TrimSpace(signature) == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing resp_data or sign", pluginpayment.ErrCallbackRejected)
	}
	publicKey, err := parsePublicKeyPEM(resolved["huifu_public_key"])
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	if !verifyCanonical(publicKey, respData, signature) {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid sign", pluginpayment.ErrCallbackRejected)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal([]byte(respData), &data); err != nil {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: invalid resp_data", pluginpayment.ErrCallbackRejected)
	}
	tradeNo := strings.TrimSpace(rawString(data, "req_seq_id"))
	if tradeNo == "" {
		return pluginpayment.CallbackResult{}, fmt.Errorf("%w: missing req_seq_id", pluginpayment.ErrCallbackRejected)
	}
	raw := flatten(data)
	if buyer := buyerFrom(data); buyer != "" {
		raw["buyer"] = buyer
	}
	result := pluginpayment.CallbackResult{
		TradeNo:        tradeNo,
		ChannelTradeNo: firstNonEmpty(strings.TrimSpace(rawString(data, "hf_seq_id")), strings.TrimSpace(rawString(data, "out_trans_id"))),
		Paid:           strings.TrimSpace(rawString(data, "trans_stat")) == transStatSuccess,
		Raw:            raw,
		Ack:            ackPrefix + tradeNo,
	}
	if !result.Paid {
		// P/F notifications carry no payable amount.
		return result, nil
	}
	amount, err := pluginpayment.ParseAmount(rawString(data, "trans_amt"))
	if err != nil {
		return pluginpayment.CallbackResult{}, err
	}
	result.Amount = amount
	return result, nil
}

// reqDate derives YYYYMMDD from the first eight digits of the trade number and
// falls back to today when the prefix is not a date.
func reqDate(tradeNo string, now time.Time) string {
	if len(tradeNo) >= 8 {
		candidate := tradeNo[:8]
		digits := true
		for _, char := range candidate {
			if char < '0' || char > '9' {
				digits = false
				break
			}
		}
		if digits {
			return candidate
		}
	}
	return now.Format("20060102")
}

// rawValue wraps a Go string as a JSON string value for the data object.
func rawValue(value string) json.RawMessage {
	return json.RawMessage(jsonString(value))
}

// resolveOpenID looks for the buyer identifier the JSAPI products require.
func resolveOpenID(request pluginpayment.CreateRequest, config map[string]string) string {
	for _, candidate := range []string{
		request.Extra["sub_openid"], request.Extra["openid"],
		config["sub_openid"], config["openid"],
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// flatten renders resp_data as a string map for auditing; nested objects keep
// their raw JSON text.
func flatten(data map[string]json.RawMessage) map[string]string {
	fields := make(map[string]string, len(data))
	for name, raw := range data {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			fields[name] = text
			continue
		}
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		fields[name] = trimmed
	}
	return fields
}

// buyerFrom extracts the payer identity from the family-specific response block.
func buyerFrom(data map[string]json.RawMessage) string {
	for _, wrapper := range []string{"alipay_response", "wx_response"} {
		raw, exists := data[wrapper]
		if !exists {
			continue
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil {
			continue
		}
		if buyer := strings.TrimSpace(firstNonEmpty(rawString(nested, "buyer_id"), rawString(nested, "sub_openid"))); buyer != "" {
			return buyer
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// compile-time assertions
var (
	_ pluginpayment.Plugin           = (*Plugin)(nil)
	_ pluginpayment.Configurable     = (*Plugin)(nil)
	_ pluginpayment.CallbackVerifier = (*Plugin)(nil)
)
