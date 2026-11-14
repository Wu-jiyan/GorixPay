<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { profitSharingApi } from '@/api/profitSharing';
import type { ProfitReceiver, ProfitSharingOrder } from '@/api/profitSharing';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

interface ReceiverForm {
  merchant_id: string;
  receiver_type: string;
  account: string;
  name: string;
  rate_percent: number | null;
}

const message = useMessage();

const tab = ref('receivers');

const receivers = ref<ProfitReceiver[]>([]);
const orders = ref<ProfitSharingOrder[]>([]);
const receiversLoading = ref(false);
const ordersLoading = ref(false);

const showForm = ref(false);
const submitting = ref(false);
const editingId = ref<string | null>(null);
const editingStatus = ref('active');
const formRef = ref<FormInst | null>(null);
const retryingId = ref('');

const form = reactive<ReceiverForm>({
  merchant_id: '',
  receiver_type: 'alipay',
  account: '',
  name: '',
  rate_percent: 0,
});

const {
  page: receiverPage,
  pageSize: receiverPageSize,
  total: receiverTotal,
  pagination: receiverPagination,
  onPage: onReceiverPage,
  onPageSize: onReceiverPageSize,
} = useServerPaging(loadReceivers);

const {
  page: orderPage,
  pageSize: orderPageSize,
  total: orderTotal,
  pagination: orderPagination,
  onPage: onOrderPage,
  onPageSize: onOrderPageSize,
} = useServerPaging(loadOrders);

const receiverTypeOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信', value: 'wxpay' },
  { label: '银行卡', value: 'bank' },
  { label: 'USDT', value: 'usdt' },
];

const receiverTypeLabels: Record<string, string> = {
  alipay: '支付宝',
  wxpay: '微信',
  bank: '银行卡',
  usdt: 'USDT',
};

const orderStatusMap: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  pending: { text: '待处理', type: 'warning' },
  processing: { text: '处理中', type: 'info' },
  success: { text: '成功', type: 'success' },
  completed: { text: '成功', type: 'success' },
  done: { text: '成功', type: 'success' },
  failed: { text: '失败', type: 'error' },
  skipped: { text: '跳过', type: 'default' },
};

const rules: FormRules = {
  merchant_id: { required: true, message: '请输入商户 ID', trigger: ['blur', 'input'] },
  receiver_type: { required: true, message: '请选择接收方类型', trigger: ['change', 'blur'] },
  account: { required: true, message: '请输入收款账号', trigger: ['blur', 'input'] },
  name: { required: true, message: '请输入接收方名称', trigger: ['blur', 'input'] },
  rate_percent: { required: true, type: 'number', message: '请输入分账比例', trigger: ['blur', 'change'] },
};

const isEditing = computed(() => editingId.value !== null);

async function loadReceivers() {
  receiversLoading.value = true;
  try {
    const data = await profitSharingApi.receivers({ page: receiverPage.value, page_size: receiverPageSize.value });
    const items = itemsOf<ProfitReceiver>(data);
    receivers.value = items;
    receiverTotal.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    receiversLoading.value = false;
  }
}

async function loadOrders() {
  ordersLoading.value = true;
  try {
    const data = await profitSharingApi.orders({ page: orderPage.value, page_size: orderPageSize.value });
    const items = itemsOf<ProfitSharingOrder>(data);
    orders.value = items;
    orderTotal.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    ordersLoading.value = false;
  }
}

function handleTab(value: string) {
  tab.value = value;
  if (value === 'receivers') void loadReceivers();
  else void loadOrders();
}

function resetForm() {
  form.merchant_id = '';
  form.receiver_type = 'alipay';
  form.account = '';
  form.name = '';
  form.rate_percent = 0;
}

function openCreate() {
  editingId.value = null;
  editingStatus.value = 'active';
  resetForm();
  showForm.value = true;
}

