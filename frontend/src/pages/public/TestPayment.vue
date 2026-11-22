<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { NText, useMessage } from 'naive-ui';
import { ArrowForwardOutline, RefreshOutline, ShieldCheckmarkOutline } from '@vicons/ionicons5';
import PublicShell from '@/components/PublicShell.vue';
import PaymentBrandIcon from '@/components/PaymentBrandIcon.vue';
import { publicTestApi, type PublicTestOrder, type TestPageConfig } from '@/api/testPayment';
import { errorMessage } from '@/api/client';
import { copyText, paymentTypeText } from '@/utils/format';

// 对应上游 Epay 首页的「支付测试」：选支付方式、填金额，直接下一笔真实订单。
// 页面不登录，所以能不能用、能选哪些方式、最大金额全部由后端给（后端还会限流）。
const message = useMessage();
const loading = ref(true);
const submitting = ref(false);
const config = ref<TestPageConfig | null>(null);
const paymentType = ref('');
const amount = ref<number | null>(0.01);
const result = ref<PublicTestOrder | null>(null);

// 二维码尺寸与描述列数跟屏幕走：手机上二维码缩到 180，描述单列。
const narrow = ref(false);
let mql: MediaQueryList | null = null;
function syncNarrow(event: MediaQueryListEvent) {
  narrow.value = event.matches;
}
const qrSize = computed(() => (narrow.value ? 180 : 220));
const metaColumns = computed(() => (narrow.value ? 1 : 3));

const minYuan = computed(() => centsToYuan(config.value?.min_cents ?? 1));
const maxYuan = computed(() => centsToYuan(config.value?.max_cents ?? 100));
const presets = computed(() => [0.01, 0.1, 1].filter((value) => value >= minYuan.value && value <= maxYuan.value));
const methods = computed(() => config.value?.methods ?? []);

const payInfoText = computed(() => {
  const info = result.value?.pay_info;
  if (typeof info === 'string') return info;
  if (info == null) return '';
  try {
    return JSON.stringify(info);
  } catch {
    return String(info);
  }
});
const isQrcode = computed(() => result.value?.pay_type === 'qrcode' && /^https?:/.test(payInfoText.value));
const isJump = computed(() => result.value?.pay_type === 'jump' || result.value?.pay_type === 'urlscheme');

function centsToYuan(cents: number) {
  return Math.round(cents) / 100;
}

function yuanToCents(value: number | null) {
  if (value == null) return 0;
  return Math.round(value * 100);
}

