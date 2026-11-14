<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import { NTag, useMessage, type DataTableColumns, type SelectOption } from 'naive-ui';
import { financeApi, type LedgerRecord, type RecordsQuery } from '@/api/merchantFinance';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime } from '@/utils/format';

const message = useMessage();

const TYPE_LABELS: Record<string, string> = {
  payment: '订单支付',
  income: '订单支付',
  refund: '退款',
  settlement: '结算',
  transfer: '代付',
  recharge: '余额充值',
  deposit: '保证金',
  rebate: '邀请返佣',
  split: '分账',
  cert_fee: '认证费',
  adjustment: '人工调整',
  opening_balance: '期初余额',
};

function typeText(type?: string | null): string {
  const key = (type ?? '').toLowerCase();
  return TYPE_LABELS[key] ?? (type || '-');
}

const loading = ref(false);
const exporting = ref(false);
const items = ref<LedgerRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const filters = reactive({
  type: null as string | null,
  range: null as [number, number] | null,
});

const typeOptions: SelectOption[] = Object.entries(TYPE_LABELS)
  .filter(([value]) => value !== 'income')
  .map(([value, label]) => ({ label, value }));

const rowKey = (row: LedgerRecord) => row.id ?? `${row.created_at}-${row.reference ?? ''}-${row.amount}`;

function directionMeta(direction?: string) {
  return direction === 'debit'
    ? { text: '支出', type: 'error' as const, color: '#dc2626', sign: '-' }
    : { text: '收入', type: 'success' as const, color: '#059669', sign: '+' };
}

const columns: DataTableColumns<LedgerRecord> = [
  {
    title: '时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '类型',
    key: 'type',
    width: 130,
    render: (row) => typeText(row.type),
  },
  {
    title: '方向',
    key: 'direction',
    width: 90,
    render: (row) => {
      const meta = directionMeta(row.direction);
      return h(NTag, { size: 'small', type: meta.type, bordered: false }, () => meta.text);
    },
  },
  {
    title: '金额',
    key: 'amount',
    width: 140,
    align: 'right',
    render: (row) => {
      const meta = directionMeta(row.direction);
      return h('span', { class: 'amount-cell' }, [
        meta.sign,
        h(MoneyText, { amount: row.amount, color: meta.color, strong: false }),
      ]);
    },
  },
  {
    title: '余额',
    key: 'balance_after',
    width: 140,
    align: 'right',
    render: (row) =>
      row.balance_after === null || row.balance_after === undefined
        ? '-'
        : h(MoneyText, { amount: row.balance_after, color: '#334155', strong: false }),
  },
  {
    title: '关联单号',
    key: 'reference',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.reference || '-'),
  },
];

const pagination = computed(() => ({
  page: page.value,
  pageSize: pageSize.value,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

function buildFilters(): Omit<RecordsQuery, 'page' | 'page_size'> {
  const query: Omit<RecordsQuery, 'page' | 'page_size'> = {};
  if (filters.type) query.type = filters.type;
  if (filters.range && filters.range.length === 2) {
    query.created_from = dayjs(filters.range[0]).startOf('day').format('YYYY-MM-DD HH:mm:ss');
    query.created_to = dayjs(filters.range[1]).endOf('day').format('YYYY-MM-DD HH:mm:ss');
  }
  return query;
}

async function load() {
  loading.value = true;
  try {
    const res = await financeApi.records({
      ...buildFilters(),
      page: page.value,
      page_size: pageSize.value,
    });
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function handleExport() {
  if (exporting.value) return;
  exporting.value = true;
  try {
    const blob = await financeApi.exportRecords(buildFilters());
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `records-${dayjs().format('YYYYMMDD-HHmmss')}.csv`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    message.success('导出已开始下载');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    exporting.value = false;
  }
}

function handleSearch() {
  page.value = 1;
  void load();
}

function handleReset() {
  filters.type = null;
  filters.range = null;
  page.value = 1;
  void load();
}

function handlePageChange(value: number) {
  page.value = value;
  void load();
}

function handlePageSizeChange(value: number) {
  pageSize.value = value;
  page.value = 1;
  void load();
}

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="page">
    <PageHeader title="资金明细" subtitle="账本分录逐笔可查，支持按时间与类型筛选">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
        <n-button size="small" type="primary" :loading="exporting" @click="handleExport">导出 CSV</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="table-card">
      <div class="toolbar">
        <n-select
          v-model:value="filters.type"
          :options="typeOptions"
          placeholder="分录类型"
          clearable
          style="width: 160px"
        />
        <n-date-picker
          v-model:value="filters.range"
          type="daterange"
          clearable
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 260px"
        />
        <n-button type="primary" @click="handleSearch">查询</n-button>
        <n-button quaternary @click="handleReset">重置</n-button>
      </div>

      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="980"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无资金记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
.table-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
:deep(.amount-cell) {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
@media (max-width: 768px) {
  .toolbar :deep(.n-select),
  .toolbar :deep(.n-date-picker) {
    width: 100% !important;
  }
}
</style>
