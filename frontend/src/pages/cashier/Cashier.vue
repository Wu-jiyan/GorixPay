<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import axios from 'axios';
import dayjs from 'dayjs';
import { useMessage } from 'naive-ui';
import { cashierApi } from '@/api';
import type { CashierOrder, OrderStatus, StartPaymentResult } from '@/api/types';
import { errorMessage } from '@/api/client';
import BrandMark from '@/components/BrandMark.vue';
import PaymentBrandIcon from '@/components/PaymentBrandIcon.vue';
import { siteName } from '@/stores/site';
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
// 订单的 payment_type 既可能是品牌（alipay / wxpay / qqpay …），也可能是支付形态（jump / qrcode）。
// 只给能对上品牌的画图标；形态类不硬塞，免得买家把跳转/扫码当成某个钱包。
const BRAND_PAY_TYPES = new Set(['alipay', 'wxpay', 'qqpay', 'usdt', 'bank', 'cashier', 'sandbox']);
const payTypeBrand = computed(() => {
  const type = order.value?.payment_type ?? '';
  return BRAND_PAY_TYPES.has(type) ? type : '';
});
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

/* ---------- 纯展示辅助：屏幕宽度 / 二维码尺寸 / 面板标题 ---------- */
// 二维码尺寸跟着屏幕走：手机上固定 220 会顶出卡片，180 才放得下（390px 宽实测）。
const viewportWidth = ref(typeof window === 'undefined' ? 1280 : window.innerWidth);
const isNarrow = computed(() => viewportWidth.value <= 768);
const qrSize = computed(() => (isNarrow.value ? 180 : 220));

const panelTitle = computed(() => {
  if (payType.value === 'qrcode') return '扫码支付';
  if (payType.value === 'jump' || payType.value === 'urlscheme') return '跳转支付';
  return '支付信息';
});

// 轮询是否在跑（仅用于"正在确认支付结果…"的转圈反馈，不改轮询本身）
const pollingActive = ref(false);

function clearTimers() {
  if (tickTimer !== undefined) {
    window.clearInterval(tickTimer);
    tickTimer = undefined;
  }
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer);
    pollTimer = undefined;
    pollingActive.value = false;
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
  pollingActive.value = false;
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
  pollingActive.value = true;
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

// 页面切到后台时暂停轮询，切回来再按原间隔（2 秒）继续；接口与间隔都没动。
let pausedByHide = false;
function handleVisibilityChange() {
  if (document.hidden) {
    if (pollTimer !== undefined) {
      pausedByHide = true;
      stopPolling();
    }
    return;
  }
  if (!pausedByHide) return;
  pausedByHide = false;
  const status = order.value?.status;
  if ((status === 'pending' || status === 'processing') && canPay.value) startPolling();
}

function handleResize() {
  viewportWidth.value = window.innerWidth;
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
  document.addEventListener('visibilitychange', handleVisibilityChange);
  window.addEventListener('resize', handleResize);
  void loadOrder();
});

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  window.removeEventListener('resize', handleResize);
  clearTimers();
});
</script>

