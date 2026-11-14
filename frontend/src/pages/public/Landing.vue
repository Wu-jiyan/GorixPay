<script setup lang="ts">
import { computed } from 'vue';
import { formatDateTime, paymentTypeText } from '@/utils/format';
import { siteFooter, siteInfo, siteLogo, siteName } from '@/stores/site';
import PublicNav from '@/components/PublicNav.vue';

interface PaymentMethodCard {
  key: string;
  name: string;
  desc: string;
  badge: string;
  color: string;
}

interface AnnouncementSnippet {
  title: string;
  content?: string;
  published_at?: string | null;
  created_at?: string | null;
}

const DEFAULT_METHODS: PaymentMethodCard[] = [
  { key: 'alipay', name: '支付宝', desc: '扫码 / 跳转 / 手机网站支付', badge: '支', color: '#1677ff' },
  { key: 'wxpay', name: '微信支付', desc: 'Native 扫码 / H5 / JSAPI', badge: '微', color: '#07c160' },
  { key: 'qqpay', name: 'QQ 钱包', desc: '扫码与跳转支付', badge: 'Q', color: '#12b7f5' },
  { key: 'bank', name: '网银支付', desc: '银行卡网关直连', badge: '银', color: '#7c3aed' },
];

// 站点名称、Logo、页脚由 stores/site 统一提供（App.vue 挂载时已请求一次）
const announcement = computed<AnnouncementSnippet | null>(() => {
  const item = siteInfo.value.announcement;
  if (!item) return null;
  const title = (item.title || '').trim();
  const content = (item.content || '').trim();
  if (!title && !content) return null;
  return {
    title: title || '平台公告',
    content,
    published_at: item.published_at ?? null,
    created_at: item.created_at ?? null,
  };
});

const methodCatalog = new Map(DEFAULT_METHODS.map((item) => [item.key, item]));
const methods = computed<PaymentMethodCard[]>(() => {
  const types = siteInfo.value.payment_types ?? [];
  if (types.length === 0) return DEFAULT_METHODS;
  return types.map<PaymentMethodCard>((key) => {
    const known = methodCatalog.get(key);
    if (known) return known;
    const label = paymentTypeText(key);
    return {
      key,
      name: label,
      desc: '聚合支付通道',
      badge: label.slice(0, 1).toUpperCase(),
      color: '#2563eb',
    };
  });
});

const metrics = [
  { value: '10', label: '支付渠道插件' },
  { value: '19', label: 'V1 V2 标准接口' },
  { value: '109', label: '易支付一致性用例' },
];

const steps = [
  { title: '注册商户', desc: '邮箱或手机号注册，即时开通商户号与收银台。' },
  { title: '配置密钥', desc: '使用 MD5 密钥快速联调，或生成 V2 RSA 密钥对完成双向签名。' },
  { title: '对接下单', desc: 'submit.php 页面跳转、mapi.php 接口下单，或使用 V2 /api/pay/create。' },
  { title: '收款与通知', desc: '收银台完成支付，平台按订单金额入账并异步通知你的服务器。' },
];

const advantages = [
  {
    title: 'V1 / V2 双协议并存',
    desc: '完全兼容易支付 V1（submit.php、mapi.php、api.php），同时提供 V2 的 RSA-SHA256 双向签名与时间戳防重放。',
  },
  {
    title: '资金账本可核对',
    desc: '每笔支付、退款、结算、代付都写入复式账本，余额、冻结与流水一一对应，报表随时对账。',
  },
  {
    title: '订单金额不可篡改',
    desc: '下单后金额锁定，回调必须与订单金额完全一致，收银台与沙箱支付不接受客户端金额。',
  },
  {
    title: '通知可重试、可追踪',
    desc: '异步通知走 Outbox 队列，失败自动退避重试，运营后台可查看每条投递结果。',
  },
];

</script>

