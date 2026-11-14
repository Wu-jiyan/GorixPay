<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useMessage, type SelectOption } from 'naive-ui';
import {
  merchantPayCodeApi,
  type PayCodeResult,
  type PublicPayType,
  type TestOrderResult,
} from '@/api/merchantPayCode';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { copyText, formatDateTime, yuanToCents } from '@/utils/format';

const message = useMessage();

const activeTab = ref<'code' | 'test'>('code');

const FALLBACK_TYPES: PublicPayType[] = [
  { code: 'alipay', name: '支付宝' },
  { code: 'wxpay', name: '微信支付' },
  { code: 'qqpay', name: 'QQ 钱包' },
  { code: 'bank', name: '网银' },
];

const payTypeLoading = ref(true);
const payTypes = ref<PublicPayType[]>([]);

const payTypeOptions = computed<SelectOption[]>(() =>
  payTypes.value.map((item) => ({ label: item.name || item.code, value: item.code })),
);

// 收款码
const codeForm = reactive({ payment_type: 'alipay', amountYuan: '' });
const codeLoading = ref(false);
const codeResult = ref<PayCodeResult | null>(null);

// 测试支付
const testForm = reactive({ payment_type: 'alipay', amountYuan: '0.01' });
const testLoading = ref(false);
const testResult = ref<TestOrderResult | null>(null);

async function loadPayTypes() {
  payTypeLoading.value = true;
  try {
    const items = await merchantPayCodeApi.payTypes();
    payTypes.value = items.length > 0 ? items : FALLBACK_TYPES;
  } catch (err) {
    payTypes.value = FALLBACK_TYPES;
    message.error(errorMessage(err));
  } finally {
    payTypeLoading.value = false;
  }
}

async function generateCode() {
  if (codeLoading.value) return;
  if (!codeForm.payment_type) {
    message.error('请选择支付方式');
    return;
  }
  let amountCents: number | undefined;
  if (codeForm.amountYuan.trim()) {
    const cents = yuanToCents(codeForm.amountYuan);
    if (!Number.isFinite(cents) || cents <= 0) {
      message.error('请输入有效的收款金额');
      return;
    }
    amountCents = cents;
  }
  codeLoading.value = true;
  try {
    codeResult.value = await merchantPayCodeApi.payCode({
      payment_type: codeForm.payment_type,
      amount_cents: amountCents,
    });
    message.success('收款码已生成');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    codeLoading.value = false;
  }
}

async function createTestOrder() {
  if (testLoading.value) return;
  if (!testForm.payment_type) {
    message.error('请选择支付方式');
    return;
  }
  const cents = yuanToCents(testForm.amountYuan);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的测试金额');
    return;
  }
  if (cents > 100) {
    message.warning('测试支付金额上限为 1.00 元');
    return;
  }
  testLoading.value = true;
  try {
    testResult.value = await merchantPayCodeApi.testOrder({
      payment_type: testForm.payment_type,
      amount_cents: cents,
    });
    message.success('测试订单已创建，请通过收银台完成支付');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    testLoading.value = false;
  }
}

async function handleCopy(text: string, label = '链接') {
  if (!text) return;
  const ok = await copyText(text);
  if (ok) message.success(`${label}已复制`);
  else message.error('复制失败，请手动选择文本复制');
}

function payInfoText(result: TestOrderResult | null): string {
  if (!result) return '';
  const info = result.pay_info;
  if (typeof info === 'string') return info;
  return JSON.stringify(info, null, 2);
}

onMounted(() => {
  void loadPayTypes();
});
</script>

