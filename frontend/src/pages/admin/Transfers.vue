<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import {
  AddOutline,
  CashOutline,
  DownloadOutline,
  GiftOutline,
  PaperPlaneOutline,
  RefreshOutline,
  SearchOutline,
  TrashOutline,
} from '@vicons/ionicons5';
import { adminApi } from '@/api';
import type { Transfer } from '@/api';
import { adminTransfersApi } from '@/api/adminTransfers';
import type { RedBatch } from '@/api/adminTransfers';
import { exportCsvUrl } from '@/api/reports';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { financeStatus, formatDateTime, formatMoney } from '@/utils/format';
import { asRow, httpStatus, numberOr, useNarrow, usePendingAction, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<Transfer[]>([]);
const loading = ref(false);
const notAvailable = ref(false);
const status = ref<string | null>(null);
const keyword = ref('');
const merchantId = ref('');

const processing = ref(false);
const showReject = ref(false);
const rejectTarget = ref<Transfer | null>(null);
const rejectionReason = ref('');

const actions = usePendingAction();

const showRedBatches = ref(false);
const redBatches = ref<RedBatch[]>([]);
const redLoading = ref(false);
const showRedCancel = ref(false);
const redCancelTarget = ref<RedBatch | null>(null);
const redCancelReason = ref('');
const redCancelSubmitting = ref(false);
const redActions = usePendingAction();

const redPaging = useServerPaging(loadRedBatches);

interface ManualForm {
  merchant_id: string;
  amount_yuan: number | null;
  destination: string;
  remark: string;
  idempotency_key: string;
}

interface BatchRow {
  amount_yuan: number | null;
  destination: string;
}

const showManual = ref(false);
const manualSubmitting = ref(false);
const manualFormRef = ref<FormInst | null>(null);
const manualForm = reactive<ManualForm>({
  merchant_id: '',
  amount_yuan: null,
  destination: '',
  remark: '',
  idempotency_key: '',
});

const showBatch = ref(false);
const batchSubmitting = ref(false);
const batchMerchantId = ref('');
const batchRemark = ref('');
const batchRows = ref<BatchRow[]>([{ amount_yuan: null, destination: '' }]);

const manualRules: FormRules = {
  merchant_id: { required: true, message: '请输入商户 ID', trigger: ['blur', 'input'] },
  amount_yuan: { required: true, type: 'number', message: '请输入金额', trigger: ['blur', 'change'] },
  destination: { required: true, message: '请输入收款账户', trigger: ['blur', 'input'] },
};

const batchTotalCents = computed(() =>
  batchRows.value.reduce((acc, row) => acc + Math.round(numberOr(row.amount_yuan, 0) * 100), 0),
);

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.transfers({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
      merchant_id: merchantId.value.trim() || undefined,
    });
    rows.value = data.items;
    total.value = data.total;
    notAvailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      rows.value = [];
      total.value = 0;
      notAvailable.value = true;
      return;
    }
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