<template>
  <div class="landing">
    <PublicNav />

    <section class="hero">
      <div class="hero-inner">
        <div class="hero-copy">
          <span class="hero-badge">易支付 V1 / V2 协议</span>
          <h1 class="hero-title">换域名就能用的易支付</h1>
          <p class="hero-subtitle">
            submit.php、mapi.php、api.php 的参数、错误码和异步通知都和上游一致，商户 SDK 不用改。新增 V2
            RSA-SHA256 双向签名、复式账本和插件化渠道。
          </p>
          <div class="hero-actions">
            <n-button type="primary" size="large" tag="a" href="/register">免费注册</n-button>
            <n-button size="large" quaternary tag="a" href="/doc">查看文档</n-button>
          </div>
          <p class="hero-note">无需预付费 · 沙箱渠道本地联调 · 商户可自助轮换 MD5 与 V2 RSA 密钥</p>
        </div>
        <div class="hero-panel">
          <div class="panel-head">
            <span class="dot" />
            <span class="dot dim" />
            <span class="dot dim" />
            <span class="panel-title">mapi.php</span>
          </div>
          <pre class="panel-code">POST /mapi.php
pid=10001
out_trade_no=ORDER20261113001
type=alipay
name=年度会员
money=29.90
notify_url=https://shop.example.com/notify
sign=8f14e45fceea167a5a36dedd4bea2543
sign_type=MD5

→ {"code":1,"trade_no":"20261113...","pay_type":"qrcode","pay_info":"..."}</pre>
          <div class="panel-foot">V2 同款能力：<span class="mono">/api/pay/create</span> 返回 RSA 签名响应</div>
        </div>
      </div>
      <div class="metrics">
        <div v-for="item in metrics" :key="item.label" class="metric">
          <div class="metric-value">{{ item.value }}</div>
          <div class="metric-label">{{ item.label }}</div>
        </div>
      </div>
    </section>

    <section v-if="announcement" class="section">
      <n-alert type="info" :bordered="false" :title="announcement.title">
        <div v-if="announcement.content" class="announcement-text">{{ announcement.content }}</div>
        <div class="announcement-time">
          {{ formatDateTime(announcement.published_at ?? announcement.created_at) }}
        </div>
      </n-alert>
    </section>

    <section class="section">
      <div class="section-head">
        <h2 class="section-title">支持的支付方式</h2>
        <p class="section-desc">同一套下单参数，按需切换支付类型，收银台自动渲染扫码或跳转。</p>
      </div>
      <div class="method-grid">
        <div v-for="item in methods" :key="item.key" class="method-card">
          <div class="method-badge" :style="{ background: item.color }">{{ item.badge }}</div>
          <div class="method-body">
            <div class="method-name">{{ item.name }}</div>
            <div class="method-desc">{{ item.desc }}</div>
            <div class="method-key mono">{{ item.key }}</div>
          </div>
        </div>
      </div>
    </section>

    <section class="section flow-section">
      <div class="section-head">
        <h2 class="section-title">四步完成接入</h2>
        <p class="section-desc">从注册到回调入账，每一步都有文档与示例代码。</p>
      </div>
      <div class="flow-grid">
        <div v-for="(step, index) in steps" :key="step.title" class="flow-card">
          <div class="flow-index">{{ index + 1 }}</div>
          <div class="flow-title">{{ step.title }}</div>
          <div class="flow-desc">{{ step.desc }}</div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="section-head">
        <h2 class="section-title">为什么选择我们</h2>
        <p class="section-desc">协议兼容、账本清晰、签名安全，运营侧与研发侧都省心。</p>
      </div>
      <div class="advantage-grid">
        <div v-for="item in advantages" :key="item.title" class="advantage-card">
          <div class="advantage-title">{{ item.title }}</div>
          <div class="advantage-desc">{{ item.desc }}</div>
        </div>
      </div>
    </section>

    <section class="cta-band">
      <div class="cta-inner">
        <div>
          <h2 class="cta-title">准备好接入 {{ siteName }} 了吗？</h2>
          <p class="cta-desc">注册后即可获取商户号与密钥，沙箱环境验证完整支付闭环。</p>
        </div>
        <div class="cta-actions">
          <n-button type="primary" size="large" tag="a" href="/register">免费注册</n-button>
          <n-button size="large" ghost tag="a" href="/doc">阅读接口文档</n-button>
        </div>
      </div>
    </section>

    <footer class="footer">
      <div class="footer-inner">
        <div class="footer-brand">
          <img v-if="siteLogo" :src="siteLogo" class="brand-logo" alt="" />
          <span v-else class="brand-mark">G</span>
          <div>
            <div class="footer-name">{{ siteName }}</div>
            <div class="footer-desc">聚合支付 · 易支付协议兼容 · V2 安全增强</div>
          </div>
        </div>
        <div class="footer-links">
          <router-link to="/doc">开发文档</router-link>
          <router-link to="/login">商户登录</router-link>
          <router-link to="/register">免费注册</router-link>
          <a href="/admin/login">运营入口</a>
        </div>
      </div>
      <div v-if="siteFooter" class="footer-meta">{{ siteFooter }}</div>
      <div class="footer-copy">© {{ new Date().getFullYear() }} {{ siteName }}. 保留所有权利。</div>
    </footer>
  </div>