<template>
  <div class="cashier-page">
    <div class="cashier-shell">
      <header class="cashier-head">
        <brand-mark :size="30" subtitle="收银台" />
        <n-tag size="small" round :bordered="false" type="success">安全支付</n-tag>
      </header>

      <n-card class="cashier-card" :bordered="false">
        <!-- 加载 -->
        <div v-if="viewState === 'loading'" class="state-loading">
          <n-spin size="large" />
          <n-text depth="3">正在加载订单信息…</n-text>
        </div>

        <!-- 打不开 -->
        <n-result
          v-else-if="viewState === 'error'"
          status="error"
          title="无法打开收银台"
          :description="errorMsg"
        >
          <template #footer>
            <n-button v-if="tradeNo" type="primary" block @click="loadOrder">重新加载</n-button>
            <n-button v-else text tag="a" href="/">返回首页</n-button>
          </template>
        </n-result>

        <!-- 已支付 -->
        <n-result
          v-else-if="viewState === 'paid'"
          status="success"
          title="支付成功"
          :description="`订单 ${order?.trade_no ?? ''} 已完成付款`"
        >
          <template #footer>
            <div class="result-body">
              <n-descriptions :column="1" size="small" label-placement="left" class="result-meta">
                <n-descriptions-item label="支付金额">{{ formatMoney(order?.amount) }}</n-descriptions-item>
                <n-descriptions-item label="支付时间">{{ formatDateTime(paidAt) }}</n-descriptions-item>
              </n-descriptions>
              <n-text v-if="order?.return_url" depth="3" class="result-hint">
                正在返回商户页面，如未自动跳转请点击下方按钮
              </n-text>
              <n-button type="primary" block tag="a" href="/">返回首页</n-button>
            </div>
          </template>
        </n-result>

        <!-- 已关闭 -->
        <n-result
          v-else-if="viewState === 'closed'"
          status="warning"
          title="订单已关闭"
          description="该订单已关闭，无法继续支付。请返回商户重新下单。"
        >
          <template #footer>
            <div class="result-body">
              <n-button type="primary" block @click="loadOrder">刷新状态</n-button>
              <n-button block tag="a" href="/">返回首页</n-button>
            </div>
          </template>
        </n-result>

        <!-- 支付失败 -->
        <n-result
          v-else-if="viewState === 'failed'"
          status="error"
          title="支付失败"
          description="支付未完成，请返回商户重新发起支付或联系商户处理。"
        >
          <template #footer>
            <div class="result-body">
              <n-button type="primary" block @click="loadOrder">刷新状态</n-button>
              <n-button block tag="a" href="/">返回首页</n-button>
            </div>
          </template>
        </n-result>

        <!-- 已超时 -->
        <n-result
          v-else-if="viewState === 'expired'"
          status="error"
          title="订单已超时"
          description="支付时间已结束，该订单不能再付款。请返回商户重新下单后再支付。"
        >
          <template #footer>
            <div class="result-body">
              <n-button type="primary" block tag="a" href="/">返回首页</n-button>
              <n-button block @click="loadOrder">刷新状态</n-button>
            </div>
          </template>
        </n-result>

        <!-- 待支付 -->
        <div v-else class="pay-body">
          <div class="pay-head">
            <n-text depth="2" class="pay-name">{{ order?.name || '订单支付' }}</n-text>
            <n-statistic :value="formatMoney(order?.amount)" class="pay-amount">
              <template #label>应付金额</template>
            </n-statistic>
            <div class="pay-countdown">
              <n-text depth="3">剩余支付时间</n-text>
              <span class="countdown">{{ countdownText }}</span>
            </div>
          </div>

          <n-descriptions :column="1" size="small" label-placement="left" class="pay-meta">
            <n-descriptions-item label="订单号">
              <n-space align="center" :size="6">
                <span class="mono order-no">{{ order?.trade_no }}</span>
                <n-button text type="primary" size="tiny" @click="handleCopy(order?.trade_no ?? '')">
                  复制
                </n-button>
              </n-space>
            </n-descriptions-item>
            <n-descriptions-item label="支付方式">
              <n-space align="center" :size="8">
                <payment-brand-icon v-if="payTypeBrand" :type="payTypeBrand" :size="18" />
                <span>{{ paymentTypeText(order?.payment_type) }}</span>
              </n-space>
            </n-descriptions-item>
            <n-descriptions-item label="商品名称">{{ order?.name || '-' }}</n-descriptions-item>
            <n-descriptions-item label="订单状态">
              <n-tag size="small" round :bordered="false" :type="order?.status === 'processing' ? 'info' : 'warning'">
                {{ order?.status === 'processing' ? '支付中' : '待支付' }}
              </n-tag>
            </n-descriptions-item>
          </n-descriptions>

          <n-alert v-if="expired" type="warning" :bordered="false" class="pay-alert">
            订单已过期，无法继续支付，请返回商户重新下单。
          </n-alert>

          <!-- 轮询反馈：能付款时一直显示，让买家知道页面在盯着结果 -->
          <div v-if="pollingActive" class="polling-row">
            <n-spin size="small" />
            <n-text depth="3" class="polling-text">正在确认支付结果…每 2 秒自动检测一次</n-text>
          </div>

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
            <n-text depth="3" class="pay-tip">
              点击后将跳转到对应渠道完成支付，本页会自动刷新支付结果。
            </n-text>
          </div>

          <n-card v-else size="small" class="pay-panel" :bordered="true">
            <template #header>
              <n-space align="center" :size="8">
                <n-text strong>{{ panelTitle }}</n-text>
                <n-tag size="tiny" round :bordered="false">{{ payType || 'unknown' }}</n-tag>
              </n-space>
            </template>

            <div v-if="qrImageUrl" class="qr-box">
              <div class="qr-frame" :style="{ width: `${qrSize + 14}px` }">
                <img :src="qrImageUrl" alt="支付二维码" class="qr-image" />
              </div>
              <n-text depth="3" class="qr-tip">
                请使用{{ paymentTypeText(order?.payment_type) }}扫描二维码完成支付
              </n-text>
            </div>

            <div v-else-if="payType === 'qrcode' && payInfoText" class="qr-box">
              <div class="qr-frame">
                <n-qr-code :value="payInfoText" :size="qrSize" :padding="8" />
              </div>
              <n-text depth="3" class="qr-tip">
                请使用{{ paymentTypeText(order?.payment_type) }}扫描二维码完成支付
              </n-text>
              <n-button size="small" block secondary @click="handleCopy(payInfoText)">
                复制二维码内容
              </n-button>
              <n-collapse class="qr-raw">
                <n-collapse-item title="查看二维码原始内容" name="raw">
                  <pre class="pay-info mono">{{ payInfoText }}</pre>
                </n-collapse-item>
              </n-collapse>
            </div>

            <div v-else-if="payLink" class="link-box">
              <n-text depth="3" class="qr-tip">
                已在新窗口打开支付页面，如未打开请点击下方按钮。
              </n-text>
              <n-button
                type="primary"
                block
                tag="a"
                :href="payLink"
                target="_blank"
                rel="noopener noreferrer"
              >
                打开支付页面
              </n-button>
            </div>

            <div v-else class="info-box">
              <pre class="pay-info mono">{{ payInfoText || '渠道未返回支付信息' }}</pre>
              <n-button size="small" block :disabled="!payInfoText" @click="handleCopy(payInfoText)">
                复制支付信息
              </n-button>
            </div>

            <template #footer>
              <n-space align="center" justify="space-between" :size="10" class="panel-foot">
                <n-text depth="3" class="panel-foot-text">
                  支付完成后本页每 2 秒自动检测订单状态
                </n-text>
                <n-button text type="primary" :loading="starting" :disabled="!canPay" @click="handleStart">
                  重新发起支付
                </n-button>
              </n-space>
            </template>
          </n-card>
        </div>
      </n-card>

      <footer class="cashier-foot">
        <n-text depth="3">本页面由 {{ siteName }} 提供，支付信息仅用于本次交易</n-text>
        <router-link to="/doc" class="foot-link">查看接入文档</router-link>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.cashier-page {
  min-height: 100vh;
  background: var(--gp-bg);
  padding: 40px var(--gp-gutter) 32px;
  display: flex;
  align-items: flex-start;
  justify-content: center;
}
.cashier-shell {
  width: 100%;
  max-width: 460px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.cashier-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 2px;
}
.cashier-card {
  border-radius: var(--gp-radius-lg);
  box-shadow: var(--gp-shadow);
}
.cashier-card :deep(.n-card__content) {
  padding: 22px;
}
.state-loading {
  padding: 48px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  font-size: 13px;
}
.result-body {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.result-meta {
  text-align: left;
  margin: 0 auto;
  max-width: 300px;
}
.result-hint {
  font-size: 12.5px;
}
.pay-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.pay-head {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--gp-line-soft);
}
.pay-name {
  font-size: 14px;
  max-width: 100%;
  text-align: center;
  word-break: break-word;
}
.pay-amount :deep(.n-statistic-label) {
  font-size: 12.5px;
  color: var(--gp-text-3);
  text-align: center;
}
.pay-amount :deep(.n-statistic-value__content),
.pay-amount :deep(.n-statistic-value) {
  font-size: 34px;
  font-weight: 700;
  letter-spacing: -0.5px;
  color: var(--gp-text-1);
  font-variant-numeric: tabular-nums;
}
.pay-countdown {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
}
.countdown {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  font-weight: 600;
  color: #d03050;
  font-variant-numeric: tabular-nums;
}
.pay-meta {
  margin: 0;
}
.order-no {
  font-size: 12.5px;
  color: var(--gp-text-2);
  word-break: break-all;
}
.polling-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--gp-radius);
  background: var(--gp-accent-soft);
  border: 1px solid var(--gp-line-soft);
}
.polling-text {
  font-size: 12.5px;
  line-height: 1.5;
}
.pay-action {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.pay-tip {
  font-size: 12px;
  line-height: 1.7;
  text-align: center;
}
.pay-panel {
  border-radius: var(--gp-radius);
  background: var(--gp-bg-soft);
}
.pay-panel :deep(.n-card__content) {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.qr-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
/* 二维码必须压在白底上，深色皮肤也保持白，否则扫不出来 */
.qr-frame {
  display: grid;
  place-items: center;
  padding: 6px;
  border: 1px solid var(--gp-line);
  border-radius: var(--gp-radius);
  background: #fff;
  max-width: 100%;
}
.qr-image {
  display: block;
  width: 100%;
  height: auto;
  max-width: 100%;
}
.qr-tip {
  font-size: 12.5px;
  line-height: 1.7;
  text-align: center;
}
.qr-raw {
  width: 100%;
}
.qr-raw :deep(.n-collapse-item__header-main) {
  font-size: 12.5px;
}
.pay-info {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--gp-line);
  border-radius: var(--gp-radius-sm);
  background: var(--gp-surface);
  color: var(--gp-text-2);
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 220px;
  overflow: auto;
}
.info-box,
.link-box {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}
.panel-foot {
  width: 100%;
}
.panel-foot-text {
  font-size: 12px;
  line-height: 1.6;
}
.cashier-foot {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  font-size: 11.5px;
  text-align: center;
  line-height: 1.6;
}
.foot-link {
  color: var(--gp-accent);
}
@media (max-width: 768px) {
  .cashier-page {
    padding: 18px var(--gp-gutter) 24px;
  }
  .cashier-shell {
    gap: 12px;
  }
  .cashier-card :deep(.n-card__content) {
    padding: 16px;
  }
  .pay-amount :deep(.n-statistic-value__content),
  .pay-amount :deep(.n-statistic-value) {
    font-size: 28px;
  }
  .pay-head {
    padding-bottom: 12px;
  }
  .state-loading {
    padding: 36px 0;
  }
}
</style>