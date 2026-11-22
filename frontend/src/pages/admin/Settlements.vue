<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, DataTableRowKey, SelectOption } from 'naive-ui';
import {
  CheckmarkOutline,
  CloseOutline,
  CopyOutline,
  DownloadOutline,
  RefreshOutline,
  SearchOutline,
} from '@vicons/ionicons5';
import { adminApi } from '@/api';
import type { Settlement } from '@/api';
import { adminSettlementsApi } from '@/api/adminSettlements';
import { exportCsvUrl } from '@/api/reports';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { copyText, financeStatus, formatDateTime, formatMoney } from '@/utils/format';
import { asRow, fieldText, prettyJson, useNarrow, usePendingAction, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

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

const showDetail = ref(false);
const detail = ref<Settlement | null>(null);
const detailLoading = ref(false);

const actions = usePendingAction();
const batchAction = usePendingAction();

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

const transferLabels: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  none: { text: '未打款', type: 'default' },
  pending: { text: '打款中', type: 'warning' },
  done: { text: '已打款', type: 'success' },
  failed: { text: '打款失败', type: 'error' },
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

/** 详情抽屉：先用行数据铺底，再取 GET /admin/settlements/:id 拿全字段 */
async function openDetail(row: Settlement) {
  detail.value = row;
  showDetail.value = true;
  detailLoading.value = true;
  try {
    detail.value = await adminApi.settlement(row.id);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    detailLoading.value = false;
  }
}

async function copyDetail() {
  const ok = await copyText(prettyJson(detail.value));
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
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
  if (!meta) return h(NText, { depth: 3 }, () => (raw === '-' ? '-' : raw));
  return h(NTag, { size: 'small', bordered: false, type: meta.type }, () => meta.text);
}

/** 详情抽屉里用纯文本展示打款状态（表格里已经有同源的 tag 渲染） */
function transferText(status?: string): string {
  const meta = transferLabels[status ?? ''];
  return meta ? meta.text : status || '-';
}

const columns = computed<DataTableColumns<Settlement>>(() => {
  const pendingSet = actions.pending.value;
  const batchBusy = batchAction.pending.value.has('batch:approve');
  return [
    { type: 'selection', disabled: () => batchBusy },
    {
      title: '结算单号',
      key: 'settlement_no',
      width: 200,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.settlement_no || '-'),
    },
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 230,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.merchant_id || '-'),
    },
    {
      title: '结算金额',
      key: 'amount',
      width: 130,
      align: 'right',
      render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
    },
    {
      title: '收款去向',
      key: 'destination',
      minWidth: 180,
      ellipsis: { tooltip: true },
      render: (row) => row.destination || '-',
    },
    {
      title: '状态',
      key: 'status',
      width: 100,
      render: (row) =>
        h(NTag, { size: 'small', bordered: false, type: financeStatus(row.status).type }, () =>
          financeStatus(row.status).text,
        ),
    },
    { title: '打款', key: 'transfer_status', width: 110, render: (row) => transferCell(row) },
    {
      title: '创建时间',
      key: 'created_at',
      width: 170,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 250,
      fixed: 'right',
      render: (row) => {
        const approving = pendingSet.has(`approve:${row.id}`);
        const transferring = pendingSet.has(`auto:${row.id}`);
        const detailButton = h(
          NButton,
          { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) },
          () => '详情',
        );
        if (row.status === 'pending') {
          return h(NSpace, { size: 8, wrap: false }, () => [
            detailButton,
            h(
              NPopconfirm,
              { onPositiveClick: () => approve(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'small', text: true, type: 'primary', loading: approving, disabled: approving },
                    () => '通过',
                  ),
                default: () => '确认通过该结算申请？',
              },
            ),
            h(
              NButton,
              {
                size: 'small',
                text: true,
                type: 'error',
                disabled: approving,
                onClick: () => openReject(row),
              },
              () => '驳回',
            ),
          ]);
        }
        if (row.status === 'approved') {
          return h(NSpace, { size: 8, wrap: false }, () => [
            detailButton,
            h(
              NPopconfirm,
              { onPositiveClick: () => autoTransfer(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      size: 'small',
                      text: true,
                      type: 'primary',
                      loading: transferring,
                      disabled: transferring || fieldText(row, 'transfer_status') === 'done',
                    },
                    () => '自动打款',
                  ),
                default: () => '为该结算单创建代付单？需要商户已配置结算资料且存在自动打款规则。',
              },
            ),
          ]);
        }
        return detailButton;
      },
    },
  ];
});

onMounted(load);
</script>

