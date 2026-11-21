<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import axios from 'axios';
import { useMessage } from 'naive-ui';
import { cashierApi } from '@/api';
import type { CashierOrder } from '@/api/types';
import { errorMessage } from '@/api/client';
import BrandMark from '@/components/BrandMark.vue';
import PaymentBrandIcon from '@/components/PaymentBrandIcon.vue';
import { copyText, formatDateTime, formatMoney, orderStatus, paymentTypeText } from '@/utils/format';

const route = useRoute();
const router = useRouter();
const message = useMessage();

const tradeNo = String(route.params.tradeNo ?? '');
const loading = ref(true);
const errorMsg = ref('');
const order = ref<CashierOrder | null>(null);
const submitting = ref(false);

const statusInfo = computed(() => orderStatus(order.value?.status));

const BRAND_PAY_TYPES = new Set(['alipay', 'wxpay', 'qqpay', 'usdt', 'bank', 'cashier', 'sandbox']);
const payTypeBrand = computed(() => {
  const type = order.value?.payment_type ?? '';
  return BRAND_PAY_TYPES.has(type) ? type : '';
});

async function loadOrder() {
  if (!tradeNo) {
    loading.value = false;
    errorMsg.value = '缺少订单号，无法进入沙箱支付';
    return;
  }
  loading.value = true;
  errorMsg.value = '';
  try {
    order.value = await cashierApi.order(tradeNo);
  } catch (err) {
    const status = axios.isAxiosError(err) ? err.response?.status : undefined;
    if (status === 404) {
      errorMsg.value = '订单不存在或链接已失效，请返回商户重新下单';
    } else {
      errorMsg.value = errorMessage(err, '订单加载失败，请稍后重试');
    }
  } finally {
    loading.value = false;
  }
}

async function handleComplete() {
  if (submitting.value) return;
  submitting.value = true;
  try {
    const res = await cashierApi.sandboxComplete(tradeNo);
    message.success(res.status === 'paid' ? '沙箱支付成功，正在返回收银台' : '沙箱支付已提交');
    await router.replace({ path: '/cashier.php', query: { trade_no: tradeNo } });
  } catch (err) {
    const status = axios.isAxiosError(err) ? err.response?.status : undefined;
    if (status === 404) {
      message.error('沙箱渠道未启用，或订单不存在');
    } else if (status === 409) {
      message.error('订单当前状态不允许模拟支付');
    } else {
      message.error(errorMessage(err, '模拟支付失败，请稍后重试'));
    }
  } finally {
    submitting.value = false;
  }
}

async function handleCopy(text?: string) {
  if (!text) return;
  const ok = await copyText(text);
  if (ok) message.success('已复制');
  else message.error('复制失败，请手动选择文本复制');
}

onMounted(() => {
  void loadOrder();
});
</script>