async function approve(row: Transfer) {
  await actions.run(`approve:${row.id}`, async () => {
    try {
      await adminApi.processTransfer(row.id, 'approve');
      message.success('代付已通过');
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

function exportCsv() {
  window.open(
    exportCsvUrl('transfers', {
      status: status.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
      merchant_id: merchantId.value.trim() || undefined,
    }),
    '_blank',
  );
}

async function loadRedBatches() {
  redLoading.value = true;
  try {
    const data = await adminTransfersApi.redBatches({
      page: redPaging.page.value,
      page_size: redPaging.pageSize.value,
      merchant_id: merchantId.value.trim() || undefined,
    });
    const items = Array.isArray(asRow(data).items) ? (asRow(data).items as RedBatch[]) : [];
    redBatches.value = items;
    redPaging.total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    redLoading.value = false;
  }
}

function openRedBatches() {
  showRedBatches.value = true;
  void loadRedBatches();
}

function openRedCancel(row: RedBatch) {
  redCancelTarget.value = row;
  redCancelReason.value = '';
  showRedCancel.value = true;
}

async function submitRedCancel() {
  const target = redCancelTarget.value;
  if (!target) return;
  await redActions.run(`cancel:${target.batch_no}`, async () => {
    redCancelSubmitting.value = true;
    try {
      const result = await adminTransfersApi.cancelRed(target.batch_no, redCancelReason.value.trim());
      const cancelled = numberOr(asRow(result).cancelled, 0);
      message.success(`已撤销 ${cancelled} 个红包`);
      showRedCancel.value = false;
      await loadRedBatches();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      redCancelSubmitting.value = false;
    }
  });
}

function openReject(row: Transfer) {
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
    await adminApi.processTransfer(target.id, 'reject', rejectionReason.value.trim());
    message.success('代付已驳回');
    showReject.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    processing.value = false;
  }
}

function openManual() {
  manualForm.merchant_id = merchantId.value.trim();
  manualForm.amount_yuan = null;
  manualForm.destination = '';
  manualForm.remark = '';
  manualForm.idempotency_key = '';
  showManual.value = true;
}

async function submitManual() {
  try {
    await manualFormRef.value?.validate();
  } catch {
    return;
  }
  const cents = Math.round(numberOr(manualForm.amount_yuan, 0) * 100);
  if (cents <= 0) {
    message.warning('金额需要大于 0');
    return;
  }
  manualSubmitting.value = true;
  try {
    await adminTransfersApi.manual({
      merchant_id: manualForm.merchant_id.trim(),
      amount_cents: cents,
      destination: manualForm.destination.trim(),
      remark: manualForm.remark.trim() || undefined,
      idempotency_key: manualForm.idempotency_key.trim() || undefined,
    });
    message.success('已提交手动付款');
    showManual.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    manualSubmitting.value = false;
  }
}

function openBatch() {
  batchMerchantId.value = merchantId.value.trim();
  batchRemark.value = '';
  batchRows.value = [{ amount_yuan: null, destination: '' }];
  showBatch.value = true;
}

function addBatchRow() {
  batchRows.value = [...batchRows.value, { amount_yuan: null, destination: '' }];
}

function removeBatchRow(index: number) {
  batchRows.value = batchRows.value.filter((_, i) => i !== index);
  if (batchRows.value.length === 0) addBatchRow();
}

async function submitBatch() {
  if (batchMerchantId.value.trim() === '') {
    message.warning('请输入商户 ID');
    return;
  }
  const items = batchRows.value
    .map((row) => ({
      amount_cents: Math.round(numberOr(row.amount_yuan, 0) * 100),
      destination: row.destination.trim(),
    }))
    .filter((item) => item.amount_cents > 0 && item.destination !== '');
  if (items.length === 0) {
    message.warning('至少填写一行有效的金额与收款账户');
    return;
  }
  if (items.length !== batchRows.value.length) {
    message.warning('有行金额或收款账户为空，已忽略这些行');
  }
  batchSubmitting.value = true;
  try {
    const result = await adminTransfersApi.batch({
      merchant_id: batchMerchantId.value.trim(),
      items,
      remark: batchRemark.value.trim() || undefined,
    });
    const batchNo = asRow(result).batch_no;
    const created = numberOr(asRow(result).created, items.length);
    message.success(`批量付款已提交：${created} 笔${typeof batchNo === 'string' ? `，批次号 ${batchNo}` : ''}`);
    showBatch.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    batchSubmitting.value = false;
  }
}

const columns = computed<DataTableColumns<Transfer>>(() => {
  const pendingSet = actions.pending.value;
  return [
    {
      title: '代付单号',
      key: 'transfer_no',
      width: 200,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.transfer_no || '-'),
    },
    {
      title: '商户代付单号',
      key: 'out_transfer_no',
      width: 190,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.out_transfer_no || '-'),
    },
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 230,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.merchant_id || '-'),
    },
    {
      title: '代付金额',
      key: 'amount',
      width: 130,
      align: 'right',
      render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
    },
    {
      title: '收款人',
      key: 'account_name',
      width: 130,
      ellipsis: { tooltip: true },
      render: (row) => row.account_name || '-',
    },
    {
      title: '收款账号',
      key: 'account_no',
      width: 190,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.account_no || '-'),
    },
    {
      title: '开户行',
      key: 'bank_name',
      minWidth: 160,
      ellipsis: { tooltip: true },
      render: (row) => row.bank_name || '-',
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
    {
      title: '创建时间',
      key: 'created_at',
      width: 170,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      fixed: 'right',
      render: (row) => {
        if (row.status !== 'pending') return h(NText, { depth: 3 }, () => '已处理');
        const approving = pendingSet.has(`approve:${row.id}`);
        return h(NSpace, { size: 8, wrap: false }, () => [
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
              default: () => '确认通过该代付申请？',
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
      },
    },
  ];
});

const redColumns = computed<DataTableColumns<RedBatch>>(() => {
  const pendingSet = redActions.pending.value;
  return [
    {
      title: '批次号',
      key: 'batch_no',
      width: 210,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.batch_no || '-'),
    },
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 230,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.merchant_id || '-'),
    },
    {
      title: '总额',
      key: 'amount',
      width: 130,
      align: 'right',
      render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
    },
    {
      title: '领取 / 总数',
      key: 'count',
      width: 120,
      align: 'right',
      render: (row) => `${numberOr(row.claimed_count, 0)} / ${numberOr(row.count, 0)}`,
    },
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
      title: '创建时间',
      key: 'created_at',
      width: 170,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 110,
      fixed: 'right',
      render: (row) => {
        const cancelling = pendingSet.has(`cancel:${row.batch_no}`);
        return h(
          NButton,
          {
            size: 'small',
            text: true,
            type: 'error',
            loading: cancelling,
            disabled: cancelling,
            onClick: () => openRedCancel(row),
          },
          () => '撤销',
        );
      },
    },
  ];
});

