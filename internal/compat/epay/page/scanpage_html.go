package page

// 买家支付页（服务端直出，不依赖前端构建产物）。
//
// 视觉规范取自 Stripe Checkout 的分层外观、支付宝收银台的元素分级与微信支付二维码规范，
// 落地成一句话：单列、一个金额、一个主动作、没有干扰。配色沿用站点令牌（浅灰底 / 白卡 /
// 1px 细边 / 14px 圆角 / 克制阴影 / 无渐变），品牌色只出现在主按钮与极小徽标上。
//
// 三种形态：
//   - 电脑：展示二维码，扫了就付；
//   - 手机 + 微信：微信 H5 链接在浏览器里付不了，主操作是「复制链接，去微信付款」；
//   - 手机 + 支付宝：按钮直接指支付链接，由支付宝唤起 App 落到本单，不需要二维码。
const scanPageHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="format-detection" content="telephone=no">
<meta name="color-scheme" content="light">
<title>支付 ¥{{.Amount}}</title>
<style>
:root{
--ink1:#0f172a;--ink2:#475569;--ink3:#64748b;--line:#e2e8f0;--bg:#f5f7fb;--card:#fff;
--primary:#2563eb;--disabled:#cbd5e1;
--r-sm:8px;--r-md:12px;--r-lg:14px;
--shadow:0 1px 2px rgba(15,23,42,.04),0 8px 24px rgba(15,23,42,.06);
--card-w:440px;--pad:24px;--btn-h:52px;
}
*{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
html,body{margin:0;padding:0}
body{
background:var(--bg);color:var(--ink1);font-size:15px;line-height:1.6;
font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Hiragino Sans GB","Microsoft YaHei","Helvetica Neue",Arial,sans-serif;
-webkit-font-smoothing:antialiased;padding:20px 16px calc(24px + env(safe-area-inset-bottom));
display:flex;flex-direction:column;align-items:center;
}
.shell{width:100%;max-width:var(--card-w)}
/* 站点标识：先回答"我在谁的页面" */
.brand{display:flex;align-items:center;gap:8px;padding:0 4px 14px}
.brand img{height:22px;width:auto;display:block;border-radius:6px}
.brand span{font-size:16px;font-weight:600;color:var(--ink1);letter-spacing:-.1px}
.card{background:var(--card);border:1px solid var(--line);border-radius:var(--r-lg);box-shadow:var(--shadow)}
.head{padding:var(--pad) var(--pad) 0}
.body{padding:0 var(--pad) var(--pad)}
/* 收款方：人话一行，不用表单字段的样子 */
.payee{margin:0;font-size:14px;line-height:1.6;color:var(--ink2)}
.payee b{font-weight:600;color:var(--ink1)}
.title{margin:12px 0 0;font-size:17px;font-weight:600;line-height:1.45;color:var(--ink1)}
/* 金额：视觉唯一中心 */
.amount{margin-top:20px}
.amount-label{display:block;font-size:12px;color:var(--ink3);letter-spacing:.02em}
.amount-value{margin:2px 0 0;font-size:40px;font-weight:600;line-height:1.1;letter-spacing:-.02em;
color:var(--ink1);font-variant-numeric:tabular-nums}
.amount-value i{font-style:normal;font-size:22px;font-weight:600;margin-right:1px;vertical-align:baseline}
.amount-value em{font-style:normal;font-size:.68em;font-weight:600}
.subject{margin:8px 0 0;font-size:16px;line-height:1.5;color:var(--ink1);
display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;word-break:break-all}
/* 主动作 */
.action{margin-top:24px}
.qr{width:208px;margin:0 auto;padding:16px;background:#fff;border:1px solid var(--line);border-radius:var(--r-lg)}
.qr svg{display:block;width:100%;height:auto}
.qr-hint{margin:14px 0 0;text-align:center;font-size:12.5px;line-height:1.65;color:var(--ink3)}
.btn{display:flex;align-items:center;justify-content:center;gap:8px;width:100%;height:var(--btn-h);
border:0;border-radius:var(--r-md);font-size:16px;font-weight:600;font-family:inherit;text-decoration:none;
cursor:pointer;transition:filter .12s ease;padding:0 20px}
.btn:active{filter:brightness(.94)}
.btn svg{flex:none;display:block}
.btn.primary{background:{{.IconColor}};color:#fff}
.btn.secondary{background:var(--card);color:var(--ink1);border:1px solid var(--line);height:48px;margin-top:10px}
.btn[aria-disabled="true"]{background:var(--disabled);cursor:not-allowed}
.hint{margin:12px 0 0;font-size:13px;line-height:1.65;color:var(--ink3)}
.textlink{display:flex;align-items:center;justify-content:center;height:32px;margin-top:8px;
font-size:14px;font-weight:500;color:var(--ink3);text-decoration:none}
.textlink:active{color:var(--ink1)}
/* 支付链接：微信场景的核心证据，不折叠、不藏 */
.link{margin-top:16px}
.link-label{display:block;font-size:12px;color:var(--ink3);margin-bottom:6px}
.link-row{display:flex;gap:8px;align-items:stretch}
.link-text{flex:1;min-width:0;display:block;line-height:40px;height:42px;font-size:12.5px;color:var(--ink2);
border:1px solid var(--line);border-radius:var(--r-sm);padding:0 12px;background:#f8fafc;
font-family:ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.2px;
white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.link-row button{flex:none;height:42px;padding:0 16px;font-size:13px;font-weight:600;border-radius:var(--r-sm);
border:1px solid var(--line);background:var(--card);color:var(--ink1);cursor:pointer;font-family:inherit}
.link-row button:active{background:#f1f5f9}
/* 信任与透明 */
.trust{margin:18px 0 0;padding:0;list-style:none;display:grid;gap:7px}
.trust li{position:relative;padding-left:16px;font-size:12.5px;line-height:1.6;color:var(--ink3)}
.trust li::before{content:"";position:absolute;left:0;top:7px;width:5px;height:5px;border-radius:50%;background:#cbd5e1}
.divider{height:1px;background:var(--line);margin:18px 0}
/* 明细 */
.meta{margin:0;padding:0;display:grid;gap:12px}
.row{display:flex;justify-content:space-between;gap:16px;font-size:13.5px;line-height:1.55}
.key{flex:none;color:var(--ink3);margin:0}
.val{margin:0;color:var(--ink1);text-align:right;word-break:break-all}
.val.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;
font-variant-numeric:tabular-nums;letter-spacing:.3px}
.countdown{font-variant-numeric:tabular-nums;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.countdown.warn{color:#dc2626}
.status{margin:12px 0 0;min-height:20px;text-align:center;font-size:13px;line-height:1.6;color:var(--ink2)}
.status.error{color:#dc2626}
.foot{margin:18px auto 0;max-width:320px;text-align:center;font-size:12px;line-height:1.75;color:#94a3b8}
.is-expired .action,.is-expired .link{opacity:.55;pointer-events:none}
@media (min-width:768px){
  :root{--card-w:460px;--pad:32px;--btn-h:48px}
  body{padding-top:40px}
  .amount-value{font-size:42px}
  .amount-value i{font-size:23px}
}
</style></head>
<body>
<div class="shell">
  <div class="brand">
    {{if .LogoURL}}<img src="{{.LogoURL}}" alt="{{.SiteName}}">{{end}}
    <span>{{if .SiteName}}{{.SiteName}}{{else}}收银台{{end}}</span>
  </div>

  <div class="card" id="card">
    <div class="head">
      {{if .MerchantName}}<p class="payee">收款方：<b>{{.MerchantName}}</b></p>{{end}}
      <h1 class="title">{{.Title}}</h1>
      <div class="amount">
        <span class="amount-label">应付金额</span>
        <p class="amount-value"><i>¥</i>{{.AmountInt}}<em>{{.AmountDec}}</em></p>
        <p class="subject">{{.Name}}</p>
      </div>
    </div>

    <div class="body">
      <div class="action">
        {{if .Desktop}}
          {{if .QR}}<div class="qr">{{.QR}}</div>{{end}}
          <p class="qr-hint">打开微信或支付宝，扫一扫<br>请勿向他人出示此二维码</p>
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
          <a class="btn primary" href="{{.PrimaryHref}}" data-open="1">
            <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="{{.IconPath}}" fill="#fff"/></svg>
            <span>打开支付宝付款</span>
          </a>
          <p class="hint">点击后会自动唤起支付宝并进入本笔订单；没打开就用下方链接。</p>
        {{else}}
          {{if .QR}}<div class="qr">{{.QR}}</div>{{end}}
          <a class="btn primary" href="{{.PrimaryHref}}">打开支付页</a>
          <p class="hint">也可以复制下方链接，在对应的 App 里打开。</p>
        {{end}}
      </div>

      <div class="link">
        <span class="link-label">{{if .Desktop}}支付链接{{else}}支付链接（复制到 {{.Brand}} 打开）{{end}}</span>
        <div class="link-row">
          <span class="link-text" id="paylink" role="textbox" aria-readonly="true" aria-label="支付链接">{{.RawURL}}</span>
          <input type="hidden" id="paylinkfull" value="{{.RawURL}}">
          <button type="button" onclick="copyLink(this)">复制</button>
        </div>
      </div>

      <ul class="trust">
        <li>款项直接结算至商户账户，平台不经手你的资金。</li>
        <li>本页面全程 HTTPS 加密传输，不保存你的支付密码。</li>
        <li>付款后请停留在本页，页面会自动更新支付状态。</li>
      </ul>

      <div class="divider"></div>

      <dl class="meta">
        <div class="row"><dt class="key">订单号</dt><dd class="val mono">{{.TradeNo}}</dd></div>
        <div class="row"><dt class="key">支付方式</dt><dd class="val">{{.Brand}}</dd></div>
        <div class="row"><dt class="key">下单时间</dt><dd class="val">{{.CreatedText}}</dd></div>
        <div class="row"><dt class="key">剩余时间</dt><dd class="val"><span class="countdown" id="countdown">--:--</span></dd></div>
      </dl>

      <div class="status" id="status"></div>
    </div>
  </div>

  <div class="foot">
    本页金额与商品以商户页面为准<br>如有疑问请联系收款方
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
// 唤起型按钮：点一次就短暂禁用，避免连点重复下单
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
// 倒计时：归零后停下动作区并给出下一步，不弹窗遮挡
var deadline={{.ExpiresAtMS}};
var countdown=document.getElementById('countdown');
var card=document.getElementById('card');
var statusEl=document.getElementById('status');
function tick(){
  if(!deadline){countdown.textContent='—';return false;}
  var left=deadline-Date.now();
  if(left<=0){
    countdown.textContent='已超时';
    countdown.className='countdown warn';
    card.className='card is-expired';
    statusEl.className='status error';
    statusEl.textContent='订单已超时，请返回商户重新下单';
    return true;
  }
  var total=Math.floor(left/1000),h=Math.floor(total/3600),m=Math.floor(total%3600/60),s=total%60;
  var pad=function(n){return n<10?'0'+n:''+n;};
  countdown.textContent=(h>0?h+':':'')+pad(m)+':'+pad(s);
  if(left<60000){countdown.className='countdown warn';}
  return false;
}
if(!tick()){setInterval(tick,1000);}
// 轮询：付完自动走，和上游 wxpay_wap 的 loadmsg() 一样
var pollTimer=setInterval(function(){
  fetch('{{.PollURL}}',{credentials:'omit',cache:'no-store'})
    .then(function(r){return r.json();})
    .then(function(d){
      if(d&&d.paid){clearInterval(pollTimer);
        statusEl.className='status';
        statusEl.textContent='支付成功，正在返回商户…';
        window.location.replace('{{.PaidURL}}');}
    }).catch(function(){});
},2000);
</script>
</body></html>`
