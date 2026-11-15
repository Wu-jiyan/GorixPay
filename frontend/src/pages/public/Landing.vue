<script setup lang="ts">
import { computed, ref } from 'vue';
import {
  NAlert,
  NButton,
  NCard,
  NCollapse,
  NCollapseItem,
  NCode,
  NDivider,
  NGi,
  NGrid,
  NIcon,
  NInputNumber,
  NNumberAnimation,
  NSelect,
  NSlider,
  NSpace,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useMessage,
} from 'naive-ui';
import {
  ArrowForwardOutline,
  BookOutline,
  CheckmarkCircleOutline,
  ExtensionPuzzleOutline,
  GitCompareOutline,
  LibraryOutline,
  LockClosedOutline,
  RepeatOutline,
  StatsChartOutline,
} from '@vicons/ionicons5';
import PublicShell from '@/components/PublicShell.vue';
import PaymentBrandIcon from '@/components/PaymentBrandIcon.vue';
import { copyText, formatDateTime, paymentTypeText } from '@/utils/format';
import { siteInfo } from '@/stores/site';

const message = useMessage();

/* ---------- 公告 ---------- */
const announcement = computed(() => {
  const item = siteInfo.value.announcement;
  if (!item) return null;
  const title = (item.title || '').trim();
  const content = (item.content || '').trim();
  if (!title && !content) return null;
  return {
    title: title || '平台公告',
    content,
    time: formatDateTime(item.published_at || item.created_at || ''),
  };
});
const announcementVisible = ref(true);

/* ---------- 费率试算：首页唯一的"要动手"的东西 ---------- */
const amount = ref<number | null>(1000);
const rateOptions = [
  { label: '0.38%', value: 0.38 },
  { label: '0.60%', value: 0.6 },
  { label: '1.00%', value: 1 },
];
const rate = ref(0.6);
// 滑块不接受 null，单独包一层，输入框清空时按 0 算
const sliderAmount = computed({
  get: () => amount.value ?? 0,
  set: (value: number) => {
    amount.value = value;
  },
});
const realAmount = computed(() => Math.max(0, Number(amount.value ?? 0)));
const fee = computed(() => Math.round(realAmount.value * rate.value) / 100);
const receive = computed(() => Math.max(0, realAmount.value - fee.value));
const yuan = (n: number) => n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/* ---------- 支付方式 ---------- */
interface MethodCard {
  key: string;
  name: string;
  tags: string[];
}
const METHOD_META: Record<string, { name: string; tags: string[] }> = {
  alipay: { name: '支付宝', tags: ['扫码', '跳转', 'H5'] },
  wxpay: { name: '微信支付', tags: ['Native', 'JSAPI', 'H5'] },
  qqpay: { name: 'QQ 钱包', tags: ['扫码', '跳转'] },
  bank: { name: '网银', tags: ['网关直连'] },
  usdt: { name: 'USDT', tags: ['链上收款'] },
  cashier: { name: '收银台', tags: ['聚合页'] },
};
const methods = computed<MethodCard[]>(() => {
  const types = siteInfo.value.payment_types ?? [];
  const list = types.length > 0 ? types : ['alipay', 'wxpay', 'qqpay', 'bank'];
  return list.map((key) => ({
    key,
    name: METHOD_META[key]?.name ?? paymentTypeText(key),
    tags: METHOD_META[key]?.tags ?? ['聚合通道'],
  }));
});

/* ---------- 指标 ---------- */
const metrics = [
  { value: 10, suffix: '个', label: '支付渠道插件' },
  { value: 19, suffix: '个', label: 'V1 / V2 接口' },
  { value: 109, suffix: '条', label: '一致性用例' },
];

/* ---------- 四步 ---------- */
const steps = [
  { title: '注册开通', desc: '邮箱注册，立即拿到 PID 与收银台域名。' },
  { title: '配置密钥', desc: '复制 MD5 密钥，或生成 V2 RSA 密钥对。' },
  { title: '发起支付', desc: '表单跳转或接口下单，拿二维码链接。' },
  { title: '回调入账', desc: '异步通知验签通过，金额入账可提现。' },
];

