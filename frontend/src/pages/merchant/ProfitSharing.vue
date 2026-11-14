<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import {
  NButton,
  NPopconfirm,
  useMessage,
  type DataTableColumns,
  type SelectOption,
} from 'naive-ui';
import {
  financeApi,
  type ProfitSharingOrder,
  type ProfitSharingReceiver,
} from '@/api/merchantFinance';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime } from '@/utils/format';

const message = useMessage();

const activeTab = ref<'receivers' | 'orders'>('receivers');

const receiverLoading = ref(true);
const receivers = ref<ProfitSharingReceiver[]>([]);
const receiverModalVisible = ref(false);
const receiverSaving = ref(false);
const editingId = ref<string | null>(null);
const form = reactive({
  receiver_type: 'personal',
  account: '',
  name: '',
  ratePercent: '5.00',
});

const orderLoading = ref(true);
const orders = ref<ProfitSharingOrder[]>([]);
const orderTotal = ref(0);
const orderPage = ref(1);
const orderPageSize = ref(10);

const receiverTypeOptions: SelectOption[] = [
  { label: '个人', value: 'personal' },
  { label: '企业', value: 'corp' },
  { label: '银行卡', value: 'bank' },
  { label: '支付宝', value: 'alipay' },
  { label: '微信', value: 'wxpay' },
];

const RECEIVER_TYPE_LABELS: Record<string, string> = {
  personal: '个人',
  corp: '企业',
  bank: '银行卡',
  alipay: '支付宝',
  wxpay: '微信',
};

function receiverTypeText(type?: string): string {
  return RECEIVER_TYPE_LABELS[type ?? ''] ?? (type || '-');
}

const receiverRowKey = (row: ProfitSharingReceiver) => row.id;
const orderRowKey = (row: ProfitSharingOrder) => row.id;

function openCreate() {
  editingId.value = null;
  form.receiver_type = 'personal';
  form.account = '';
  form.name = '';
  form.ratePercent = '5.00';
  receiverModalVisible.value = true;
}

function openEdit(row: ProfitSharingReceiver) {
  editingId.value = row.id;
  form.receiver_type = row.receiver_type || 'personal';
  form.account = row.account || '';
  form.name = row.name || '';
  form.ratePercent = (row.rate_bp / 100).toFixed(2);
  receiverModalVisible.value = true;
}

async function loadReceivers() {
  receiverLoading.value = true;
  try {
    receivers.value = await financeApi.receivers();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    receiverLoading.value = false;
  }
}

async function saveReceiver() {
  if (receiverSaving.value) return;
  const account = form.account.trim();
  const name = form.name.trim();
  const percent = Number(form.ratePercent);
  if (!account) {
    message.error('请填写接收方账号');
    return;
  }
  if (!name) {
    message.error('请填写接收方名称');
    return;
  }
  if (!Number.isFinite(percent) || percent <= 0 || percent > 100) {
    message.error('分账比例需在 0 到 100 之间');
    return;
  }
  const rateBp = Math.round(percent * 100);
  receiverSaving.value = true;
  try {
    if (editingId.value) {
      await financeApi.updateReceiver(editingId.value, {
        receiver_type: form.receiver_type,
        account,
        name,
        rate_bp: rateBp,
      });
      message.success('接收方已更新');
    } else {
      await financeApi.createReceiver({ receiver_type: form.receiver_type, account, name, rate_bp: rateBp });
      message.success('接收方已添加');
    }
    receiverModalVisible.value = false;
    await loadReceivers();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    receiverSaving.value = false;
  }
}