</template>

<style scoped>
.landing {
  background: #fff;
  min-height: 100vh;
}
.hero {
  background: radial-gradient(1200px 500px at 15% -10%, #dbeafe 0%, transparent 60%),
    linear-gradient(135deg, #f8fbff 0%, #eef4ff 55%, #f5f7fb 100%);
  padding: 56px 20px 32px;
  border-bottom: 1px solid #e8eefc;
}
.hero-inner {
  max-width: 1160px;
  margin: 0 auto;
  display: grid;
  grid-template-columns: 1.05fr 0.95fr;
  gap: 44px;
  align-items: center;
}
.hero-badge {
  display: inline-block;
  padding: 5px 12px;
  border-radius: 999px;
  background: rgba(37, 99, 235, 0.1);
  color: #1d4ed8;
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 18px;
}
.hero-title {
  font-size: 44px;
  line-height: 1.18;
  margin: 0 0 16px;
  color: #0f172a;
  letter-spacing: -0.5px;
}
.hero-subtitle {
  font-size: 15px;
  line-height: 1.8;
  color: #475569;
  margin: 0 0 26px;
  max-width: 540px;
}
.hero-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
.hero-note {
  margin: 18px 0 0;
  color: #64748b;
  font-size: 12.5px;
}
.hero-panel {
  background: #0f172a;
  border-radius: 16px;
  padding: 16px 18px 18px;
  box-shadow: 0 24px 60px rgba(15, 23, 42, 0.28);
  color: #e2e8f0;
  overflow: hidden;
}
.panel-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-bottom: 12px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.24);
}
.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: #f87171;
}
.dot.dim {
  background: #64748b;
}
.panel-title {
  margin-left: 8px;
  font-size: 12px;
  color: #94a3b8;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.panel-code {
  margin: 14px 0 0;
  font-size: 12px;
  line-height: 1.75;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
  color: #cbd5f5;
}
.panel-foot {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgba(148, 163, 184, 0.24);
  font-size: 12px;
  color: #94a3b8;
}
.panel-foot .mono {
  color: #7dd3fc;
}
.metrics {
  max-width: 1160px;
  margin: 40px auto 0;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}
