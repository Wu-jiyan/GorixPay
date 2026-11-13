<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { useMessage, type DataTableColumns, type SelectOption } from 'naive-ui';
import { merchantApi } from '@/api';
import type { PageQuery, Refund } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime } from '@/utils/format';

const message = useMessage();

const loading = ref(false);
const items = ref<Refund[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const filters = reactive({ status: null as string | null });

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

const rowKey = (row: Refund) => row.id;

const columns: DataTableColumns<Refund> = [
  {
    title: '退款单号',
    key: 'refund_no',
    width: 190,
    render: (row) => h('span', { class: 'mono-cell' }, row.refund_no),
  },
  {
    title: '商户退款单号',
    key: 'out_refund_no',
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell muted-cell' }, row.out_refund_no),
  },
  {
    title: '退款金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount, color: '#dc2626' }),
  },
  { title: '退款原因', key: 'reason', minWidth: 160, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '申请时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '审核备注',
    key: 'rejection_reason',
    minWidth: 140,
    ellipsis: { tooltip: true },
    render: (row) => row.rejection_reason || '-',
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

async function load() {
  loading.value = true;
  try {
    const query: PageQuery = { page: page.value, page_size: pageSize.value };
    if (filters.status) query.status = filters.status;
    const res = await merchantApi.refunds(query);
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function handleSearch() {
  page.value = 1;
  void load();
}

function handleReset() {
  filters.status = null;
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
    <PageHeader title="退款记录" subtitle="查看退款申请的处理进度与审核结果">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="table-card">
      <div class="toolbar">
        <n-select
          v-model:value="filters.status"
          :options="statusOptions"
          placeholder="退款状态"
          clearable
          style="width: 150px"
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
        :scroll-x="1100"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      />
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
:deep(.muted-cell) {
  color: #64748b;
}
@media (max-width: 768px) {
  .toolbar :deep(.n-select) {
    width: 100% !important;
  }
}
</style>