async function toggleReceiver(row: ProfitSharingReceiver) {
  try {
    await financeApi.updateReceiver(row.id, { status: row.status === 'active' ? 'disabled' : 'active' });
    message.success(row.status === 'active' ? '已停用' : '已启用');
    await loadReceivers();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function removeReceiver(row: ProfitSharingReceiver) {
  try {
    await financeApi.deleteReceiver(row.id);
    message.success('接收方已删除');
    await loadReceivers();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

const receiverColumns: DataTableColumns<ProfitSharingReceiver> = [
  {
    title: '类型',
    key: 'receiver_type',
    width: 110,
    render: (row) => receiverTypeText(row.receiver_type),
  },
  {
    title: '账号',
    key: 'account',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.account || '-'),
  },
  { title: '名称', key: 'name', minWidth: 140, ellipsis: { tooltip: true } },
  {
    title: '比例',
    key: 'rate_bp',
    width: 100,
    align: 'right',
    render: (row) => `${((row.rate_bp ?? 0) / 100).toFixed(2)}%`,
  },
  {
    title: '状态',
    key: 'status',
    width: 96,
    render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 210,
    fixed: 'right',
    render: (row) => {
      const enabled = row.status === 'active';
      const actions = [
        h(NButton, { text: true, type: 'primary', size: 'small', onClick: () => openEdit(row) }, () => '编辑'),
        h(
          NButton,
          { text: true, type: 'info', size: 'small', onClick: () => void toggleReceiver(row) },
          () => (enabled ? '停用' : '启用'),
        ),
        h(
          NPopconfirm,
          { onPositiveClick: () => void removeReceiver(row), positiveText: '删除', negativeText: '取消' },
          {
            trigger: () => h(NButton, { text: true, type: 'error', size: 'small' }, () => '删除'),
            default: () => '删除后该接收方不再参与新的分账，确认删除？',
          },
        ),
      ];
      return h('div', { class: 'action-cell' }, actions);
    },
  },
];

const orderColumns: DataTableColumns<ProfitSharingOrder> = [
  {
    title: '订单号',
    key: 'trade_no',
    minWidth: 210,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.trade_no || row.out_trade_no || row.order_id || '-'),
  },
  {
    title: '分账金额',
    key: 'amount',
    width: 140,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

const orderPagination = computed(() => ({
  page: orderPage.value,
  pageSize: orderPageSize.value,
  itemCount: orderTotal.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

async function loadOrders() {
  orderLoading.value = true;
  try {
    const res = await financeApi.sharingOrders({ page: orderPage.value, page_size: orderPageSize.value });
    orders.value = res.items;
    orderTotal.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    orderLoading.value = false;
  }
}

function handleOrderPageChange(value: number) {
  orderPage.value = value;
  void loadOrders();
}

function handleOrderPageSizeChange(value: number) {
  orderPageSize.value = value;
  orderPage.value = 1;
  void loadOrders();
}

function handleRefresh() {
  void loadReceivers();
  void loadOrders();
}

onMounted(() => {
  handleRefresh();
});
</script>

<template>
  <div class="page">
    <PageHeader title="分账管理" subtitle="维护分账接收方，查看每笔订单的分账流水">
      <template #actions>
        <n-button size="small" :loading="receiverLoading" @click="handleRefresh">刷新</n-button>
        <n-button size="small" type="primary" @click="openCreate">新增接收方</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="main-card">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <n-tab-pane name="receivers" tab="我的接收方">
          <n-data-table
            :columns="receiverColumns"
            :data="receivers"
            :loading="receiverLoading"
            :bordered="false"
            :row-key="receiverRowKey"
            :pagination="false"
            :scroll-x="900"
            size="small"
          >
            <template #empty>
              <n-empty description="还没有添加分账接收方" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="orders" tab="分账流水">
          <n-data-table
            remote
            :columns="orderColumns"
            :data="orders"
            :loading="orderLoading"
            :pagination="orderPagination"
            :bordered="false"
            :row-key="orderRowKey"
            :scroll-x="760"
            size="small"
            @update:page="handleOrderPageChange"
            @update:page-size="handleOrderPageSizeChange"
          >
            <template #empty>
              <n-empty description="暂无分账流水" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <n-modal
      v-model:show="receiverModalVisible"
      preset="card"
      :title="editingId ? '编辑接收方' : '新增接收方'"
      style="max-width: 480px"
    >
      <n-form label-placement="top">
        <n-form-item label="接收方类型">
          <n-select v-model:value="form.receiver_type" :options="receiverTypeOptions" />
        </n-form-item>
        <n-form-item label="账号">
          <n-input v-model:value="form.account" placeholder="支付宝账号 / 银行卡号 / 商户号" :maxlength="128" />
        </n-form-item>
        <n-form-item label="名称">
          <n-input v-model:value="form.name" placeholder="接收方名称，用于对账展示" :maxlength="64" />
        </n-form-item>
        <n-form-item label="分账比例（%）">
          <n-input v-model:value="form.ratePercent" placeholder="例如 5.00">
            <template #suffix>%</template>
          </n-input>
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        比例按订单金额计算、向下取整到分；平台手续费先扣，再按比例分账。
      </n-alert>
      <template #footer>
        <div class="modal-footer">
          <n-button @click="receiverModalVisible = false">取消</n-button>
          <n-button type="primary" :loading="receiverSaving" @click="saveReceiver">保存</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.main-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
:deep(.action-cell) {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
@media (max-width: 768px) {
  :deep(.action-cell) {
    gap: 6px;
  }
}
</style>
