<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import {
  NButton,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
  useMessage,
  type DataTableColumns,
  type SelectOption,
} from 'naive-ui';
import { AddOutline, DownloadOutline, RefreshOutline } from '@vicons/ionicons5';
import {
  financeApi,
  type ProfitSharingOrder,
  type ProfitSharingReceiver,
} from '@/api/merchantFinance';
import { errorMessage } from '@/api/client';
import { downloadMerchantCsv } from '@/api/merchantExports';
import AdminPage from '@/components/AdminPage.vue';
import { activeStatus, financeStatus, formatDateTime, formatMoney } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

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
  ratePercent: null as number | null,
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
  form.ratePercent = 5;
  receiverModalVisible.value = true;
}

function openEdit(row: ProfitSharingReceiver) {
  editingId.value = row.id;
  form.receiver_type = row.receiver_type || 'personal';
  form.account = row.account || '';
  form.name = row.name || '';
  form.ratePercent = Number(((row.rate_bp ?? 0) / 100).toFixed(2));
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
    render: (row) => h(NText, { class: 'mono' }, () => row.account || '-'),
  },
  { title: '名称', key: 'name', minWidth: 140, ellipsis: { tooltip: true } },
  {
    title: '比例',
    key: 'rate_bp',
    width: 110,
    align: 'right',
    render: (row) => `${((row.rate_bp ?? 0) / 100).toFixed(2)}%`,
  },
  {
    title: '状态',
    key: 'status',
    width: 96,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: activeStatus(row.status).type }, () =>
        activeStatus(row.status).text,
      ),
  },
  {
    title: '操作',
    key: 'actions',
    width: 210,
    fixed: 'right',
    render: (row) => {
      const enabled = row.status === 'active';
      return h(NSpace, { size: 8, wrap: false }, () => [
        h(NButton, { text: true, type: 'primary', size: 'small', onClick: () => openEdit(row) }, () => '编辑'),
        h(NButton, { text: true, type: 'info', size: 'small', onClick: () => void toggleReceiver(row) }, () =>
          enabled ? '停用' : '启用',
        ),
        h(
          NPopconfirm,
          {
            onPositiveClick: () => void removeReceiver(row),
            positiveText: '删除',
            negativeText: '取消',
          },
          {
            trigger: () => h(NButton, { text: true, type: 'error', size: 'small' }, () => '删除'),
            default: () => '删除后该接收方不再参与新的分账，确认删除？',
          },
        ),
      ]);
    },
  },
];

const orderColumns: DataTableColumns<ProfitSharingOrder> = [
  {
    title: '订单号',
    key: 'trade_no',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render: (row) =>
      h(NText, { class: 'mono' }, () => row.trade_no || row.out_trade_no || row.order_id || '-'),
  },
  {
    title: '分账金额',
    key: 'amount',
    width: 140,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: financeStatus(row.status).type }, () =>
        financeStatus(row.status).text,
      ),
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

const exporting = ref(false);

async function handleExport() {
  if (exporting.value) return;
  exporting.value = true;
  try {
    const result = await downloadMerchantCsv('profit-sharing');
    if (result.truncated) {
      message.warning(`导出达到行数上限，仅包含前 ${result.rows} 行：${result.filename}`);
    } else {
      message.success(`已导出 ${result.filename}`);
    }
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    exporting.value = false;
  }
}

onMounted(() => {
  handleRefresh();
});
</script>

<template>
  <admin-page title="分账管理" subtitle="维护分账接收方，查看每笔订单的分账流水">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="receiverLoading" @click="handleRefresh">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" :loading="exporting" @click="handleExport">
          <template #icon>
            <n-icon :component="DownloadOutline" />
          </template>
          导出 CSV
        </n-button>
      </n-space>
    </template>

    <n-card :bordered="false">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <n-tab-pane name="receivers" tab="我的接收方">
          <n-alert type="info" :bordered="false" style="margin-bottom: 12px">
            比例按订单金额计算、向下取整到分；平台手续费先扣，再按比例分账。
          </n-alert>
          <n-button type="primary" style="margin-bottom: 12px" @click="openCreate">
            <template #icon>
              <n-icon :component="AddOutline" />
            </template>
            新增接收方
          </n-button>
          <n-data-table
            :columns="receiverColumns"
            :data="receivers"
            :loading="receiverLoading"
            :bordered="false"
            :row-key="receiverRowKey"
            :pagination="false"
            :scroll-x="980"
            :size="isNarrow ? 'small' : 'medium'"
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
            :size="isNarrow ? 'small' : 'medium'"
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
      style="width: 620px; max-width: 94vw"
    >
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="接收方类型">
          <n-select v-model:value="form.receiver_type" :options="receiverTypeOptions" style="width: 100%" />
        </n-form-item>
        <n-form-item label="账号">
          <n-input v-model:value="form.account" placeholder="支付宝账号 / 银行卡号 / 商户号" :maxlength="128" />
        </n-form-item>
        <n-form-item label="名称">
          <n-input v-model:value="form.name" placeholder="接收方名称，用于对账展示" :maxlength="64" />
        </n-form-item>
        <n-form-item label="分账比例（%）">
          <n-input-number
            v-model:value="form.ratePercent"
            :precision="2"
            :min="0.01"
            :max="100"
            placeholder="例如 5.00"
            style="width: 100%"
          >
            <template #suffix>%</template>
          </n-input-number>
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        比例按订单金额计算、向下取整到分；平台手续费先扣，再按比例分账。
      </n-alert>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="receiverModalVisible = false">取消</n-button>
          <n-button type="primary" :loading="receiverSaving" @click="saveReceiver">保存</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
