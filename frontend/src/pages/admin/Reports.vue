<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { exportCsvUrl, reportsApi } from '@/api/reports';
import type { ReportQuery, ReportRow } from '@/api/reports';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, fieldNumber, itemsOf, numberOr, pick, textOf, toISOString, useServerPaging } from './shared';

const message = useMessage();

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

function exportQuery(keyword: string, range: [number, number] | null): ReportQuery {
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

const buyerColumns = computed<DataTableColumns<ReportRow>>(() => [
  {
    title: '买家',
    key: 'buyer',
    minWidth: 220,
    ellipsis: true,
    render: (row) =>
      h('span', { class: 'mono' }, textOf(pick(row, 'buyer', 'buyer_id', 'payer', 'payer_id', 'account', 'client_ip'))),
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
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: fieldNumber(row, 'total_amount', 'amount', 'paid_amount', 'sum_amount') }),
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
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, textOf(pick(row, 'merchant_id', 'id'))),
  },
  {
    title: '商户名称',
    key: 'display_name',
    minWidth: 150,
    ellipsis: true,
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
    width: 140,
    align: 'right',
    render: (row) => h(MoneyText, { amount: fieldNumber(row, 'paid_amount', 'total_amount', 'payment_amount') }),
  },
  {
    title: '退款金额',
    key: 'refund_amount',
    width: 140,
    align: 'right',
    render: (row) => h(MoneyText, { amount: fieldNumber(row, 'refund_amount', 'refunded_amount', 'refunds'), color: '#b91c1c' }),
  },
  {
    title: '手续费估算',
    key: 'fee_amount',
    width: 140,
    align: 'right',
    render: (row) => h(MoneyText, { amount: fieldNumber(row, 'fee_amount', 'fee_cents', 'fee'), color: '#0f766e' }),
  },
]);

onMounted(() => {
  void loadBuyers();
  void loadMerchants();
});
</script>

<template>
  <div class="page">
    <PageHeader title="报表" subtitle="按买家与商户维度聚合的笔数、金额与手续费估算，可导出 CSV">
      <template #actions>
        <n-button secondary :loading="buyersLoading || merchantsLoading" @click="loadBuyers(); loadMerchants()">
          刷新
        </n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" title="买家统计" class="section">
      <template #header-extra>
        <n-button size="small" type="primary" secondary @click="exportBuyers">导出 CSV</n-button>
      </template>
      <div class="toolbar">
        <n-input
          v-model:value="buyerKeyword"
          placeholder="买家标识 / 付款方账号"
          clearable
          style="width: 240px"
          @keyup.enter="searchBuyers"
        />
        <n-date-picker
          v-model:value="buyerRange"
          type="daterange"
          clearable
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 260px"
        />
        <n-button type="primary" @click="searchBuyers">查询</n-button>
        <n-button @click="resetBuyerFilters">重置</n-button>
        <span class="muted small">共 {{ buyerTotal }} 条</span>
      </div>
      <n-data-table
        :columns="buyerColumns"
        :data="buyers"
        :loading="buyersLoading"
        :pagination="buyerPagination"
        :row-key="(row: ReportRow) => String(row.__rowKey ?? '')"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onBuyerPage"
        @update:page-size="onBuyerPageSize"
      >
        <template #empty>
          <n-empty description="暂无买家统计数据" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-card :bordered="false" title="商户统计" class="section">
      <template #header-extra>
        <n-button size="small" type="primary" secondary @click="exportMerchants">导出 CSV</n-button>
      </template>
      <div class="toolbar">
        <n-input
          v-model:value="merchantKeyword"
          placeholder="商户 ID / 名称"
          clearable
          style="width: 240px"
          @keyup.enter="searchMerchants"
        />
        <n-date-picker
          v-model:value="merchantRange"
          type="daterange"
          clearable
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 260px"
        />
        <n-button type="primary" @click="searchMerchants">查询</n-button>
        <n-button @click="resetMerchantFilters">重置</n-button>
        <span class="muted small">共 {{ merchantTotal }} 条</span>
      </div>
      <n-data-table
        :columns="merchantColumns"
        :data="merchants"
        :loading="merchantsLoading"
        :pagination="merchantPagination"
        :row-key="(row: ReportRow) => String(row.__rowKey ?? '')"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onMerchantPage"
        @update:page-size="onMerchantPageSize"
      >
        <template #empty>
          <n-empty description="暂无商户统计数据" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
.section {
  margin-bottom: 16px;
}
.section:last-child {
  margin-bottom: 0;
}
.small {
  font-size: 12px;
}
</style>
