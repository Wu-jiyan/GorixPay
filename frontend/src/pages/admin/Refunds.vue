<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { DownloadOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { adminApi } from '@/api';
import type { Refund } from '@/api';
import { exportCsvUrl } from '@/api/reports';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { financeStatus, formatDateTime, formatMoney } from '@/utils/format';
import { useNarrow, usePendingAction, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<Refund[]>([]);
const loading = ref(false);
const status = ref<string | null>(null);
const keyword = ref('');
const merchantId = ref('');

const processing = ref(false);
const showReject = ref(false);
const rejectTarget = ref<Refund | null>(null);
const rejectionReason = ref('');

const actions = usePendingAction();

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.refunds({
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

function exportCsv() {
  window.open(
    exportCsvUrl('refunds', {
      status: status.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
      merchant_id: merchantId.value.trim() || undefined,
    }),
    '_blank',
  );
}

async function approve(row: Refund) {
  await actions.run(`approve:${row.id}`, async () => {
    try {
      await adminApi.processRefund(row.id, 'approve');
      message.success('退款已通过并处理');
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

function openReject(row: Refund) {
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
    await adminApi.processRefund(target.id, 'reject', rejectionReason.value.trim());
    message.success('退款已驳回');
    showReject.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    processing.value = false;
  }
}

const columns = computed<DataTableColumns<Refund>>(() => {
  const pendingSet = actions.pending.value;
  return [
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
      width: 190,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.out_refund_no || '-'),
    },
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 240,
      ellipsis: { tooltip: true },
      render: (row) => h(NText, { class: 'mono' }, () => row.merchant_id || '-'),
    },
    {
      title: '退款金额',
      key: 'amount',
      width: 130,
      align: 'right',
      render: (row) => h(NText, { class: 'amount', type: 'error' }, () => formatMoney(row.amount)),
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
      title: '驳回原因',
      key: 'rejection_reason',
      minWidth: 160,
      ellipsis: { tooltip: true },
      render: (row) => row.rejection_reason || '-',
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
              default: () => '确认通过该退款申请？资金将按账本规则退回。',
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

onMounted(load);
</script>

<template>
  <admin-page title="退款管理" subtitle="审核商户退款申请，通过后由账本完成资金回退">
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
      </n-space>
    </template>

    <n-card size="small" :bordered="false">
      <n-form :show-feedback="false" :label-placement="isNarrow ? 'top' : 'left'">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="关键字">
              <n-input
                v-model:value="keyword"
                placeholder="退款单号 / 商户订单号"
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

      <n-data-table
        :remote="true"
        :loading="loading"
        :columns="columns"
        :data="rows"
        :pagination="pagination"
        :row-key="(row: Refund) => row.id"
        :scroll-x="1200"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-modal v-model:show="showReject" preset="card" title="驳回退款申请" style="width: 560px; max-width: 94vw">
      <n-descriptions v-if="rejectTarget" :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left">
        <n-descriptions-item label="退款单号">
          <n-text class="mono">{{ rejectTarget.refund_no }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="退款金额">
          <n-text class="amount" type="error">{{ formatMoney(rejectTarget.amount) }}</n-text>
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
          <n-button type="error" :loading="processing" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
