<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { Channel } from '@/api';
import { testPaymentApi } from '@/api/testPayment';
import type { TestOrderResult } from '@/api/testPayment';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { copyText, paymentTypeText } from '@/utils/format';
import { asRow, itemsOf, prettyJson } from './shared';

const message = useMessage();

const channels = ref<Channel[]>([]);
const loading = ref(false);
const submitting = ref(false);

const channelId = ref<string | null>(null);
const paymentType = ref<string>('alipay');
const amountYuan = ref<number | null>(0.01);

const result = ref<TestOrderResult | null>(null);

const paymentTypeOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信支付', value: 'wxpay' },
  { label: 'QQ 钱包', value: 'qqpay' },
  { label: '银联', value: 'unionpay' },
  { label: '网银', value: 'bank' },
  { label: 'USDT', value: 'usdt' },
  { label: '收银台', value: 'cashier' },
];

const channelOptions = computed<SelectOption[]>(() =>
  channels.value.map((item) => ({
    label: `${item.name || item.id}（${item.plugin_code || '-'} / ${paymentTypeText(item.payment_type)}）`,
    value: item.id,
  })),
);

const selectedChannel = computed<Channel | null>(
  () => channels.value.find((item) => item.id === channelId.value) ?? null,
);

const resultRows = computed(() => {
  const data = asRow(result.value);
  const rows: { label: string; value: string; mono?: boolean }[] = [];
  if (data.trade_no) rows.push({ label: '订单号', value: String(data.trade_no), mono: true });
  if (data.pay_type) rows.push({ label: 'pay_type', value: String(data.pay_type), mono: true });
  if (data.cashier_url) rows.push({ label: '收银台', value: String(data.cashier_url), mono: true });
  return rows;
});

const payInfoText = computed(() => {
  const info = asRow(result.value).pay_info;
  if (info === undefined || info === null || info === '') return '';
  return prettyJson(info);
});

const cashierUrl = computed(() => {
  const url = asRow(result.value).cashier_url;
  return typeof url === 'string' ? url : '';
});

async function loadChannels() {
  loading.value = true;
  try {
    channels.value = itemsOf<Channel>(await adminApi.channels());
    if (!channelId.value && channels.value.length > 0) {
      const first = channels.value[0];
      channelId.value = first.id;
      paymentType.value = first.payment_type || 'alipay';
    }
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function onChannelChange(id: string) {
  channelId.value = id;
  const channel = channels.value.find((item) => item.id === id);
  if (channel?.payment_type) paymentType.value = channel.payment_type;
}

async function submit() {
  if (!channelId.value) {
    message.warning('请先选择支付渠道');
    return;
  }
  const amount = amountYuan.value ?? 0;
  const cents = Math.round(amount * 100);
  if (cents <= 0) {
    message.warning('金额需要大于 0');
    return;
  }
  submitting.value = true;
  try {
    result.value = await testPaymentApi.createTestOrder(channelId.value, {
      payment_type: paymentType.value,
      amount_cents: cents,
    });
    message.success('测试订单已创建');
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function copy(url: string) {
  const ok = await copyText(url);
  if (ok) message.success('已复制');
  else message.warning('复制失败，请手动选择');
}

function openCashier() {
  if (cashierUrl.value) window.open(cashierUrl.value, '_blank');
}

onMounted(loadChannels);
</script>

<template>
  <div class="page">
    <PageHeader title="测试支付" subtitle="用指定渠道创建一笔测试订单，验证上游参数是否配通">
      <template #actions>
        <n-button secondary :loading="loading" @click="loadChannels">刷新渠道</n-button>
      </template>
    </PageHeader>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      测试订单只用于验证渠道配置，建议金额 0.01 元；是否真实下单由渠道插件决定，可能是 mock 上游。
    </n-alert>

    <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16">
      <n-gi>
        <n-card :bordered="false" title="下单参数">
          <n-form label-placement="left" label-width="100">
            <n-form-item label="支付渠道">
              <n-select
                :value="channelId"
                :options="channelOptions"
                :loading="loading"
                filterable
                placeholder="选择支付渠道"
                @update:value="onChannelChange"
              />
            </n-form-item>
            <n-form-item label="支付方式">
              <n-select v-model:value="paymentType" :options="paymentTypeOptions" />
            </n-form-item>
            <n-form-item label="金额（元）">
              <n-input-number
                v-model:value="amountYuan"
                :min="0.01"
                :step="0.01"
                :precision="2"
                style="width: 180px"
              />
            </n-form-item>
          </n-form>
          <p v-if="selectedChannel" class="muted small">
            当前渠道：{{ selectedChannel.name }} · 插件 {{ selectedChannel.plugin_code }} ·
            优先级 {{ selectedChannel.priority ?? '-' }} · 状态 {{ selectedChannel.status ?? '-' }}
          </p>
          <n-space justify="end" style="margin-top: 12px">
            <n-button type="primary" :loading="submitting" :disabled="channels.length === 0" @click="submit">
              创建测试订单
            </n-button>
          </n-space>
          <n-empty v-if="!loading && channels.length === 0" description="还没有可用渠道，先去支付渠道页创建" size="small" />
        </n-card>
      </n-gi>

      <n-gi>
        <n-card :bordered="false" title="返回结果">
          <n-empty v-if="!result" description="还没有创建测试订单" />
          <template v-else>
            <n-descriptions :column="1" label-placement="left" bordered size="small">
              <n-descriptions-item v-for="row in resultRows" :key="row.label" :label="row.label">
                <span :class="{ mono: row.mono }">{{ row.value }}</span>
              </n-descriptions-item>
            </n-descriptions>
            <div v-if="payInfoText" class="payinfo">
              <div class="payinfo-label">pay_info</div>
              <pre class="json">{{ payInfoText }}</pre>
            </div>
            <n-space v-if="cashierUrl" justify="end" style="margin-top: 12px">
              <n-button size="small" @click="copy(cashierUrl)">复制收银台链接</n-button>
              <n-button size="small" type="primary" @click="openCashier">新窗口打开</n-button>
            </n-space>
          </template>
        </n-card>
      </n-gi>
    </n-grid>
  </div>
</template>

<style scoped>
.small {
  font-size: 12px;
}
.payinfo {
  margin-top: 14px;
}
.payinfo-label {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 6px;
}
.json {
  margin: 0;
  max-height: 320px;
  overflow: auto;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 10px;
  padding: 12px 14px;
  font-size: 12px;
  line-height: 1.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
