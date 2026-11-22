package page

import (
	"context"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"

	"gorixpay/internal/payment/model"
)

// scanPage is the data behind the per-channel scan page. The layout follows
// upstream's includes/pages templates: wxpay_qrcode.php for a desktop (code plus
// a "copy the link into WeChat" recipe) and wxpay_wap.php for a phone (code plus
// an "open WeChat" button, because a phone cannot scan its own screen).
type scanPage struct {
	Brand    string // 微信支付 / 支付宝 / 银联 …
	IsWeChat bool   // 微信类支付方式：需要微信 App
	IsAlipay bool   // 支付宝类支付方式：需要支付宝 App
	IsMobile bool
	Amount   string
	Name     string
	TradeNo  string
	QR       template.HTML // inline SVG; trusted, produced by renderQRSVG
	DoneURL  string        // /pay/checkpay/<trade_no>/ — manual "已支付" check
	PaidURL  string        // /pay/return/<trade_no>/ — auto redirect after payment
	PollURL  string        // /getshop.php?act=poll&trade_no=… — order status JSON
	RawURL   string        // the channel URL, shown verbatim for copying
	SiteName string
	// Theme picks the hero gradient: t-wechat / t-alipay / t-default.
	Theme string
	// PrimaryHref is what the big button opens on a phone. For 支付宝 it is the
	// channel url itself, because that url wakes the 支付宝 app and lands on this
	// payment; for 微信 it is the app scheme, since an H5 url cannot pay outside
	// the WeChat browser and the buyer has to carry the link over by hand.
	// template.URL keeps html/template from rewriting the weixin:// scheme to
	// #ZgotmplZ.
	PrimaryHref  template.URL
	PrimaryLabel string
	Recipe       string // step-by-step for the copy-the-link route
	// CreatedText / ExpiresAtMS feed the "transparent order" block: when the order
	// was created and how long it stays payable.
	CreatedText string
	ExpiresAtMS int64
	// TotalMS is the whole payable window, so the page can draw a countdown bar
	// instead of only printing mm:ss.
	TotalMS int64
	// StatusText is the small state chip on the card (待支付 / 已支付 / 已超时).
	StatusText string
	// Desktop picks the QR layout; on a phone the page offers either the
	// copy-the-link recipe (微信) or a button that wakes the wallet (支付宝).
	Desktop   bool
	IconPath  template.HTMLAttr
	IconColor string
	// MerchantName is the payee shown above the amount ("谁在收钱"), which is the
	// first thing a buyer looks for on a page they did not open themselves.
	MerchantName string
	// Title is the one-line instruction for this device/wallet combination.
	Title string
	// AmountInt / AmountDec split the amount so the decimals can be set smaller.
	AmountInt string
	AmountDec string
	// LogoURL is the configured site logo; empty means the wordmark stands alone
	// (a drawn placeholder would read as a mock-up, not a brand).
	LogoURL string
}

