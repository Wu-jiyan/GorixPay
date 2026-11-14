<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { adminApi } from '@/api';
import { adminLedgerApi } from '@/api/adminLedger';
import type { LedgerEntryRow } from '@/api/adminLedger';
import { exportCsvUrl } from '@/api/reports';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { copyText, formatDateTime } from '@/utils/format';
import {
  asRow,
  fieldText,
  itemsOf,
  normalizeLedgerAccount,
  numberOr,
  previewJson,
  prettyJson,
  textOf,
  usePendingAction,
  useServerPaging,
} from './shared';
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

const pending = usePendingAction();

const showAccount = ref(false);
const accountId = ref('');
const accountDetail = ref<Row | null>(null);
const accountEntries = ref<LedgerEntryRow[]>([]);
const accountLoading = ref(false);
const accountEntriesLoading = ref(false);

const showTransaction = ref(false);
const transactionDetail = ref<Row | null>(null);
const transactionEntries = ref<LedgerEntryRow[]>([]);
const transactionLoading = ref(false);

const showOperation = ref(false);
const operationDetail = ref<Row | null>(null);
const operationLoading = ref(false);
const operationReason = ref('');

const accountEntriesPaging = useServerPaging(loadAccountEntries);

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

function exportLedger() {
  window.open(exportCsvUrl('ledger'), '_blank');
}

async function loadAccountDetail() {
  if (accountId.value === '') return;
  accountLoading.value = true;
  try {
    accountDetail.value = asRow(await adminLedgerApi.account(accountId.value));
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    accountLoading.value = false;
  }
}

async function loadAccountEntries() {
  if (accountId.value === '') return;
  accountEntriesLoading.value = true;
  try {
    const data = await adminLedgerApi.entries(accountId.value, {
      page: accountEntriesPaging.page.value,
      page_size: accountEntriesPaging.pageSize.value,
    });
    const items = itemsOf<LedgerEntryRow>(data);
    accountEntries.value = items;
    accountEntriesPaging.total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    accountEntriesLoading.value = false;
  }
}

function openAccount(row: LedgerAccountRow) {
  accountId.value = row.id;
  accountDetail.value = null;
  accountEntries.value = [];
  accountEntriesPaging.resetPage();
  showAccount.value = true;
  void loadAccountDetail();
  void loadAccountEntries();
}

async function openTransaction(row: Row) {
  const id = textOf(row.id, '');
  showTransaction.value = true;
  transactionDetail.value = row;
  transactionEntries.value = [];
  if (id === '') return;
  transactionLoading.value = true;
  try {
    const data = await adminLedgerApi.transaction(id);
    const detail = asRow(data);
    if (Object.keys(asRow(detail.transaction)).length > 0) transactionDetail.value = asRow(detail.transaction);
    transactionEntries.value = itemsOf<LedgerEntryRow>(detail.entries);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    transactionLoading.value = false;
  }
}

async function openOperation(row: Row) {
  const id = textOf(row.id, '');
  showOperation.value = true;
  operationDetail.value = row;
  operationReason.value = '';
  if (id === '') return;
  operationLoading.value = true;
  try {
    operationDetail.value = asRow(await adminLedgerApi.operation(id));
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    operationLoading.value = false;
  }
}

