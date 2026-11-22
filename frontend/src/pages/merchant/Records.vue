<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import { NTag, NText, useMessage, type DataTableColumns, type SelectOption } from 'naive-ui';
import { DownloadOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { financeApi, type LedgerRecord, type RecordsQuery } from '@/api/merchantFinance';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime, formatMoney } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

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
    ? { text: '支出', type: 'error' as const, sign: '-' }
    : { text: '收入', type: 'success' as const, sign: '+' };
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
    width: 150,
    align: 'right',
    render: (row) => {
      const meta = directionMeta(row.direction);
      return h(NText, { class: 'amount', type: meta.type }, () => `${meta.sign}${formatMoney(row.amount)}`);
    },
  },
  {
    title: '余额',
    key: 'balance_after',
    width: 150,
    align: 'right',
    render: (row) =>
      row.balance_after === null || row.balance_after === undefined
        ? '-'
        : h(NText, { class: 'amount', depth: 2 }, () => formatMoney(row.balance_after)),
  },
  {
    title: '关联单号',
    key: 'reference',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.reference || '-'),
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
  <admin-page title="资金明细" subtitle="账本分录逐笔可查，支持按时间与类型筛选">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" :loading="exporting" @click="handleExport">
          <template #icon>
            <n-icon :component="DownloadOutline" />
          </template>
          导出 CSV
        </n-button>
      </n-space>
    </template>

    <n-card :bordered="false">
      <n-form :show-feedback="false">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="分录类型">
              <n-select
                v-model:value="filters.type"
                :options="typeOptions"
                placeholder="全部类型"
                clearable
                style="width: 100%"
              />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="时间区间">
              <n-date-picker
                v-model:value="filters.range"
                type="daterange"
                clearable
                start-placeholder="开始日期"
                end-placeholder="结束日期"
                style="width: 100%"
              />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-space :wrap="true">
              <n-button type="primary" :loading="loading" @click="handleSearch">
                <template #icon>
                  <n-icon :component="SearchOutline" />
                </template>
                查询
              </n-button>
              <n-button @click="handleReset">重置</n-button>
            </n-space>
          </n-gi>
        </n-grid>
      </n-form>

      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1060"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无资金记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
