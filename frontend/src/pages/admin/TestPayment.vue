<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { SelectOption } from 'naive-ui';
import {
  CardOutline,
  CopyOutline,
  OpenOutline,
  RefreshOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { Channel } from '@/api';
import { testPaymentApi } from '@/api/testPayment';
import type { TestOrderResult } from '@/api/testPayment';
import { errorMessage } from '@/api/client';
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

const tradeNo = computed(() => {
  const value = asRow(result.value).trade_no;
  return typeof value === 'string' ? value : '';
});

const payType = computed(() => {
  const value = asRow(result.value).pay_type;
  return typeof value === 'string' ? value : '';
});

const cashierUrl = computed(() => {
  const value = asRow(result.value).cashier_url;
  return typeof value === 'string' ? value : '';
});

const payInfoText = computed(() => {
  const info = asRow(result.value).pay_info;
  if (info === undefined || info === null || info === '') return '';
  return prettyJson(info);
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

async function copy(text: string) {
  if (!text) {
    message.warning('没有可复制的内容');
    return;
  }
  const ok = await copyText(text);
  if (ok) message.success('已复制');
  else message.warning('复制失败，请手动选择');
}

function openCashier() {
  if (cashierUrl.value) window.open(cashierUrl.value, '_blank');
}

onMounted(loadChannels);
</script>

<template>
  <admin-page title="测试支付" subtitle="用指定渠道创建一笔测试订单，验证上游参数是否配通">
    <template #actions>
      <n-button :loading="loading" @click="loadChannels">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        刷新渠道
      </n-button>
    </template>

    <n-alert type="info" :bordered="false" :show-icon="true" style="margin-bottom: 16px">
      测试订单只用于验证渠道配置，建议金额 0.01 元；是否真实下单由渠道插件决定，可能是 mock
      上游。测试单不计入报表与对账。
    </n-alert>

    <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16">
      <n-gi>
        <n-card size="small" :bordered="false" title="下单参数">
          <n-form label-placement="left" label-width="180">
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
              <n-select v-model:value="paymentType" :options="paymentTypeOptions" style="max-width: 260px" />
            </n-form-item>
            <n-form-item label="金额（元）">
              <n-input-number
                v-model:value="amountYuan"
                :min="0.01"
                :step="0.01"
                :precision="2"
                style="width: 200px"
              >
                <template #suffix>元</template>
              </n-input-number>
            </n-form-item>
            <n-form-item label="说明">
              <n-text depth="3">
                按上面的渠道与金额创建一笔测试单，提交后返回订单号与收银台链接；不写入商户业务报表。
              </n-text>
            </n-form-item>
          </n-form>

          <n-alert v-if="selectedChannel" type="default" :bordered="false">
            当前渠道：{{ selectedChannel.name }} · 插件 {{ selectedChannel.plugin_code }} · 优先级
            {{ selectedChannel.priority ?? '-' }} · 状态 {{ selectedChannel.status ?? '-' }}
          </n-alert>

          <template #footer>
            <n-space justify="space-between" align="center">
              <n-text depth="3">
                {{ channels.length > 0 ? `可选渠道 ${channels.length} 个` : '暂无可用渠道' }}
              </n-text>
              <n-button
                type="primary"
                :loading="submitting"
                :disabled="channels.length === 0"
                @click="submit"
              >
                <template #icon><n-icon><CardOutline /></n-icon></template>
                创建测试订单
              </n-button>
            </n-space>
          </template>
        </n-card>

        <n-empty
          v-if="!loading && channels.length === 0"
          description="还没有可用渠道，先去支付渠道页创建"
          size="small"
          style="margin-top: 12px"
        />
      </n-gi>

      <n-gi>
        <n-card size="small" :bordered="false" title="订单结果">
          <n-empty v-if="!result" description="还没有创建测试订单" size="small" />
          <n-form v-else label-placement="left" label-width="180">
            <n-form-item label="订单号">
              <n-input :value="tradeNo" readonly placeholder="-">
                <template #suffix>
                  <n-button text type="primary" @click="copy(tradeNo)">
                    <template #icon><n-icon><CopyOutline /></n-icon></template>
                  </n-button>
                </template>
              </n-input>
            </n-form-item>
            <n-form-item label="支付方式">
              <n-space align="center" :size="8">
                <n-tag size="small" round :bordered="false" type="info">
                  {{ paymentTypeText(payType) }}
                </n-tag>
                <n-text v-if="payType" depth="3">{{ payType }}</n-text>
              </n-space>
            </n-form-item>
            <n-form-item label="收银台链接">
              <n-space align="center" :size="8" style="width: 100%">
                <n-input :value="cashierUrl" readonly placeholder="-">
                  <template #suffix>
                    <n-button text type="primary" @click="copy(cashierUrl)">
                      <template #icon><n-icon><CopyOutline /></n-icon></template>
                    </n-button>
                  </template>
                </n-input>
                <n-button :disabled="!cashierUrl" @click="openCashier">
                  <template #icon><n-icon><OpenOutline /></n-icon></template>
                  新窗口打开
                </n-button>
              </n-space>
            </n-form-item>
            <n-form-item v-if="payInfoText" label="pay_info">
              <n-code :code="payInfoText" language="json" word-wrap />
            </n-form-item>
          </n-form>
        </n-card>
      </n-gi>
    </n-grid>
  </admin-page>
</template>
