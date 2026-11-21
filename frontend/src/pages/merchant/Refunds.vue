<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NTag, NText, useMessage, type DataTableColumns, type SelectOption } from 'naive-ui';
import { CopyOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { PageQuery, Refund } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { copyText, financeStatus, formatDateTime, formatMoney } from '@/utils/format';
import { prettyJson } from '../admin/shared';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(false);
const items = ref<Refund[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const filters = reactive({ status: null as string | null });

const showDetail = ref(false);
const detail = ref<Refund | null>(null);
const detailLoading = ref(false);

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
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right',
    render: (row) =>
      h(NButton, { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) }, () => '详情'),
  },
];

/** 退款单详情：GET /merchant/refunds/:id（列表不回 ledger / processed 字段） */
async function openDetail(row: Refund) {
  detail.value = row;
  showDetail.value = true;
  detailLoading.value = true;
  try {
    detail.value = await merchantApi.refund(row.id);
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    detailLoading.value = false;
  }
}

async function copyDetail() {
  const ok = await copyText(prettyJson(detail.value));
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
}

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
      <n-form :show-feedback="false">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="退款状态">
              <n-select
                v-model:value="filters.status"
                :options="statusOptions"
                placeholder="全部状态"
                clearable
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
        :scroll-x="1140"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无退款记录" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showDetail" :width="isNarrow ? '92vw' : 560" placement="right">
      <n-drawer-content title="退款单详情" closable>
        <n-spin :show="detailLoading">
          <template v-if="detail">
            <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
              <n-descriptions-item label="退款单号">
                <span class="mono">{{ detail.refund_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag size="small" round :bordered="false" :type="financeStatus(detail.status).type">
                  {{ financeStatus(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="商户退款单号">
                <span class="mono">{{ detail.out_refund_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="退款金额">
                <span class="amount">{{ formatMoney(detail.amount) }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="币种">{{ detail.currency || '-' }}</n-descriptions-item>
              <n-descriptions-item label="原支付订单" :span="isNarrow ? 1 : 2">
                <span class="mono">{{ detail.payment_order_id || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="退款原因" :span="isNarrow ? 1 : 2">{{ detail.reason || '-' }}</n-descriptions-item>
              <n-descriptions-item label="审核备注" :span="isNarrow ? 1 : 2">
                {{ detail.rejection_reason || '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
              <n-descriptions-item label="更新时间">{{ formatDateTime(detail.updated_at) }}</n-descriptions-item>
            </n-descriptions>

            <n-space justify="end" style="margin-top: 12px">
              <n-button size="small" text type="primary" @click="copyDetail">
                <template #icon>
                  <n-icon :component="CopyOutline" />
                </template>
                复制 JSON
              </n-button>
            </n-space>
            <n-code :code="prettyJson(detail)" language="json" word-wrap />
          </template>
          <n-empty v-else description="暂无数据" size="small" />
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>
