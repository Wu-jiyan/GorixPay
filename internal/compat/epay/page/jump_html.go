package page

// 跳转页与扫码页同一套视觉语言（站点令牌 + 品牌色点缀）。它只出现在通道返回 jump
// 结果时：上游那边是一句 <script>location.replace</script>，这里补上手动入口和
// 「换个支付方式」，免得通道拒绝后买家回不去。
const jumpPageHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>正在前往{{.Brand}} · ¥{{.Amount}}</title>
<style>
:root{color-scheme:light;--c:#2563eb;--ink1:#1f2937;--ink2:#475569;--ink3:#64748b;--line:#e2e8f0;--bg:#f5f7fb;--surface:#fff;--soft:#f8fafc;
--radius-sm:8px;--radius:10px;--radius-lg:14px;--shadow:0 1px 2px rgba(15,23,42,.04),0 8px 24px rgba(15,23,42,.06)}
body.t-wechat{--c:#07c160}
body.t-alipay{--c:#1677ff}
*{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
html,body{margin:0}
body{min-height:100vh;background:var(--bg);color:var(--ink1);font-size:14px;line-height:1.6;
font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;
-webkit-font-smoothing:antialiased;padding-bottom:max(20px,env(safe-area-inset-bottom))}
.top{border-bottom:1px solid var(--line);background:var(--surface)}
.top-inner{max-width:460px;margin:0 auto;padding:12px 16px;display:flex;align-items:center;gap:8px}
.dot{width:18px;height:18px;border-radius:6px;background:var(--c);flex:none;position:relative}
.dot::after{content:"";position:absolute;inset:5px;border-radius:2px;background:#fff;opacity:.9}
.top-name{font-weight:600;font-size:14px}
.top-badge{margin-left:auto;font-size:12px;color:var(--ink3);border:1px solid var(--line);border-radius:999px;padding:2px 9px}
main{max-width:460px;margin:0 auto;padding:14px 16px 0}
.card{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-lg);box-shadow:var(--shadow);padding:18px}
.payline{display:flex;align-items:center;gap:8px}
.paymark{width:24px;height:24px;border-radius:7px;background:var(--c);flex:none;position:relative}
.paymark::after{content:"";position:absolute;inset:7px;border-radius:3px;background:#fff;opacity:.92}
.payname{font-size:14px;font-weight:600}
.paytag{margin-left:auto;font-size:11px;color:var(--c);background:#f1f5f9;border-radius:999px;padding:2px 8px;font-weight:500}
.amount{margin:16px 0 0;font-size:34px;font-weight:600;letter-spacing:-.6px;line-height:1.15;
font-variant-numeric:tabular-nums}
.amount small{font-size:17px;font-weight:600;margin-right:2px;color:var(--ink2)}
.subject{margin:4px 0 0;font-size:13px;color:var(--ink3);word-break:break-all}
.hr{height:1px;background:var(--line);margin:16px 0}
.hint{margin:0 2px 0;font-size:12.5px;color:var(--ink2);line-height:1.75}
.btn{display:block;text-align:center;text-decoration:none;border-radius:var(--radius);padding:13px 16px;font-size:15px;
font-weight:600;margin-top:12px;transition:opacity .12s ease}
.btn:active{opacity:.88}
.btn.primary{background:var(--c);color:#fff}
.btn.ghost{background:var(--surface);color:var(--ink2);border:1px solid var(--line);font-weight:500;font-size:14px;padding:11px 16px}
.link{display:block;text-align:center;margin-top:12px;font-size:12.5px;color:var(--ink3);text-decoration:none}
.row{display:flex;gap:12px;padding:9px 0;border-bottom:1px solid var(--line);font-size:12.5px}
.row:last-child{border-bottom:0}
.row dt{flex:none;width:78px;margin:0;color:var(--ink3)}
.row dd{flex:1;margin:0;text-align:right;color:var(--ink2);word-break:break-all}
.row dd.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}
.trust{margin:14px 2px 0;padding:0;list-style:none;display:grid;gap:8px}
.trust li{display:flex;gap:8px;font-size:12px;color:var(--ink3);line-height:1.6;position:relative}
.trust li::before{content:"";flex:none;width:14px;height:14px;margin-top:2px;border-radius:50%;border:1px solid var(--line);background:var(--surface)}
.trust li::after{content:"";position:absolute;width:5px;height:3px;border-left:1.4px solid var(--c);
border-bottom:1.4px solid var(--c);transform:rotate(-45deg);margin:6px 0 0 4px}
.foot{margin:16px 2px 0;text-align:center;font-size:11.5px;color:#94a3b8;line-height:1.8}
@media (min-width:600px){main{padding-top:24px}.card{padding:22px}}
</style></head>
<body class="{{.Theme}}">
<div class="top"><div class="top-inner">
  <span class="dot"></span><span class="top-name">收银台</span><span class="top-badge">安全支付</span>
</div></div>
<main>
  <div class="card">
    <div class="payline">
      <span class="paymark"></span>
      <span class="payname">{{.Brand}}</span>
      <span class="paytag">官方通道</span>
    </div>
    <div class="amount"><small>¥</small>{{.Amount}}</div>
    <div class="subject">{{.Name}}</div>
    <div class="hr"></div>
    <p class="hint">正在为你打开{{.Brand}}收银页，如未自动跳转，请点下方按钮继续。</p>
    <a class="btn primary" href="{{.Target}}">继续支付</a>
    <a class="btn ghost" href="{{.DoneURL}}">我已完成支付</a>
    <a class="link" href="{{.BackURL}}">换个支付方式</a>
  </div>
  <dl class="card" style="margin-top:12px">
    <div class="row"><dt>商品</dt><dd>{{.Name}}</dd></div>
    <div class="row"><dt>订单号</dt><dd class="mono">{{.TradeNo}}</dd></div>
  </dl>
  <ul class="trust">
    <li>全程 HTTPS 加密传输，支付信息不经过第三方留存</li>
    <li>付款实时到账商户，支付结果自动同步</li>
  </ul>
  <div class="foot">本页面由支付平台提供，仅用于本次交易</div>
</main>
<script>setTimeout(function(){window.location.replace({{printf "%q" .Target}});},600);</script>
</body></html>`