func (handler *Handler) scanPageData(ctx context.Context, order model.Order, content, device string) scanPage {
	// 微信支付的二维码和"复制链接"都指向站内页，和上游一样（Epay huifu_plugin.php
	// wxpay() 给的 code_url 是 /pay/wxwappay/{trade_no}/）：只有站内页才知道打开它的是
	// 不是微信，进而换 T_JSAPI；通道的 H5 地址直接发到聊天框里，在 App 内打不开。
	qrContent := content
	if isWeChatPayment(order.PaymentType) && isWebURL(content) {
		qrContent = handler.siteURL + "/pay/wxwappay/" + url.PathEscape(order.TradeNo) + "/"
	}
	page := scanPage{
		Brand:    scanBrand(order.PaymentType),
		IsWeChat: isWeChatPayment(order.PaymentType),
		IsAlipay: isAlipayPayment(order.PaymentType),
		IsMobile: device != "pc",
		Amount:   formatAmount(order.Amount),
		Name:     strings.TrimSpace(order.Name),
		TradeNo:  order.TradeNo,
		DoneURL:  handler.siteURL + "/pay/checkpay/" + url.PathEscape(order.TradeNo) + "/",
		PaidURL:  handler.siteURL + "/pay/return/" + url.PathEscape(order.TradeNo) + "/",
		PollURL:  handler.siteURL + "/getshop.php?act=poll&trade_no=" + url.QueryEscape(order.TradeNo),
		RawURL:   qrContent,
		SiteName: handler.siteDisplayName(ctx),
		Theme:    scanTheme(order.PaymentType),
		// 透明清单：让买家看到这笔单子是什么时候建的、还有多久能付。
		CreatedText:  order.CreatedAt.Local().Format("2006-01-02 15:04:05"),
		Desktop:      device == "pc",
		IconPath:     template.HTMLAttr(brandIconFor(order.PaymentType).Path), //nolint:gosec // static brand path
		IconColor:    brandIconFor(order.PaymentType).Hex,
		MerchantName: handler.merchantName(ctx, order.MerchantID),
		AmountInt:    amountInt(formatAmount(order.Amount)),
		AmountDec:    amountDec(formatAmount(order.Amount)),
		LogoURL:      handler.siteLogo(ctx),
	}
	if !order.ExpiresAt.IsZero() {
		page.ExpiresAtMS = order.ExpiresAt.UnixMilli()
		if !order.CreatedAt.IsZero() {
			page.TotalMS = order.ExpiresAt.Sub(order.CreatedAt).Milliseconds()
		}
	}
	page.StatusText = "待支付"
	switch {
	case page.IsWeChat:
		page.Title = "在微信中完成支付"
		// 微信的 H5 链接离开微信就付不了，所以主动作是复制；「打开微信」只是把人
		// 送到微信，链接还得自己粘过去（上游 wxpay_wap 的按钮也是 weixin:// 本体）。
		page.PrimaryHref = template.URL("weixin://")
		page.PrimaryLabel = "打开微信"
		page.Recipe = "复制后打开微信，粘贴发送给「文件传输助手」或好友，点开这条链接即可付款。"
	case page.IsAlipay:
		// 支付宝：把支付链接装进 alipays:// 的 url 参数，点一下由系统唤起支付宝 App
		// 并直接落在本笔订单上。直接把通道的 H5 地址当 href 只会打开一个网页，
		// 不会唤起 App —— 这是「点了没反应、跳去通道页」的原因。
		page.Title = "使用支付宝完成支付"
		page.PrimaryHref = template.URL(alipayOpenScheme(qrContent)) //nolint:gosec // built from the channel url
		page.PrimaryLabel = "打开支付宝付款"
		page.Recipe = "没打开支付宝时，复制链接在支付宝或手机浏览器里粘贴打开。"
	default:
		page.Title = "完成支付"
		page.PrimaryHref = template.URL(qrContent) //nolint:gosec // channel url
		page.PrimaryLabel = "打开支付页"
		page.Recipe = "如果二维码打不开，复制上面的链接在手机浏览器里打开。"
	}
	if page.Desktop {
		page.Title = "请扫码完成支付"
	}
	// An image payload (data:image/...) is not a link the buyer can copy into a
	// chat window; upstream checks the same prefix (wxpay_qrcode.php).
	if !strings.HasPrefix(qrContent, "data:image/") {
		page.QR = template.HTML(renderQRSVG(qrContent)) //nolint:gosec // fixed SVG builder
	}
	return page
}

// scanBrand names the payment method the way the page title should read.
func scanBrand(paymentType string) string {
	normalized := strings.ToLower(paymentType)
	switch {
	case strings.Contains(normalized, "wx"), strings.Contains(normalized, "wechat"):
		return "微信支付"
	case strings.Contains(normalized, "alipay"):
		return "支付宝"
	case strings.Contains(normalized, "union"), strings.Contains(normalized, "bank"):
		return "银联"
	default:
		return "扫码支付"
	}
}

// siteLogo is the configured logo url, or "" so the page shows the wordmark alone.
func (handler *Handler) siteLogo(ctx context.Context) string {
	return handler.settingValue(ctx, "site.logo_url")
}

