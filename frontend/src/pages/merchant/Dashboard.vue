<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { Announcement, MerchantDashboard, Order } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatCard from '@/components/StatCard.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime, formatMoney } from '@/utils/format';
import { useRouter } from 'vue-router';

const router = useRouter();
const message = useMessage();

const statsLoading = ref(true);
const ordersLoading = ref(true);
const announcementsLoading = ref(false);
const stats = ref<MerchantDashboard | null>(null);
const orders = ref<Order[]>([]);
const announcements = ref<Announcement[]>([]);

const columns: DataTableColumns<Order> = [
  {
    title: '订单号',
    key: 'trade_no',
    width: 190,
    render: (row) => h('span', { class: 'mono-cell' }, row.trade_no),
  },
  { title: '商品名称', key: 'name', ellipsis: { tooltip: true } },
  {
    title: '金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => h(StatusTag, { status: row.status }),
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

const announcementList = computed(() => announcements.value.slice(0, 5));
const rowKey = (row: Order) => row.id;

async function loadStats() {
  statsLoading.value = true;
  try {
    stats.value = await merchantApi.dashboard();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    statsLoading.value = false;
  }
}

async function loadOrders() {
  ordersLoading.value = true;
  try {
    const res = await merchantApi.orders({ page: 1, page_size: 5 });
    orders.value = res.items;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    ordersLoading.value = false;
  }
}

async function loadAnnouncements() {
  announcementsLoading.value = true;
  try {
    announcements.value = await merchantApi.announcements();
  } catch {
    // 公告接口不可用时静默降级，不影响概览页
    announcements.value = [];
  } finally {
    announcementsLoading.value = false;
  }
}

onMounted(() => {
  void loadStats();
  void loadOrders();
  void loadAnnouncements();
});
</script>

<template>
  <div class="page">
    <PageHeader title="商户概览" subtitle="今日经营数据、最近订单与平台公告">
      <template #actions>
        <n-button size="small" @click="loadStats">刷新数据</n-button>
        <n-button size="small" type="primary" @click="router.push('/merchant/orders')">全部订单</n-button>
      </template>
    </PageHeader>

    <n-spin :show="statsLoading">
      <div class="stat-grid">
        <StatCard label="今日订单" :value="stats?.today_order_count ?? 0" hint="今日创建的支付订单数" />
        <StatCard
          label="今日收款"
          :value="formatMoney(stats?.today_paid_amount)"
          hint="今日完成支付的订单金额"
          accent="#059669"
        />
        <StatCard
          label="累计收款"
          :value="formatMoney(stats?.total_paid_amount)"
          hint="历史成功支付合计"
          accent="#2563eb"
        />
        <StatCard
          label="可用余额"
          :value="formatMoney(stats?.available_balance)"
          :hint="`冻结 ${formatMoney(stats?.frozen_balance)}`"
          accent="#7c3aed"
        />
      </div>
    </n-spin>

    <div class="pending-row">
      <n-card :bordered="false" class="pending-card">
        <div class="pending-grid">
          <div class="pending-item">
            <span class="pending-label">冻结金额</span>
            <span class="pending-value">{{ formatMoney(stats?.frozen_balance) }}</span>
          </div>
          <div class="pending-item">
            <span class="pending-label">待审核退款</span>
            <span class="pending-value">{{ stats?.pending_refunds ?? 0 }} 笔</span>
          </div>
          <div class="pending-item">
            <span class="pending-label">待审核结算</span>
            <span class="pending-value">{{ stats?.pending_settlements ?? 0 }} 笔</span>
          </div>
        </div>
      </n-card>
    </div>

    <div class="main-grid">
      <n-card :bordered="false" title="最近订单" class="panel-card">
        <template #header-extra>
          <n-button text type="primary" size="small" @click="router.push('/merchant/orders')">查看全部</n-button>
        </template>
        <n-data-table
          :columns="columns"
          :data="orders"
          :loading="ordersLoading"
          :bordered="false"
          size="small"
          :row-key="rowKey"
          :pagination="false"
        >
          <template #empty>
            <n-empty description="还没有订单，去创建一笔试试" size="small" />
          </template>
        </n-data-table>
      </n-card>

      <n-card :bordered="false" title="平台公告" class="panel-card">
        <template #header-extra>
          <n-tag v-if="announcements.length" size="small" :bordered="false" type="info">
            {{ announcements.length }} 条
          </n-tag>
        </template>
        <n-spin :show="announcementsLoading">
          <div v-if="announcementList.length" class="announcement-list">
            <div v-for="item in announcementList" :key="item.id" class="announcement-item">
              <div class="announcement-title">{{ item.title }}</div>
              <div class="announcement-content">{{ item.content }}</div>
              <div class="announcement-time">{{ formatDateTime(item.published_at ?? item.created_at) }}</div>
            </div>
          </div>
          <n-empty v-else description="暂无公告" size="small" />
        </n-spin>
      </n-card>
    </div>
  </div>
</template>

<style scoped>
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}
.pending-row {
  margin-top: 16px;
}
.pending-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.pending-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
}
.pending-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.pending-label {
  font-size: 12px;
  color: #94a3b8;
}
.pending-value {
  font-size: 16px;
  font-weight: 600;
  color: #1e293b;
  font-variant-numeric: tabular-nums;
}
.main-grid {
  margin-top: 16px;
  display: grid;
  grid-template-columns: minmax(0, 1.7fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.panel-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.announcement-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.announcement-item {
  padding-bottom: 12px;
  border-bottom: 1px dashed #eef2f7;
}
.announcement-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}
.announcement-title {
  font-weight: 600;
  font-size: 13.5px;
  color: #1e293b;
}
.announcement-content {
  margin-top: 5px;
  font-size: 12.5px;
  color: #64748b;
  line-height: 1.7;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.announcement-time {
  margin-top: 6px;
  font-size: 11.5px;
  color: #94a3b8;
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
@media (max-width: 1100px) {
  .main-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .pending-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 14px;
  }
}
</style>