async function load() {
  loading.value = true;
  try {
    const page = await publicTestApi.page();
    config.value = page;
    if (paymentType.value === '' && page.methods.length > 0) {
      paymentType.value = page.methods[0].code;
    }
    if (page.min_cents > 0 && yuanToCents(amount.value) < page.min_cents) {
      amount.value = centsToYuan(page.min_cents);
    }
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

async function submit() {
  const cents = yuanToCents(amount.value);
  if (paymentType.value === '') {
    message.warning('请选择支付方式');
    return;
  }
  if (cents < yuanToCents(minYuan.value) || cents > yuanToCents(maxYuan.value)) {
    message.warning(`金额需在 ￥${minYuan.value} 到 ￥${maxYuan.value} 之间`);
    return;
  }
  submitting.value = true;
  try {
    const created = await publicTestApi.create({ payment_type: paymentType.value, amount_cents: cents });
    result.value = created;
    // 和易支付的 test.php 一样：下单后直接把浏览器送到支付页（服务端渲染二维码 /
    // 复制链接 / 唤起按钮），不在这一页再解析一次二维码。后端已把 cashier_url
    // 指向站内支付页（订单带了支付方式时不再绕收银台）。
    if (created.cashier_url) {
      window.location.assign(created.cashier_url);
      return;
    }
    message.success('测试订单已创建');
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function copyTradeNo() {
  if (!result.value) return;
  const ok = await copyText(result.value.trade_no);
  if (ok) message.success('订单号已复制');
  else message.warning('复制失败，请手动选择');
}

function restart() {
  result.value = null;
}

function pickPreset(value: number) {
  amount.value = value;
}

onMounted(() => {
  mql = window.matchMedia('(max-width: 768px)');
  narrow.value = mql.matches;
  mql.addEventListener('change', syncNarrow);
  void load();
});
onUnmounted(() => mql?.removeEventListener('change', syncNarrow));
</script>

<template>
  <public-shell>
    <div class="test-wrap">
      <n-card class="test-card" :bordered="true">
        <template #header>
          <div class="card-head">
            <div>
              <n-text strong class="card-title">支付测试</n-text>
              <n-text depth="3" class="card-sub">选一个支付方式下单，流程和商户真实接入完全一致</n-text>
            </div>
            <n-space align="center" :size="6" class="head-badge">
              <n-icon :component="ShieldCheckmarkOutline" size="14" />
              <n-text depth="3" class="gp-hint">真实通道</n-text>
            </n-space>
          </div>
        </template>

        <n-skeleton v-if="loading" text :repeat="6" />

        <n-result
          v-else-if="config && !config.enabled"
          status="info"
          title="支付测试未开放"
          :description="config.reason || '站点暂时关闭了支付测试'"
        >
          <template #footer>
            <n-button @click="load">重新检测</n-button>
          </template>
        </n-result>

        <n-result
          v-else-if="config && methods.length === 0"
          status="warning"
          title="暂无可用支付方式"
          :description="config.reason || '还没有配置可用的支付通道'"
        >
          <template #footer>
            <n-button @click="load">重新检测</n-button>
          </template>
        </n-result>

        <div v-else-if="result" class="result">
          <n-alert type="success" :bordered="false" title="测试订单已创建">
            支付完成后订单会自动入账，可在商户后台的订单列表里看到这笔「支付测试」。
          </n-alert>

          <div class="result-body">
            <div v-if="isQrcode" class="qr-box">
              <n-qr-code :value="payInfoText" :size="qrSize" />
              <n-text depth="3" class="gp-hint">用手机扫码支付</n-text>
            </div>
            <n-space vertical :size="10" class="result-meta">
              <n-descriptions :column="metaColumns" label-placement="top" size="small" :bordered="false">
                <n-descriptions-item label="订单号">
                  <n-space align="center" :size="6">
                    <n-text code>{{ result.trade_no }}</n-text>
                    <n-button text type="primary" size="tiny" @click="copyTradeNo">复制</n-button>
                  </n-space>
                </n-descriptions-item>
                <n-descriptions-item label="支付方式">
                  <n-space align="center" :size="6">
                    <payment-brand-icon :type="result.payment_type" :size="16" />
                    <n-text>{{ paymentTypeText(result.payment_type) }}</n-text>
                  </n-space>
                </n-descriptions-item>
                <n-descriptions-item label="金额">
                  <n-text strong>￥{{ centsToYuan(result.amount).toFixed(2) }}</n-text>
                </n-descriptions-item>
              </n-descriptions>

              <n-space :size="10" class="result-actions" :wrap="true">
                <n-button
                  v-if="isJump || isQrcode"
                  type="primary"
                  tag="a"
                  :href="payInfoText"
                  target="_blank"
                  rel="noopener"
                >
                  <template #icon>
                    <n-icon :component="ArrowForwardOutline" />
                  </template>
                  {{ isJump ? '前往支付' : '打开支付链接' }}
                </n-button>
                <n-button tag="a" :href="result.cashier_url" target="_blank" rel="noopener">打开收银台</n-button>
                <n-button quaternary @click="restart">
                  <template #icon>
                    <n-icon :component="RefreshOutline" />
                  </template>
                  再测一次
                </n-button>
              </n-space>
              <n-text v-if="!isQrcode && !isJump" depth="3" class="gp-hint" style="word-break: break-all">
                {{ payInfoText }}
              </n-text>
            </n-space>
          </div>
        </div>

        <div v-else class="form">
          <n-form label-placement="top" @submit.prevent="submit">
            <n-form-item label="支付方式">
              <n-grid cols="1 s:2 m:3" responsive="screen" :x-gap="10" :y-gap="10" style="width: 100%">
                <n-gi v-for="method in methods" :key="method.code">
                  <button
                    type="button"
                    class="method"
                    :class="{ 'method-active': paymentType === method.code }"
                    @click="paymentType = method.code"
                  >
                    <payment-brand-icon :type="method.code" :size="22" />
                    <span>{{ method.name }}</span>
                  </button>
                </n-gi>
              </n-grid>
            </n-form-item>

            <n-form-item :label="`金额（￥${minYuan} - ￥${maxYuan}）`">
              <n-space vertical :size="8" style="width: 100%">
                <n-input-number
                  v-model:value="amount"
                  :min="minYuan"
                  :max="maxYuan"
                  :step="0.01"
                  :precision="2"
                  size="large"
                  style="width: 100%"
                >
                  <template #prefix>￥</template>
                </n-input-number>
                <n-space :size="8" :wrap="true">
                  <n-button v-for="value in presets" :key="value" size="small" @click="pickPreset(value)">
                    ￥{{ value.toFixed(2) }}
                  </n-button>
                </n-space>
              </n-space>
            </n-form-item>

            <n-button type="primary" size="large" block :loading="submitting" @click="submit">
              发起测试支付
            </n-button>
          </n-form>

          <n-text depth="3" class="form-note">
            测试订单归属演示商户<template v-if="config?.merchant">「{{ config.merchant }}」</template>，每个 IP 每小时最多
            {{ config?.hourly_limit ?? 10 }} 笔；未支付的订单会在有效期内自动关闭。
          </n-text>
        </div>
      </n-card>
    </div>
  </public-shell>
</template>

<style scoped>
.test-wrap {
  padding: 40px 0 56px;
  display: flex;
  justify-content: center;
}
.test-card {
  width: 100%;
  max-width: 720px;
}
.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.card-title {
  display: block;
  font-size: 17px;
}
.card-sub {
  display: block;
  margin-top: 2px;
  font-size: 12.5px;
}
.head-badge {
  flex: none;
}
.method {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--gp-radius);
  border: 1px solid var(--gp-line);
  background: var(--gp-surface);
  color: var(--gp-text-2);
  font-size: 13.5px;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s, color 0.15s;
  text-align: left;
}
.method:hover {
  border-color: var(--gp-accent);
}
.method-active {
  border-color: var(--gp-accent);
  background: var(--gp-accent-soft);
  color: var(--gp-text-1);
  font-weight: 600;
}
.form-note {
  display: block;
  margin-top: 14px;
  font-size: 12px;
  line-height: 1.7;
}
.result-body {
  margin-top: 16px;
  display: flex;
  gap: 20px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.qr-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--gp-line);
  border-radius: var(--gp-radius-lg);
  background: var(--gp-surface);
}
.result-meta {
  flex: 1;
  min-width: 240px;
}
@media (max-width: 768px) {
  .test-wrap {
    padding: 20px 0 40px;
  }
  .head-badge {
    display: none;
  }
  .result-body {
    flex-direction: column;
    align-items: stretch;
  }
  .qr-box {
    align-self: center;
  }
}
</style>