function openEdit(row: ProfitReceiver) {
  editingId.value = row.id;
  editingStatus.value = row.status || 'active';
  form.merchant_id = row.merchant_id || '';
  form.receiver_type = row.receiver_type || 'alipay';
  form.account = row.account || '';
  form.name = row.name || '';
  form.rate_percent = numberOr(row.rate_bp, 0) / 100;
  showForm.value = true;
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const payload = {
      merchant_id: form.merchant_id.trim(),
      receiver_type: form.receiver_type,
      account: form.account.trim(),
      name: form.name.trim(),
      rate_bp: Math.round(numberOr(form.rate_percent, 0) * 100),
      status: editingStatus.value,
    };
    if (editingId.value) {
      await profitSharingApi.updateReceiver(editingId.value, payload);
      message.success('接收方已更新');
    } else {
      await profitSharingApi.createReceiver(payload);
      message.success('接收方已创建');
    }
    showForm.value = false;
    await loadReceivers();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function removeReceiver(row: ProfitReceiver) {
  try {
    await profitSharingApi.deleteReceiver(row.id);
    message.success('接收方已删除');
    await loadReceivers();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

async function retry(row: ProfitSharingOrder) {
  retryingId.value = row.id;
  try {
    await profitSharingApi.retryOrder(row.id);
    message.success('已提交重试');
    await loadOrders();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    retryingId.value = '';
  }
}

function orderStatusTag(status: string) {
  const resolved = orderStatusMap[status] ?? { text: status || '-', type: 'default' as const };
  return h(NTag, { size: 'small', round: true, type: resolved.type }, { default: () => resolved.text });
}

const receiverColumns = computed<DataTableColumns<ProfitReceiver>>(() => [
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
  },
  {
    title: '类型',
    key: 'receiver_type',
    width: 100,
    render: (row) => receiverTypeLabels[row.receiver_type || ''] ?? row.receiver_type ?? '-',
  },
  {
    title: '收款账号',
    key: 'account',
    minWidth: 200,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.account || '-'),
  },
  { title: '接收方名称', key: 'name', minWidth: 140, ellipsis: true, render: (row) => row.name || '-' },
  {
    title: '比例',
    key: 'rate_bp',
    width: 100,
    align: 'right',
    render: (row) => `${(numberOr(row.rate_bp, 0) / 100).toFixed(2)}%`,
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
              NButton,
              { size: 'tiny', quaternary: true, type: 'info', onClick: () => openEdit(row) },
              { default: () => '编辑' },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => removeReceiver(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                default: () => '确认删除该接收方？',
              },
            ),
          ],
        },
      ),
  },
]);

const orderColumns = computed<DataTableColumns<ProfitSharingOrder>>(() => {
  const retrying = retryingId.value;
  return [
    {
      title: '订单号',
      key: 'order_id',
      width: 260,
      ellipsis: true,
      render: (row) => h('span', { class: 'mono' }, row.order_id || '-'),
    },
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 240,
      ellipsis: true,
      render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
    },
    {
      title: '接收方',
      key: 'receiver_id',
      width: 240,
      ellipsis: true,
      render: (row) => h('span', { class: 'mono' }, row.receiver_id || '-'),
    },
    {
      title: '金额',
      key: 'amount',
      width: 130,
      align: 'right',
      render: (row) => h(MoneyText, { amount: numberOr(row.amount, 0) }),
    },
    {
      title: '状态',
      key: 'status',
      width: 100,
      render: (row) => orderStatusTag(row.status),
    },
    {
      title: '失败原因',
      key: 'error',
      minWidth: 200,
      ellipsis: true,
      render: (row) => row.error || '-',
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
      width: 110,
      fixed: 'right',
      render: (row) =>
        h(
          NButton,
          {
            size: 'tiny',
            quaternary: true,
            type: 'primary',
            loading: retrying === row.id,
            onClick: () => retry(row),
          },
          { default: () => '重试' },
        ),
    },
  ];
});

onMounted(() => {
  void loadReceivers();
});
</script>

<template>
  <div class="page">
    <PageHeader title="分账" subtitle="分账接收方维护与每笔订单的分账执行流水">
      <template #actions>
        <n-button secondary :loading="receiversLoading || ordersLoading" @click="handleTab(tab)">刷新</n-button>
        <n-button v-if="tab === 'receivers'" type="primary" @click="openCreate">新建接收方</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="receivers" tab="接收方">
          <n-data-table
            :columns="receiverColumns"
            :data="receivers"
            :loading="receiversLoading"
            :pagination="receiverPagination"
            :row-key="(row: ProfitReceiver) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
            remote
            @update:page="onReceiverPage"
            @update:page-size="onReceiverPageSize"
          >
            <template #empty>
              <n-empty description="暂无分账接收方" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="orders" tab="分账流水">
          <n-data-table
            :columns="orderColumns"
            :data="orders"
            :loading="ordersLoading"
            :pagination="orderPagination"
            :row-key="(row: ProfitSharingOrder) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
            remote
            @update:page="onOrderPage"
            @update:page-size="onOrderPageSize"
          >
            <template #empty>
              <n-empty description="暂无分账流水" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑接收方' : '新建接收方'"
      style="width: 640px; max-width: 94vw"
    >
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="130">
        <n-form-item label="商户 ID" path="merchant_id">
          <n-input v-model:value="form.merchant_id" placeholder="接收方所属商户" />
        </n-form-item>
        <n-form-item label="接收方类型" path="receiver_type">
          <n-select v-model:value="form.receiver_type" :options="receiverTypeOptions" tag filterable />
        </n-form-item>
        <n-form-item label="收款账号" path="account">
          <n-input v-model:value="form.account" placeholder="支付宝账号 / 微信 openid / 银行卡号 / USDT 地址" />
        </n-form-item>
        <n-form-item label="接收方名称" path="name">
          <n-input v-model:value="form.name" placeholder="收款人姓名或备注名" />
        </n-form-item>
        <n-form-item label="分账比例（%）" path="rate_percent">
          <div class="row-inline">
            <n-input-number
              v-model:value="form.rate_percent"
              :min="0"
              :max="100"
              :step="0.01"
              :precision="2"
              style="width: 180px"
            />
            <span class="muted small">后端按万分比存储，填 5 表示 5%</span>
          </div>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.row-inline {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.small {
  font-size: 12px;
}
</style>
