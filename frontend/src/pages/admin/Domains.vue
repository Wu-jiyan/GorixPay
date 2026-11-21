<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import type { Component } from 'vue';
import { NButton, NIcon, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import {
  CheckmarkOutline,
  CloseOutline,
  RefreshOutline,
  ReloadOutline,
  SearchOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { MerchantDomain } from '@/api';
import { errorMessage } from '@/api/client';
import { formatDateTime } from '@/utils/format';
import { httpStatus, itemsOf, numberOr, useNarrow, usePendingAction, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<MerchantDomain[]>([]);
const loading = ref(false);
const unavailable = ref(false);
const status = ref<string | null>(null);

const processing = ref(false);
const showReject = ref(false);
const rejectTarget = ref<MerchantDomain | null>(null);
const rejectionReason = ref('');

const actions = usePendingAction();

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

const domainStatusMap: Record<
  string,
  { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }
> = {
  pending: { text: '待审核', type: 'warning' },
  approved: { text: '已通过', type: 'success' },
  rejected: { text: '已驳回', type: 'error' },
};

function renderIcon(icon: Component) {
  return () => h(NIcon, null, { default: () => h(icon) });
}

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
  await actions.run(`approve:${row.id}`, async () => {
    try {
      await adminApi.reviewDomain(row.id, 'approve');
      message.success('域名已通过');
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
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

function statusTagOf(value: string) {
  const resolved = domainStatusMap[value] ?? { text: value || '-', type: 'default' as const };
  return h(
    NTag,
    { size: 'small', round: true, bordered: false, type: resolved.type },
    { default: () => resolved.text },
  );
}

const columns = computed<DataTableColumns<MerchantDomain>>(() => {
  const pendingSet = actions.pending.value;
  return [
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 280,
      ellipsis: true,
      render: (row) => h(NText, { code: true }, { default: () => row.merchant_id || '-' }),
    },
    {
      title: '支付域名',
      key: 'domain',
      minWidth: 240,
      ellipsis: true,
      render: (row) => h(NText, { code: true }, { default: () => row.domain || '-' }),
    },
    { title: '状态', key: 'status', width: 110, render: (row) => statusTagOf(row.status) },
    {
      title: '驳回原因',
      key: 'rejection_reason',
      minWidth: 180,
      ellipsis: true,
      render: (row) => row.rejection_reason || '-',
    },
    {
      title: '提交时间',
      key: 'created_at',
      width: 180,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 190,
      fixed: 'right',
      render: (row) => {
        if (row.status !== 'pending') {
          return h(NText, { depth: 3 }, { default: () => '已处理' });
        }
        const approving = pendingSet.has(`approve:${row.id}`);
        return h(NSpace, { size: 4, wrap: false }, {
          default: () => [
            h(
              NPopconfirm,
              { onPositiveClick: () => approve(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      size: 'tiny',
                      quaternary: true,
                      type: 'primary',
                      loading: approving,
                      disabled: approving,
                    },
                    { icon: renderIcon(CheckmarkOutline), default: () => '通过' },
                  ),
                default: () => '确认通过该支付域名？通过后下单可校验该域名。',
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
              { icon: renderIcon(CloseOutline), default: () => '驳回' },
            ),
          ],
        });
      },
    },
  ];
});

onMounted(load);
</script>

<template>
  <admin-page title="域名审核" subtitle="商户提交的支付域名白名单审核，启用域名校验后生效">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        刷新
      </n-button>
    </template>

    <n-alert v-if="unavailable" type="warning" :bordered="false" :show-icon="true" style="margin-bottom: 16px">
      域名审核接口未启用：后端 <n-text code>/admin/domains</n-text> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
        <n-gi>
          <n-select
            v-model:value="status"
            :options="statusOptions"
            placeholder="全部状态"
            clearable
            style="width: 100%"
          />
        </n-gi>
        <n-gi>
          <n-space align="center" :wrap="true">
            <n-button type="primary" @click="search">
              <template #icon><n-icon><SearchOutline /></n-icon></template>
              查询
            </n-button>
            <n-button @click="resetFilters">
              <template #icon><n-icon><ReloadOutline /></n-icon></template>
              重置
            </n-button>
          </n-space>
        </n-gi>
        <n-gi>
          <n-text depth="3" style="font-size: 12px">共 {{ total }} 条</n-text>
        </n-gi>
      </n-grid>

      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: MerchantDomain) => row.id"
        :bordered="false"
        :single-line="false"
        :scroll-x="1200"
        :size="isNarrow ? 'small' : 'medium'"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty :description="unavailable ? '接口未启用' : '暂无域名申请'" size="small">
            <template v-if="unavailable" #extra>
              <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
            </template>
          </n-empty>
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showReject"
      preset="card"
      title="驳回支付域名"
      style="width: 560px; max-width: 94vw"
    >
      <n-descriptions v-if="rejectTarget" :column="isNarrow ? 1 : 2" label-placement="left" bordered size="small">
        <n-descriptions-item label="域名">
          <n-text code>{{ rejectTarget.domain }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="商户 ID">
          <n-text code>{{ rejectTarget.merchant_id || '-' }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="当前状态">
          <n-tag
            size="small"
            round
            :bordered="false"
            :type="(domainStatusMap[rejectTarget.status] ?? { type: 'default' }).type"
          >
            {{ (domainStatusMap[rejectTarget.status] ?? { text: rejectTarget.status || '-' }).text }}
          </n-tag>
        </n-descriptions-item>
        <n-descriptions-item label="提交时间">
          {{ formatDateTime(rejectTarget.created_at) }}
        </n-descriptions-item>
      </n-descriptions>

      <n-form
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 180"
        style="margin-top: 16px"
      >
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
        <n-space justify="end" :wrap="true">
          <n-button @click="showReject = false">取消</n-button>
          <n-button type="error" :loading="processing" @click="submitReject">
            <template #icon><n-icon><CloseOutline /></n-icon></template>
            确认驳回
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