async function processOperation(action: 'approve' | 'reject') {
  const id = textOf(asRow(operationDetail.value).id, '');
  if (id === '') return;
  if (action === 'reject' && operationReason.value.trim() === '') {
    message.warning('请填写驳回原因');
    return;
  }
  await pending.run(`operation:${id}`, async () => {
    try {
      const updated = await adminLedgerApi.processOperation(id, action, operationReason.value.trim());
      operationDetail.value = asRow(updated);
      operationReason.value = '';
      message.success(action === 'approve' ? '操作已通过' : '操作已驳回');
      await loadOperations();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
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
  {
    title: '操作',
    key: 'actions',
    width: 100,
    fixed: 'right',
    render: (row) =>
      h(NButton, { size: 'tiny', quaternary: true, type: 'info', onClick: () => openAccount(row) }, {
        default: () => '分录',
      }),
  },
];

const entryColumns: DataTableColumns<LedgerEntryRow> = [
  { title: '序号', key: 'sequence', width: 70, align: 'right', render: (row) => row.sequence ?? '-' },
  { title: '方向', key: 'direction', width: 80, render: (row) => row.direction || '-' },
  { title: '桶', key: 'bucket', width: 90, render: (row) => row.bucket || '-' },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: typeof row.amount === 'number' ? row.amount : null }),
  },
  {
    title: '分录后余额',
    key: 'balance_after',
    width: 140,
    align: 'right',
    render: (row) => h(MoneyText, { amount: typeof row.balance_after === 'number' ? row.balance_after : null }),
  },
  {
    title: '账户 ID',
    key: 'account_id',
    minWidth: 220,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.account_id || '-'),
  },
  {
    title: '时间',
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
    width: 110,
    fixed: 'right',
    render: (row) =>
      h(NButton, { size: 'tiny', quaternary: true, type: 'info', onClick: () => openOperation(row) }, {
        default: () => '审批',
      }),
  },
];

function rowProps(row: Row) {
  return { style: 'cursor: pointer', onClick: () => openTransaction(row) };
}

function showRawTransaction() {
  openDetail(asRow(transactionDetail));
}

onMounted(() => {
  void loadAccounts();
});
</script>