<template>
  <div class="page">
    <PageHeader title="收款码" subtitle="生成聚合收款码，或用小额订单自测支付链路">
      <template #actions>
        <n-button size="small" :loading="payTypeLoading" @click="loadPayTypes">刷新支付方式</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="main-card">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <n-tab-pane name="code" tab="收款码">
          <div class="form-grid">
            <div class="field">
              <label class="field-label">支付方式</label>
              <n-select v-model:value="codeForm.payment_type" :options="payTypeOptions" :loading="payTypeLoading" />
            </div>
            <div class="field">
              <label class="field-label">金额（元，选填）</label>
              <n-input v-model:value="codeForm.amountYuan" placeholder="留空生成不定额收款码">
                <template #prefix>¥</template>
              </n-input>
            </div>
            <div class="field field-submit">
              <n-button type="primary" :loading="codeLoading" @click="generateCode">生成收款码</n-button>
            </div>
          </div>
          <p class="tip">填写金额会生成固定金额的收款码；留空时由付款方输入金额。</p>

          <div v-if="codeResult" class="result-box">
            <div class="code-image">
              <span v-if="codeResult.code_svg" class="code-svg" v-html="codeResult.code_svg"></span>
              <span v-else class="code-empty">渠道未返回二维码</span>
            </div>
            <div class="result-meta">
              <div class="meta-row">
                <span class="meta-label">收款链接</span>
                <span class="meta-value mono">{{ codeResult.url || '-' }}</span>
              </div>
              <div class="meta-row">
                <span class="meta-label">过期时间</span>
                <span class="meta-value">{{ formatDateTime(codeResult.expires_at) }}</span>
              </div>
              <div class="result-actions">
                <n-button
                  v-if="codeResult.url"
                  type="primary"
                  tag="a"
                  :href="codeResult.url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  打开收款页
                </n-button>
                <n-button secondary :disabled="!codeResult.url" @click="handleCopy(codeResult.url, '收款链接')">
                  复制链接
                </n-button>
              </div>
            </div>
          </div>
        </n-tab-pane>

        <n-tab-pane name="test" tab="测试支付">
          <div class="form-grid">
            <div class="field">
              <label class="field-label">支付方式</label>
              <n-select v-model:value="testForm.payment_type" :options="payTypeOptions" :loading="payTypeLoading" />
            </div>
            <div class="field">
              <label class="field-label">金额（元，上限 1.00）</label>
              <n-input v-model:value="testForm.amountYuan" placeholder="例如 0.01">
                <template #prefix>¥</template>
              </n-input>
            </div>
            <div class="field field-submit">
              <n-button type="primary" :loading="testLoading" @click="createTestOrder">发起测试支付</n-button>
            </div>
          </div>
          <p class="tip">测试订单会走当前启用的渠道，金额上限 1.00 元；创建后请通过收银台完成支付。</p>

          <div v-if="testResult" class="result-box">
            <div class="result-meta result-meta-wide">
              <div class="meta-row">
                <span class="meta-label">订单号</span>
                <span class="meta-value mono">{{ testResult.trade_no }}</span>
              </div>
              <div class="meta-row">
                <span class="meta-label">支付类型</span>
                <span class="meta-value">{{ testResult.pay_type || '-' }}</span>
              </div>
              <div class="meta-row">
                <span class="meta-label">收银台</span>
                <span class="meta-value mono">{{ testResult.cashier_url || '-' }}</span>
              </div>
              <div class="meta-row" v-if="payInfoText(testResult)">
                <span class="meta-label">pay_info</span>
                <span class="meta-value mono pre-wrap">{{ payInfoText(testResult) }}</span>
              </div>
              <div class="result-actions">
                <n-button
                  v-if="testResult.cashier_url"
                  type="primary"
                  tag="a"
                  :href="testResult.cashier_url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  打开收银台
                </n-button>
                <n-button
                  secondary
                  :disabled="!testResult.cashier_url"
                  @click="handleCopy(testResult.cashier_url, '收银台链接')"
                >
                  复制链接
                </n-button>
                <n-button text :disabled="!testResult.cashier_url" @click="handleCopy(payInfoText(testResult), 'pay_info')">
                  复制 pay_info
                </n-button>
              </div>
            </div>
          </div>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </div>
</template>

<style scoped>
.main-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: 16px;
  align-items: end;
}
.field {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.field-label {
  font-size: 12.5px;
  color: #64748b;
}
.field-submit .n-button {
  min-width: 132px;
}
.tip {
  margin: 12px 0 0;
  font-size: 12px;
  color: #94a3b8;
  line-height: 1.7;
}
.result-box {
  margin-top: 18px;
  padding-top: 18px;
  border-top: 1px dashed #e2e8f0;
  display: flex;
  gap: 24px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.code-image {
  width: 200px;
  height: 200px;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  background: #fff;
  display: grid;
  place-items: center;
  padding: 10px;
  flex: none;
}
.code-svg {
  display: block;
  width: 100%;
  height: 100%;
}
.code-svg :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
}
.code-empty {
  font-size: 12.5px;
  color: #94a3b8;
}
.result-meta {
  flex: 1;
  min-width: 280px;
}
.result-meta-wide {
  width: 100%;
}
.meta-row {
  display: flex;
  gap: 12px;
  padding: 6px 0;
  font-size: 13px;
  border-bottom: 1px dashed #f1f5f9;
}
.meta-label {
  color: #94a3b8;
  flex: none;
  width: 76px;
}
.meta-value {
  color: #334155;
  word-break: break-all;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}
.pre-wrap {
  white-space: pre-wrap;
}
.result-actions {
  margin-top: 14px;
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
}
@media (max-width: 900px) {
  .form-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .field-submit .n-button {
    width: 100%;
  }
}
</style>