<template>
  <div class="sandbox-page">
    <div class="sandbox-shell">
      <header class="sandbox-head">
        <brand-mark :size="30" subtitle="沙箱支付" />
        <n-tag size="small" round :bordered="false" type="warning">测试环境</n-tag>
      </header>

      <n-card class="sandbox-card" :bordered="false">
        <n-alert type="warning" :bordered="false" title="沙箱支付环境" class="sandbox-alert">
          仅用于开发联调，不会产生真实扣款，也不会触发上游渠道。点击「模拟支付成功」将按订单金额入账并推送异步通知。
        </n-alert>

        <div v-if="loading" class="state-loading">
          <n-spin size="large" />
          <n-text depth="3">正在加载订单信息…</n-text>
        </div>

        <n-result
          v-else-if="errorMsg"
          status="error"
          title="无法进入沙箱支付"
          :description="errorMsg"
        >
          <template #footer>
            <div class="result-body">
              <n-button type="primary" block @click="loadOrder">重新加载</n-button>
              <n-button block tag="a" href="/">返回首页</n-button>
            </div>
          </template>
        </n-result>

        <template v-else>
          <div class="order-head">
            <n-text depth="2" class="order-name">{{ order?.name || '沙箱测试商品' }}</n-text>
            <n-statistic :value="formatMoney(order?.amount)" class="order-amount">
              <template #label>订单金额</template>
            </n-statistic>
            <n-space align="center" justify="center" :size="8">
              <n-tag size="small" round :bordered="false" :type="statusInfo.type">{{ statusInfo.text }}</n-tag>
              <n-space align="center" :size="6">
                <payment-brand-icon v-if="payTypeBrand" :type="payTypeBrand" :size="16" />
                <n-text depth="3" class="order-type">{{ paymentTypeText(order?.payment_type) }}</n-text>
              </n-space>
            </n-space>
          </div>

          <n-descriptions :column="1" size="small" label-placement="left" class="order-meta">
            <n-descriptions-item label="订单号">
              <n-space align="center" :size="6">
                <span class="mono order-no">{{ order?.trade_no }}</span>
                <n-button text type="primary" size="tiny" @click="handleCopy(order?.trade_no)">复制</n-button>
              </n-space>
            </n-descriptions-item>
            <n-descriptions-item label="金额（分）">
              <span class="mono">{{ order?.amount }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="币种">{{ order?.currency }}</n-descriptions-item>
            <n-descriptions-item label="过期时间">{{ formatDateTime(order?.expires_at) }}</n-descriptions-item>
          </n-descriptions>

          <n-alert v-if="order?.status === 'paid'" type="success" :bordered="false" class="sandbox-alert">
            该订单已支付，可返回收银台查看支付结果。
          </n-alert>
          <n-alert
            v-else-if="order?.status === 'closed' || order?.status === 'failed'"
            type="error"
            :bordered="false"
            class="sandbox-alert"
          >
            订单当前状态为「{{ statusInfo.text }}」，无法模拟支付。
          </n-alert>

          <div class="submit-box">
            <n-button
              type="primary"
              size="large"
              block
              :loading="submitting"
              :disabled="!order || order.status !== 'pending'"
              @click="handleComplete"
            >
              模拟支付成功
            </n-button>
            <n-text depth="3" class="sandbox-tip">完成后会跳回收银台，由收银台轮询确认最终状态。</n-text>
          </div>
        </template>
      </n-card>

      <footer class="sandbox-foot">
        <router-link to="/" class="foot-link">返回首页</router-link>
        <span class="foot-sep">·</span>
        <router-link to="/doc" class="foot-link">开发文档</router-link>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.sandbox-page {
  min-height: 100vh;
  background: var(--gp-bg);
  padding: 40px var(--gp-gutter) 32px;
  display: flex;
  align-items: flex-start;
  justify-content: center;
}
.sandbox-shell {
  width: 100%;
  max-width: 480px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.sandbox-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 2px;
}
.sandbox-card {
  border-radius: var(--gp-radius-lg);
  box-shadow: var(--gp-shadow);
}
.sandbox-card :deep(.n-card__content) {
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.sandbox-alert {
  margin: 0;
}
.state-loading {
  padding: 44px 0;
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
.order-head {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--gp-line-soft);
}
.order-name {
  font-size: 14px;
  max-width: 100%;
  text-align: center;
  word-break: break-word;
}
.order-amount :deep(.n-statistic-label) {
  font-size: 12.5px;
  color: var(--gp-text-3);
  text-align: center;
}
.order-amount :deep(.n-statistic-value__content),
.order-amount :deep(.n-statistic-value) {
  font-size: 32px;
  font-weight: 700;
  letter-spacing: -0.5px;
  color: var(--gp-text-1);
  font-variant-numeric: tabular-nums;
}
.order-type {
  font-size: 12.5px;
}
.order-meta {
  margin: 0;
}
.order-no {
  font-size: 12.5px;
  color: var(--gp-text-2);
  word-break: break-all;
}
.submit-box {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.sandbox-tip {
  font-size: 12px;
  line-height: 1.7;
  text-align: center;
}
.sandbox-foot {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--gp-text-3);
}
.foot-link {
  color: var(--gp-accent);
}
.foot-sep {
  color: var(--gp-text-3);
}
@media (max-width: 768px) {
  .sandbox-page {
    padding: 18px var(--gp-gutter) 24px;
  }
  .sandbox-shell {
    gap: 12px;
  }
  .sandbox-card :deep(.n-card__content) {
    padding: 16px;
    gap: 14px;
  }
  .order-amount :deep(.n-statistic-value__content),
  .order-amount :deep(.n-statistic-value) {
    font-size: 28px;
  }
  .state-loading {
    padding: 32px 0;
  }
}
</style>