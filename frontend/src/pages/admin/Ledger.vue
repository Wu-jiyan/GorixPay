<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { adminApi } from '@/api';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { copyText, formatDateTime } from '@/utils/format';
import { normalizeLedgerAccount, previewJson, prettyJson, textOf, useServerPaging } from './shared';
import type { LedgerAccountRow } from './shared';

type Row = Record<string, unknown>;

const message = useMessage();

const tab = ref('accounts');

const accounts = ref<LedgerAccountRow[]>([]);
const transactions = ref<Row[]>([]);
const operations = ref<Row[]>([]);

const accountsLoading = ref(false);
const transactionsLoading = ref(false);
const operationsLoading = ref(false);

const showDetail = ref(false);
const detailRow = ref<Row | null>(null);

const {
  page: accountsPage,
  pageSize: accountsPageSize,
  total: accountsTotal,
  pagination: accountsPagination,
  onPage: onAccountsPage,
  onPageSize: onAccountsPageSize,
} = useServerPaging(loadAccounts);

const {
  page: transactionsPage,
  pageSize: transactionsPageSize,
  total: transactionsTotal,
  pagination: transactionsPagination,
  onPage: onTransactionsPage,
  onPageSize: onTransactionsPageSize,
} = useServerPaging(loadTransactions);

const {
  page: operationsPage,
  pageSize: operationsPageSize,
  total: operationsTotal,
  pagination: operationsPagination,
  onPage: onOperationsPage,
  onPageSize: onOperationsPageSize,
} = useServerPaging(loadOperations);

const columnLabels: Record<string, string> = {
  id: '交易 ID',
  idempotency_key: '幂等键',
  reference_type: '关联类型',
  reference_id: '关联单号',
  description: '摘要',
  metadata: '元数据',
  created_at: '创建时间',
  updated_at: '更新时间',
};

async function loadAccounts() {
  accountsLoading.value = true;
  try {
    const data = await adminApi.ledgerAccounts({
      page: accountsPage.value,
      page_size: accountsPageSize.value,
    });
    accounts.value = data.items.map((item) => normalizeLedgerAccount(item));
    accountsTotal.value = data.total;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    accountsLoading.value = false;
  }
}

async function loadTransactions() {
  transactionsLoading.value = true;
  try {
    const data = await adminApi.ledgerTransactions({
      page: transactionsPage.value,
      page_size: transactionsPageSize.value,
    });
    transactions.value = data.items;
    transactionsTotal.value = data.total;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    transactionsLoading.value = false;
  }
}

async function loadOperations() {
  operationsLoading.value = true;
  try {
    const data = await adminApi.ledgerOperations({
      page: operationsPage.value,
      page_size: operationsPageSize.value,
    });
    operations.value = data.items;
    operationsTotal.value = data.total;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    operationsLoading.value = false;
  }
}

function handleTab(value: string) {
  tab.value = value;
  if (value === 'accounts') void loadAccounts();
  else if (value === 'transactions') void loadTransactions();
  else void loadOperations();
}

function openDetail(row: Row) {
  detailRow.value = row;
  showDetail.value = true;
}

async function copyDetail() {
  const ok = await copyText(prettyJson(detailRow.value));
  if (ok) message.success('已复制到剪贴板');
  else message.warning('复制失败，请手动选择文本');
}

function cellOf(key: string, value: unknown) {
  if (key === 'created_at' || key === 'updated_at' || key === 'processed_at' || key === 'available_at') {
    return formatDateTime(typeof value === 'string' ? value : null);
  }
  if (typeof value === 'object' && value !== null) {
    return h('span', { class: 'muted' }, previewJson(value, 60));
  }
  const text = textOf(value);
  if (key === 'id' || key === 'idempotency_key' || key === 'reference_id' || key === 'account_id') {
    return h('span', { class: 'mono' }, previewJson(text, 40));
  }
  if (text.length > 60) {
    return h('span', { title: text }, previewJson(text, 60));
  }
  return text;
}

const transactionKeys = computed<string[]>(() => {
  const keys: string[] = [];
  transactions.value.forEach((row) => {
    Object.keys(row).forEach((key) => {
      if (!keys.includes(key)) keys.push(key);
    });
  });
  return keys;
});

