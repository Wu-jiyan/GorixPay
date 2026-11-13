<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import axios from 'axios';
import dayjs from 'dayjs';
import { useMessage } from 'naive-ui';
import { cashierApi } from '@/api';
import type { CashierOrder, OrderStatus, StartPaymentResult } from '@/api/types';
import { errorMessage } from '@/api/client';
import { copyText, formatDateTime, formatMoney, paymentTypeText } from '@/utils/format';

const route = useRoute();
const message = useMessage();

const tradeNo = String(route.query.trade_no ?? '');
const loading = ref(true);
const errorMsg = ref('');
const order = ref<CashierOrder | null>(null);
const paidAt = ref<string | null>(null);
const starting = ref(false);
const payResult = ref<StartPaymentResult | null>(null);
const serverExpired = ref(false);
const now = ref(Date.now());

let tickTimer: number | undefined;
let pollTimer: number | undefined;
let returnTimer: number | undefined;

const isImageUrl = (value: string): boolean => {
  if (/^data:image\//i.test(value)) return true;
  if (!/^(https?:\/\/|\/)/i.test(value)) return false;
  return /\.(png|jpe?g|gif|webp|svg|bmp)(\?.*)?$/i.test(value);
};

const expiresAt = computed(() => {
  const raw = order.value?.expires_at;
  if (!raw) return 0;
  const time = dayjs(raw).valueOf();
  return Number.isFinite(time) ? time : 0;
});
const remainingMs = computed(() => (expiresAt.value ? Math.max(0, expiresAt.value - now.value) : 0));
const expired = computed(() => Boolean(order.value) && expiresAt.value > 0 && remainingMs.value <= 0);

const countdownText = computed(() => {
  if (!expiresAt.value) return '--:--';
  const total = Math.max(0, Math.ceil(remainingMs.value / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const pad = (value: number) => String(value).padStart(2, '0');
  return hours > 0 ? `${pad(hours)}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`;
});

const viewState = computed<'loading' | 'error' | 'paid' | 'closed' | 'failed' | 'expired' | 'pay'>(() => {
  if (loading.value) return 'loading';
  if (errorMsg.value) return 'error';
  const status = order.value?.status;
  if (status === 'paid') return 'paid';
  if (status === 'closed') return 'closed';
  if (status === 'failed') return 'failed';
  if (serverExpired.value || expired.value) return 'expired';
  return 'pay';
});

const payType = computed(() => payResult.value?.pay_type ?? '');
const payInfoText = computed(() => {
  const info = payResult.value?.pay_info;
  if (typeof info === 'string') return info;
  if (info && typeof info === 'object') return JSON.stringify(info, null, 2);
  return '';
});
const qrImageUrl = computed(() => {
  if (payType.value !== 'qrcode') return '';
  const info = payResult.value?.pay_info;
  if (typeof info !== 'string') return '';
  return isImageUrl(info) ? info : '';
});
const payLink = computed(() => {
  if (payType.value !== 'jump' && payType.value !== 'urlscheme') return '';
  const info = payResult.value?.pay_info;
  return typeof info === 'string' && /^https?:\/\//i.test(info) ? info : '';
});
const canPay = computed(() => {
  const status = order.value?.status;
  return Boolean(order.value) && (status === 'pending' || status === 'processing') && !expired.value && !serverExpired.value;
});

function clearTimers() {
  if (tickTimer !== undefined) {
    window.clearInterval(tickTimer);
    tickTimer = undefined;
  }
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer);
    pollTimer = undefined;
  }
  if (returnTimer !== undefined) {
    window.clearTimeout(returnTimer);
    returnTimer = undefined;
  }
}

function stopPolling() {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer);
    pollTimer = undefined;
  }
}

function stopTick() {
  if (tickTimer !== undefined) {
    window.clearInterval(tickTimer);
    tickTimer = undefined;
  }
}

function applyStatus(status: string, paidTime?: string | null) {
  if (!order.value) return;
  order.value.status = status as OrderStatus;
  if (status === 'paid') {
    paidAt.value = paidTime ?? new Date().toISOString();
    stopPolling();
    stopTick();
    scheduleReturn();
  } else if (status === 'closed' || status === 'failed') {
    stopPolling();
    stopTick();
  }
}

// 支付完成后浏览器要回到商户 return_url，并带上与异步通知同格式的签名参数，
// 这样商户的 EpayCore::verifyReturn 才能验签通过（上游 /pay/return/{trade_no}/）。
function scheduleReturn() {
  if (returnTimer !== undefined) return;
  if (!order.value?.return_url) return;
  returnTimer = window.setTimeout(() => {
    window.location.replace(`/pay/return/${encodeURIComponent(tradeNo)}/`);
  }, 1200);
}

