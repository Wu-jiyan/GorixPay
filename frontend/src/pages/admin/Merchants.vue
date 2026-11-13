<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { Credential, LedgerAccount, Merchant } from '@/api';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import MoneyText from '@/components/MoneyText.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { normalizeLedgerAccount, useServerPaging } from './shared';
import type { LedgerAccountRow } from './shared';

type MerchantDetail = Merchant & { credentials?: Credential[]; accounts?: LedgerAccount[] };

const message = useMessage();

const rows = ref<Merchant[]>([]);
const loading = ref(false);
const keyword = ref('');
const status = ref<string | null>(null);

const showDetail = ref(false);
const detail = ref<MerchantDetail | null>(null);
const detailLoading = ref(false);

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '全部状态', value: '' },
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'disabled' },
];

function statusParam(): string | undefined {
  return status.value ? status.value : undefined;
}

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.merchants({
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value.trim() || undefined,
      status: statusParam(),
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
  keyword.value = '';
  status.value = null;
  resetPage();
  void load();
}

async function toggleStatus(row: Merchant) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  try {
    await adminApi.setMerchantStatus(row.id, next);
    message.success(next === 'active' ? '商户已启用' : '商户已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

async function openDetail(row: Merchant) {
  showDetail.value = true;
  detail.value = null;
  detailLoading.value = true;
  try {
    detail.value = await adminApi.merchant(row.id);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    detailLoading.value = false;
  }
}

const detailAccounts = computed<LedgerAccountRow[]>(() =>
  (detail.value?.accounts ?? []).map((item) => normalizeLedgerAccount(item)),
);

const columns = computed<DataTableColumns<Merchant>>(() => [
  {
    title: '商户 ID',
    key: 'id',
    width: 280,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.id),
  },
  {
    title: '商户名称',
    key: 'display_name',
    minWidth: 140,
    ellipsis: true,
    render: (row) => row.display_name || '-',
  },
  {
    title: '邮箱',
    key: 'email',
    minWidth: 160,
    ellipsis: true,
    render: (row) => row.email || '-',
  },
  {
    title: '手机号',
    key: 'phone',
    width: 130,
    render: (row) => row.phone || '-',
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
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
    render: (row) =>
      h(
        NSpace,
        { size: 4, wrap: false },
        {
          default: () => [
            h(
              NPopconfirm,
              { onPositiveClick: () => toggleStatus(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'tiny', quaternary: true, type: row.status === 'active' ? 'warning' : 'primary' },
                    { default: () => (row.status === 'active' ? '禁用' : '启用') },
                  ),
                default: () => (row.status === 'active' ? '确认禁用该商户？' : '确认启用该商户？'),
              },
            ),
            h(
              NButton,
              { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) },
              { default: () => '详情' },
            ),
          ],
        },
      ),
  },
]);

const credentialColumns: DataTableColumns<Credential> = [
  {
    title: '类型',
    key: 'credential_type',
    width: 90,
    render: (row) => (row.credential_type === 'rsa' ? 'V2 RSA' : 'V1 MD5'),
  },
  {
    title: 'Key ID',
    key: 'key_id',
    minWidth: 140,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.key_id || '-'),
  },
  { title: '状态', key: 'status', width: 90 },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

const accountColumns: DataTableColumns<LedgerAccountRow> = [
  { title: '账户 ID', key: 'id', minWidth: 200, ellipsis: true, render: (row) => h('span', { class: 'mono' }, row.id) },
  { title: '归属类型', key: 'owner_type', width: 100, render: (row) => row.owner_type || '-' },
  { title: '用途', key: 'purpose', width: 110, render: (row) => row.purpose || '-' },
  { title: '币种', key: 'currency', width: 80, render: (row) => row.currency || '-' },
  { title: '状态', key: 'status', width: 90, render: (row) => h(StatusTag, { status: row.status, kind: 'active' }) },
  {
    title: '可用余额',
    key: 'available',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.available }),
  },
  {
    title: '冻结金额',
    key: 'frozen',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.frozen, color: '#b45309' }),
  },
];

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="商户管理" subtitle="查看商户资料、密钥与账本余额，控制商户启用状态">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <n-input
        v-model:value="keyword"
        placeholder="搜索名称 / 邮箱 / 手机号"
        clearable
        style="width: 260px"
        @keyup.enter="search"
      />
      <n-select
        v-model:value="status"
        :options="statusOptions"
        placeholder="全部状态"
        clearable
        style="width: 160px"
      />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
    </div>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Merchant) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-drawer v-model:show="showDetail" :width="620" placement="right">
      <n-drawer-content :title="detail?.display_name ?? '商户详情'" closable>
        <n-spin :show="detailLoading">
          <n-empty v-if="!detail && !detailLoading" description="暂无商户数据" />
          <template v-else-if="detail">
            <n-descriptions :column="1" label-placement="left" bordered size="small">
              <n-descriptions-item label="商户 ID">
                <span class="mono">{{ detail.id }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="商户名称">{{ detail.display_name || '-' }}</n-descriptions-item>
              <n-descriptions-item label="邮箱">{{ detail.email || '-' }}</n-descriptions-item>
              <n-descriptions-item label="手机号">{{ detail.phone || '-' }}</n-descriptions-item>
              <n-descriptions-item label="状态">
                <StatusTag :status="detail.status" kind="active" />
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
            </n-descriptions>

            <n-divider>密钥（Credentials）</n-divider>
            <n-data-table
              :columns="credentialColumns"
              :data="detail.credentials ?? []"
              :bordered="false"
              :single-line="false"
              size="small"
              :row-key="(row: Credential) => row.id"
            >
              <template #empty>
                <n-empty description="暂无密钥" size="small" />
              </template>
            </n-data-table>

            <n-divider>账本账户与余额</n-divider>
            <n-data-table
              :columns="accountColumns"
              :data="detailAccounts"
              :bordered="false"
              :single-line="false"
              size="small"
              :row-key="(row: LedgerAccountRow) => row.id"
            >
              <template #empty>
                <n-empty description="暂无账本账户" size="small" />
              </template>
            </n-data-table>
          </template>
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>
