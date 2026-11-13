<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { adminApi } from '@/api';
import type { AdminDashboard, DailyStat } from '@/api';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatCard from '@/components/StatCard.vue';
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

const chart = computed(() => {
  const max = dailyStats.value.reduce((acc, item) => Math.max(acc, item.paid_amount || 0), 0);
  return dailyStats.value.map((item) => {
    const amount = item.paid_amount || 0;
    return {
      date: item.date,
      short: typeof item.date === 'string' ? item.date.slice(5) : '',
      amount,
      orderCount: item.order_count || 0,
      paidCount: item.paid_count || 0,
      height: max > 0 ? Math.max(2, Math.round((amount / max) * 100)) : 2,
    };
  });
});

const hasChart = computed(() => chart.value.length > 0);
const chartIsEmpty = computed(() => chart.value.every((item) => item.amount === 0));

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
  <div class="page">
    <PageHeader title="数据概览" subtitle="平台经营数据、支付走势与通知队列状态">
      <template #actions>
        <n-button type="primary" secondary :loading="loading" @click="load">刷新数据</n-button>
      </template>
    </PageHeader>

    <n-spin :show="loading">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi>
          <StatCard label="商户总数" :value="dashboard?.merchant_count ?? 0" hint="全平台已入驻商户" />
        </n-gi>
        <n-gi>
          <StatCard label="订单总数" :value="dashboard?.order_count ?? 0" hint="含全部状态" />
        </n-gi>
        <n-gi>
          <StatCard
            label="支付订单数"
            :value="dashboard?.paid_order_count ?? 0"
            hint="已入账订单"
            accent="#0f766e"
          />
        </n-gi>
        <n-gi>
          <StatCard
            label="支付金额"
            :value="formatMoney(dashboard?.paid_amount ?? 0)"
            hint="累计支付金额"
            accent="#2563eb"
          />
        </n-gi>
      </n-grid>

      <n-card :bordered="false" class="section" title="近 14 日支付走势">
        <template #header-extra>
          <span class="muted hint">柱高按当日最大金额归一化</span>
        </template>
        <n-empty v-if="!hasChart" description="暂无统计数据" />
        <div v-else class="chart">
          <div v-for="item in chart" :key="item.date" class="chart-col">
            <div class="chart-tip">
              <div class="chart-tip-date">{{ item.date }}</div>
              <div class="chart-tip-amount">{{ formatMoney(item.amount) }}</div>
              <div class="chart-tip-meta">订单 {{ item.orderCount }} · 支付 {{ item.paidCount }}</div>
            </div>
            <div class="chart-track">
              <div class="chart-bar" :style="{ height: `${item.height}%` }"></div>
            </div>
            <div class="chart-axis">{{ item.short }}</div>
          </div>
        </div>
        <p v-if="hasChart && chartIsEmpty" class="muted empty-hint">近 14 日暂无支付金额</p>
      </n-card>

      <n-card :bordered="false" class="section" title="通知队列（Outbox）">
        <template #header-extra>
          <n-button text type="primary" @click="router.push('/admin/outbox')">查看详情</n-button>
        </template>
        <n-empty v-if="outboxCards.length === 0" description="暂无队列数据" />
        <n-grid v-else cols="2 s:3 m:5" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi v-for="card in outboxCards" :key="card.key">
            <div class="mini">
              <div class="mini-label">{{ card.label }}</div>
              <div class="mini-value">{{ card.value }}</div>
            </div>
          </n-gi>
        </n-grid>
      </n-card>

      <n-card :bordered="false" class="section" title="快捷入口">
        <n-space :size="10">
          <n-button secondary type="primary" @click="router.push('/admin/orders')">订单管理</n-button>
          <n-button secondary type="primary" @click="router.push('/admin/channels')">支付渠道</n-button>
          <n-button secondary type="primary" @click="router.push('/admin/announcements')">公告管理</n-button>
          <n-button secondary type="primary" @click="router.push('/admin/merchants')">商户管理</n-button>
          <n-button secondary @click="router.push('/admin/ledger')">资金账本</n-button>
          <n-button secondary @click="router.push('/admin/refunds')">退款审核</n-button>
        </n-space>
      </n-card>
    </n-spin>
  </div>
</template>

<style scoped>
.section {
  margin-top: 16px;
}
.hint {
  font-size: 12px;
}
.chart {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  height: 220px;
  padding: 8px 4px 0;
  overflow: visible;
}
.chart-col {
  position: relative;
  flex: 1 1 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  height: 100%;
  min-width: 0;
}
.chart-track {
  width: 100%;
  flex: 1;
  display: flex;
  align-items: flex-end;
  justify-content: center;
}
.chart-bar {
  width: 70%;
  max-width: 34px;
  border-radius: 6px 6px 2px 2px;
  background: linear-gradient(180deg, #3b82f6 0%, #2563eb 100%);
  transition: height 0.25s ease, background 0.2s ease;
  min-height: 3px;
}
.chart-col:hover .chart-bar {
  background: linear-gradient(180deg, #06b6d4 0%, #2563eb 100%);
}
.chart-axis {
  margin-top: 6px;
  font-size: 11px;
  color: #94a3b8;
  white-space: nowrap;
  transform: scale(0.95);
}
.chart-tip {
  position: absolute;
  bottom: calc(100% - 40px);
  left: 50%;
  transform: translate(-50%, -6px);
  background: rgba(15, 23, 42, 0.92);
  color: #fff;
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  line-height: 1.5;
  white-space: nowrap;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.15s ease;
  z-index: 5;
}
.chart-col:hover .chart-tip {
  opacity: 1;
}
.chart-tip-date {
  color: #cbd5e1;
}
.chart-tip-amount {
  font-weight: 600;
}
.chart-tip-meta {
  color: #94a3b8;
  font-size: 11px;
}
.mini {
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 12px 14px;
  background: #fafbfc;
}
.mini-label {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 4px;
}
.mini-value {
  font-size: 20px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.empty-hint {
  margin: 10px 0 0;
  font-size: 12px;
}
</style>