function startPolling() {
  stopPolling();
  pollTimer = window.setInterval(async () => {
    if (!tradeNo) return;
    try {
      const res = await cashierApi.status(tradeNo);
      applyStatus(res.status, res.paid_at ?? null);
    } catch {
      // 单次轮询失败不打扰用户，下一轮继续
    }
  }, 2000);
}

async function loadOrder() {
  if (!tradeNo) {
    loading.value = false;
    errorMsg.value = '缺少订单号参数，无法打开收银台';
    return;
  }
  loading.value = true;
  errorMsg.value = '';
  try {
    const data = await cashierApi.order(tradeNo);
    order.value = data;
    serverExpired.value = false;
    if (data.pay_type && data.pay_info !== undefined) {
      payResult.value = {
        pay_type: data.pay_type,
        pay_info: data.pay_info,
        status: data.status,
        channel: data.channel,
      };
    }
    if (data.status === 'pending' || data.status === 'processing') startPolling();
    if (data.status === 'paid') scheduleReturn();
    // 商户在 submit.php 里已经指定支付方式时，上游会直接把浏览器送到通道页面；
    // 我们把这一步放进收银台，所以这里自动发起一次，避免买家多点一次。
    if (
      (data.status === 'pending' || data.status === 'processing') &&
      data.payment_type &&
      data.payment_type !== 'cashier'
    ) {
      void handleStart();
    }
  } catch (err) {
    const status = axios.isAxiosError(err) ? err.response?.status : undefined;
    if (status === 404) {
      errorMsg.value = '订单不存在或链接已失效，请返回商户重新下单';
    } else if (status === 401 || status === 403) {
      errorMsg.value = '订单访问凭证异常，请刷新页面后重试';
    } else {
      errorMsg.value = errorMessage(err, '订单加载失败，请稍后重试或联系商户');
    }
  } finally {
    loading.value = false;
  }
}

async function handleStart() {
  if (starting.value || !canPay.value) return;
  starting.value = true;
  try {
    const res = await cashierApi.start(tradeNo);
    payResult.value = res;
    if (res.status) applyStatus(res.status);
    const info = typeof res.pay_info === 'string' ? res.pay_info : '';
    if (res.pay_type === 'jump' || res.pay_type === 'urlscheme') {
      if (info && /^https?:\/\//i.test(info)) {
        const opened = window.open(info, '_blank', 'noopener,noreferrer');
        if (!opened) {
          message.warning('浏览器拦截了新窗口，请点击下方链接手动打开支付页面');
        } else {
          message.info('已打开支付页面，请在新窗口完成支付');
        }
      } else {
        message.info('请按收银台展示的支付指引完成付款');
      }
    } else if (res.pay_type === 'qrcode') {
      message.info('请使用手机扫码完成支付');
    } else {
      message.info('已生成支付信息，请按提示完成支付');
    }
    if (res.status === 'paid') message.success('订单已支付成功');
  } catch (err) {
    const status = axios.isAxiosError(err) ? err.response?.status : undefined;
    if (status === 409) {
      message.error('该订单已支付，请勿重复支付');
      applyStatus('paid');
    } else if (status === 410) {
      message.error('订单已过期，请返回商户重新下单');
      serverExpired.value = true;
      stopPolling();
      stopTick();
    } else if (status === 503) {
      message.error('当前没有可用的支付通道，请稍后再试');
    } else {
      message.error(errorMessage(err, '发起支付失败，请稍后重试'));
    }
  } finally {
    starting.value = false;
  }
}

async function handleCopy(text: string) {
  if (!text) return;
  const ok = await copyText(text);
  if (ok) message.success('已复制');
  else message.error('复制失败，请手动选择文本复制');
}

watch(expired, (value) => {
  if (value) {
    stopPolling();
    stopTick();
  }
});

onMounted(() => {
  tickTimer = window.setInterval(() => {
    now.value = Date.now();
  }, 1000);
  void loadOrder();
});

onBeforeUnmount(() => {
  clearTimers();
});
</script>

