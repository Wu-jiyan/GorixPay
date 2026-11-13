<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { MerchantDomain } from '@/api';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { httpStatus, itemsOf, numberOr, useServerPaging } from './shared';

const message = useMessage();

const rows = ref<MerchantDomain[]>([]);
const loading = ref(false);
const unavailable = ref(false);
const status = ref<string | null>(null);

const processing = ref(false);
const showReject = ref(false);
const rejectTarget = ref<MerchantDomain | null>(null);
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
    const data = await adminApi.domains({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
    });
    const items = itemsOf<MerchantDomain>(data.items);
    rows.value = items;
    total.value = numberOr(data.total, items.length);
    unavailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      rows.value = [];
      total.value = 0;
      unavailable.value = true;
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
  resetPage();
  void load();
}

async function approve(row: MerchantDomain) {
  try {
    await adminApi.reviewDomain(row.id, 'approve');
    message.success('域名已通过');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function openReject(row: MerchantDomain) {
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
    await adminApi.reviewDomain(target.id, 'reject', rejectionReason.value.trim());
    message.success('域名已驳回');
    showReject.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    processing.value = false;
  }
}

const columns = computed<DataTableColumns<MerchantDomain>>(() => [
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 280,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
  },
  {
    title: '支付域名',
    key: 'domain',
    minWidth: 220,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.domain || '-'),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '驳回原因',
    key: 'rejection_reason',
    minWidth: 160,
    ellipsis: true,
    render: (row) => row.rejection_reason || '-',
  },
  {
    title: '提交时间',
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
      return h(NSpace, { size: 4, wrap: false }, {
        default: () => [
          h(
            NPopconfirm,
            { onPositiveClick: () => approve(row) },
            {
              trigger: () =>
                h(NButton, { size: 'tiny', quaternary: true, type: 'primary' }, { default: () => '通过' }),
              default: () => '确认通过该支付域名？通过后下单可校验该域名。',
            },
          ),
          h(
            NButton,
            { size: 'tiny', quaternary: true, type: 'error', onClick: () => openReject(row) },
            { default: () => '驳回' },
          ),
        ],
      });
    },
  },
]);

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="域名审核" subtitle="商户提交的支付域名白名单审核，启用域名校验后生效">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="unavailable" type="warning" :bordered="false" style="margin-bottom: 16px">
      域名审核接口未启用：后端 <span class="mono">/admin/domains</span> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <div class="toolbar">
      <n-select v-model:value="status" :options="statusOptions" placeholder="全部状态" clearable style="width: 150px" />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
      <span class="muted" style="font-size: 12px">共 {{ total }} 条</span>
    </div>

    <n-card :bordered="false">
      <n-empty v-if="unavailable" description="接口未启用" size="large">
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
        :row-key="(row: MerchantDomain) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无域名申请" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="showReject" preset="card" title="驳回支付域名" style="width: 500px; max-width: 94vw">
      <n-descriptions v-if="rejectTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="域名">
          <span class="mono">{{ rejectTarget.domain }}</span>
        </n-descriptions-item>
        <n-descriptions-item label="商户 ID">
          <span class="mono">{{ rejectTarget.merchant_id || '-' }}</span>
        </n-descriptions-item>
      </n-descriptions>
      <div class="field">
        <div class="field-label">驳回原因</div>
        <n-input
          v-model:value="rejectionReason"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 6 }"
          placeholder="请填写驳回原因，商户可见"
        />
      </div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showReject = false">取消</n-button>
          <n-button type="error" :loading="processing" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.field {
  margin-top: 16px;
}
.field-label {
  font-size: 13px;
  color: #4b5563;
  margin-bottom: 6px;
}
</style>
