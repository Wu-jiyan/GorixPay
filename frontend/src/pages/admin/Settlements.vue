<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, DataTableRowKey, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { Settlement } from '@/api';
import { adminSettlementsApi } from '@/api/adminSettlements';
import { exportCsvUrl } from '@/api/reports';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, fieldText, usePendingAction, useServerPaging } from './shared';

const message = useMessage();

const rows = ref<Settlement[]>([]);
const loading = ref(false);
const status = ref<string | null>(null);
const keyword = ref('');
const merchantId = ref('');

const checkedKeys = ref<DataTableRowKey[]>([]);

const showReject = ref(false);
const rejectTarget = ref<Settlement | null>(null);
const rejectionReason = ref('');
const batchReject = ref(false);
const rejectSubmitting = ref(false);

const actions = usePendingAction();
const batchAction = usePendingAction();

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

const transferLabels: Record<string, { text: string; color: string }> = {
  none: { text: '未打款', color: '#6b7280' },
  pending: { text: '打款中', color: '#b45309' },
  done: { text: '已打款', color: '#0f766e' },
  failed: { text: '打款失败', color: '#b91c1c' },
};

const selectedCount = computed(() => checkedKeys.value.length);

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

function resetSelection() {
  checkedKeys.value = [];
}

function filterQuery() {
  return {
    status: status.value ?? undefined,
    keyword: keyword.value.trim() || undefined,
    merchant_id: merchantId.value.trim() || undefined,
  };
}

function exportCsv() {
  window.open(exportCsvUrl('settlements', filterQuery()), '_blank');
}

async function approve(row: Settlement) {
  await actions.run(`approve:${row.id}`, async () => {
    try {
      await adminApi.processSettlement(row.id, 'approve');
      message.success('结算已通过');
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

async function autoTransfer(row: Settlement) {
  await actions.run(`auto:${row.id}`, async () => {
    try {
      const result = await adminSettlementsApi.autoTransfer(row.id);
      const data = asRow(result);
      if (data.skipped === true) {
        message.warning(`未打款：${fieldText(result, 'reason')}`);
      } else if (typeof data.transfer_no === 'string' && data.transfer_no !== '') {
        message.success(`已创建代付单 ${data.transfer_no}`);
      } else {
        message.success('已触发自动打款');
      }
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

function openReject(row: Settlement) {
  batchReject.value = false;
  rejectTarget.value = row;
  rejectionReason.value = '';
  showReject.value = true;
}

function openBatchReject() {
  if (checkedKeys.value.length === 0) {
    message.warning('先勾选要驳回的结算单');
    return;
  }
  batchReject.value = true;
  rejectTarget.value = null;
  rejectionReason.value = '';
  showReject.value = true;
}

async function submitReject() {
  const reason = rejectionReason.value.trim();
  if (reason === '') {
    message.warning('请填写驳回原因');
    return;
  }
  rejectSubmitting.value = true;
  try {
    if (batchReject.value) {
      const ids = checkedKeys.value.map((key) => String(key));
      const result = await adminSettlementsApi.batchProcess(ids, 'reject', reason);
      message.success(`已驳回 ${result.processed ?? ids.length} 笔结算单`);
      resetSelection();
    } else {
      const target = rejectTarget.value;
      if (!target) return;
      await adminApi.processSettlement(target.id, 'reject', reason);
      message.success('结算已驳回');
    }
    showReject.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    rejectSubmitting.value = false;
  }
}

async function batchApprove() {
  if (checkedKeys.value.length === 0) {
    message.warning('先勾选要通过的结算单');
    return;
  }
  await batchAction.run('batch:approve', async () => {
    try {
      const ids = checkedKeys.value.map((key) => String(key));
      const result = await adminSettlementsApi.batchProcess(ids, 'approve');
      message.success(`已通过 ${result.processed ?? ids.length} 笔结算单`);
      resetSelection();
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

function transferCell(row: Settlement) {
  const raw = fieldText(row, 'transfer_status');
  const meta = transferLabels[raw];
  if (!meta) return h('span', { class: 'muted' }, raw === '-' ? '-' : raw);
  return h('span', { style: `color: ${meta.color}` }, meta.text);
}

const columns = computed<DataTableColumns<Settlement>>(() => {
  const pendingSet = actions.pending.value;
  const batchBusy = batchAction.pending.value.has('batch:approve');
  return [
    { type: 'selection', disabled: () => batchBusy },
    {
      title: '结算单号',
      key: 'settlement_no',
      width: 190,
      ellipsis: true,
      render: (row) => h('span', { class: 'mono' }, row.settlement_no || '-'),
    },
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 250,
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
      minWidth: 170,
      ellipsis: true,
      render: (row) => row.destination || '-',
    },
    {
      title: '状态',
      key: 'status',
      width: 90,
      render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
    },
    { title: '打款', key: 'transfer_status', width: 100, render: (row) => transferCell(row) },
    {
      title: '创建时间',
      key: 'created_at',
      width: 170,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      fixed: 'right',
      render: (row) => {
        const approving = pendingSet.has(`approve:${row.id}`);
        const transferring = pendingSet.has(`auto:${row.id}`);
        if (row.status === 'pending') {
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
                      h(
                        NButton,
                        { size: 'tiny', quaternary: true, type: 'primary', loading: approving, disabled: approving },
                        { default: () => '通过' },
                      ),
                    default: () => '确认通过该结算申请？',
                  },
                ),
                h(
                  NButton,
                  {
                    size: 'tiny',
                    quaternary: true,
                    type: 'error',
                    disabled: approving,
                    onClick: () => openReject(row),
                  },
                  { default: () => '驳回' },
                ),
              ],
            },
          );
        }
        if (row.status === 'approved') {
          return h(
            NPopconfirm,
            { onPositiveClick: () => autoTransfer(row) },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: 'tiny',
                    quaternary: true,
                    type: 'info',
                    loading: transferring,
                    disabled: transferring || fieldText(row, 'transfer_status') === 'done',
                  },
                  { default: () => '自动打款' },
                ),
              default: () => '为该结算单创建代付单？需要商户已配置结算资料且存在自动打款规则。',
            },
          );
        }
        return h('span', { class: 'muted' }, '已处理');
      },
    },
  ];
});

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="结算管理" subtitle="商户结算申请审核，支持批量审批与审核后自动打款">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button secondary @click="exportCsv">导出 CSV</n-button>
        <n-button
          type="primary"
          :disabled="selectedCount === 0"
          :loading="batchAction.pending.value.has('batch:approve')"
          @click="batchApprove"
        >
          批量通过<span v-if="selectedCount > 0">（{{ selectedCount }}）</span>
        </n-button>
        <n-button type="error" secondary :disabled="selectedCount === 0" @click="openBatchReject">
          批量驳回
        </n-button>
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
      <span class="muted small">共 {{ total }} 条</span>
    </div>

    <n-card :bordered="false">
      <n-data-table
        v-model:checked-row-keys="checkedKeys"
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

    <n-modal
      v-model:show="showReject"
      preset="card"
      :title="batchReject ? `批量驳回 ${selectedCount} 笔结算单` : '驳回结算申请'"
      style="width: 520px; max-width: 94vw"
    >
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
          <n-button type="error" :loading="rejectSubmitting" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.small {
  font-size: 12px;
}
</style>