/* ---------- 代码示例 ---------- */
const apiTabs = [
  {
    key: 'mapi',
    label: 'mapi.php',
    note: '接口下单，返回二维码链接',
    code: `POST /mapi.php
pid=10001
type=alipay
out_trade_no=ORDER-20260101-001
money=29.90
name=会员充值
notify_url=https://shop.example.com/notify
sign_type=MD5
sign=9f14a45fccca167a5a36ded40aa2a543
→ {"code":1,"pay_type":"qrcode","pay_info":"https://qr.alipay.com/..."}`,
  },
  {
    key: 'submit',
    label: 'submit.php',
    note: '表单直接跳到收银台',
    code: `POST /submit.php
pid=10001
type=wxpay
out_trade_no=ORDER-20260101-002
money=99.00
name=订单 99 元
notify_url=https://shop.example.com/notify
return_url=https://shop.example.com/return
sign_type=MD5
sign=5b1c0f0e8a2f4d6c9e7a1b3d5f7a9c1e
→ 302 到收银台页面`,
  },
  {
    key: 'v2',
    label: '/api/pay/create',
    note: 'V2 协议，RSA-SHA256 双向签名',
    code: `POST /api/v2/pay/create
X-Pid: 10001
X-Timestamp: 1767225600
X-Nonce: 8f4c1a2b
X-Signature: base64(RSASSA-PKCS1-v1_5(SHA256, body))
{"out_trade_no":"ORDER-20260101-003","amount":2990,"pay_type":"alipay"}
← {"code":0,"data":{"trade_no":"...","pay_url":"...","expired_at":"..."}}`,
  },
];

const activeApi = ref('mapi');
const activeCode = computed(() => apiTabs.find((tab) => tab.key === activeApi.value)?.code ?? '');

async function copyCode() {
  const ok = await copyText(activeCode.value);
  if (ok) message.success('已复制');
  else message.error('复制失败，请手动选中');
}

/* ---------- 能力 ---------- */
const features = [
  { icon: GitCompareOutline, title: 'V1 / V2 并存', desc: '老代码不用改' },
  { icon: LibraryOutline, title: '复式账本', desc: '每笔进出都对得上' },
  { icon: LockClosedOutline, title: '金额锁定', desc: '回调强校验' },
  { icon: RepeatOutline, title: '通知重试', desc: '失败自动退避' },
  { icon: ExtensionPuzzleOutline, title: '插件通道', desc: '按需挂载' },
  { icon: StatsChartOutline, title: '对账报表', desc: '随查随导' },
];

/* ---------- FAQ ---------- */
const faqs = [
  { q: '接入需要改多少代码？', a: '原来提交到易支付的表单，把域名换成这里即可；接口下单只需换 mapi.php 的地址。' },
  { q: '签名和密钥在哪里看？', a: '注册后进入商户中心「账号与密钥」，可自助轮换 MD5 密钥，也可生成 V2 的 RSA 密钥对。' },
  { q: '结算怎么算？', a: '订单支付成功后进入可结算余额，按用户组的结算周期自动生成结算单，运营后台与商户端都能看到明细。' },
  { q: '出问题怎么排查？', a: '每笔订单都能看到通道请求、回调投递、账本流水三条链路，定位不用猜。' },
];

const go = (path: string) => {
  window.location.href = path;
};
</script>

