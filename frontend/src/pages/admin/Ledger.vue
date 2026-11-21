<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NCollapseItem, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  CashOutline,
  CheckmarkCircleOutline,
  CloseOutline,
  CopyOutline,
  DownloadOutline,
  ListOutline,
  RefreshOutline,
  SwapHorizontalOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import { adminLedgerApi } from '@/api/adminLedger';
import type { LedgerEntryRow } from '@/api/adminLedger';
import { exportCsvUrl } from '@/api/reports';
import { errorMessage } from '@/api/client';
import { activeStatus, copyText, formatDateTime, formatMoney, orderStatus } from '@/utils/format';
import {
  asRow,
  fieldDateTime,
  fieldText,
  itemsOf,
  normalizeLedgerAccount,
  numberOr,
  pick,
  previewJson,
  prettyJson,
  textOf,
  usePendingAction,
  useNarrow,
  useServerPaging,
} from './shared';
import type { LedgerAccountRow } from './shared';

type Row = Record<string, unknown>;

const message = useMessage();
const isNarrow = useNarrow();

const tab = ref('accounts');

const accounts = ref<LedgerAccountRow[]>([]);
const transactions = ref<Row[]>([]);
const operations = ref<Row[]>([]);

const accountsLoading = ref(false);
const transactionsLoading = ref(false);
const operationsLoading = ref(false);

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
const entryPagination = accountEntriesPaging.pagination;
const entryTotal = accountEntriesPaging.total;

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

const timeKeys = ['created_at', 'updated_at', 'processed_at', 'available_at'];

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

function reloadCurrent() {
  handleTab(tab.value);
}

function exportLedger() {
  window.open(exportCsvUrl('ledger'), '_blank');
}

async function copyJson(value: unknown) {
  const ok = await copyText(prettyJson(value));
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
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

const accountStatus = computed(() => activeStatus(fieldText(asRow(accountDetail.value).account, 'status')));

const operationId = computed(() => textOf(asRow(operationDetail.value).id, ''));
const operationPending = computed(() => fieldText(operationDetail.value, 'status') === 'pending');
const operationBusy = computed(() => pending.pending.value.has(`operation:${operationId.value}`));

interface TimelineNode {
  key: string;
  title: string;
  time: string;
  content: string;
  type: 'default' | 'info' | 'success' | 'warning' | 'error';
}

const operationTimeline = computed<TimelineNode[]>(() => {
  const row = asRow(operationDetail.value);
  const rejected = textOf(pick(row, 'rejection_reason'), '');
  const nodes: TimelineNode[] = [
    {
      key: 'created',
      title: '资金操作提交',
      time: fieldDateTime(row, 'created_at'),
      content: `发起人 ${fieldText(row, 'created_by')}`,
      type: 'info',
    },
  ];
  if (pick(row, 'processed_at') !== undefined) {
    nodes.push({
      key: 'processed',
      title: rejected === '' ? '审批通过' : '审批驳回',
      time: fieldDateTime(row, 'processed_at'),
      content: rejected === '' ? `处理人 ${fieldText(row, 'processed_by')}` : `驳回原因：${rejected}`,
      type: rejected === '' ? 'success' : 'error',
    });
  } else {
    nodes.push({ key: 'waiting', title: '等待审批', time: '-', content: '尚未产生处理记录', type: 'warning' });
  }
  return nodes;
});

function statusTag(status: string, kind: 'active' | 'order') {
  const resolved = kind === 'active' ? activeStatus(status) : orderStatus(status);
  return h(NTag, { size: 'small', round: true, bordered: false, type: resolved.type }, {
    default: () => resolved.text,
  });
}

function moneyNode(amount: number | null, type?: 'default' | 'success' | 'warning' | 'error') {
  return h(NText, { class: 'amount', type }, { default: () => formatMoney(amount) });
}

function monoNode(value: string) {
  return h('span', { class: 'mono' }, value || '-');
}

function cellOf(key: string, value: unknown) {
  if (timeKeys.includes(key)) {
    return formatDateTime(typeof value === 'string' ? value : null);
  }
  if (typeof value === 'object' && value !== null) {
    return previewJson(value, 60);
  }
  return textOf(value);
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
    width: 260,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(row.id),
  },
  { title: '归属类型', key: 'owner_type', width: 100, render: (row) => row.owner_type || '-' },
  {
    title: '归属 ID',
    key: 'owner_id',
    width: 240,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(row.owner_id),
  },
  { title: '用途', key: 'purpose', width: 110, render: (row) => row.purpose || '-' },
  { title: '币种', key: 'currency', width: 80, render: (row) => row.currency || '-' },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => statusTag(row.status, 'active'),
  },
  {
    title: '可用余额',
    key: 'available',
    width: 140,
    align: 'right',
    render: (row) => moneyNode(row.available),
  },
  {
    title: '冻结金额',
    key: 'frozen',
    width: 140,
    align: 'right',
    render: (row) => moneyNode(row.frozen, 'warning'),
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
    width: 90,
    fixed: 'right',
    render: (row) =>
      h(NButton, { text: true, size: 'small', type: 'primary', onClick: () => openAccount(row) }, {
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
    render: (row) => moneyNode(typeof row.amount === 'number' ? row.amount : null),
  },
  {
    title: '分录后余额',
    key: 'balance_after',
    width: 140,
    align: 'right',
    render: (row) => moneyNode(typeof row.balance_after === 'number' ? row.balance_after : null),
  },
  {
    title: '账户 ID',
    key: 'account_id',
    width: 220,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(row.account_id ?? ''),
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
    minWidth: key === 'metadata' ? 240 : 160,
    ellipsis: key === 'metadata' ? false : { tooltip: true },
    render: (row: Row) => cellOf(key, row[key]),
  })),
);

