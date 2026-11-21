<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import {
  CashOutline,
  LockClosedOutline,
  NotificationsOutline,
  ReceiptOutline,
  RefreshOutline,
  ReturnDownBackOutline,
  TimeOutline,
  TrendingUpOutline,
  WalletOutline,
} from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { Announcement, MerchantDailyStat, MerchantDashboard, Order } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime, formatMoney, orderStatus } from '@/utils/format';
import { useNarrow } from './shared';

const router = useRouter();
const message = useMessage();
const isNarrow = useNarrow();

const statsLoading = ref(true);
const ordersLoading = ref(true);
const announcementsLoading = ref(false);
const dailyLoading = ref(false);
const stats = ref<MerchantDashboard | null>(null);
const orders = ref<Order[]>([]);
const announcements = ref<Announcement[]>([]);
const dailyStats = ref<MerchantDailyStat[]>([]);

/** 近 7 天趋势没有图表库，用宽度百分比画条形，最大一笔撑满整条 */
const dailyMax = computed(() => dailyStats.value.reduce((max, item) => Math.max(max, item.paid_amount || 0), 0));
const dailyTotal = computed(() => dailyStats.value.reduce((sum, item) => sum + (item.paid_amount || 0), 0));

function barWidth(item: MerchantDailyStat): string {
  const max = dailyMax.value;
  if (max <= 0) return '0%';
  const ratio = Math.max(0, Math.min(1, (item.paid_amount || 0) / max));
  // 有成交但金额很小时也留 4% 宽度，否则条子看不见
  return `${item.paid_amount > 0 ? Math.max(4, Math.round(ratio * 100)) : 0}%`;
}

const columns: DataTableColumns<Order> = [
  {
    title: '订单号',
    key: 'trade_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.trade_no || '-'),
  },
  { title: '商品名称', key: 'name', minWidth: 160, ellipsis: { tooltip: true } },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: orderStatus(row.status).type }, () =>
        orderStatus(row.status).text,
      ),
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

/** 近 7 天成交趋势：GET /merchant/stats/daily?days=7 */
async function loadDaily() {
  dailyLoading.value = true;
  try {
    dailyStats.value = await merchantApi.dailyStats(7);
  } catch (err) {
    dailyStats.value = [];
    message.error(errorMessage(err));
  } finally {
    dailyLoading.value = false;
  }
}

function reload() {
  void loadStats();
  void loadOrders();
  void loadAnnouncements();
  void loadDaily();
}

onMounted(reload);
</script>

