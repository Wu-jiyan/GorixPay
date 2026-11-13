<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { Settlement } from '@/api';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { useServerPaging } from './shared';

const message = useMessage();

const rows = ref<Settlement[]>([]);
const loading = ref(false);
const status = ref<string | null>(null);
const keyword = ref('');
const merchantId = ref('');

const processing = ref(false);
const showReject = ref(false);
const rejectTarget = ref<Settlement | null>(null);
const rejectionReason = ref('');

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.settlements({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
      merchant_id: merchantId.value.trim() || undefined,
    });
    rows.value = data.items;
    total.value = data.total;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function search() {
  resetPage();
  void load();
}

function resetFilters() {
  status.value = null;
  keyword.value = '';
  merchantId.value = '';
  resetPage();
  void load();
}

async function approve(row: Settlement) {
  try {
    await adminApi.processSettlement(row.id, 'approve');
    message.success('结算已通过');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function openReject(row: Settlement) {
  rejectTarget.value = row;
  rejectionReason.value = '';
  showReject.value = true;
}

async function submitReject() {
  const target = rejectTarget.value;
  if (!target) return;
  if (!rejectionReason.value.trim()) {
    message.warning('请填写驳回原因');
    return;
  }
  processing.value = true;
  try {
    await adminApi.processSettlement(target.id, 'reject', rejectionReason.value.trim());
    message.success('结算已驳回');
    showReject.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    processing.value = false;
  }
}

const columns = computed<DataTableColumns<Settlement>>(() => [
  {
    title: '结算单号',
    key: 'settlement_no',
    width: 190,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.settlement_no || '-'),
  },
  {
    title: '商户结算单号',
    key: 'out_settlement_no',
    width: 180,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.out_settlement_no || '-'),
  },
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
  },
  {
    title: '结算金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount, color: '#0f766e' }),
  },
  {
    title: '收款去向',
    key: 'destination',
    minWidth: 180,
    ellipsis: true,
    render: (row) => row.destination || '-',
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    fixed: 'right',
    render: (row) => {
      if (row.status !== 'pending') return h('span', { class: 'muted' }, '已处理');
      return h(
        NSpace,
        { size: 4, wrap: false },
        {
          default: () => [
            h(
              NPopconfirm,
              { onPositiveClick: () => approve(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'tiny', quaternary: true, type: 'primary' }, { default: () => '通过' }),
                default: () => '确认通过该结算申请？',
              },
            ),
            h(
              NButton,
              { size: 'tiny', quaternary: true, type: 'error', onClick: () => openReject(row) },
              { default: () => '驳回' },
            ),
          ],
        },
      );
    },
  },
]);

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="结算管理" subtitle="商户结算申请审核，通过与驳回均记入审计">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <n-input
        v-model:value="keyword"
        placeholder="结算单号 / 商户单号"
        clearable
        style="width: 240px"
        @keyup.enter="search"
      />
      <n-input v-model:value="merchantId" placeholder="商户 ID" clearable style="width: 240px" @keyup.enter="search" />
      <n-select v-model:value="status" :options="statusOptions" placeholder="全部状态" clearable style="width: 150px" />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
    </div>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Settlement) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无结算申请" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="showReject" preset="card" title="驳回结算申请" style="width: 520px; max-width: 94vw">
      <n-descriptions v-if="rejectTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="结算单号">
          <span class="mono">{{ rejectTarget.settlement_no }}</span>
        </n-descriptions-item>
        <n-descriptions-item label="结算金额">
          <MoneyText :amount="rejectTarget.amount" />
        </n-descriptions-item>
      </n-descriptions>
      <n-form-item label="驳回原因" style="margin-top: 16px">
        <n-input
          v-model:value="rejectionReason"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 6 }"
          placeholder="请填写驳回原因，商户可见"
        />
      </n-form-item>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showReject = false">取消</n-button>
          <n-button type="error" :loading="processing" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>