const operationColumns: DataTableColumns<Row> = [
  {
    title: '操作 ID',
    key: 'id',
    width: 250,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(textOf(row.id, '')),
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
    render: (row) => statusTag(textOf(row.status, 'pending'), 'order'),
  },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => moneyNode(typeof row.amount === 'number' ? row.amount : null),
  },
  {
    title: '账户 ID',
    key: 'account_id',
    width: 240,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(textOf(row.account_id, '')),
  },
  {
    title: '关联单号',
    key: 'reference_id',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(textOf(row.reference_id, '')),
  },
  {
    title: '摘要',
    key: 'description',
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) => textOf(row.description),
  },
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
      h(NButton, { text: true, size: 'small', type: 'primary', onClick: () => openOperation(row) }, {
        default: () => '审批',
      }),
  },
];

function rowProps(row: Row) {
  return { style: 'cursor: pointer', onClick: () => openTransaction(row) };
}

onMounted(() => {
  void loadAccounts();
});
</script>

<template>
  <admin-page title="资金账本" subtitle="复式记账账户、交易流水与人工资金操作">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="accountsLoading || transactionsLoading || operationsLoading" @click="reloadCurrent">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新当前页签
        </n-button>
        <n-button @click="exportLedger">
          <template #icon>
            <n-icon :component="DownloadOutline" />
          </template>
          导出 CSV
        </n-button>
      </n-space>
    </template>

    <n-card size="small" :bordered="false">
      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="accounts">
          <template #tab>
            <n-space :size="6" align="center">
              <n-icon :component="CashOutline" />
              <span>账户</span>
            </n-space>
          </template>
          <n-data-table
            :columns="accountColumns"
            :data="accounts"
            :loading="accountsLoading"
            :pagination="accountsPagination"
            :row-key="(row: LedgerAccountRow) => row.id"
            :scroll-x="1600"
            :size="isNarrow ? 'small' : 'medium'"
            remote
            @update:page="onAccountsPage"
            @update:page-size="onAccountsPageSize"
          >
            <template #empty>
              <n-empty description="暂无账本账户" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="transactions">
          <template #tab>
            <n-space :size="6" align="center">
              <n-icon :component="SwapHorizontalOutline" />
              <span>流水</span>
            </n-space>
          </template>
          <n-data-table
            :columns="transactionColumns"
            :data="transactions"
            :loading="transactionsLoading"
            :pagination="transactionsPagination"
            :row-key="(row: Row) => String(row.id ?? '')"
            :row-props="rowProps"
            :scroll-x="1200"
            :size="isNarrow ? 'small' : 'medium'"
            remote
            @update:page="onTransactionsPage"
            @update:page-size="onTransactionsPageSize"
          >
            <template #empty>
              <n-empty description="暂无账本流水" size="small" />
            </template>
          </n-data-table>
          <n-text depth="3" style="display: block; margin-top: 10px; font-size: 12px">
            点击任意流水行，右侧查看该笔交易的分录与原始数据。
          </n-text>
        </n-tab-pane>

        <n-tab-pane name="operations">
          <template #tab>
            <n-space :size="6" align="center">
              <n-icon :component="ListOutline" />
              <span>操作</span>
            </n-space>
          </template>
          <n-data-table
            :columns="operationColumns"
            :data="operations"
            :loading="operationsLoading"
            :pagination="operationsPagination"
            :row-key="(row: Row) => String(row.id ?? '')"
            :scroll-x="1600"
            :size="isNarrow ? 'small' : 'medium'"
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

    <n-drawer v-model:show="showAccount" :width="isNarrow ? '92vw' : 640" placement="right">
      <n-drawer-content :title="`账户 ${accountId}`" closable>
        <n-spin :show="accountLoading">
          <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
            <n-descriptions-item label="账户 ID">
              <span class="mono">{{ accountId }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="状态">
              <n-tag size="small" round :bordered="false" :type="accountStatus.type">{{ accountStatus.text }}</n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="归属类型">
              {{ fieldText(asRow(accountDetail).account, 'owner_type') }}
            </n-descriptions-item>
            <n-descriptions-item label="归属 ID">
              <span class="mono">{{ fieldText(asRow(accountDetail).account, 'owner_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="用途">
              {{ fieldText(asRow(accountDetail).account, 'purpose') }}
            </n-descriptions-item>
            <n-descriptions-item label="币种">
              {{ fieldText(asRow(accountDetail).account, 'currency') }}
            </n-descriptions-item>
            <n-descriptions-item label="可用余额">
              <span class="amount">{{ formatMoney(numberOr(asRow(asRow(accountDetail).balance).available, 0)) }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="冻结金额">
              <span class="amount">{{ formatMoney(numberOr(asRow(asRow(accountDetail).balance).frozen, 0)) }}</span>
            </n-descriptions-item>
          </n-descriptions>

          <n-divider title-placement="left">
            分录（{{ entryTotal }} 条）
          </n-divider>
          <n-space justify="end" style="margin-bottom: 10px">
            <n-button size="small" :loading="accountEntriesLoading" @click="loadAccountEntries">
              <template #icon>
                <n-icon :component="RefreshOutline" />
              </template>
              刷新分录
            </n-button>
          </n-space>
          <n-data-table
            :columns="entryColumns"
            :data="accountEntries"
            :loading="accountEntriesLoading"
            :pagination="entryPagination"
            :row-key="(row: LedgerEntryRow) => String(row.id ?? row.sequence ?? '')"
            :scroll-x="900"
            :size="isNarrow ? 'small' : 'medium'"
            remote
            @update:page="accountEntriesPaging.onPage"
            @update:page-size="accountEntriesPaging.onPageSize"
          >
            <template #empty>
              <n-empty description="该账户还没有分录" size="small" />
            </template>
          </n-data-table>
        </n-spin>
      </n-drawer-content>
    </n-drawer>

    <n-drawer v-model:show="showTransaction" :width="isNarrow ? '92vw' : 640" placement="right">
      <n-drawer-content title="交易明细" closable>
        <n-spin :show="transactionLoading">
          <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
            <n-descriptions-item label="交易 ID">
              <span class="mono">{{ fieldText(transactionDetail, 'id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="幂等键">
              <span class="mono">{{ fieldText(transactionDetail, 'idempotency_key') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="关联类型">
              {{ fieldText(transactionDetail, 'reference_type') }}
            </n-descriptions-item>
            <n-descriptions-item label="关联单号">
              <span class="mono">{{ fieldText(transactionDetail, 'reference_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="摘要" :span="isNarrow ? 1 : 2">
              {{ fieldText(transactionDetail, 'description') }}
            </n-descriptions-item>
            <n-descriptions-item label="创建时间" :span="isNarrow ? 1 : 2">
              {{ formatDateTime(textOf(pick(transactionDetail, 'created_at'), '')) }}
            </n-descriptions-item>
          </n-descriptions>

          <n-divider title-placement="left">
            分录（{{ transactionEntries.length }} 条）
          </n-divider>
          <n-data-table
            :columns="entryColumns"
            :data="transactionEntries"
            :row-key="(row: LedgerEntryRow) => String(row.id ?? row.sequence ?? '')"
            :scroll-x="900"
            :pagination="false"
            :size="isNarrow ? 'small' : 'medium'"
          >
            <template #empty>
              <n-empty description="这笔交易没有分录" size="small" />
            </template>
          </n-data-table>

          <n-divider title-placement="left">
            原始数据
          </n-divider>
          <n-collapse :default-expanded-names="['raw']">
            <n-collapse-item title="原始 JSON" name="raw">
              <n-space justify="end" style="margin-bottom: 8px">
                <n-button text type="primary" size="small" @click="copyJson(transactionDetail)">
                  <template #icon>
                    <n-icon :component="CopyOutline" />
                  </template>
                  复制 JSON
                </n-button>
              </n-space>
              <n-code :code="prettyJson(transactionDetail)" language="json" word-wrap />
            </n-collapse-item>
          </n-collapse>
        </n-spin>
      </n-drawer-content>
    </n-drawer>

    <n-drawer v-model:show="showOperation" :width="isNarrow ? '92vw' : 640" placement="right">
      <n-drawer-content title="资金操作审批" closable>
        <n-spin :show="operationLoading">
          <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
            <n-descriptions-item label="操作 ID">
              <span class="mono">{{ fieldText(operationDetail, 'id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="类型">
              {{ fieldText(operationDetail, 'operation_type') }}
            </n-descriptions-item>
            <n-descriptions-item label="状态">
              <n-tag
                size="small"
                round
                :bordered="false"
                :type="orderStatus(fieldText(operationDetail, 'status')).type"
              >
                {{ orderStatus(fieldText(operationDetail, 'status')).text }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="金额">
              <span class="amount">{{ formatMoney(numberOr(asRow(operationDetail).amount, 0)) }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="账户 ID">
              <span class="mono">{{ fieldText(operationDetail, 'account_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="对手账户">
              <span class="mono">{{ fieldText(operationDetail, 'counterparty_account_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="关联单号">
              <span class="mono">{{ fieldText(operationDetail, 'reference_id') }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="摘要">{{ fieldText(operationDetail, 'description') }}</n-descriptions-item>
            <n-descriptions-item label="驳回原因" :span="isNarrow ? 1 : 2">
              {{ fieldText(operationDetail, 'rejection_reason') }}
            </n-descriptions-item>
          </n-descriptions>

          <n-divider title-placement="left">
            审批轨迹
          </n-divider>
          <n-timeline>
            <n-timeline-item
              v-for="node in operationTimeline"
              :key="node.key"
              :title="node.title"
              :content="node.content"
              :time="node.time"
              :type="node.type"
            />
          </n-timeline>

          <template v-if="operationPending">
            <n-divider title-placement="left">
              审批
            </n-divider>
            <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160" :show-feedback="false">
              <n-form-item label="驳回原因">
                <n-input v-model:value="operationReason" placeholder="驳回时必填，通过可留空" />
              </n-form-item>
            </n-form>
          </template>

          <n-divider title-placement="left">
            原始数据
          </n-divider>
          <n-collapse>
            <n-collapse-item title="原始 JSON" name="raw">
              <n-space justify="end" style="margin-bottom: 8px">
                <n-button text type="primary" size="small" @click="copyJson(operationDetail)">
                  <template #icon>
                    <n-icon :component="CopyOutline" />
                  </template>
                  复制 JSON
                </n-button>
              </n-space>
              <n-code :code="prettyJson(operationDetail)" language="json" word-wrap />
            </n-collapse-item>
          </n-collapse>
        </n-spin>

        <template #footer>
          <n-space justify="end" :wrap="true">
            <n-button @click="showOperation = false">关闭</n-button>
            <template v-if="operationPending">
              <n-button
                type="error"
                secondary
                :loading="operationBusy"
                :disabled="operationBusy"
                @click="processOperation('reject')"
              >
                <template #icon>
                  <n-icon :component="CloseOutline" />
                </template>
                驳回
              </n-button>
              <n-popconfirm @positive-click="processOperation('approve')">
                <template #trigger>
                  <n-button type="primary" :loading="operationBusy" :disabled="operationBusy">
                    <template #icon>
                      <n-icon :component="CheckmarkCircleOutline" />
                    </template>
                    通过
                  </n-button>
                </template>
                确认通过该资金操作？会产生账本分录。
              </n-popconfirm>
            </template>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>
