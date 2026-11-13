<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import axios from 'axios';
import { useMessage } from 'naive-ui';
import { cashierApi } from '@/api';
import type { CashierOrder } from '@/api/types';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney, orderStatus, paymentTypeText } from '@/utils/format';

const route = useRoute();
const router = useRouter();
const message = useMessage();

const tradeNo = String(route.params.tradeNo ?? '');
const loading = ref(true);
const errorMsg = ref('');
const order = ref<CashierOrder | null>(null);
const submitting = ref(false);

const statusInfo = computed(() => orderStatus(order.value?.status));

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

onMounted(() => {
  void loadOrder();
});
</script>

<template>
  <div class="sandbox-page">
    <div class="sandbox-card">
      <div class="sandbox-brand">
        <span class="brand-mark">G</span>
        <div>
          <div class="brand-name">GorixPay</div>
          <div class="brand-sub">沙箱支付</div>
        </div>
      </div>

      <n-alert type="warning" :bordered="false" class="sandbox-alert" title="沙箱支付环境">
        仅用于开发联调，不会产生真实扣款，也不会触发上游渠道。点击「模拟支付成功」将按订单金额入账并推送异步通知。
      </n-alert>

      <div v-if="loading" class="sandbox-loading">
        <n-spin size="large" />
        <p class="loading-text">正在加载订单信息…</p>
      </div>

      <n-result
        v-else-if="errorMsg"
        status="error"
        title="无法进入沙箱支付"
        :description="errorMsg"
      >
        <template #footer>
          <n-button @click="loadOrder">重新加载</n-button>
        </template>
      </n-result>

      <template v-else>
        <div class="order-box">
          <div class="order-name">{{ order?.name || '沙箱测试商品' }}</div>
          <div class="order-amount">{{ formatMoney(order?.amount) }}</div>
          <div class="order-status">
            <n-tag :type="statusInfo.type" size="small" round>{{ statusInfo.text }}</n-tag>
            <span class="order-type">{{ paymentTypeText(order?.payment_type) }}</span>
          </div>
        </div>

        <div class="order-meta">
          <div class="meta-row">
            <span class="meta-label">订单号</span>
            <span class="meta-value mono">{{ order?.trade_no }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">金额（分）</span>
            <span class="meta-value mono">{{ order?.amount }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">币种</span>
            <span class="meta-value">{{ order?.currency }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">过期时间</span>
            <span class="meta-value">{{ formatDateTime(order?.expires_at) }}</span>
          </div>
        </div>

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

        <n-button
          type="primary"
          size="large"
          block
          class="submit-btn"
          :loading="submitting"
          :disabled="!order || order.status !== 'pending'"
          @click="handleComplete"
        >
          模拟支付成功
        </n-button>
        <p class="sandbox-tip">完成后会跳回收银台，由收银台轮询确认最终状态。</p>
      </template>

      <div class="sandbox-foot">
        <router-link to="/">返回首页</router-link>
        <span class="sep">·</span>
        <router-link to="/doc">开发文档</router-link>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sandbox-page {
  min-height: 100vh;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 48px 16px;
  background: radial-gradient(900px 420px at 15% 0%, #fef3c7 0%, transparent 62%),
    linear-gradient(160deg, #fffdf5 0%, #f8fafc 55%, #f5f7fb 100%);
}
.sandbox-card {
  width: 100%;
  max-width: 480px;
  background: #fff;
  border-radius: 18px;
  box-shadow: 0 20px 56px rgba(15, 23, 42, 0.12);
  padding: 26px 26px 20px;
}
.sandbox-brand {
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
  background: linear-gradient(135deg, #d97706, #f59e0b);
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
.sandbox-alert {
  margin-top: 16px;
}
.sandbox-loading {
  padding: 48px 0;
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
.order-box {
  text-align: center;
  padding: 22px 0 18px;
  border-bottom: 1px dashed #e2e8f0;
}
.order-name {
  font-size: 15px;
  color: #334155;
  margin-bottom: 10px;
  word-break: break-all;
}
.order-amount {
  font-size: 36px;
  font-weight: 700;
  color: #0f172a;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.5px;
}
.order-status {
  margin-top: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  font-size: 12.5px;
  color: #64748b;
}
.order-type {
  color: #94a3b8;
}
.order-meta {
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
.submit-btn {
  margin-top: 20px;
}
.sandbox-tip {
  margin: 12px 0 0;
  font-size: 12px;
  color: #94a3b8;
  text-align: center;
  line-height: 1.7;
}
.sandbox-foot {
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid #eef2f7;
  font-size: 11.5px;
  color: #94a3b8;
  display: flex;
  gap: 6px;
  justify-content: center;
}
.sandbox-foot a:hover {
  color: #2563eb;
}
.sep {
  color: #cbd5e1;
}
@media (max-width: 768px) {
  .sandbox-page {
    padding: 20px 12px;
  }
  .sandbox-card {
    padding: 20px 16px 16px;
    border-radius: 14px;
  }
  .order-amount {
    font-size: 30px;
  }
}
</style>