// settingValue reads one system_settings row, unwrapping the JSON string the
// console writes. It answers "" for a missing database or a missing key.
func (handler *Handler) settingValue(ctx context.Context, key string) string {
	if handler.db == nil {
		return ""
	}
	var raw string
	if err := handler.db.WithContext(ctx).Table("system_settings").
		Select("setting_value").Where("setting_key = ?", key).Limit(1).Scan(&raw).Error; err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(raw)
	var decoded string
	if json.Unmarshal([]byte(trimmed), &decoded) == nil {
		trimmed = strings.TrimSpace(decoded)
	}
	return trimmed
}

// alipayOpenScheme wraps a payment url in 支付宝's "open this page in the app"
// scheme: appId=20000067 is the built-in browser. Opening the raw channel url in a
// mobile browser only renders a web page; this scheme is what actually wakes the
// app and lands on the payment.
func alipayOpenScheme(payURL string) string {
	target := strings.TrimSpace(payURL)
	if target == "" {
		return "alipays://"
	}
	return "alipays://platformapi/startApp?appId=20000067&url=" + url.QueryEscape(target)
}

// merchantName is the payee's display name, shown above the amount. An empty
// answer drops the line instead of inventing one.
func (handler *Handler) merchantName(ctx context.Context, merchantID string) string {
	if handler.db == nil || strings.TrimSpace(merchantID) == "" {
		return ""
	}
	var name string
	err := handler.db.WithContext(ctx).Table("merchants").
		Select("display_name").Where("id = ?", merchantID).Limit(1).Scan(&name).Error
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

// amountInt / amountDec split "12.34" so the page can render the decimals smaller.
func amountInt(amount string) string {
	if index := strings.IndexByte(amount, '.'); index >= 0 {
		return amount[:index]
	}
	return amount
}

func amountDec(amount string) string {
	if index := strings.IndexByte(amount, '.'); index >= 0 {
		return amount[index:]
	}
	return ""
}

// siteDisplayName is the headline on the payment page: the configured site name,
// falling back to the bare host so the page never looks unfinished.
func (handler *Handler) siteDisplayName(ctx context.Context) string {
	if name := handler.settingValue(ctx, "site.name"); name != "" {
		return name
	}
	return siteHost(handler.siteURL)
}

// siteHost is the bare host of the public URL, used in the page footer.
func siteHost(publicURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

// scanTheme names the hero colour band: the brand colour of the wallet, or a
// neutral blue for anything else.
func scanTheme(paymentType string) string {
	switch {
	case isWeChatPayment(paymentType):
		return "t-wechat"
	case isAlipayPayment(paymentType):
		return "t-alipay"
	default:
		return "t-default"
	}
}

func isWeChatPayment(paymentType string) bool {
	normalized := strings.ToLower(paymentType)
	return strings.Contains(normalized, "wx") || strings.Contains(normalized, "wechat")
}

// isAlipayPayment names the 支付宝 family, which needs the 支付宝 app for the same
// reason 微信 needs WeChat: the link only pays inside it.
func isAlipayPayment(paymentType string) bool {
	return strings.Contains(strings.ToLower(paymentType), "alipay")
}

// jumpPage is the data behind the "carrying the buyer to the channel" page that
// upstream renders for a jump result instead of answering with a 302.
type jumpPage struct {
	Brand     string
	IsWeChat  bool
	Amount    string
	Name      string
	TradeNo   string
	Target    string // the channel url the browser should open
	BackURL   string // cashier, so the buyer can pick another method
	DoneURL   string // /pay/checkpay/<trade_no>/ — "我已支付" check
	ExpiresAt int64  // unused today, kept so the template reads like the others
	Theme     string // t-wechat / t-alipay / t-default
}

// jumpPageTemplate mirrors Epay/includes/lib/Payment.php:60-72: upstream echoes a
// script that replaces the location. The page adds a manual link and a way back,
// because a 302 would throw away the cashier once the channel answers "no".
var jumpPageTemplate = template.Must(template.New("jump").Parse(jumpPageHTML))

// scanPageTemplate keeps the page dependency free — no CDN, no framework — while
// matching the upstream flow: show the code, offer the copy/open-the-app route for
// the cases where the code cannot be scanned, and poll the order so the browser
// leaves on its own once the money arrives.
var scanPageTemplate = template.Must(template.New("scan").Parse(scanPageHTML))
