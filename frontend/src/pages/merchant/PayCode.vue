<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { useMessage, type SelectOption } from 'naive-ui';
import { CopyOutline, OpenOutline, QrCodeOutline, RefreshOutline } from '@vicons/ionicons5';
import {
  merchantPayCodeApi,
  type PayCodeResult,
  type PublicPayType,
  type TestOrderResult,
} from '@/api/merchantPayCode';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import PaymentBrandIcon from '@/components/PaymentBrandIcon.vue';
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

/** 支付方式下拉项：左侧品牌图标，type 直接取支付方式 code（alipay / wxpay / qqpay …） */
function payTypeLabel(option: SelectOption) {
  const value = String(option.value ?? '');
  return h('span', { style: 'display:inline-flex;align-items:center;gap:8px' }, [
    h(PaymentBrandIcon, { type: value, size: 22 }),
    h('span', null, typeof option.label === 'string' ? option.label : value),
  ]);
}

// 收款码
const codeForm = reactive({ payment_type: 'alipay', amountYuan: null as number | null });
const codeLoading = ref(false);
const codeResult = ref<PayCodeResult | null>(null);

// 测试支付
const testForm = reactive({ payment_type: 'alipay', amountYuan: 0.01 as number | null });
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
  if (codeForm.amountYuan !== null && codeForm.amountYuan !== undefined) {
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
  const cents = yuanToCents(testForm.amountYuan ?? 0);
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
  <admin-page title="收款码" subtitle="生成聚合收款码，或用小额订单自测支付链路">
    <template #actions>
      <n-button :loading="payTypeLoading" @click="loadPayTypes">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新支付方式
      </n-button>
    </template>

    <n-card size="small" :bordered="false">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <n-tab-pane name="code" tab="收款码">
          <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16">
            <n-gi>
              <n-form label-placement="left" label-width="160">
                <n-form-item label="支付方式">
                  <n-select
                    v-model:value="codeForm.payment_type"
                    :options="payTypeOptions"
                    :loading="payTypeLoading"
                    :render-label="payTypeLabel"
                  />
                </n-form-item>
                <n-form-item label="金额（元，选填）">
                  <n-input-number
                    v-model:value="codeForm.amountYuan"
                    :precision="2"
                    :min="0.01"
                    placeholder="留空生成不定额收款码"
                    clearable
                    style="width: 100%"
                  >
                    <template #prefix>¥</template>
                  </n-input-number>
                </n-form-item>
                <n-form-item>
                  <n-button type="primary" :loading="codeLoading" @click="generateCode">
                    <template #icon>
                      <n-icon :component="QrCodeOutline" />
                    </template>
                    生成收款码
                  </n-button>
                </n-form-item>
              </n-form>
              <n-alert type="default" :bordered="false">
                填写金额会生成固定金额的收款码；留空时由付款方输入金额。
              </n-alert>
            </n-gi>

            <n-gi>
              <n-card size="small" :bordered="false" title="收款二维码">
                <n-spin :show="codeLoading">
                  <n-space vertical align="center" :size="16">
                    <img
                      v-if="codeResult && codeResult.code_svg"
                      :src="'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(codeResult.code_svg)"
                      alt="收款二维码"
                      style="display: block; width: 220px; height: 220px"
                    />
                    <n-empty v-else description="渠道未返回二维码" size="small" />

                    <n-form v-if="codeResult" label-placement="left" label-width="110" style="width: 100%">
                      <n-form-item label="收银台链接">
                        <n-input :value="codeResult.url || ''" readonly placeholder="生成后展示收款链接">
                          <template #suffix>
                            <n-button
                              text
                              type="primary"
                              :disabled="!codeResult.url"
                              @click="handleCopy(codeResult.url, '收款链接')"
                            >
                              <template #icon>
                                <n-icon :component="CopyOutline" />
                              </template>
                              复制
                            </n-button>
                          </template>
                        </n-input>
                      </n-form-item>
                      <n-form-item label="过期时间">
                        {{ formatDateTime(codeResult.expires_at) }}
                      </n-form-item>
                    </n-form>
                    <n-empty v-else description="还没有生成收款码" size="small" />

                    <n-space v-if="codeResult">
                      <n-button
                        v-if="codeResult.url"
                        type="primary"
                        tag="a"
                        :href="codeResult.url"
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        <template #icon>
                          <n-icon :component="OpenOutline" />
                        </template>
                        打开收款页
                      </n-button>
                      <n-button secondary :disabled="!codeResult.url" @click="handleCopy(codeResult.url, '收款链接')">
                        复制链接
                      </n-button>
                    </n-space>
                  </n-space>
                </n-spin>
              </n-card>
            </n-gi>
          </n-grid>
        </n-tab-pane>

        <n-tab-pane name="test" tab="测试支付">
          <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16">
            <n-gi>
              <n-form label-placement="left" label-width="160">
                <n-form-item label="支付方式">
                  <n-select
                    v-model:value="testForm.payment_type"
                    :options="payTypeOptions"
                    :loading="payTypeLoading"
                    :render-label="payTypeLabel"
                  />
                </n-form-item>
                <n-form-item label="金额（元，上限 1.00）">
                  <n-input-number
                    v-model:value="testForm.amountYuan"
                    :precision="2"
                    :min="0.01"
                    :max="1"
                    placeholder="例如 0.01"
                    style="width: 100%"
                  >
                    <template #prefix>¥</template>
                  </n-input-number>
                </n-form-item>
                <n-form-item>
                  <n-button type="primary" :loading="testLoading" @click="createTestOrder">发起测试支付</n-button>
                </n-form-item>
              </n-form>
              <n-alert type="default" :bordered="false">
                测试订单会走当前启用的渠道，金额上限 1.00 元；创建后请通过收银台完成支付。
              </n-alert>
            </n-gi>

            <n-gi>
              <n-spin :show="testLoading">
                <n-form v-if="testResult" label-placement="left" label-width="110">
                  <n-form-item label="订单号">
                    <n-input :value="testResult.trade_no" readonly class="mono" />
                  </n-form-item>
                  <n-form-item label="支付类型">
                    {{ testResult.pay_type || '-' }}
                  </n-form-item>
                  <n-form-item label="收银台">
                    <n-input :value="testResult.cashier_url || ''" readonly placeholder="暂无收银台链接">
                      <template #suffix>
                        <n-button
                          text
                          type="primary"
                          :disabled="!testResult.cashier_url"
                          @click="handleCopy(testResult.cashier_url, '收银台链接')"
                        >
                          <template #icon>
                            <n-icon :component="CopyOutline" />
                          </template>
                          复制
                        </n-button>
                      </template>
                    </n-input>
                  </n-form-item>
                  <n-form-item v-if="payInfoText(testResult)" label="pay_info">
                    <n-code :code="payInfoText(testResult)" language="json" word-wrap />
                  </n-form-item>
                </n-form>
                <n-empty v-else description="还没有创建测试订单" size="small" />

                <n-space v-if="testResult" style="margin-top: 16px">
                  <n-button
                    v-if="testResult.cashier_url"
                    type="primary"
                    tag="a"
                    :href="testResult.cashier_url"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <template #icon>
                      <n-icon :component="OpenOutline" />
                    </template>
                    打开收银台
                  </n-button>
                  <n-button
                    secondary
                    :disabled="!testResult.cashier_url"
                    @click="handleCopy(testResult.cashier_url, '收银台链接')"
                  >
                    复制链接
                  </n-button>
                  <n-button text :disabled="!payInfoText(testResult)" @click="handleCopy(payInfoText(testResult), 'pay_info')">
                    复制 pay_info
                  </n-button>
                </n-space>
              </n-spin>
            </n-gi>
          </n-grid>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </admin-page>
</template>