<template>
  <admin-page title="结算管理" subtitle="商户结算申请审核，支持批量审批与审核后自动打款">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button @click="exportCsv">
          <template #icon>
            <n-icon :component="DownloadOutline" />
          </template>
          导出 CSV
        </n-button>
        <n-button
          type="primary"
          :disabled="selectedCount === 0"
          :loading="batchAction.pending.value.has('batch:approve')"
          @click="batchApprove"
        >
          <template #icon>
            <n-icon :component="CheckmarkOutline" />
          </template>
          批量通过<span v-if="selectedCount > 0">（{{ selectedCount }}）</span>
        </n-button>
        <n-button type="error" secondary :disabled="selectedCount === 0" @click="openBatchReject">
          <template #icon>
            <n-icon :component="CloseOutline" />
          </template>
          批量驳回
        </n-button>
      </n-space>
    </template>

    <n-card :bordered="false">
      <n-form :show-feedback="false" :label-placement="isNarrow ? 'top' : 'left'">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="关键字">
              <n-input
                v-model:value="keyword"
                placeholder="结算单号 / 商户单号"
                clearable
                style="width: 100%"
                @keyup.enter="search"
              >
                <template #prefix>
                  <n-icon :component="SearchOutline" />
                </template>
              </n-input>
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="商户 ID">
              <n-input
                v-model:value="merchantId"
                placeholder="按商户筛选"
                clearable
                style="width: 100%"
                @keyup.enter="search"
              >
                <template #prefix>
                  <n-icon :component="SearchOutline" />
                </template>
              </n-input>
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="状态">
              <n-select
                v-model:value="status"
                :options="statusOptions"
                placeholder="全部状态"
                clearable
                style="width: 100%"
              />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item :show-label="false">
              <n-space :wrap="true" align="center">
                <n-button type="primary" @click="search">
                  <template #icon>
                    <n-icon :component="SearchOutline" />
                  </template>
                  查询
                </n-button>
                <n-button @click="resetFilters">重置</n-button>
              </n-space>
            </n-form-item>
          </n-gi>
        </n-grid>
      </n-form>

      <n-data-table
        :bordered="false"
        v-model:checked-row-keys="checkedKeys"
        :remote="true"
        :loading="loading"
        :columns="columns"
        :data="rows"
        :pagination="pagination"
        :row-key="(row: Settlement) => row.id"
        :scroll-x="1400"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-modal
      v-model:show="showReject"
      preset="card"
      :title="batchReject ? `批量驳回 ${selectedCount} 笔结算单` : '驳回结算申请'"
      style="width: 560px; max-width: 94vw"
    >
      <n-descriptions v-if="rejectTarget" :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left">
        <n-descriptions-item label="结算单号">
          <n-text class="mono">{{ rejectTarget.settlement_no }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="结算金额">
          <n-text class="amount">{{ formatMoney(rejectTarget.amount) }}</n-text>
        </n-descriptions-item>
      </n-descriptions>

      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160" style="margin-top: 16px">
        <n-form-item label="驳回原因">
          <n-input
            v-model:value="rejectionReason"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 6 }"
            placeholder="请填写驳回原因，商户可见"
          />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showReject = false">取消</n-button>
          <n-button type="error" :loading="rejectSubmitting" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-drawer v-model:show="showDetail" :width="isNarrow ? '92vw' : 640" placement="right">
      <n-drawer-content title="结算单详情" closable>
        <n-spin :show="detailLoading">
          <template v-if="detail">
            <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
              <n-descriptions-item label="结算单号">
                <span class="mono">{{ detail.settlement_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag size="small" round :bordered="false" :type="financeStatus(detail.status).type">
                  {{ financeStatus(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="商户结算单号">
                <span class="mono">{{ detail.out_settlement_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="结算金额">
                <span class="amount">{{ formatMoney(detail.amount) }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="币种">{{ detail.currency || '-' }}</n-descriptions-item>
              <n-descriptions-item label="手续费">
                <span class="amount">{{ formatMoney(detail.fee_cents ?? 0) }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="商户 ID">
                <span class="mono">{{ detail.merchant_id || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="来源">{{ detail.source || '-' }}</n-descriptions-item>
              <n-descriptions-item label="打款状态">{{ transferText(detail.transfer_status) }}</n-descriptions-item>
              <n-descriptions-item label="打款时间">{{ formatDateTime(detail.transfer_at) }}</n-descriptions-item>
              <n-descriptions-item label="收款去向" :span="isNarrow ? 1 : 2">
                {{ detail.destination || '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="驳回原因" :span="isNarrow ? 1 : 2">
                <span :class="{ 'danger-text': !!detail.rejection_reason }">{{ detail.rejection_reason || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="账本交易 ID" :span="isNarrow ? 1 : 2">
                <span class="mono">{{ detail.ledger_transaction_id || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="审核人">
                <span class="mono">{{ detail.processed_by || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="审核时间">{{ formatDateTime(detail.processed_at) }}</n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
              <n-descriptions-item label="更新时间">{{ formatDateTime(detail.updated_at) }}</n-descriptions-item>
              <n-descriptions-item label="记录 ID" :span="isNarrow ? 1 : 2">
                <span class="mono">{{ detail.id }}</span>
              </n-descriptions-item>
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

<style scoped>
.danger-text {
  color: var(--gp-danger);
}
</style>
