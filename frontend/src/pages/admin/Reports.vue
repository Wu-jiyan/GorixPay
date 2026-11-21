<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NGridItem, NText, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { DownloadOutline, PeopleOutline, RefreshOutline, SearchOutline, StatsChartOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { exportCsvUrl, reportsApi } from '@/api/reports';
import type { ExportQuery, ReportQuery, ReportRow } from '@/api/reports';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney } from '@/utils/format';
import { asRow, fieldNumber, itemsOf, numberOr, pick, textOf, toISOString, useNarrow, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const buyers = ref<ReportRow[]>([]);
const merchants = ref<ReportRow[]>([]);
const buyersLoading = ref(false);
const merchantsLoading = ref(false);

const buyerKeyword = ref('');
const buyerRange = ref<[number, number] | null>(null);
const merchantKeyword = ref('');
const merchantRange = ref<[number, number] | null>(null);

const {
  page: buyerPage,
  pageSize: buyerPageSize,
  total: buyerTotal,
  pagination: buyerPagination,
  onPage: onBuyerPage,
  onPageSize: onBuyerPageSize,
  resetPage: resetBuyerPage,
} = useServerPaging(loadBuyers);

const {
  page: merchantPage,
  pageSize: merchantPageSize,
  total: merchantTotal,
  pagination: merchantPagination,
  onPage: onMerchantPage,
  onPageSize: onMerchantPageSize,
  resetPage: resetMerchantPage,
} = useServerPaging(loadMerchants);

function queryOf(keyword: string, range: [number, number] | null, page: number, pageSize: number): ReportQuery {
  return {
    page,
    page_size: pageSize,
    keyword: keyword.trim() || undefined,
    created_from: range ? toISOString(range[0]) : undefined,
    created_to: range ? toISOString(range[1]) : undefined,
  };
}

function exportQuery(keyword: string, range: [number, number] | null): ExportQuery {
  return {
    keyword: keyword.trim() || undefined,
    created_from: range ? toISOString(range[0]) : undefined,
    created_to: range ? toISOString(range[1]) : undefined,
  };
}

async function loadBuyers() {
  buyersLoading.value = true;
  try {
    const data = await reportsApi.buyers(queryOf(buyerKeyword.value, buyerRange.value, buyerPage.value, buyerPageSize.value));
    const items = itemsOf<ReportRow>(data);
    // 统计行没有稳定主键，用「页码 + 行号」造一个，避免买家标识重复时 Vue 复用错行
    buyers.value = items.map((row, index) => ({ ...row, __rowKey: `${buyerPage.value}-${index}` }));
    buyerTotal.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    buyersLoading.value = false;
  }
}

async function loadMerchants() {
  merchantsLoading.value = true;
  try {
    const data = await reportsApi.merchants(
      queryOf(merchantKeyword.value, merchantRange.value, merchantPage.value, merchantPageSize.value),
    );
    const items = itemsOf<ReportRow>(data);
    merchants.value = items.map((row, index) => ({ ...row, __rowKey: `${merchantPage.value}-${index}` }));
    merchantTotal.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    merchantsLoading.value = false;
  }
}

function searchBuyers() {
  resetBuyerPage();
  void loadBuyers();
}

function resetBuyerFilters() {
  buyerKeyword.value = '';
  buyerRange.value = null;
  resetBuyerPage();
  void loadBuyers();
}

function searchMerchants() {
  resetMerchantPage();
  void loadMerchants();
}

function resetMerchantFilters() {
  merchantKeyword.value = '';
  merchantRange.value = null;
  resetMerchantPage();
  void loadMerchants();
}

function reloadAll() {
  void loadBuyers();
  void loadMerchants();
}

function exportBuyers() {
  window.open(exportCsvUrl('buyers', exportQuery(buyerKeyword.value, buyerRange.value)), '_blank');
}

function exportMerchants() {
  window.open(exportCsvUrl('merchants', exportQuery(merchantKeyword.value, merchantRange.value)), '_blank');
}

function dateCell(row: ReportRow, ...keys: string[]): string {
  const value = pick(row, ...keys);
  return typeof value === 'string' ? formatDateTime(value) : '-';
}

function sumOf(rows: ReportRow[], ...keys: string[]): number {
  return rows.reduce((total, row) => total + (fieldNumber(row, ...keys) ?? 0), 0);
}

const buyerPageAmount = computed(() => sumOf(buyers.value, 'total_amount', 'amount', 'paid_amount', 'sum_amount'));
const merchantPageAmount = computed(() => sumOf(merchants.value, 'paid_amount', 'total_amount', 'payment_amount'));
const merchantPageRefund = computed(() => sumOf(merchants.value, 'refund_amount', 'refunded_amount', 'refunds'));
const merchantPageFee = computed(() => sumOf(merchants.value, 'fee_amount', 'fee_cents', 'fee'));

// 卡片数字全部来自「已返回的数据」：条数用后端 total，金额只汇总当前页，避免让人误读成全站合计
const statCards = computed(() => [
  { key: 'buyers', label: '买家记录', value: String(buyerTotal.value) },
  { key: 'merchants', label: '商户记录', value: String(merchantTotal.value) },
  { key: 'buyer-amount', label: '本页买家支付', value: formatMoney(buyerPageAmount.value) },
  { key: 'merchant-amount', label: '本页商户支付', value: formatMoney(merchantPageAmount.value) },
  { key: 'merchant-refund', label: '本页退款金额', value: formatMoney(merchantPageRefund.value) },
  { key: 'merchant-fee', label: '本页手续费估算', value: formatMoney(merchantPageFee.value) },
]);

function moneyNode(amount: number | null, type?: 'default' | 'success' | 'error' | 'warning') {
  return h(NText, { class: 'amount', type }, { default: () => formatMoney(amount) });
}

function monoNode(value: string) {
  return h('span', { class: 'mono' }, value || '-');
}

const buyerColumns = computed<DataTableColumns<ReportRow>>(() => [
  {
    title: '买家',
    key: 'buyer',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render: (row) =>
      monoNode(textOf(pick(row, 'buyer', 'buyer_id', 'payer', 'payer_id', 'account', 'client_ip'))),
  },
  {
    title: '笔数',
    key: 'order_count',
    width: 100,
    align: 'right',
    render: (row) => textOf(pick(row, 'order_count', 'count', 'orders', 'paid_count', 'total_orders')),
  },
  {
    title: '金额',
    key: 'total_amount',
    width: 140,
    align: 'right',
    render: (row) => moneyNode(fieldNumber(row, 'total_amount', 'amount', 'paid_amount', 'sum_amount')),
  },
  {
    title: '首次支付',
    key: 'first_at',
    width: 170,
    render: (row) => dateCell(row, 'first_at', 'first_time', 'first_order_at', 'created_from'),
  },
  {
    title: '最近支付',
    key: 'last_at',
    width: 170,
    render: (row) => dateCell(row, 'last_at', 'last_time', 'last_order_at', 'updated_at', 'created_to'),
  },
]);

const merchantColumns = computed<DataTableColumns<ReportRow>>(() => [
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 260,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(textOf(pick(row, 'merchant_id', 'id'))),
  },
  {
    title: '商户名称',
    key: 'display_name',
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) => textOf(pick(row, 'display_name', 'merchant_name', 'name')),
  },
  {
    title: '订单数',
    key: 'order_count',
    width: 100,
    align: 'right',
    render: (row) => textOf(pick(row, 'order_count', 'orders', 'total_orders')),
  },
  {
    title: '支付金额',
    key: 'paid_amount',
    width: 150,
    align: 'right',
    render: (row) => moneyNode(fieldNumber(row, 'paid_amount', 'total_amount', 'payment_amount')),
  },
  {
    title: '退款金额',
    key: 'refund_amount',
    width: 150,
    align: 'right',
    render: (row) => moneyNode(fieldNumber(row, 'refund_amount', 'refunded_amount', 'refunds'), 'error'),
  },
  {
    title: '手续费估算',
    key: 'fee_amount',
    width: 150,
    align: 'right',
    render: (row) => moneyNode(fieldNumber(row, 'fee_amount', 'fee_cents', 'fee'), 'success'),
  },
]);

