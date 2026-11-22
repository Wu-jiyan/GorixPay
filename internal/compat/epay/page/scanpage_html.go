package page

// 买家支付页（服务端直出，不依赖前端构建产物）。
//
// 结构参考正规收银台的「摘要 + 操作」双栏：左边说清"这笔钱是什么"（支付方式、
// 收款方、金额、客服），右边说清"你要做什么"（扫码 / 唤起 App / 复制链接）。
// 配色仍是本站令牌（浅灰底 / 白卡 / 1px 细边 / 14px 圆角 / 克制阴影 / 无渐变），
// 品牌色只出现在徽标、状态条、主按钮与勾选图标上。
//
// 三种形态：
//   - 电脑：展示二维码，扫了就付；
//   - 手机 + 微信：微信 H5 链接在浏览器里付不了，主操作是「复制链接，去微信付款」；
//   - 手机 + 支付宝：按钮装成 alipays:// 唤起 App，直接落到本笔订单。
const scanPageHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="format-detection" content="telephone=no">
<meta name="color-scheme" content="light">
<title>支付 ¥{{.Amount}}</title>
<style>
:root{
--ink1:#0f172a;--ink2:#475569;--ink3:#64748b;--line:#e2e8f0;--bg:#f5f7fb;--card:#fff;--soft:#f8fafc;
--primary:#2563eb;--disabled:#cbd5e1;
--r-sm:8px;--r-md:12px;--r-lg:14px;
--shadow:0 1px 2px rgba(15,23,42,.04),0 8px 24px rgba(15,23,42,.06);
--card-w:460px;--pad:24px;--btn-h:52px;
}
*{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
html,body{margin:0;padding:0}
body{
background:var(--bg);color:var(--ink1);font-size:15px;line-height:1.6;
font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Hiragino Sans GB","Microsoft YaHei","Helvetica Neue",Arial,sans-serif;
-webkit-font-smoothing:antialiased;padding:18px 16px calc(24px + env(safe-area-inset-bottom));
display:flex;flex-direction:column;align-items:center;
}
.shell{width:100%;max-width:var(--card-w)}
/* 顶部：谁的页面 + 安全标识 */
.brand{display:flex;align-items:center;gap:8px;padding:0 4px 14px}
.brand img{height:22px;width:auto;display:block;border-radius:6px}
.brand-mark{width:22px;height:22px;border-radius:7px;background:var(--primary);position:relative;flex:none}
.brand-mark::after{content:"";position:absolute;inset:7px;border-radius:2px;background:#fff;opacity:.95}
.brand span{font-size:16px;font-weight:600;color:var(--ink1);letter-spacing:-.1px}
.brand-secure{margin-left:auto;display:inline-flex;align-items:center;gap:5px;font-size:11.5px;
color:var(--ink3);border:1px solid var(--line);border-radius:999px;padding:3px 9px;background:var(--card)}
.brand-secure svg{display:block}
.card{background:var(--card);border:1px solid var(--line);border-radius:var(--r-lg);box-shadow:var(--shadow);overflow:hidden}
.statebar{height:3px;background:{{.IconColor}};opacity:.9}
/* 摘要区（桌面左栏 / 手机首屏） */
.summary{padding:var(--pad)}
.payline{display:flex;align-items:center;gap:10px;margin-bottom:14px}
.payline svg{flex:none;display:block}
.pay-name{font-size:14px;font-weight:600;color:var(--ink1)}
.pay-chip{margin-left:auto;font-size:11px;font-weight:600;color:var(--ink3);background:var(--soft);
border:1px solid var(--line);border-radius:999px;padding:3px 9px;white-space:nowrap}
.payee{margin:0;font-size:13px;line-height:1.6;color:var(--ink3)}
.payee b{font-weight:600;color:var(--ink2)}
.title{margin:12px 0 0;font-size:17px;font-weight:600;line-height:1.45;color:var(--ink1)}
.amount{margin:18px 0 0;padding:16px 0;border-top:1px solid var(--line);border-bottom:1px solid var(--line)}
.amount-label{display:block;font-size:12px;color:var(--ink3);letter-spacing:.02em}
.amount-value{margin:2px 0 0;font-size:40px;font-weight:600;line-height:1.1;letter-spacing:-.02em;
color:var(--ink1);font-variant-numeric:tabular-nums}
.amount-value i{font-style:normal;font-size:22px;font-weight:600;margin-right:1px}
.amount-value em{font-style:normal;font-size:.68em;font-weight:600}
.subject{margin:8px 0 0;font-size:15px;line-height:1.5;color:var(--ink2);
display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;word-break:break-all}
/* 客服：只在运营填了联系方式时出现 */
.support{margin:16px 0 0;padding:12px 14px;border:1px solid var(--line);border-radius:var(--r-md);background:var(--soft)}
.support-title{display:block;font-size:12px;color:var(--ink3);margin-bottom:4px}
.support-body{display:flex;align-items:center;gap:8px;flex-wrap:wrap;font-size:13px;color:var(--ink2)}
.support-body b{font-weight:600;color:var(--ink1)}
.support-link{color:var(--primary);text-decoration:none;font-weight:600}
/* 操作区（桌面右栏 / 手机下半） */
.action-col{padding:0 var(--pad) var(--pad);border-top:1px solid var(--line)}
.section-title{margin:20px 0 12px;font-size:15px;font-weight:600;color:var(--ink1)}
/* 桌面二维码区自己会说明怎么扫，recipe 只在手机上陪按钮出现 */
.recipe{margin:-6px 0 12px;font-size:12.5px;line-height:1.7;color:var(--ink3)}
.qr-box{padding:16px;background:var(--soft);border:1px solid var(--line);border-radius:var(--r-md);text-align:center}
.qr{width:188px;margin:0 auto;padding:12px;background:#fff;border:1px solid var(--line);border-radius:var(--r-md)}
.qr svg{display:block;width:100%;height:auto}
.qr-hint{margin:12px 0 0;font-size:12.5px;line-height:1.7;color:var(--ink3)}
.wallet-row{display:flex;align-items:center;justify-content:center;gap:8px;margin-top:10px;flex-wrap:wrap}
.wallet{display:inline-flex;align-items:center;gap:5px;padding:4px 10px;border:1px solid var(--line);
border-radius:999px;background:#fff;font-size:12px;color:var(--ink2)}
.wallet svg{display:block}
.btn{display:flex;align-items:center;justify-content:center;gap:8px;width:100%;height:var(--btn-h);
border:0;border-radius:var(--r-md);font-size:16px;font-weight:600;font-family:inherit;text-decoration:none;
cursor:pointer;transition:filter .12s ease;padding:0 20px;margin-top:14px}
.btn:active{filter:brightness(.94)}
.btn svg{flex:none;display:block}
.btn.primary{background:{{.IconColor}};color:#fff}
.btn.secondary{background:var(--card);color:var(--ink1);border:1px solid var(--line);height:48px}
.btn.ghost{background:var(--card);color:var(--ink2);border:1px solid var(--line);height:46px;font-size:14px}
.btn[aria-disabled="true"]{background:var(--disabled);cursor:not-allowed}
.hint{margin:10px 0 0;font-size:12.5px;line-height:1.65;color:var(--ink3);text-align:center}
/* 支付链接 */
.link{margin-top:18px}
.link-label{display:block;font-size:12px;color:var(--ink3);margin-bottom:6px}
.link-row{display:flex;gap:8px;align-items:stretch}
.link-text{flex:1;min-width:0;display:block;line-height:40px;height:42px;font-size:12.5px;color:var(--ink2);
border:1px solid var(--line);border-radius:var(--r-sm);padding:0 12px;background:var(--soft);
font-family:ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.2px;
white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.link-row button{flex:none;height:42px;padding:0 16px;font-size:13px;font-weight:600;border-radius:var(--r-sm);
border:1px solid var(--line);background:var(--card);color:var(--ink1);cursor:pointer;font-family:inherit}
.link-row button:active{background:#f1f5f9}
/* 信任说明 */
.trust{margin:18px 0 0;padding:0;list-style:none;display:grid;gap:9px}
.trust li{position:relative;padding-left:22px;font-size:12.5px;line-height:1.6;color:var(--ink3)}
.trust li::before{content:"";position:absolute;left:0;top:3px;width:14px;height:14px;border-radius:50%;
background:{{.IconColor}};opacity:.12}
.trust li::after{content:"";position:absolute;left:4px;top:7px;width:6px;height:3px;
border-left:1.6px solid {{.IconColor}};border-bottom:1.6px solid {{.IconColor}};transform:rotate(-45deg)}
/* 明细：两列并排，和正规收银台一样一眼能扫 */
.meta{margin:18px 0 0;padding:0;display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px 18px;
border-top:1px solid var(--line);padding-top:16px}
.row{display:flex;flex-direction:column;gap:2px;min-width:0}
.key{margin:0;font-size:12px;color:var(--ink3)}
.val{margin:0;font-size:13.5px;color:var(--ink1);word-break:break-all}
.val.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;
font-variant-numeric:tabular-nums;letter-spacing:.2px}
.status{margin:12px 0 0;min-height:20px;text-align:center;font-size:13px;line-height:1.6;color:var(--ink2)}
.status.error{color:#dc2626}
.status.ok{color:{{.IconColor}};font-weight:600}
.foot{margin:16px auto 0;max-width:360px;text-align:center;font-size:12px;line-height:1.75;color:#94a3b8}
.foot a{color:var(--ink3);text-decoration:none}
/* 桌面：摘要 + 操作双栏。间距比手机紧一档，让 720/768 高度的笔记本也能一屏放下 */
@media (min-width:860px){
  :root{--card-w:900px;--pad:26px;--btn-h:48px}
  body{padding-top:28px}
  .brand{padding-bottom:11px}
  .card{display:grid;grid-template-columns:320px minmax(0,1fr)}
  .summary{background:var(--soft)}
  .statebar{grid-column:1 / -1}
  .action-col{border-top:0;border-left:1px solid var(--line);padding:var(--pad)}
  .section-title{margin-top:0;margin-bottom:10px}
  .recipe{display:none}
  .amount{margin-top:14px;padding:12px 0}
  .amount-value{font-size:38px}
  .title{margin-top:10px;font-size:16px}
  .qr-box{padding:12px}
  .qr{width:164px;padding:10px}
  .qr-hint{margin-top:9px}
  .link{margin-top:14px}
  .trust{margin-top:14px;gap:7px}
  .meta{margin-top:14px;padding-top:13px;gap:11px 18px}
  .btn.ghost{margin-top:12px}
  .foot{margin-top:12px;max-width:520px}
}
@media (max-width:520px){
  .meta{grid-template-columns:1fr;gap:12px}
}
/* 手机上左栏标题已经说完「这是笔什么支付」，右栏再写一遍动作会与按钮文字撞车；
   右栏只留一句说明 + 按钮。桌面上右栏的「扫码完成支付」是必要信息，保留。 */
@media (max-width:859px){
  .action-col .section-title{display:none}
  .recipe{margin-top:0}
}
</style></head>
<body>
<div class="shell">
  <div class="brand">
    {{if .LogoURL}}<img src="{{.LogoURL}}" alt="{{.SiteName}}">{{else}}<span class="brand-mark"></span>{{end}}
    <span>{{if .SiteName}}{{.SiteName}}{{else}}收银台{{end}}</span>
    <span class="brand-secure">
      <svg width="11" height="11" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 1 3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11z" fill="#94a3b8"/></svg>
      安全支付
    </span>
  </div>

  <div class="card" id="card">
    <div class="statebar"></div>

    <div class="summary">
      <div class="payline">
        <svg width="22" height="22" viewBox="0 0 24 24" role="img" aria-label="{{.Brand}}"><path d="{{.IconPath}}" fill="{{.IconColor}}"/></svg>
        <span class="pay-name">{{.Brand}}</span>
        <span class="pay-chip">{{.StatusText}}</span>
      </div>
      {{if .MerchantName}}<p class="payee">收款方：<b>{{.MerchantName}}</b></p>{{end}}
      <h1 class="title">{{.Title}}</h1>

      <div class="amount">
        <span class="amount-label">应付金额</span>
        <p class="amount-value"><i>¥</i>{{.AmountInt}}<em>{{.AmountDec}}</em></p>
        <p class="subject">{{.Name}}</p>
      </div>

      <ul class="trust">
        <li>款项直接结算至商户账户，平台不经手你的资金。</li>
        <li>本页面全程 HTTPS 加密传输，不保存你的支付密码。</li>
        <li>付款后请停留在本页，页面会自动更新支付状态。</li>
      </ul>

      {{if or .ContactQQ .ContactEmail}}
      <div class="support">
        <span class="support-title">支付遇到问题？</span>
        <div class="support-body">
          {{if .ContactQQ}}<span>客服 QQ <b>{{.ContactQQ}}</b></span>{{end}}
          {{if and .ContactQQ .ContactEmail}}<span>·</span>{{end}}
          {{if .ContactEmail}}<a class="support-link" href="mailto:{{.ContactEmail}}">{{.ContactEmail}}</a>{{end}}
        </div>
      </div>
      {{end}}
    </div>

    <div class="action-col">
      <h2 class="section-title">{{.ActionTitle}}</h2>

      {{if .Desktop}}
        {{if .QR}}
        <div class="qr-box">
          <div class="qr">{{.QR}}</div>
          <p class="qr-hint">打开微信或支付宝「扫一扫」完成付款<br>请勿向他人出示此二维码</p>
          <div class="wallet-row">
            <span class="wallet">
              <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true"><path d="M8.7 4.5C4.9 4.5 1.9 6.9 1.9 9.8c0 1.7.9 3.2 2.5 4.3l-.6 1.8 2.1-1.1c.7.2 1.5.3 2.3.3h.5a5 5 0 0 1-.2-1.4c0-2.9 3-5.3 6.6-5.3h.4c-.6-2.1-3.2-3.9-6.8-3.9Zm-2.3 3.1a.9.9 0 1 1 0 1.8.9.9 0 0 1 0-1.8Zm4.6 0a.9.9 0 1 1 0 1.8.9.9 0 0 1 0-1.8Z" fill="#07C160"/><path d="M22.1 13.6c0-2.4-2.4-4.4-5.4-4.4s-5.4 2-5.4 4.4 2.4 4.4 5.4 4.4c.6 0 1.2-.1 1.7-.2l1.7.9-.5-1.5c1.5-.9 2.5-2.2 2.5-3.6Zm-7.1-1.4a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Zm3.7 0a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Z" fill="#07C160"/></svg>
              微信支付
            </span>
            <span class="wallet">
              <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true"><path d="{{.IconPath}}" fill="#1677FF"/></svg>
              支付宝
            </span>
          </div>
        </div>
        {{end}}
      {{else if .IsWeChat}}
        <button class="btn primary" type="button" onclick="copyLink(this)">
          <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="M16 1H4a2 2 0 0 0-2 2v14h2V3h12zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2zm0 16H8V7h11z" fill="#fff"/></svg>
          <span>复制链接，去微信付款</span>
        </button>
        <a class="btn secondary" href="{{.PrimaryHref}}">
          <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="{{.IconPath}}" fill="{{.IconColor}}"/></svg>
          <span>打开微信</span>
        </a>
        <p class="hint">复制链接后打开微信，粘贴发送给「文件传输助手」即可付款。</p>
      {{else if .IsAlipay}}
        <a class="btn primary" href="{{.PrimaryHref}}" data-open="1" style="margin-top:0">
          <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="{{.IconPath}}" fill="#fff"/></svg>
          <span>打开支付宝付款</span>
        </a>
        <p class="hint">点击后会自动唤起支付宝并进入本笔订单；没打开就用下方链接。</p>
      {{else}}
        {{if .QR}}<div class="qr-box"><div class="qr">{{.QR}}</div></div>{{end}}
        <a class="btn primary" href="{{.PrimaryHref}}" style="margin-top:0">打开支付页</a>
        <p class="hint">也可以复制下方链接，在对应的 App 里打开。</p>
      {{end}}

      <div class="link">
        <span class="link-label">{{if .Desktop}}支付链接{{else}}支付链接（复制到 {{.Brand}} 打开）{{end}}</span>
        <div class="link-row">
          <span class="link-text" id="paylink" role="textbox" aria-readonly="true" aria-label="支付链接">{{.RawURL}}</span>
          <input type="hidden" id="paylinkfull" value="{{.RawURL}}">
          <button type="button" onclick="copyLink(this)">复制</button>
        </div>
      </div>

      <dl class="meta">
        <div class="row"><dt class="key">订单号</dt><dd class="val mono">{{.TradeNo}}</dd></div>
        <div class="row"><dt class="key">支付方式</dt><dd class="val">{{.Brand}}</dd></div>
        <div class="row"><dt class="key">下单时间</dt><dd class="val">{{.CreatedText}}</dd></div>
        <div class="row"><dt class="key">订单状态</dt><dd class="val" id="statetext">{{.StatusText}}</dd></div>
      </dl>

      <div class="status" id="status"></div>

      <button class="btn ghost" type="button" onclick="confirmPaid(this)">我已完成支付</button>
    </div>
  </div>

  <div class="foot">
    本页金额与商品以商户页面为准，如有疑问请联系收款方<br>
    由 {{if .SiteName}}{{.SiteName}}{{else}}本平台}}{{end}} 提供支付服务
  </div>
</div>
<script>
var defaultCopyLabel='复制';
function copyLink(trigger){
  var input=document.getElementById('paylinkfull');
  var value=input?input.value:'';
  var label=trigger?trigger.querySelector('span'):null;
  var original=label?label.textContent:(trigger?trigger.textContent:defaultCopyLabel);
  var flash=function(){
    if(label){label.textContent='已复制，请到微信粘贴';}
    else if(trigger){trigger.textContent='已复制';}
    setTimeout(function(){
      if(label){label.textContent=original;}
      else if(trigger){trigger.textContent=original;}
    },2000);
  };
  var fallback=function(){
    var area=document.createElement('textarea');
    area.value=value;area.setAttribute('readonly','');
    area.style.position='fixed';area.style.opacity='0';
    document.body.appendChild(area);area.select();
    try{document.execCommand('copy');flash();}catch(e){}
    document.body.removeChild(area);
  };
  if(navigator.clipboard&&navigator.clipboard.writeText){
    navigator.clipboard.writeText(value).then(flash,fallback);
  }else{fallback();}
}
var opener=document.querySelector('[data-open="1"]');
if(opener){
  opener.addEventListener('click',function(){
    var label=opener.querySelector('span');
    setTimeout(function(){
      if(document.hidden){return;}
      if(label){label.textContent='正在打开支付宝…';}
      opener.setAttribute('aria-disabled','true');
      opener.style.pointerEvents='none';
      setTimeout(function(){
        if(label){label.textContent='打开支付宝付款';}
        opener.removeAttribute('aria-disabled');
        opener.style.pointerEvents='';
      },2500);
    },120);
  });
}
var statusEl=document.getElementById('status');
var statetext=document.getElementById('statetext');
// 一屏放下：内容比视口高时按比例整体缩放（不改字号层级、不改结构），
// 保证底部那两行说明永远可见，而不是被折到视口外让人以为丢了。
// 只在桌面生效 —— 手机上纵向滚动是正常交互，缩放反而伤可读性。
var shell=document.querySelector('.shell');
function fitToViewport(){
  if(!shell){return;}
  shell.style.zoom='';
  if(window.innerWidth<860){return;}
  // 以「真实是否还溢出」为准逐步收，而不是一次性算比例：Chrome 在 zoom 之后
  // 报告的尺寸口径并不统一，算一次容易差几像素，底部那行照样被切。
  var zoom=1;
  for(var step=0; step<8; step++){
    if(document.documentElement.scrollHeight<=window.innerHeight){break;}
    zoom=Math.max(0.7, zoom-0.035);
    shell.style.zoom=zoom.toFixed(3);
    if(zoom<=0.7){break;}
  }
}
fitToViewport();
window.addEventListener('resize',function(){clearTimeout(fitToViewport._timer);fitToViewport._timer=setTimeout(fitToViewport,120);});
window.addEventListener('load',fitToViewport);
if(document.fonts&&document.fonts.ready){document.fonts.ready.then(fitToViewport);}
// 主动确认：先查一次状态再决定去哪儿。已付才跳，没付只提示，
// 这样点了不会瞬间被弹回原页面（那正是「返回商户」按钮被删掉的原因）。
function confirmPaid(trigger){
  trigger.setAttribute('aria-disabled','true');
  fetch('{{.PollURL}}',{credentials:'omit',cache:'no-store'})
    .then(function(r){return r.json();})
    .then(function(d){
      if(d&&d.paid){window.location.replace('{{.DoneURL}}');return;}
      statusEl.className='status';
      statusEl.textContent='暂未收到付款，支付成功后本页会自动跳转';
      trigger.removeAttribute('aria-disabled');
    })
    .catch(function(){
      statusEl.className='status error';
      statusEl.textContent='状态查询失败，请稍后重试';
      trigger.removeAttribute('aria-disabled');
    });
}
// 轮询：付完自动走，和上游 wxpay_wap 的 loadmsg() 一样
var pollTimer=setInterval(function(){
  fetch('{{.PollURL}}',{credentials:'omit',cache:'no-store'})
    .then(function(r){return r.json();})
    .then(function(d){
      if(d&&d.paid){clearInterval(pollTimer);
        if(statetext){statetext.textContent='已支付';}
        statusEl.className='status ok';
        statusEl.textContent='支付成功，正在返回商户…';
        window.location.replace('{{.PaidURL}}');}
    }).catch(function(){});
},2000);
</script>
</body></html>`
