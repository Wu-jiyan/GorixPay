<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { NButton, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  CashOutline,
  CheckmarkCircleOutline,
  NotificationsOutline,
  PeopleOutline,
  ReceiptOutline,
  RefreshOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { AdminDashboard, DailyStat } from '@/api';
import { errorMessage } from '@/api/client';
import { formatMoney } from '@/utils/format';

const router = useRouter();
const message = useMessage();

const dashboard = ref<AdminDashboard | null>(null);
const dailyStats = ref<DailyStat[]>([]);
const outboxStats = ref<Record<string, number>>({});
const loading = ref(false);

const outboxLabels: Record<string, string> = {
  pending: '待发送',
  processing: '处理中',
  completed: '已完成',
  dead: '死信',
  total: '总计',
};

const outboxCards = computed(() =>
  Object.entries(outboxStats.value).map(([key, value]) => ({
    key,
    label: outboxLabels[key] ?? key,
    value: typeof value === 'number' ? value : Number(value) || 0,
  })),
);

const totalPaid = computed(() =>
  dailyStats.value.reduce((sum, item) => sum + (item.paid_amount || 0), 0),
);
const totalPaidCount = computed(() =>
  dailyStats.value.reduce((sum, item) => sum + (item.paid_count || 0), 0),
);

const dailyColumns: DataTableColumns<DailyStat> = [
  { title: '日期', key: 'date', width: 140 },
  {
    title: '订单数',
    key: 'order_count',
    align: 'right',
    width: 110,
    render: (row) => String(row.order_count ?? 0),
  },
  {
    title: '支付笔数',
    key: 'paid_count',
    align: 'right',
    width: 110,
    render: (row) => String(row.paid_count ?? 0),
  },
  {
    title: '支付金额',
    key: 'paid_amount',
    align: 'right',
    render: (row) =>
      h(NText, { class: 'amount' }, () => formatMoney(row.paid_amount || 0)),
  },
  {
    title: '状态',
    key: 'state',
    width: 110,
    render: (row) =>
      (row.paid_count || 0) > 0
        ? h(NTag, { type: 'success', size: 'small', bordered: false }, () => '有成交')
        : h(NTag, { size: 'small', bordered: false }, () => '无成交'),
  },
];

const quickLinks = [
  { label: '订单管理', path: '/admin/orders' },
  { label: '支付渠道', path: '/admin/channels' },
  { label: '公告管理', path: '/admin/announcements' },
  { label: '商户管理', path: '/admin/merchants' },
  { label: '资金账本', path: '/admin/ledger' },
  { label: '退款审核', path: '/admin/refunds' },
];

async function load() {
  loading.value = true;
  try {
    const [summary, stats, outbox] = await Promise.all([
      adminApi.dashboard(),
      adminApi.dailyStats(14),
      adminApi.outboxStats(),
    ]);
    dashboard.value = summary;
    dailyStats.value = stats;
    outboxStats.value = outbox;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <admin-page title="数据概览" subtitle="平台经营数据、支付走势与通知队列状态">
    <template #actions>
      <n-button secondary :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新数据
      </n-button>
    </template>

    <n-spin :show="loading">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-card size="small">
            <n-statistic label="商户总数" :value="dashboard?.merchant_count ?? 0">
              <template #prefix>
                <n-icon :component="PeopleOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" style="font-size: 12px">全平台已入驻商户</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small">
            <n-statistic label="订单总数" :value="dashboard?.order_count ?? 0">
              <template #prefix>
                <n-icon :component="ReceiptOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" style="font-size: 12px">含全部状态</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small">
            <n-statistic label="支付订单数" :value="dashboard?.paid_order_count ?? 0">
              <template #prefix>
                <n-icon :component="CheckmarkCircleOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" style="font-size: 12px">已入账订单</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small">
            <n-statistic label="支付金额" :value="formatMoney(dashboard?.paid_amount ?? 0)">
              <template #prefix>
                <n-icon :component="CashOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" style="font-size: 12px">累计支付金额</n-text>
          </n-card>
        </n-gi>
      </n-grid>

      <n-card size="small" style="margin-top: 16px">
        <template #header>近 14 天支付走势</template>
        <template #header-extra>
          <n-space :size="16" :wrap="true">
            <n-text depth="3">合计支付 {{ totalPaidCount }} 笔</n-text>
            <n-text depth="3">合计 {{ formatMoney(totalPaid) }}</n-text>
          </n-space>
        </template>
        <n-data-table
          :columns="dailyColumns"
          :data="dailyStats"
          :loading="loading"
          :bordered="false"
          :single-line="false"
          size="small"
          :max-height="320"
          :pagination="false"
          :scroll-x="860"
          :row-key="(row: DailyStat) => row.date"
        />
        <n-empty v-if="!dailyStats.length && !loading" description="还没有订单数据" style="padding: 24px 0" />
      </n-card>

      <n-card size="small" style="margin-top: 16px">
        <template #header>
          <n-space align="center" :size="8">
            <n-icon :component="NotificationsOutline" />
            <span>通知队列</span>
          </n-space>
        </template>
        <template #header-extra>
          <n-button text type="primary" @click="router.push('/admin/outbox')">查看详情</n-button>
        </template>
        <n-grid cols="2 s:3 m:5" responsive="screen" :x-gap="16" :y-gap="12">
          <n-gi v-for="card in outboxCards" :key="card.key">
            <n-statistic :label="card.label" :value="card.value" />
          </n-gi>
        </n-grid>
        <n-text v-if="!outboxCards.length" depth="3">队列为空</n-text>
      </n-card>

      <n-card size="small" style="margin-top: 16px">
        <template #header>快捷入口</template>
        <n-space :wrap="true">
          <n-button v-for="link in quickLinks" :key="link.path" secondary @click="router.push(link.path)">
            {{ link.label }}
          </n-button>
        </n-space>
      </n-card>
    </n-spin>
  </admin-page>
</template>