onMounted(load);
</script>

<template>
  <admin-page title="代付管理" subtitle="商户代付申请审核，需扣减可用余额或冻结资金">
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
        <n-button @click="openRedBatches">
          <template #icon>
            <n-icon :component="GiftOutline" />
          </template>
          红包批次
        </n-button>
        <n-button @click="openBatch">
          <template #icon>
            <n-icon :component="PaperPlaneOutline" />
          </template>
          批量付款
        </n-button>
        <n-button type="primary" @click="openManual">
          <template #icon>
            <n-icon :component="CashOutline" />
          </template>
          手动付款
        </n-button>
      </n-space>
    </template>

    <n-alert v-if="notAvailable" type="warning" :bordered="false" style="margin-bottom: 16px">
      代付接口未启用：后端 <n-text class="mono">/admin/transfers</n-text> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-form :show-feedback="false" :label-placement="isNarrow ? 'top' : 'left'">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="关键字">
              <n-input
                v-model:value="keyword"
                placeholder="代付单号 / 商户单号"
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
              <n-space :wrap="true">
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

      <n-empty v-if="notAvailable" description="接口未启用">
        <template #extra>
          <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
        </template>
      </n-empty>

      <n-data-table
        v-else
        :remote="true"
        :loading="loading"
        :columns="columns"
        :data="rows"
        :pagination="pagination"
        :row-key="(row: Transfer) => row.id"
        :scroll-x="1500"
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-modal v-model:show="showReject" preset="card" title="驳回代付申请" style="width: 560px; max-width: 94vw">
      <n-descriptions v-if="rejectTarget" :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left">
        <n-descriptions-item label="代付单号">
          <n-text class="mono">{{ rejectTarget.transfer_no }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="代付金额">
          <n-text class="amount">{{ formatMoney(rejectTarget.amount) }}</n-text>
        </n-descriptions-item>
      </n-descriptions>

      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120" style="margin-top: 16px">
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
          <n-button type="error" :loading="processing" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showManual" preset="card" title="手动付款" style="width: 640px; max-width: 94vw">
      <n-alert type="warning" :bordered="false" style="margin-bottom: 14px">
        手动付款会直接生成一笔代付单，后端按账本规则扣减可用余额；金额与收款账户请核对后再提交。
      </n-alert>

      <n-form ref="manualFormRef" :model="manualForm" :rules="manualRules" :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120">
        <n-form-item label="商户 ID" path="merchant_id">
          <n-input v-model:value="manualForm.merchant_id" placeholder="这笔付款记在哪个商户名下" />
        </n-form-item>
        <n-form-item label="金额（元）" path="amount_yuan">
          <n-input-number
            v-model:value="manualForm.amount_yuan"
            :min="0.01"
            :step="1"
            :precision="2"
            style="width: 100%; max-width: 240px"
          />
        </n-form-item>
        <n-form-item label="收款账户" path="destination">
          <n-input v-model:value="manualForm.destination" placeholder="银行卡号 / 支付宝账号 / USDT 地址" />
        </n-form-item>
        <n-form-item label="备注">
          <n-input v-model:value="manualForm.remark" placeholder="可选，例如：人工补单" />
        </n-form-item>
        <n-form-item label="幂等键">
          <n-input v-model:value="manualForm.idempotency_key" placeholder="可选；重试时填同一个值可防重复出账" />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showManual = false">取消</n-button>
          <n-button type="primary" :loading="manualSubmitting" @click="submitManual">提交付款</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showBatch" preset="card" title="批量付款" style="width: 820px; max-width: 96vw">
      <n-alert type="warning" :bordered="false" style="margin-bottom: 14px">
        所有行记在同一个商户名下，合计金额会先从该商户可用余额扣减；空行或金额为 0 的行提交时会被忽略。
      </n-alert>

      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120">
        <n-form-item label="商户 ID">
          <n-input v-model:value="batchMerchantId" placeholder="这笔批次记在哪个商户名下" />
        </n-form-item>
        <n-form-item label="备注">
          <n-input v-model:value="batchRemark" placeholder="可选，例如：批量代付-11 月" />
        </n-form-item>
        <n-form-item label="付款明细">
          <n-space vertical :size="10" style="width: 100%">
            <n-grid
              v-for="(row, index) in batchRows"
              :key="index"
              :cols="isNarrow ? 1 : 3"
              :x-gap="10"
              :y-gap="10"
              style="width: 100%"
            >
              <n-gi>
                <n-input-number
                  v-model:value="row.amount_yuan"
                  :min="0"
                  :step="1"
                  :precision="2"
                  placeholder="金额（元）"
                  style="width: 100%"
                />
              </n-gi>
              <n-gi>
                <n-input v-model:value="row.destination" placeholder="收款账户" style="width: 100%" />
              </n-gi>
              <n-gi>
                <n-button size="small" text type="error" @click="removeBatchRow(index)">
                  <template #icon>
                    <n-icon :component="TrashOutline" />
                  </template>
                  删除
                </n-button>
              </n-gi>
            </n-grid>
            <n-button size="small" dashed @click="addBatchRow">
              <template #icon>
                <n-icon :component="AddOutline" />
              </template>
              添加一行
            </n-button>
          </n-space>
        </n-form-item>
      </n-form>

      <n-space align="center" :size="6" :wrap="true">
        <n-text depth="3">合计</n-text>
        <n-text class="amount">{{ formatMoney(batchTotalCents) }}</n-text>
        <n-text depth="3" style="font-size: 12px">（{{ batchRows.length }} 行）</n-text>
      </n-space>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showBatch = false">取消</n-button>
          <n-button type="primary" :loading="batchSubmitting" @click="submitBatch">提交批次</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-drawer v-model:show="showRedBatches" :width="isNarrow ? '92vw' : 900" placement="right">
      <n-drawer-content title="红包批次" closable>
        <n-alert type="info" :bordered="false" style="margin-bottom: 14px">
          撤销会把该批次里仍是待领取的红包退回商户可用余额；已领取的部分不受影响。
        </n-alert>

        <n-space align="center" justify="space-between" :wrap="true" style="margin-bottom: 12px">
          <n-text depth="3" style="font-size: 12px">共 {{ redPaging.total.value }} 个批次</n-text>
          <n-button size="small" :loading="redLoading" @click="loadRedBatches">
            <template #icon>
              <n-icon :component="RefreshOutline" />
            </template>
            刷新
          </n-button>
        </n-space>

        <n-data-table
          :remote="true"
          :loading="redLoading"
          :columns="redColumns"
          :data="redBatches"
          :pagination="redPaging.pagination.value"
          :row-key="(row: RedBatch) => row.batch_no"
          :scroll-x="1100"
          @update:page="redPaging.onPage"
          @update:page-size="redPaging.onPageSize"
        />
      </n-drawer-content>
    </n-drawer>

    <n-modal v-model:show="showRedCancel" preset="card" title="撤销红包批次" style="width: 560px; max-width: 94vw">
      <n-descriptions v-if="redCancelTarget" :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left">
        <n-descriptions-item label="批次号">
          <n-text class="mono">{{ redCancelTarget.batch_no }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="商户 ID">
          <n-text class="mono">{{ redCancelTarget.merchant_id }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="总额">
          <n-text class="amount">{{ formatMoney(redCancelTarget.amount) }}</n-text>
        </n-descriptions-item>
      </n-descriptions>

      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120" style="margin-top: 16px">
        <n-form-item label="撤销原因">
          <n-input v-model:value="redCancelReason" placeholder="可选，记录到审计" />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showRedCancel = false">取消</n-button>
          <n-button type="error" :loading="redCancelSubmitting" @click="submitRedCancel">确认撤销</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