<template>
  <public-shell>
    <n-alert
      v-if="announcement && announcementVisible"
      class="top-alert"
      type="info"
      closable
      :bordered="false"
      :title="announcement.title"
      @close="announcementVisible = false"
    >
      {{ announcement.content }}
      <n-text v-if="announcement.time" depth="3" style="font-size: 12px"> · {{ announcement.time }}</n-text>
    </n-alert>

    <!-- 首屏：左边一句话，右边直接能算 -->
    <section class="hero">
      <div class="hero-copy">
        <n-space :size="8" align="center">
          <n-tag type="success" size="small" :bordered="false">兼容易支付 V1 / V2</n-tag>
          <n-tag size="small" :bordered="false">沙箱可跑通</n-tag>
        </n-space>
        <n-text tag="h1" class="hero-title">换掉接口域名，立刻开始收款</n-text>
        <n-text depth="2" class="hero-sub">
          submit.php、mapi.php、api.php 的参数、错误码、异步通知全部对齐；另有一套 RSA 双向签名的 V2 接口。
        </n-text>
        <n-space :size="12" class="hero-cta">
          <n-button type="primary" size="large" @click="go('/register')">
            免费开通商户号
            <template #icon>
              <n-icon :component="ArrowForwardOutline" />
            </template>
          </n-button>
          <n-button size="large" @click="go('/doc')">
            <template #icon>
              <n-icon :component="BookOutline" />
            </template>
            看接口文档
          </n-button>
        </n-space>
        <n-space :size="16" class="hero-points">
          <n-space :size="4" align="center" v-for="point in ['免预付费', '自助轮换密钥', '账本可对账']" :key="point">
            <n-icon :component="CheckmarkCircleOutline" size="15" color="#18a058" />
            <n-text depth="2" style="font-size: 13px">{{ point }}</n-text>
          </n-space>
        </n-space>
      </div>

      <n-card class="calc-card" size="medium">
        <template #header>
          <n-space align="center" :size="8">
            <n-text strong>到账试算</n-text>
            <n-tag size="small" :bordered="false">示例费率</n-tag>
          </n-space>
        </template>
        <n-space vertical :size="14">
          <div>
            <n-text depth="3" class="field-label">订单金额（元）</n-text>
            <n-input-number
              v-model:value="amount"
              :min="0"
              :max="1000000"
              :step="100"
              size="large"
              style="width: 100%"
              placeholder="输入金额"
            />
          </div>
          <n-slider v-model:value="sliderAmount" :min="0" :max="50000" :step="100" />
          <div>
            <n-text depth="3" class="field-label">费率</n-text>
            <n-select v-model:value="rate" :options="rateOptions" />
          </div>
          <n-divider style="margin: 2px 0" />
          <n-grid :cols="2" :x-gap="12">
            <n-gi>
              <n-statistic label="手续费">
                <n-number-animation :from="0" :to="fee" :precision="2" />
                <template #suffix>元</template>
              </n-statistic>
            </n-gi>
            <n-gi>
              <n-statistic label="商户到账">
                <n-number-animation :from="0" :to="receive" :precision="2" />
                <template #suffix>元</template>
              </n-statistic>
            </n-gi>
          </n-grid>
          <n-text depth="3" style="font-size: 12px">
            按 ￥{{ yuan(realAmount) }} 计：手续费 ￥{{ yuan(fee) }}，到账 ￥{{ yuan(receive) }}。实际费率以开通的用户组为准。
          </n-text>
        </n-space>
      </n-card>
    </section>

    <!-- 指标 -->
    <n-grid class="metrics" cols="1 s:3" responsive="screen" :x-gap="16" :y-gap="12">
      <n-gi v-for="metric in metrics" :key="metric.label">
        <n-card size="small" :bordered="true">
          <div class="metric-value">
            <n-number-animation :from="0" :to="metric.value" />
            <span class="metric-suffix">{{ metric.suffix }}</span>
          </div>
          <div class="metric-label">{{ metric.label }}</div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 支付方式 -->
    <section class="block">
      <div class="block-head">
        <n-text tag="h2" class="block-title">支持哪些支付方式</n-text>
        <n-text depth="3">同一套参数，按 type 换通道，收银台自动出扫码或跳转。</n-text>
      </div>
      <n-grid cols="1 s:2 m:3" responsive="screen" :x-gap="14" :y-gap="14">
        <n-gi v-for="method in methods" :key="method.key">
          <n-card size="small" hoverable class="method-card">
            <div class="method-row">
              <span class="method-icon">
                <payment-brand-icon :type="method.key" :size="26" />
              </span>
              <div class="method-body">
                <n-text strong>{{ method.name }}</n-text>
                <n-space :size="6" style="margin-top: 4px">
                  <n-tag v-for="tag in method.tags" :key="tag" size="small" :bordered="false">{{ tag }}</n-tag>
                </n-space>
              </div>
            </div>
          </n-card>
        </n-gi>
      </n-grid>
    </section>

    <!-- 四步 -->
    <section class="block">
      <div class="block-head">
        <n-text tag="h2" class="block-title">接入只要四步</n-text>
        <n-text depth="3">每一步都有现成示例，不用先读完整份文档。</n-text>
      </div>
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="14" :y-gap="14">
        <n-gi v-for="(step, index) in steps" :key="step.title">
          <n-card size="small" :bordered="true" class="step-card">
            <span class="step-index">{{ index + 1 }}</span>
            <n-text strong class="step-title">{{ step.title }}</n-text>
            <n-text depth="3" style="font-size: 12.5px">{{ step.desc }}</n-text>
          </n-card>
        </n-gi>
      </n-grid>
    </section>

    <!-- 代码 -->
    <section class="block">
      <div class="block-head">
        <n-text tag="h2" class="block-title">看接口长什么样</n-text>
        <n-text depth="3">切一下标签，直接看到请求与响应。</n-text>
      </div>
      <n-card content-style="padding-top: 6px">
        <n-tabs v-model:value="activeApi" type="line" animated>
          <n-tab-pane v-for="tab in apiTabs" :key="tab.key" :name="tab.key" :tab="tab.label">
            <n-space vertical :size="10" style="width: 100%">
              <n-space justify="space-between" align="center" :size="12" style="width: 100%">
                <n-text depth="3" style="font-size: 12.5px">{{ tab.note }}</n-text>
                <n-button size="tiny" quaternary @click="copyCode">复制</n-button>
              </n-space>
              <n-code :code="tab.code" language="http" word-wrap class="api-code" />
            </n-space>
          </n-tab-pane>
        </n-tabs>
      </n-card>
    </section>

    <!-- 能力 -->
    <section class="block">
      <div class="block-head">
        <n-text tag="h2" class="block-title">为什么敢直接换过来</n-text>
      </div>
      <n-grid cols="1 s:2 m:3" responsive="screen" :x-gap="14" :y-gap="14">
        <n-gi v-for="feature in features" :key="feature.title">
          <n-card size="small" :bordered="true" class="feature-card">
            <n-space align="center" :size="12">
              <n-icon :component="feature.icon" size="22" />
              <div>
                <n-text strong style="display: block">{{ feature.title }}</n-text>
                <n-text depth="3" style="font-size: 12.5px">{{ feature.desc }}</n-text>
              </div>
            </n-space>
          </n-card>
        </n-gi>
      </n-grid>
    </section>

    <!-- FAQ -->
    <section class="block">
      <div class="block-head">
        <n-text tag="h2" class="block-title">常见问题</n-text>
      </div>
      <n-card content-style="padding: 4px 8px">
        <n-collapse>
          <n-collapse-item v-for="faq in faqs" :key="faq.q" :title="faq.q" :name="faq.q">
            <n-text depth="2">{{ faq.a }}</n-text>
          </n-collapse-item>
        </n-collapse>
      </n-card>
    </section>

    <!-- 收尾 -->
    <n-card class="block cta-card">
      <div class="cta-inner">
        <n-space vertical :size="4">
          <n-text strong class="cta-title">现在就开一个商户号</n-text>
          <n-text depth="2">注册即得 PID、密钥与收银台，沙箱先把闭环跑一遍。</n-text>
        </n-space>
        <n-space :size="12">
          <n-button type="primary" size="large" @click="go('/register')">免费注册</n-button>
          <n-button size="large" @click="go('/login')">已有账号登录</n-button>
        </n-space>
      </div>
    </n-card>
  </public-shell>