onMounted(() => {
  void loadBuyers();
  void loadMerchants();
});
</script>

<template>
  <admin-page title="报表" subtitle="按买家与商户维度聚合的笔数、金额与手续费估算，可导出 CSV">
    <template #actions>
      <n-button :loading="buyersLoading || merchantsLoading" @click="reloadAll">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-grid cols="2 s:3 m:6" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 16px">
      <n-grid-item v-for="card in statCards" :key="card.key">
        <n-card size="small" :bordered="false">
          <n-statistic :label="card.label" :value="card.value" />
        </n-card>
      </n-grid-item>
    </n-grid>

    <n-card size="small" :bordered="false">
      <n-tabs type="line" animated>
        <n-tab-pane name="buyers">
          <template #tab>
            <n-space :size="6" align="center">
              <n-icon :component="PeopleOutline" />
              <span>买家统计</span>
            </n-space>
          </template>

          <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
            <n-gi>
              <n-input
                v-model:value="buyerKeyword"
                placeholder="买家标识 / 付款方账号"
                clearable
                style="width: 100%"
                @keyup.enter="searchBuyers"
              >
                <template #prefix>
                  <n-icon :component="SearchOutline" />
                </template>
              </n-input>
            </n-gi>
            <n-gi>
              <n-date-picker
                v-model:value="buyerRange"
                type="daterange"
                clearable
                start-placeholder="开始日期"
                end-placeholder="结束日期"
                style="width: 100%"
              />
            </n-gi>
            <n-gi>
              <n-space :wrap="true">
                <n-button type="primary" @click="searchBuyers">查询</n-button>
                <n-button @click="resetBuyerFilters">重置</n-button>
                <n-divider vertical />
                <n-button @click="exportBuyers">
                  <template #icon>
                    <n-icon :component="DownloadOutline" />
                  </template>
                  导出 CSV
                </n-button>
              </n-space>
            </n-gi>
          </n-grid>

          <n-data-table
            :columns="buyerColumns"
            :data="buyers"
            :loading="buyersLoading"
            :pagination="buyerPagination"
            :row-key="(row: ReportRow) => String(row.__rowKey ?? '')"
            :scroll-x="900"
            :size="isNarrow ? 'small' : 'medium'"
            remote
            @update:page="onBuyerPage"
            @update:page-size="onBuyerPageSize"
          >
            <template #empty>
              <n-empty description="暂无买家统计数据" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="merchants">
          <template #tab>
            <n-space :size="6" align="center">
              <n-icon :component="StatsChartOutline" />
              <span>商户统计</span>
            </n-space>
          </template>

          <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
            <n-gi>
              <n-input
                v-model:value="merchantKeyword"
                placeholder="商户 ID / 名称"
                clearable
                style="width: 100%"
                @keyup.enter="searchMerchants"
              >
                <template #prefix>
                  <n-icon :component="SearchOutline" />
                </template>
              </n-input>
            </n-gi>
            <n-gi>
              <n-date-picker
                v-model:value="merchantRange"
                type="daterange"
                clearable
                start-placeholder="开始日期"
                end-placeholder="结束日期"
                style="width: 100%"
              />
            </n-gi>
            <n-gi>
              <n-space :wrap="true">
                <n-button type="primary" @click="searchMerchants">查询</n-button>
                <n-button @click="resetMerchantFilters">重置</n-button>
                <n-divider vertical />
                <n-button @click="exportMerchants">
                  <template #icon>
                    <n-icon :component="DownloadOutline" />
                  </template>
                  导出 CSV
                </n-button>
              </n-space>
            </n-gi>
          </n-grid>

          <n-data-table
            :columns="merchantColumns"
            :data="merchants"
            :loading="merchantsLoading"
            :pagination="merchantPagination"
            :row-key="(row: ReportRow) => String(row.__rowKey ?? '')"
            :scroll-x="1080"
            :size="isNarrow ? 'small' : 'medium'"
            remote
            @update:page="onMerchantPage"
            @update:page-size="onMerchantPageSize"
          >
            <template #empty>
              <n-empty description="暂无商户统计数据" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </admin-page>
</template>
