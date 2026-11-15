<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NTag, NText, useMessage, type DataTableColumns, type SelectOption } from 'naive-ui';
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { PageQuery, Refund } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { financeStatus, formatDateTime, formatMoney } from '@/utils/format';

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
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.refund_no || '-'),
  },
  {
    title: '商户退款单号',
    key: 'out_refund_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono', depth: 3 }, () => row.out_refund_no || '-'),
  },
  {
    title: '退款金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount', type: 'error' }, () => formatMoney(row.amount)),
  },
  { title: '退款原因', key: 'reason', minWidth: 180, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: financeStatus(row.status).type }, () =>
        financeStatus(row.status).text,
      ),
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
    minWidth: 160,
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
  <admin-page title="退款记录" subtitle="查看退款申请的处理进度与审核结果">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-card size="small" :bordered="false">
      <n-form inline :show-feedback="false">
        <n-form-item label="退款状态">
          <n-select
            v-model:value="filters.status"
            :options="statusOptions"
            placeholder="全部状态"
            clearable
            style="width: 160px"
          />
        </n-form-item>
        <n-form-item>
          <n-space>
            <n-button type="primary" :loading="loading" @click="handleSearch">
              <template #icon>
                <n-icon :component="SearchOutline" />
              </template>
              查询
            </n-button>
            <n-button @click="handleReset">重置</n-button>
          </n-space>
        </n-form-item>
      </n-form>

      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1140"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无退款记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