<template>
  <admin-page title="商户概览" subtitle="今日经营数据、最近订单与平台公告">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="statsLoading" @click="reload">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新数据
        </n-button>
        <n-button type="primary" @click="router.push('/merchant/orders')">全部订单</n-button>
      </n-space>
    </template>

    <n-spin :show="statsLoading">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="可用余额" :value="formatMoney(stats?.available_balance)">
              <template #prefix>
                <n-icon :component="WalletOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">可随时结算、代付的余额</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="冻结金额" :value="formatMoney(stats?.frozen_balance)">
              <template #prefix>
                <n-icon :component="LockClosedOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">结算中或退款冻结的金额</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="今日订单" :value="stats?.today_order_count ?? 0">
              <template #prefix>
                <n-icon :component="ReceiptOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">今日创建的支付订单数</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="今日收款" :value="formatMoney(stats?.today_paid_amount)">
              <template #prefix>
                <n-icon :component="CashOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">今日完成支付的订单金额</n-text>
          </n-card>
        </n-gi>
      </n-grid>
    </n-spin>

    <n-card size="small" :bordered="false" style="margin-top: 16px">
      <n-grid cols="1 s:3" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-statistic label="累计收款" :value="formatMoney(stats?.total_paid_amount)">
            <template #prefix>
              <n-icon :component="TrendingUpOutline" />
            </template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="待审核退款" :value="stats?.pending_refunds ?? 0">
            <template #prefix>
              <n-icon :component="ReturnDownBackOutline" />
            </template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="待审核结算" :value="stats?.pending_settlements ?? 0">
            <template #prefix>
              <n-icon :component="TimeOutline" />
            </template>
          </n-statistic>
        </n-gi>
      </n-grid>
    </n-card>

    <n-card size="small" :bordered="false" style="margin-top: 16px">
      <template #header>近 7 天成交趋势</template>
      <template #header-extra>
        <n-space :size="16" :wrap="true">
          <n-text depth="3">合计 {{ formatMoney(dailyTotal) }}</n-text>
          <n-button text type="primary" :loading="dailyLoading" @click="loadDaily">刷新</n-button>
        </n-space>
      </template>
      <n-spin :show="dailyLoading">
        <div v-if="dailyStats.length" class="trend">
          <div v-for="item in dailyStats" :key="item.date" class="trend-row">
            <span class="trend-date">{{ item.date }}</span>
            <span class="trend-track">
              <span class="trend-bar" :style="{ width: barWidth(item) }" />
            </span>
            <span class="trend-amount amount">{{ formatMoney(item.paid_amount || 0) }}</span>
            <span class="trend-count gp-hint">{{ item.order_count || 0 }} 单</span>
          </div>
        </div>
        <n-empty v-else description="近 7 天还没有成交数据" size="small" />
      </n-spin>
    </n-card>

    <n-grid cols="1 m:3" responsive="screen" item-responsive :x-gap="16" :y-gap="16" style="margin-top: 16px">
      <n-gi span="1 m:2">
        <n-card size="small" :bordered="false" title="最近订单">
          <template #header-extra>
            <n-button text type="primary" @click="router.push('/merchant/orders')">查看全部</n-button>
          </template>
          <n-data-table
            :columns="columns"
            :data="orders"
            :loading="ordersLoading"
            :bordered="false"
            :size="isNarrow ? 'small' : 'medium'"
            :row-key="rowKey"
            :pagination="false"
            :scroll-x="760"
          >
            <template #empty>
              <n-empty description="还没有订单，去创建一笔试试" size="small" />
            </template>
          </n-data-table>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card size="small" :bordered="false" title="平台公告">
          <template #header-extra>
            <n-tag v-if="announcements.length" size="small" :bordered="false" type="info">
              {{ announcements.length }} 条
            </n-tag>
          </template>
          <n-spin :show="announcementsLoading">
            <n-list v-if="announcementList.length" hoverable>
              <n-list-item v-for="item in announcementList" :key="item.id">
                <n-thing :title="item.title">
                  <template #description>
                    <n-space :size="6" align="center">
                      <n-icon :component="NotificationsOutline" />
                      <n-text depth="3" class="gp-hint">
                        {{ formatDateTime(item.published_at ?? item.created_at) }}
                      </n-text>
                    </n-space>
                  </template>
                  {{ item.content }}
                </n-thing>
              </n-list-item>
            </n-list>
            <n-empty v-else description="暂无公告" size="small" />
          </n-spin>
        </n-card>
      </n-gi>
    </n-grid>
  </admin-page>
</template>

<style scoped>
/* 纯 CSS 条形趋势：不引图表库，宽度按当日成交额占比算 */
.trend {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.trend-row {
  display: grid;
  grid-template-columns: 96px 1fr 110px 56px;
  align-items: center;
  gap: 10px;
}
.trend-date {
  font-size: 12px;
  color: var(--gp-text-3);
  font-variant-numeric: tabular-nums;
}
.trend-track {
  display: block;
  height: 10px;
  border-radius: var(--gp-radius-pill);
  background: var(--gp-bg-soft);
  border: 1px solid var(--gp-line-soft);
  overflow: hidden;
}
.trend-bar {
  display: block;
  height: 100%;
  border-radius: var(--gp-radius-pill);
  background: var(--gp-accent);
  transition: width 0.3s ease;
}
.trend-amount {
  font-size: 12px;
  text-align: right;
  color: var(--gp-text-1);
}
.trend-count {
  text-align: right;
}
@media (max-width: 768px) {
  .trend-row {
    grid-template-columns: 84px 1fr 96px;
  }
  .trend-count {
    display: none;
  }
}
</style>
