<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { adminApi } from '@/api';
import type { Credential, LedgerAccount, Merchant } from '@/api';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { activeStatus, formatDateTime, formatMoney } from '@/utils/format';
import { normalizeLedgerAccount, useNarrow, useServerPaging } from './shared';
import type { LedgerAccountRow } from './shared';

type MerchantDetail = Merchant & { credentials?: Credential[]; accounts?: LedgerAccount[] };

const message = useMessage();
const isNarrow = useNarrow();

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
    width: 260,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.id),
  },
  {
    title: '商户名称',
    key: 'display_name',
    minWidth: 150,
    ellipsis: { tooltip: true },
    render: (row) => row.display_name || '-',
  },
  {
    title: '邮箱',
    key: 'email',
    minWidth: 170,
    ellipsis: { tooltip: true },
    render: (row) => row.email || '-',
  },
  {
    title: '手机号',
    key: 'phone',
    width: 140,
    render: (row) => row.phone || '-',
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: activeStatus(row.status).type }, () =>
        activeStatus(row.status).text,
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
    width: 150,
    fixed: 'right',
    render: (row) =>
      h(NSpace, { size: 8, wrap: false }, () => [
        h(
          NPopconfirm,
          { onPositiveClick: () => toggleStatus(row) },
          {
            trigger: () =>
              h(
                NButton,
                { size: 'small', text: true, type: row.status === 'active' ? 'warning' : 'primary' },
                () => (row.status === 'active' ? '禁用' : '启用'),
              ),
            default: () => (row.status === 'active' ? '确认禁用该商户？' : '确认启用该商户？'),
          },
        ),
        h(
          NButton,
          { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) },
          () => '详情',
        ),
      ]),
  },
]);

const credentialColumns: DataTableColumns<Credential> = [
  {
    title: '类型',
    key: 'credential_type',
    width: 100,
    render: (row) => (row.credential_type === 'rsa' ? 'V2 RSA' : 'V1 MD5'),
  },
  {
    title: 'Key ID',
    key: 'key_id',
    minWidth: 150,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.key_id || '-'),
  },
  { title: '状态', key: 'status', width: 100 },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

const accountColumns: DataTableColumns<LedgerAccountRow> = [
  {
    title: '账户 ID',
    key: 'id',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.id),
  },
  { title: '归属类型', key: 'owner_type', width: 110, render: (row) => row.owner_type || '-' },
  { title: '用途', key: 'purpose', width: 110, render: (row) => row.purpose || '-' },
  { title: '币种', key: 'currency', width: 90, render: (row) => row.currency || '-' },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: activeStatus(row.status).type }, () =>
        activeStatus(row.status).text,
      ),
  },
  {
    title: '可用余额',
    key: 'available',
    width: 140,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.available)),
  },
  {
    title: '冻结金额',
    key: 'frozen',
    width: 140,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.frozen)),
  },
];

onMounted(load);
</script>

<template>
  <admin-page title="商户管理" subtitle="查看商户资料、密钥与账本余额，控制商户启用状态">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
      </n-space>
    </template>

    <n-card size="small" :bordered="false">
      <n-form :show-feedback="false" style="margin-bottom: 12px">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="关键字">
              <n-input
                v-model:value="keyword"
                placeholder="搜索名称 / 邮箱 / 手机号"
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
            <n-form-item>
              <n-space align="center" :wrap="true">
                <n-button type="primary" @click="search">
                  <template #icon>
                    <n-icon :component="SearchOutline" />
                  </template>
                  查询
                </n-button>
                <n-button @click="resetFilters">重置</n-button>
                <n-text depth="3" style="font-size: 12px">共 {{ total }} 条</n-text>
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
        :row-key="(row: Merchant) => row.id"
        :scroll-x="1200"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-drawer v-model:show="showDetail" :width="isNarrow ? '92vw' : 720" placement="right">
      <n-drawer-content :title="detail?.display_name ?? '商户详情'" closable>
        <n-spin :show="detailLoading">
          <n-empty v-if="!detail && !detailLoading" description="暂无商户数据" />
          <template v-else-if="detail">
            <n-descriptions :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left">
              <n-descriptions-item label="商户 ID">
                <n-text class="mono">{{ detail.id }}</n-text>
              </n-descriptions-item>
              <n-descriptions-item label="商户名称">{{ detail.display_name || '-' }}</n-descriptions-item>
              <n-descriptions-item label="邮箱">{{ detail.email || '-' }}</n-descriptions-item>
              <n-descriptions-item label="手机号">{{ detail.phone || '-' }}</n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag :type="activeStatus(detail.status).type" size="small" :bordered="false">
                  {{ activeStatus(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
            </n-descriptions>

            <n-divider title-placement="left">密钥（Credentials）</n-divider>
            <n-data-table
              :columns="credentialColumns"
              :data="detail.credentials ?? []"
              :row-key="(row: Credential) => row.id"
              :scroll-x="700"
            />

            <n-divider title-placement="left">账本账户与余额</n-divider>
            <n-data-table
              :columns="accountColumns"
              :data="detailAccounts"
              :row-key="(row: LedgerAccountRow) => row.id"
              :scroll-x="900"
            />
          </template>
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>