<template>
  <div class="page">
    <PageHeader title="资金账本" subtitle="复式记账账户、交易流水与人工资金操作">
      <template #actions>
        <n-button secondary :loading="accountsLoading || transactionsLoading || operationsLoading" @click="handleTab(tab)">
          刷新当前页签
        </n-button>
        <n-button secondary @click="exportLedger">导出 CSV</n-button>
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

    <n-drawer v-model:show="showAccount" :width="900" placement="right">
      <n-drawer-content :title="`账户 ${accountId}`" closable>
        <n-spin :show="accountLoading">
          <n-empty v-if="!accountDetail && !accountLoading" description="暂无账户数据" />
          <template v-else>
            <n-descriptions :column="2" label-placement="left" size="small" bordered>
              <n-descriptions-item label="账户 ID">
                <span class="mono">{{ accountId }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="状态">
                {{ fieldText(asRow(accountDetail).account, 'status') }}
              </n-descriptions-item>
              <n-descriptions-item label="归属">
                {{ fieldText(asRow(accountDetail).account, 'owner_type') }} /
                <span class="mono">{{ fieldText(asRow(accountDetail).account, 'owner_id') }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="用途">{{ fieldText(asRow(accountDetail).account, 'purpose') }}</n-descriptions-item>
              <n-descriptions-item label="可用余额">
                <MoneyText :amount="numberOr(asRow(asRow(accountDetail).balance).available, 0)" />
              </n-descriptions-item>
              <n-descriptions-item label="冻结金额">
                <MoneyText :amount="numberOr(asRow(asRow(accountDetail).balance).frozen, 0)" color="#b45309" />
              </n-descriptions-item>
            </n-descriptions>
            <div class="drawer-toolbar">
              <span class="muted small">分录（{{ accountEntriesPaging.total.value }} 条）</span>
              <n-button size="small" @click="loadAccountEntries">刷新分录</n-button>
            </div>
            <n-data-table
              :columns="entryColumns"
              :data="accountEntries"
              :loading="accountEntriesLoading"
              :pagination="accountEntriesPaging.pagination.value"
              :row-key="(row: LedgerEntryRow) => String(row.id ?? row.sequence ?? '')"
              :bordered="false"
              :single-line="false"
              size="small"
              remote
              @update:page="accountEntriesPaging.onPage"
              @update:page-size="accountEntriesPaging.onPageSize"
            >
              <template #empty>
                <n-empty description="该账户还没有分录" size="small" />
              </template>
            </n-data-table>
          </template>
        </n-spin>
      </n-drawer-content>
    </n-drawer>

    <n-drawer v-model:show="showTransaction" :width="900" placement="right">
      <n-drawer-content title="交易明细" closable>
        <n-spin :show="transactionLoading">
          <n-descriptions :column="2" label-placement="left" size="small" bordered>
            <n-descriptions-item label="交易 ID">
              <span class="mono">{{ fieldText(transactionDetail, 'id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="幂等键">
              <span class="mono">{{ fieldText(transactionDetail, 'idempotency_key') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="关联">
              {{ fieldText(transactionDetail, 'reference_type') }} /
              <span class="mono">{{ fieldText(transactionDetail, 'reference_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="摘要">{{ fieldText(transactionDetail, 'description') }}</n-descriptions-item>
          </n-descriptions>
          <div class="drawer-toolbar">
            <span class="muted small">分录（{{ transactionEntries.length }} 条）</span>
            <n-button size="small" @click="showRawTransaction">查看原始 JSON</n-button>
          </div>
          <n-data-table
            :columns="entryColumns"
            :data="transactionEntries"
            :bordered="false"
            :single-line="false"
            size="small"
            :row-key="(row: LedgerEntryRow) => String(row.id ?? row.sequence ?? '')"
          >
            <template #empty>
              <n-empty description="这笔交易没有分录" size="small" />
            </template>
          </n-data-table>
        </n-spin>
      </n-drawer-content>
    </n-drawer>

    <n-drawer v-model:show="showOperation" :width="640" placement="right">
      <n-drawer-content title="资金操作审批" closable>
        <n-spin :show="operationLoading">
          <n-descriptions :column="1" label-placement="left" size="small" bordered>
            <n-descriptions-item label="操作 ID">
              <span class="mono">{{ fieldText(operationDetail, 'id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="类型">{{ fieldText(operationDetail, 'operation_type') }}</n-descriptions-item>
            <n-descriptions-item label="状态">
              {{ fieldText(operationDetail, 'status') }}
            </n-descriptions-item>
            <n-descriptions-item label="金额">
              <MoneyText :amount="numberOr(asRow(operationDetail).amount, 0)" />
            </n-descriptions-item>
            <n-descriptions-item label="账户">
              <span class="mono">{{ fieldText(operationDetail, 'account_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="对手账户">
              <span class="mono">{{ fieldText(operationDetail, 'counterparty_account_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="关联单号">
              <span class="mono">{{ fieldText(operationDetail, 'reference_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="摘要">{{ fieldText(operationDetail, 'description') }}</n-descriptions-item>
            <n-descriptions-item label="驳回原因">
              {{ fieldText(operationDetail, 'rejection_reason') }}
            </n-descriptions-item>
          </n-descriptions>
          <template v-if="fieldText(operationDetail, 'status') === 'pending'">
            <n-divider title-placement="left">审批</n-divider>
            <n-form-item label="驳回原因">
              <n-input v-model:value="operationReason" placeholder="驳回时必填，通过可留空" />
            </n-form-item>
            <n-space justify="end">
              <n-button
                type="error"
                secondary
                :loading="pending.pending.value.has(`operation:${fieldText(operationDetail, 'id')}`)"
                :disabled="pending.pending.value.has(`operation:${fieldText(operationDetail, 'id')}`)"
                @click="processOperation('reject')"
              >
                驳回
              </n-button>
              <n-popconfirm @positive-click="processOperation('approve')">
                <template #trigger>
                  <n-button
                    type="primary"
                    :loading="pending.pending.value.has(`operation:${fieldText(operationDetail, 'id')}`)"
                    :disabled="pending.pending.value.has(`operation:${fieldText(operationDetail, 'id')}`)"
                  >
                    通过
                  </n-button>
                </template>
                确认通过该资金操作？会产生账本分录。
              </n-popconfirm>
            </n-space>
          </template>
          <n-divider title-placement="left">原始数据</n-divider>
          <pre class="json">{{ prettyJson(operationDetail) }}</pre>
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>

<style scoped>
.tip {
  margin: 10px 0 0;
  font-size: 12px;
}
.drawer-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 14px 0 10px;
}
.small {
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
