<script setup lang="ts">
import { computed, h } from 'vue';
import { NTag, NText } from 'naive-ui';
import { ArrowForwardOutline, BookOutline } from '@vicons/ionicons5';
import { formatDateTime, paymentTypeText } from '@/utils/format';
import { siteFooter, siteInfo, siteLogo, siteName } from '@/stores/site';

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
  { value: 10, label: '支付渠道插件', suffix: '个' },
  { value: 19, label: 'V1 / V2 标准接口', suffix: '个' },
  { value: 109, label: '易支付一致性用例', suffix: '条' },
];

const steps = [
  { title: '注册商户', desc: '邮箱或手机号注册，即时开通商户号与收银台。' },
  { title: '配置密钥', desc: '用 MD5 密钥快速联调，或生成 V2 RSA 密钥对完成双向签名。' },
  { title: '对接下单', desc: 'submit.php 页面跳转、mapi.php 接口下单，或 V2 /api/pay/create。' },
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

const sampleRequest = `POST /mapi.php
pid=10001
out_trade_no=ORDER-20261115-0001
type=alipay
name=会员
money=29.90
notify_url=https://shop.example.com/notify
sign=9f14a45fccca167a5a36ded40aa2a543
sign_type=MD5`;

const sampleResponse = `{"code":1,"trade_no":"20261115...","pay_type":"qrcode","pay_info":"https://qr.alipay.com/..."}`;

const navLinks = [
  { label: '首页', to: '/' },
  { label: '开发文档', to: '/doc' },
  { label: '运营入口', to: '/admin/login' },
];

function go(to: string) {
  window.location.href = to;
}
</script>

<template>
  <n-layout class="landing">
    <n-layout-header bordered class="landing-header">
      <div class="landing-container landing-header-inner">
        <n-space align="center" :size="10" style="cursor: pointer" @click="go('/')">
          <img v-if="siteLogo" :src="siteLogo" alt="logo" class="landing-logo" />
          <div v-else class="landing-logo landing-logo-text">{{ (siteName || 'G').slice(0, 1) }}</div>
          <n-text strong style="font-size: 16px">{{ siteName }}</n-text>
        </n-space>
        <n-space align="center" :size="8">
          <n-button v-for="link in navLinks" :key="link.to" text @click="go(link.to)">{{ link.label }}</n-button>
          <n-button quaternary @click="go('/login')">商户登录</n-button>
          <n-button type="primary" @click="go('/register')">免费注册</n-button>
        </n-space>
      </div>
    </n-layout-header>

    <n-layout-content>
      <div class="landing-container">
        <n-alert
          v-if="announcement"
          type="info"
          :bordered="false"
          style="margin-top: 24px"
          :title="announcement.title"
        >
          <n-space vertical :size="4">
            <n-text>{{ announcement.content }}</n-text>
            <n-text depth="3" style="font-size: 12px">
              {{ formatDateTime(announcement.published_at || announcement.created_at || '') }}
            </n-text>
          </n-space>
        </n-alert>

        <!-- Hero -->
        <n-grid cols="1 m:2" responsive="screen" class="section" :x-gap="48" :y-gap="24">
          <n-gi>
            <n-space vertical :size="16">
              <n-tag type="info" :bordered="false" size="small">兼容易支付 V1 / V2 协议</n-tag>
              <n-text tag="h1" class="hero-title">换域名就能用的易支付</n-text>
              <n-text depth="2" class="hero-desc">
                submit.php、mapi.php、api.php 的参数、错误码和异步通知都和上游一致，商户 SDK 不用改。
                另提供 V2 的 RSA-SHA256 双向签名、复式账本和插件化通道。
              </n-text>
              <n-space :size="12">
                <n-button type="primary" size="large" @click="go('/register')">
                  免费注册
                  <template #icon>
                    <n-icon :component="ArrowForwardOutline" />
                  </template>
                </n-button>
                <n-button size="large" @click="go('/doc')">
                  <template #icon>
                    <n-icon :component="BookOutline" />
                  </template>
                  查看文档
                </n-button>
              </n-space>
              <n-text depth="3" style="font-size: 12.5px">无需预付费。沙箱渠道本机联调，商户可自助轮换 MD5 与 V2 RSA 密钥。</n-text>
            </n-space>
          </n-gi>
          <n-gi>
            <n-card size="small" :bordered="true" class="code-card">
              <template #header>
                <n-space align="center" :size="8">
                  <n-tag size="small" :bordered="false">POST</n-tag>
                  <n-text code>/mapi.php</n-text>
                </n-space>
              </template>
              <n-code :code="sampleRequest" language="http" word-wrap style="font-size: 12px" />
              <n-divider style="margin: 12px 0" />
              <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 6px">响应</n-text>
              <n-code :code="sampleResponse" language="json" word-wrap style="font-size: 12px" />
            </n-card>
          </n-gi>
        </n-grid>

        <!-- Metrics -->
        <n-grid cols="1 s:3" responsive="screen" :x-gap="16" :y-gap="16" class="section-tight">
          <n-gi v-for="metric in metrics" :key="metric.label">
            <n-card size="small" :bordered="false">
              <n-statistic :label="metric.label" :value="metric.value">
                <template #suffix>{{ metric.suffix }}</template>
              </n-statistic>
            </n-card>
          </n-gi>
        </n-grid>

        <!-- Payment methods -->
        <section class="section">
          <n-space vertical :size="4" style="margin-bottom: 16px">
            <n-text tag="h2" class="section-title">支持的支付方式</n-text>
            <n-text depth="3">同一套下单参数，按需切换支付类型，收银台自动渲染扫码或跳转。</n-text>
          </n-space>
          <n-grid cols="1 s:2 m:3" responsive="screen" :x-gap="16" :y-gap="16">
            <n-gi v-for="method in methods" :key="method.key">
              <n-card size="small" :bordered="true">
                <n-space align="center" :size="12">
                  <n-avatar round :size="32" :style="{ background: method.color }">{{ method.badge }}</n-avatar>
                  <n-space vertical :size="2">
                    <n-text strong>{{ method.name }}</n-text>
                    <n-text depth="3" style="font-size: 12.5px">{{ method.desc }}</n-text>
                  </n-space>
                </n-space>
                <n-text code style="display: block; margin-top: 10px; font-size: 12px">{{ method.key }}</n-text>
              </n-card>
            </n-gi>
          </n-grid>
        </section>

        <!-- Steps -->
        <section class="section">
          <n-space vertical :size="4" style="margin-bottom: 16px">
            <n-text tag="h2" class="section-title">四步完成接入</n-text>
            <n-text depth="3">从注册到回调入账，每一步都有文档与示例代码。</n-text>
          </n-space>
          <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="16" :y-gap="16">
            <n-gi v-for="(step, index) in steps" :key="step.title">
              <n-card size="small" :bordered="true">
                <n-tag size="small" :bordered="false" type="info" style="margin-bottom: 8px">第 {{ index + 1 }} 步</n-tag>
                <n-text strong style="display: block">{{ step.title }}</n-text>
                <n-text depth="3" style="font-size: 12.5px">{{ step.desc }}</n-text>
              </n-card>
            </n-gi>
          </n-grid>
        </section>

        <!-- Advantages -->
        <section class="section">
          <n-space vertical :size="4" style="margin-bottom: 16px">
            <n-text tag="h2" class="section-title">为什么选择我们</n-text>
            <n-text depth="3">协议兼容、账本清晰、签名安全，运营侧与研发侧都省心。</n-text>
          </n-space>
          <n-grid cols="1 s:2" responsive="screen" :x-gap="16" :y-gap="16">
            <n-gi v-for="item in advantages" :key="item.title">
              <n-card size="small" :bordered="true">
                <n-text strong style="display: block; margin-bottom: 6px">{{ item.title }}</n-text>
                <n-text depth="2" style="font-size: 13px">{{ item.desc }}</n-text>
              </n-card>
            </n-gi>
          </n-grid>
        </section>

        <!-- CTA -->
        <n-card :bordered="true" class="section">
          <n-space justify="space-between" align="center" :size="16" style="flex-wrap: wrap">
            <n-space vertical :size="4">
              <n-text strong style="font-size: 20px">准备好接入 {{ siteName }} 了吗？</n-text>
              <n-text depth="3">注册后即可获取商户号与密钥，沙箱环境能跑完整支付闭环。</n-text>
            </n-space>
            <n-space :size="12">
              <n-button type="primary" @click="go('/register')">免费注册</n-button>
              <n-button @click="go('/doc')">阅读开发文档</n-button>
            </n-space>
          </n-space>
        </n-card>

        <n-divider style="margin: 40px 0 20px" />
        <n-space justify="space-between" align="center" style="flex-wrap: wrap; gap: 12px; padding-bottom: 32px">
          <n-space align="center" :size="8">
            <div class="landing-logo landing-logo-text" style="width: 26px; height: 26px; font-size: 13px">
              {{ (siteName || 'G').slice(0, 1) }}
            </div>
            <n-text depth="2">{{ siteName }}</n-text>
            <n-text depth="3" style="font-size: 12px">{{ siteFooter || '聚合支付 · 易支付协议兼容' }}</n-text>
          </n-space>
          <n-space :size="16">
            <n-button text @click="go('/doc')">开发文档</n-button>
            <n-button text @click="go('/login')">商户登录</n-button>
            <n-button text @click="go('/register')">免费注册</n-button>
            <n-button text @click="go('/admin/login')">运营入口</n-button>
          </n-space>
        </n-space>
      </div>
    </n-layout-content>
  </n-layout>
</template>

<style scoped>
.landing {
  min-height: 100vh;
}
.landing-container {
  max-width: 1160px;
  margin: 0 auto;
  padding: 0 24px;
}
.landing-header {
  height: 58px;
}
.landing-header-inner {
  height: 58px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.landing-logo {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  object-fit: contain;
  background: #fff;
}
.landing-logo-text {
  display: grid;
  place-items: center;
  background: #2563eb;
  color: #fff;
  font-weight: 600;
}
.hero-title {
  font-size: 34px;
  font-weight: 700;
  line-height: 1.25;
  margin: 0;
}
.hero-desc {
  font-size: 14.5px;
  line-height: 1.85;
  margin: 0;
}
.section {
  margin-top: 48px;
}
.section-tight {
  margin-top: 32px;
}
.section-title {
  font-size: 22px;
  font-weight: 600;
  margin: 0;
}
.code-card :deep(.n-code) {
  background: transparent;
}
@media (max-width: 640px) {
  .hero-title {
    font-size: 26px;
  }
}
</style>