</template>

<style scoped>
.top-alert {
  margin-top: 20px;
}
.hero {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(320px, 0.85fr);
  gap: 40px;
  align-items: center;
  padding: 52px 0 12px;
}
.hero-title {
  display: block;
  margin: 14px 0 0;
  font-size: 38px;
  font-weight: 700;
  line-height: 1.22;
  letter-spacing: -0.5px;
}
.hero-sub {
  display: block;
  margin-top: 14px;
  font-size: 15px;
  line-height: 1.8;
  max-width: 34em;
}
.hero-cta {
  margin-top: 22px;
}
.hero-points {
  margin-top: 18px;
  flex-wrap: wrap;
}
.calc-card :deep(.n-card-header) {
  padding-bottom: 8px;
}
.field-label {
  display: block;
  margin-bottom: 6px;
  font-size: 12.5px;
}
.metrics {
  margin-top: 34px;
}
.metric-value {
  font-size: 26px;
  font-weight: 650;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
  color: var(--gp-text-1);
}
.metric-suffix {
  font-size: 13px;
  font-weight: 500;
  margin-left: 3px;
  color: var(--gp-text-3);
}
.metric-label {
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--gp-text-3);
}
.block {
  margin-top: 44px;
}
.block-head {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 16px;
}
.block-title {
  font-size: 21px;
  font-weight: 650;
  margin: 0;
}
.method-card,
.feature-card {
  height: 100%;
}
.method-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.method-icon {
  width: 44px;
  height: 44px;
  border-radius: var(--gp-radius);
  display: grid;
  place-items: center;
  background: var(--gp-bg-soft);
  border: 1px solid var(--gp-line-soft);
  flex: none;
}
.method-body {
  min-width: 0;
}
.steps-card {
  padding: 6px 4px;
}
.step-card {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.step-index {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--gp-accent-soft);
  color: var(--gp-accent);
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 6px;
}
.step-title {
  display: block;
}
.api-code {
  max-height: 320px;
  overflow: auto;
  font-size: 12.5px;
}
.cta-card {
  margin-bottom: 48px;
}
.cta-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.cta-title {
  font-size: 20px;
}
@media (max-width: 1024px) {
  .hero {
    grid-template-columns: 1fr;
    gap: 26px;
    padding-top: 34px;
  }
  .hero-title {
    font-size: 30px;
  }
}
@media (max-width: 640px) {
  .hero-title {
    font-size: 26px;
  }
  .block {
    margin-top: 32px;
  }
}
</style>