.metric {
  background: #fff;
  border: 1px solid #e8eefc;
  border-radius: 14px;
  padding: 16px 18px;
}
.metric-value {
  font-size: 24px;
  font-weight: 700;
  color: #1d4ed8;
  font-variant-numeric: tabular-nums;
}
.metric-label {
  margin-top: 4px;
  font-size: 12.5px;
  color: #64748b;
}
.announcement-text {
  font-size: 13px;
  line-height: 1.8;
  white-space: pre-wrap;
  word-break: break-word;
}
.announcement-time {
  margin-top: 6px;
  font-size: 12px;
  color: #94a3b8;
}
.section {
  max-width: 1160px;
  margin: 0 auto;
  padding: 56px 20px 0;
}
.section-head {
  margin-bottom: 24px;
}
.section-title {
  font-size: 26px;
  margin: 0 0 8px;
  color: #0f172a;
}
.section-desc {
  margin: 0;
  color: #64748b;
  font-size: 14px;
}
.method-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}
.method-card {
  display: flex;
  gap: 14px;
  padding: 18px;
  border: 1px solid #e8eefc;
  border-radius: 14px;
  background: #fff;
  transition: box-shadow 0.2s ease, transform 0.2s ease;
}
.method-card:hover {
  box-shadow: 0 12px 28px rgba(37, 99, 235, 0.12);
  transform: translateY(-2px);
}
.method-badge {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  color: #fff;
  font-weight: 700;
  display: grid;
  place-items: center;
  flex: none;
}
.method-name {
  font-weight: 600;
  color: #0f172a;
}
.method-desc {
  margin-top: 4px;
  font-size: 12.5px;
  color: #64748b;
  line-height: 1.6;
}
.method-key {
  margin-top: 8px;
  font-size: 11.5px;
  color: #94a3b8;
}
.flow-section {
  padding-top: 64px;
}
.flow-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}
.flow-card {
  position: relative;
  padding: 22px 18px;
  border-radius: 14px;
  background: linear-gradient(180deg, #f8fbff, #fff);
  border: 1px solid #e8eefc;
}
.flow-index {
  width: 30px;
  height: 30px;
  border-radius: 10px;
  background: #2563eb;
  color: #fff;
  font-weight: 700;
  display: grid;
  place-items: center;
  margin-bottom: 14px;
}
.flow-title {
  font-weight: 600;
  color: #0f172a;
  margin-bottom: 6px;
}
.flow-desc {
  font-size: 12.5px;
  color: #64748b;
  line-height: 1.7;
}
.advantage-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.advantage-card {
  padding: 22px;
  border-radius: 14px;
  background: #f8fafc;
  border: 1px solid #eef2f7;
}
.advantage-title {
  font-weight: 600;
  color: #0f172a;
  margin-bottom: 8px;
}
.advantage-desc {
  font-size: 13px;
  line-height: 1.8;
  color: #64748b;
}
.cta-band {
  margin-top: 64px;
  background: linear-gradient(135deg, #1e40af, #2563eb 55%, #06b6d4);
  color: #fff;
}
.cta-inner {
  max-width: 1160px;
  margin: 0 auto;
  padding: 40px 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
}
.cta-title {
  margin: 0 0 6px;
  font-size: 24px;
}
.cta-desc {
  margin: 0;
  font-size: 13.5px;
  color: rgba(255, 255, 255, 0.82);
}
.cta-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
.footer {
  background: #0f172a;
  color: #cbd5f5;
  padding: 40px 20px 26px;
}
.footer-inner {
  max-width: 1160px;
  margin: 0 auto;
  display: flex;
  justify-content: space-between;
  gap: 24px;
  flex-wrap: wrap;
}
.footer-brand {
  display: flex;
  align-items: center;
  gap: 12px;
}
.brand-mark {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  color: #fff;
  font-weight: 700;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
}
.brand-logo {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  object-fit: cover;
  display: block;
  flex: none;
}
.footer-name {
  font-weight: 600;
  color: #fff;
}
.footer-desc {
  font-size: 12px;
  color: #94a3b8;
  margin-top: 2px;
}
.footer-links {
  display: flex;
  gap: 20px;
  font-size: 13px;
  flex-wrap: wrap;
}
.footer-links a {
  color: #cbd5f5;
}
.footer-links a:hover {
  color: #fff;
}
.footer-meta {
  max-width: 1160px;
  margin: 22px auto 0;
  font-size: 12px;
  line-height: 1.8;
  color: #94a3b8;
  text-align: center;
  word-break: break-all;
}
.footer-copy {
  max-width: 1160px;
  margin: 26px auto 0;
  padding-top: 18px;
  border-top: 1px solid rgba(148, 163, 184, 0.2);
  font-size: 12px;
  color: #64748b;
  text-align: center;
}
@media (max-width: 960px) {
  .hero-inner {
    grid-template-columns: minmax(0, 1fr);
  }
  .hero-title {
    font-size: 34px;
  }
  .method-grid,
  .flow-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 768px) {
  .hero {
    padding: 36px 16px 24px;
  }
  .hero-title {
    font-size: 28px;
  }
  .section {
    padding: 40px 16px 0;
  }
  .section-title {
    font-size: 21px;
  }
  .method-grid,
  .flow-grid,
  .advantage-grid,
  .metrics {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
