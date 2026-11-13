<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { Transfer } from '@/api';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { httpStatus, useServerPaging } from './shared';

const message = useMessage();

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
  try {
    await adminApi.processTransfer(row.id, 'approve');
    message.success('代付已通过');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
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

const columns = computed<DataTableColumns<Transfer>>(() => [
  {
    title: '代付单号',
    key: 'transfer_no',
    width: 190,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.transfer_no || '-'),
  },
  {
    title: '商户代付单号',
    key: 'out_transfer_no',
    width: 180,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.out_transfer_no || '-'),
  },
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
  },
  {
    title: '代付金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount, color: '#7c3aed' }),
  },
  { title: '收款人', key: 'account_name', width: 120, ellipsis: true, render: (row) => row.account_name || '-' },
  {
    title: '收款账号',
    key: 'account_no',
    width: 190,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.account_no || '-'),
  },
  { title: '开户行', key: 'bank_name', minWidth: 150, ellipsis: true, render: (row) => row.bank_name || '-' },
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
                default: () => '确认通过该代付申请？',
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
    <PageHeader title="代付管理" subtitle="商户代付申请审核，需扣减可用余额或冻结资金">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="notAvailable" type="warning" :bordered="false" style="margin-bottom: 16px">
      代付接口未启用：后端 <span class="mono">/admin/transfers</span> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <div class="toolbar">
      <n-input
        v-model:value="keyword"
        placeholder="代付单号 / 商户单号"
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
      <n-empty v-if="notAvailable" description="接口未启用" size="large">
        <template #extra>
          <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
        </template>
      </n-empty>
      <n-data-table
        v-else
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Transfer) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无代付申请" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="showReject" preset="card" title="驳回代付申请" style="width: 520px; max-width: 94vw">
      <n-descriptions v-if="rejectTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="代付单号">
          <span class="mono">{{ rejectTarget.transfer_no }}</span>
        </n-descriptions-item>
        <n-descriptions-item label="代付金额">
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