const accountColumns: DataTableColumns<LedgerAccountRow> = [
  {
    title: '账户 ID',
    key: 'id',
    width: 280,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.id || '-'),
  },
  { title: '归属类型', key: 'owner_type', width: 100, render: (row) => row.owner_type || '-' },
  {
    title: '归属 ID',
    key: 'owner_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.owner_id || '-'),
  },
  { title: '用途', key: 'purpose', width: 110, render: (row) => row.purpose || '-' },
  { title: '币种', key: 'currency', width: 80, render: (row) => row.currency || '-' },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
  },
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
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

const transactionColumns = computed<DataTableColumns<Row>>(() =>
  transactionKeys.value.map((key) => ({
    title: columnLabels[key] ?? key,
    key,
    minWidth: key === 'metadata' ? 240 : 150,
    ellipsis: key !== 'metadata',
    render: (row: Row) => cellOf(key, row[key]),
  })),
);

const operationColumns: DataTableColumns<Row> = [
  {
    title: '操作 ID',
    key: 'id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, textOf(row.id)),
  },
  {
    title: '操作类型',
    key: 'operation_type',
    width: 110,
    render: (row) => textOf(row.operation_type),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => h(StatusTag, { status: textOf(row.status, 'pending'), kind: 'order' }),
  },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: typeof row.amount === 'number' ? row.amount : null }),
  },
  {
    title: '账户 ID',
    key: 'account_id',
    width: 240,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, textOf(row.account_id)),
  },
  {
    title: '关联单号',
    key: 'reference_id',
    width: 200,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, textOf(row.reference_id)),
  },
  { title: '摘要', key: 'description', minWidth: 160, ellipsis: true, render: (row) => textOf(row.description) },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(typeof row.created_at === 'string' ? row.created_at : null),
  },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right',
    render: (row) =>
      h(NButton, { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) }, {
        default: () => '详情',
      }),
  },
];

function rowProps(row: Row) {
  return { style: 'cursor: pointer', onClick: () => openDetail(row) };
}

onMounted(() => {
  void loadAccounts();
});
</script>

<template>
  <div class="page">
    <PageHeader title="资金账本" subtitle="复式记账账户、交易流水与人工资金操作">
      <template #actions>
        <n-button
          secondary
          :loading="accountsLoading || transactionsLoading || operationsLoading"
          @click="handleTab(tab)"
        >
          刷新当前页签
        </n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="accounts" tab="账户">
          <n-data-table
            :columns="accountColumns"
            :data="accounts"
            :loading="accountsLoading"
            :pagination="accountsPagination"
            :row-key="(row: LedgerAccountRow) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
            remote
            @update:page="onAccountsPage"
            @update:page-size="onAccountsPageSize"
          >
            <template #empty>
              <n-empty description="暂无账本账户" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="transactions" tab="流水">
          <n-data-table
            :columns="transactionColumns"
            :data="transactions"
            :loading="transactionsLoading"
            :pagination="transactionsPagination"
            :row-key="(row: Row) => String(row.id ?? '')"
            :row-props="rowProps"
            :bordered="false"
            :single-line="false"
            size="small"
            remote
            @update:page="onTransactionsPage"
            @update:page-size="onTransactionsPageSize"
          >
            <template #empty>
              <n-empty description="暂无账本流水" size="small" />
            </template>
          </n-data-table>
          <p class="muted tip">点击任意流水行查看完整 JSON。</p>
        </n-tab-pane>

        <n-tab-pane name="operations" tab="操作">
          <n-data-table
            :columns="operationColumns"
            :data="operations"
            :loading="operationsLoading"
            :pagination="operationsPagination"
            :row-key="(row: Row) => String(row.id ?? '')"
            :bordered="false"
            :single-line="false"
            size="small"
            remote
            @update:page="onOperationsPage"
            @update:page-size="onOperationsPageSize"
          >
            <template #empty>
              <n-empty description="暂无资金操作" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <n-modal v-model:show="showDetail" preset="card" title="明细" style="width: 760px; max-width: 94vw">
      <pre class="json">{{ prettyJson(detailRow) }}</pre>
      <template #footer>
        <n-space justify="end">
          <n-button @click="copyDetail">复制 JSON</n-button>
          <n-button type="primary" @click="showDetail = false">关闭</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.tip {
  margin: 10px 0 0;
  font-size: 12px;
}
.json {
  margin: 0;
  max-height: 60vh;
  overflow: auto;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 10px;
  padding: 14px 16px;
  font-size: 12px;
  line-height: 1.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
