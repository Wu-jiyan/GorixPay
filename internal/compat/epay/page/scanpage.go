package page

import (
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
}

func (handler *Handler) scanPageData(order model.Order, content, device string) scanPage {
	page := scanPage{
		Brand:    scanBrand(order.PaymentType),
		IsWeChat: isWeChatPayment(order.PaymentType),
		IsMobile: device != "pc",
		Amount:   formatAmount(order.Amount),
		Name:     strings.TrimSpace(order.Name),
		TradeNo:  order.TradeNo,
		DoneURL:  handler.siteURL + "/pay/checkpay/" + url.PathEscape(order.TradeNo) + "/",
		PaidURL:  handler.siteURL + "/pay/return/" + url.PathEscape(order.TradeNo) + "/",
		PollURL:  handler.siteURL + "/getshop.php?act=poll&trade_no=" + url.QueryEscape(order.TradeNo),
		RawURL:   content,
	}
	// An image payload (data:image/...) is not a link the buyer can copy into a
	// chat window; upstream checks the same prefix (wxpay_qrcode.php).
	if !strings.HasPrefix(content, "data:image/") {
		page.QR = template.HTML(renderQRSVG(content)) //nolint:gosec // fixed SVG builder
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

func isWeChatPayment(paymentType string) bool {
	normalized := strings.ToLower(paymentType)
	return strings.Contains(normalized, "wx") || strings.Contains(normalized, "wechat")
}

// scanPageTemplate keeps the page dependency free — no CDN, no framework — while
// matching the upstream flow: show the code, offer the copy/open-WeChat route for
// the cases where the code cannot be scanned, and poll the order so the browser
// leaves on its own once the money arrives.
var scanPageTemplate = template.Must(template.New("scan").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no">
<title>{{.Brand}}扫码支付</title>
<style>
:root{color-scheme:light}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:16px;
background:#f5f7fa;color:#111827;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif}
.card{background:#fff;border-radius:16px;box-shadow:0 8px 30px rgba(15,23,42,.08);padding:24px;width:100%;max-width:380px;text-align:center}
.brand{display:inline-flex;align-items:center;gap:6px;font-size:13px;color:#374151;background:#f3f4f6;border-radius:999px;padding:4px 12px;margin-bottom:14px}
.amount{font-size:34px;font-weight:600;letter-spacing:-.5px;margin:0}
.amount small{font-size:16px;font-weight:500;margin-right:2px}
.name{color:#6b7280;font-size:13px;margin:6px 0 0;word-break:break-all}
.qr{margin:18px auto 0;width:min(220px,64vw)}
.qr svg{display:block;width:100%;height:auto}
.tip{color:#6b7280;font-size:13px;margin:12px 0 0}
.actions{display:flex;gap:10px;margin-top:18px}
.actions a{flex:1;text-decoration:none;border-radius:10px;padding:11px 12px;font-size:14px;font-weight:500;border:1px solid #e5e7eb;color:#374151;background:#fff}
.actions a.primary{background:#2563eb;border-color:#2563eb;color:#fff}
.copyblock{margin-top:18px;border-top:1px solid #f1f3f5;padding-top:16px;text-align:left}
.copyblock .label{font-size:12px;color:#9ca3af;margin-bottom:6px}
.linkrow{display:flex;gap:8px;align-items:center}
.linkrow input{flex:1;min-width:0;font-size:12px;color:#374151;border:1px solid #e5e7eb;border-radius:8px;padding:8px 10px;background:#fafafa}
.linkrow button{white-space:nowrap;font-size:12px;padding:8px 12px;border-radius:8px;border:1px solid #2563eb;background:#2563eb;color:#fff;cursor:pointer}
.recipe{font-size:12px;color:#9ca3af;line-height:1.7;margin-top:10px}
.detail{margin-top:16px;border-top:1px solid #f1f3f5;padding-top:12px;text-align:left;font-size:12px;color:#6b7280;line-height:1.9}
.detail b{color:#9ca3af;font-weight:400;display:inline-block;min-width:56px}
.status{font-size:12px;color:#2563eb;margin-top:14px;min-height:16px}
</style></head>
<body><div class="card">
<div class="brand">{{.Brand}}</div>
<div class="amount"><small>￥</small>{{.Amount}}</div>
<p class="name">{{.Name}}</p>
{{if .QR}}<div class="qr">{{.QR}}</div>{{end}}
{{if .IsMobile}}
<p class="tip">{{if .IsWeChat}}请长按识别二维码，或用微信打开下面的链接完成支付{{else}}请在上方选择支付方式完成付款{{end}}</p>
<div class="actions">
{{if .IsWeChat}}<a class="primary" href="weixin://">打开微信</a>{{end}}
<a href="{{.DoneURL}}">我已完成支付</a>
</div>
{{else}}
<p class="tip">请使用{{.Brand}}扫描二维码完成支付</p>
<div class="actions">
<a href="{{.RawURL}}">当前浏览器打开</a>
<a href="{{.DoneURL}}">我已完成支付</a>
</div>
{{end}}
<div class="copyblock">
<div class="label">支付链接</div>
<div class="linkrow">
<input id="paylink" readonly value="{{.RawURL}}">
<button type="button" onclick="copyLink()">复制</button>
</div>
<div class="recipe">{{if .IsWeChat}}操作流程：复制链接 → 在微信顶部搜索框搜到自己的微信 → 打开聊天框 → 粘贴并发送 → 点击蓝色链接 → 完成付款。{{else}}如果二维码打不开，复制上面的链接在手机浏览器里打开。{{end}}</div>
</div>
<div class="detail">
<div><b>订单号</b>{{.TradeNo}}</div>
<div><b>商品</b>{{.Name}}</div>
</div>
<div class="status" id="status"></div>
</div>
<script>
var copyButton = document.querySelector('button');
function copyLink() {
  var input = document.getElementById('paylink');
  input.select();
  input.setSelectionRange(0, 99999);
  var done = function () { copyButton.textContent = '已复制'; setTimeout(function () { copyButton.textContent = '复制'; }, 1500); };
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(input.value).then(done, fallback);
  } else { fallback(); }
  function fallback() {
    try { document.execCommand('copy'); done(); } catch (error) { input.select(); }
  }
}
// 每 2 秒问一次订单状态，付完自动走掉，和上游 wxpay_wap 的 loadmsg() 一样。
var pollTimer = setInterval(function () {
  fetch('{{.PollURL}}', { credentials: 'omit', cache: 'no-store' })
    .then(function (response) { return response.json(); })
    .then(function (data) {
      if (data && data.paid) {
        clearInterval(pollTimer);
        document.getElementById('status').textContent = '支付成功，正在跳转…';
        window.location.replace('{{.PaidURL}}');
      }
    })
    .catch(function () {});
}, 2000);
document.addEventListener('visibilitychange', function () {
  if (!document.hidden) { /* 回到页面立刻问一次 */ }
});
</script>
</body></html>`))