<template>
  <div class="cashier-page">
    <div class="cashier-card">
      <div class="cashier-brand">
        <span class="brand-mark">G</span>
        <div>
          <div class="brand-name">GorixPay</div>
          <div class="brand-sub">安全收银台</div>
        </div>
      </div>

      <div v-if="viewState === 'loading'" class="cashier-loading">
        <n-spin size="large" />
        <p class="loading-text">正在加载订单信息…</p>
      </div>

      <n-result
        v-else-if="viewState === 'error'"
        status="error"
        title="无法打开收银台"
        :description="errorMsg"
      >
        <template #footer>
          <n-button v-if="tradeNo" @click="loadOrder">重新加载</n-button>
        </template>
      </n-result>

      <n-result
        v-else-if="viewState === 'paid'"
        status="success"
        title="支付成功"
        :description="`订单 ${order?.trade_no ?? ''} 已完成付款`"
      >
        <template #footer>
          <div class="result-detail">
            <div class="result-row">
              <span class="result-label">支付金额</span>
              <span class="result-amount">{{ formatMoney(order?.amount) }}</span>
            </div>
            <div class="result-row">
              <span class="result-label">支付时间</span>
              <span>{{ formatDateTime(paidAt) }}</span>
            </div>
          </div>
          <div class="result-actions">
            <n-button type="primary" tag="a" href="/">返回首页</n-button>
          </div>
        </template>
      </n-result>

      <n-result
        v-else-if="viewState === 'closed'"
        status="warning"
        title="订单已关闭"
        description="该订单已关闭或超时未支付，请返回商户重新下单。"
      >
        <template #footer>
          <n-button @click="loadOrder">刷新状态</n-button>
        </template>
      </n-result>

      <n-result
        v-else-if="viewState === 'failed'"
        status="error"
        title="支付失败"
        description="支付未完成，请返回商户重新发起支付或联系商户处理。"
      >
        <template #footer>
          <n-button @click="loadOrder">刷新状态</n-button>
        </template>
      </n-result>

      <n-result
        v-else-if="viewState === 'expired'"
        status="warning"
        title="订单已过期"
        description="支付时间已结束，请返回商户重新下单后再支付。"
      >
        <template #footer>
          <n-button @click="loadOrder">刷新状态</n-button>
        </template>
      </n-result>

      <div v-else class="pay-body">
        <div class="pay-head">
          <div class="pay-title">{{ order?.name || '订单支付' }}</div>
          <div class="pay-amount">{{ formatMoney(order?.amount) }}</div>
          <div class="pay-countdown">
            剩余支付时间
            <span class="countdown-value">{{ countdownText }}</span>
          </div>
        </div>

        <div class="pay-meta">
          <div class="meta-row">
            <span class="meta-label">订单号</span>
            <span class="meta-value mono">{{ order?.trade_no }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">支付类型</span>
            <span class="meta-value">{{ paymentTypeText(order?.payment_type) }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">订单状态</span>
            <span class="meta-value">
              <n-tag size="small" :type="order?.status === 'processing' ? 'info' : 'warning'" round>
                {{ order?.status === 'processing' ? '支付中' : '待支付' }}
              </n-tag>
            </span>
          </div>
        </div>

        <n-alert v-if="expired" type="warning" :bordered="false" class="pay-alert">
          订单已过期，无法继续支付，请返回商户重新下单。
        </n-alert>

        <div v-if="!payResult" class="pay-action">
          <n-button
            type="primary"
            size="large"
            block
            :loading="starting"
            :disabled="!canPay"
            @click="handleStart"
          >
            立即支付 {{ formatMoney(order?.amount) }}
          </n-button>
          <p class="pay-tip">点击后将跳转到对应渠道完成支付，本页会自动刷新支付结果。</p>
        </div>

        <div v-else class="pay-panel">
          <div class="panel-head">
            <span class="panel-title">
              {{
                payType === 'qrcode'
                  ? '扫码支付'
                  : payType === 'jump' || payType === 'urlscheme'
                    ? '跳转支付'
                    : '支付信息'
              }}
            </span>
            <n-tag size="tiny" :bordered="false">{{ payType || 'unknown' }}</n-tag>
          </div>

          <div v-if="qrImageUrl" class="qr-box">
            <img :src="qrImageUrl" alt="支付二维码" class="qr-image" />
            <p class="qr-tip">请使用{{ paymentTypeText(order?.payment_type) }}扫描二维码完成支付</p>
          </div>

          <div v-else-if="payType === 'qrcode'" class="qr-text">
            <p class="qr-tip">请复制下方二维码内容，使用 {{ paymentTypeText(order?.payment_type) }} 扫码或粘贴支付：</p>
            <pre class="pay-info mono">{{ payInfoText }}</pre>
            <n-button size="small" @click="handleCopy(payInfoText)">复制二维码内容</n-button>
          </div>

          <div v-else-if="payLink" class="link-box">
            <p class="qr-tip">已在新窗口打开支付页面，如未打开请点击下方按钮：</p>
            <n-button type="primary" tag="a" :href="payLink" target="_blank" rel="noopener noreferrer">
              打开支付页面
            </n-button>
          </div>

          <div v-else class="info-box">
            <pre class="pay-info mono">{{ payInfoText || '渠道未返回支付信息' }}</pre>
            <n-button size="small" :disabled="!payInfoText" @click="handleCopy(payInfoText)">复制支付信息</n-button>
          </div>

          <div class="panel-foot">
            <span class="muted">支付完成后本页每 2 秒自动检测订单状态</span>
            <n-button text type="primary" :loading="starting" @click="handleStart">重新发起支付</n-button>
          </div>
        </div>
      </div>

      <div class="cashier-foot">
        <span>本页面由 GorixPay 提供，支付信息经加密传输</span>
        <span class="foot-sep">·</span>
        <router-link to="/doc">查看接入文档</router-link>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cashier-page {
  min-height: 100vh;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 48px 16px;
  background: radial-gradient(900px 420px at 15% 0%, #dbeafe 0%, transparent 62%),
    linear-gradient(160deg, #f8fbff 0%, #eef4ff 55%, #f5f7fb 100%);
}
.cashier-card {
  width: 100%;
  max-width: 480px;
  background: #fff;
  border-radius: 18px;
  box-shadow: 0 20px 56px rgba(15, 23, 42, 0.12);
  padding: 26px 26px 20px;
}
.cashier-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 18px;
  border-bottom: 1px solid #eef2f7;
}
.brand-mark {
  width: 38px;
  height: 38px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  color: #fff;
  font-weight: 700;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
}
.brand-name {
  font-weight: 700;
  color: #0f172a;
  font-size: 16px;
}
.brand-sub {
  font-size: 11.5px;
  color: #94a3b8;
}
.cashier-loading {
  padding: 56px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
}
.loading-text {
  margin: 0;
  font-size: 13px;
  color: #64748b;
}
.pay-body {
  padding-top: 20px;
}
.pay-head {
  text-align: center;
  padding-bottom: 18px;
  border-bottom: 1px dashed #e2e8f0;
}
.pay-title {
  font-size: 15px;
  color: #334155;
  margin-bottom: 10px;
  word-break: break-all;
}
.pay-amount {
  font-size: 38px;
  font-weight: 700;
  color: #0f172a;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.5px;
}
.pay-countdown {
  margin-top: 8px;
  font-size: 12.5px;
  color: #64748b;
}
.countdown-value {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: #dc2626;
  font-weight: 600;
  margin-left: 4px;
}
.pay-meta {
  padding: 16px 0;
  border-bottom: 1px dashed #e2e8f0;
}
.meta-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
  padding: 5px 0;
}
.meta-label {
  color: #94a3b8;
  flex: none;
}
.meta-value {
  color: #334155;
  text-align: right;
  word-break: break-all;
}
.meta-value.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
}
.pay-alert {
  margin-top: 16px;
}
.pay-action {
  padding-top: 20px;
}
.pay-tip {
  margin: 12px 0 0;
  font-size: 12px;
  color: #94a3b8;
  text-align: center;
  line-height: 1.7;
}
.pay-panel {
  margin-top: 18px;
  padding: 16px;
  border: 1px solid #e8eefc;
  border-radius: 14px;
  background: #f8fbff;
}
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 14px;
}
.panel-title {
  font-weight: 600;
  color: #1e293b;
  font-size: 14px;
}
.qr-box {
  text-align: center;
}
.qr-image {
  width: 200px;
  height: 200px;
  object-fit: contain;
  border-radius: 10px;
  background: #fff;
  padding: 8px;
  border: 1px solid #e2e8f0;
}
.qr-tip {
  margin: 10px 0 0;
  font-size: 12.5px;
  color: #64748b;
  line-height: 1.7;
  text-align: center;
}
.qr-text,
.info-box,
.link-box {
  text-align: center;
}
.pay-info {
  margin: 10px 0 12px;
  padding: 12px;
  background: #0f172a;
  color: #dbeafe;
  border-radius: 10px;
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
  text-align: left;
  max-height: 220px;
  overflow: auto;
}
.panel-foot {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 12px;
  flex-wrap: wrap;
}
.result-detail {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  margin-bottom: 16px;
}
.result-row {
  font-size: 13px;
  color: #64748b;
  display: flex;
  gap: 10px;
}
.result-amount {
  color: #0f172a;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.result-actions {
  display: flex;
  justify-content: center;
}
.cashier-foot {
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid #eef2f7;
  font-size: 11.5px;
  color: #94a3b8;
  display: flex;
  gap: 6px;
  justify-content: center;
  flex-wrap: wrap;
}
.cashier-foot a:hover {
  color: #2563eb;
}
.foot-sep {
  color: #cbd5e1;
}
.muted {
  color: #94a3b8;
}
@media (max-width: 768px) {
  .cashier-page {
    padding: 20px 12px;
  }
  .cashier-card {
    padding: 20px 16px 16px;
    border-radius: 14px;
  }
  .pay-amount {
    font-size: 32px;
  }
  .qr-image {
    width: 168px;
    height: 168px;
  }
}
</style>
