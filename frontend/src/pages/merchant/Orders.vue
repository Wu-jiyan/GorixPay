<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import { NButton, useMessage, type DataTableColumns, type SelectOption } from 'naive-ui';
import { merchantApi } from '@/api';
import type { Order, PageQuery } from '@/api/types';
import { errorMessage } from '@/api/client';
import { downloadMerchantCsv, type MerchantExportQuery } from '@/api/merchantExports';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { centsToYuan, formatDateTime, formatMoney, paymentTypeText, yuanToCents } from '@/utils/format';

const message = useMessage();

const loading = ref(false);
const exporting = ref(false);
const orders = ref<Order[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const filters = reactive({
  status: null as string | null,
  payment_type: null as string | null,
  keyword: '',
  range: null as [number, number] | null,
});

const detailVisible = ref(false);
const detailOrder = ref<Order | null>(null);

const refundVisible = ref(false);
const refundSubmitting = ref(false);
const refundTarget = ref<Order | null>(null);
const refundRemaining = ref(0);
const refundForm = reactive({ amountYuan: '', reason: '' });

const statusOptions: SelectOption[] = [
  { label: '待支付', value: 'pending' },
  { label: '支付中', value: 'processing' },
  { label: '已支付', value: 'paid' },
  { label: '已关闭', value: 'closed' },
  { label: '失败', value: 'failed' },
];

const paymentTypeOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信支付', value: 'wxpay' },
  { label: 'QQ 钱包', value: 'qqpay' },
  { label: '网银', value: 'bank' },
  { label: '沙箱支付', value: 'sandbox' },
];

const rowKey = (row: Order) => row.id;

function openDetail(row: Order) {
  detailOrder.value = row;
  detailVisible.value = true;
}

function generateOutRefundNo(): string {
  return `RF${dayjs().format('YYYYMMDDHHmmss')}${Math.floor(Math.random() * 900 + 100)}`;
}

async function openRefund(row: Order) {
  refundVisible.value = false;
  refundTarget.value = row;
  refundForm.reason = '';
  refundRemaining.value = row.amount;
  refundForm.amountYuan = centsToYuan(row.amount);
  try {
    const res = await merchantApi.refunds({ page: 1, page_size: 100 });
    const refunded = res.items
      .filter((item) => item.payment_order_id === row.id && item.status !== 'rejected')
      .reduce((sum, item) => sum + item.amount, 0);
    const remaining = Math.max(0, row.amount - refunded);
    refundRemaining.value = remaining;
    refundForm.amountYuan = centsToYuan(remaining);
    if (remaining <= 0) {
      message.warning('该订单剩余可退金额为 0，无法再次申请退款');
      return;
    }
  } catch {
    // 拉取退款记录失败时退化为「按订单金额」默认值，最终以服务端校验为准
  }
  refundVisible.value = true;
}

function refundFromDetail() {
  const target = detailOrder.value;
  if (!target) return;
  detailVisible.value = false;
  void openRefund(target);
}

async function submitRefund() {
  const target = refundTarget.value;
  if (!target) return;
  const cents = yuanToCents(refundForm.amountYuan);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的退款金额');
    return;
  }
  if (cents > refundRemaining.value) {
    message.error(`退款金额不能超过剩余可退 ${formatMoney(refundRemaining.value)}`);
    return;
  }
  if (!refundForm.reason.trim()) {
    message.error('请填写退款原因');
    return;
  }
  refundSubmitting.value = true;
  try {
    await merchantApi.createRefund({
      payment_order_id: target.id,
      out_refund_no: generateOutRefundNo(),
      amount: cents,
      reason: refundForm.reason.trim(),
    });
    message.success('退款申请已提交，等待平台审核');
    refundVisible.value = false;
    void load();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    refundSubmitting.value = false;
  }
}

const columns: DataTableColumns<Order> = [
  {
    title: '订单号',
    key: 'trade_no',
    width: 190,
    render: (row) => h('span', { class: 'mono-cell' }, row.trade_no),
  },
  {
    title: '商户订单号',
    key: 'out_trade_no',
    width: 180,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell muted-cell' }, row.out_trade_no),
  },
  { title: '商品名称', key: 'name', minWidth: 140, ellipsis: { tooltip: true } },
  {
    title: '金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  {
    title: '支付方式',
    key: 'payment_type',
    width: 100,
    render: (row) => paymentTypeText(row.payment_type),
  },
  {
    title: '状态',
    key: 'status',
    width: 96,
    render: (row) => h(StatusTag, { status: row.status }),
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
    width: 140,
    fixed: 'right',
    render: (row) => {
      const actions = [
        h(
          NButton,
          { text: true, type: 'primary', size: 'small', onClick: () => openDetail(row) },
          () => '详情',
        ),
      ];
      if (row.status === 'paid') {
        actions.push(
          h(
            NButton,
            { text: true, type: 'warning', size: 'small', onClick: () => void openRefund(row) },
            () => '申请退款',
          ),
        );
      }
      return h('div', { class: 'action-cell' }, actions);
    },
  },
];

const pagination = computed(() => ({
  page: page.value,
  pageSize: pageSize.value,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

function buildQuery(): PageQuery {
  const query: PageQuery = { page: page.value, page_size: pageSize.value };
  if (filters.status) query.status = filters.status;
  if (filters.payment_type) query.payment_type = filters.payment_type;
  if (filters.keyword.trim()) query.keyword = filters.keyword.trim();
  if (filters.range && filters.range.length === 2) {
    query.created_from = dayjs(filters.range[0]).startOf('day').format('YYYY-MM-DD HH:mm:ss');
    query.created_to = dayjs(filters.range[1]).endOf('day').format('YYYY-MM-DD HH:mm:ss');
  }
  return query;
}

function buildExportQuery(): MerchantExportQuery {
  const query: MerchantExportQuery = {};
  if (filters.status) query.status = filters.status;
  if (filters.payment_type) query.payment_type = filters.payment_type;
  if (filters.keyword.trim()) query.keyword = filters.keyword.trim();
  if (filters.range && filters.range.length === 2) {
    // 导出接口按 RFC3339 解析时间区间
    query.created_from = dayjs(filters.range[0]).startOf('day').toISOString();
    query.created_to = dayjs(filters.range[1]).endOf('day').toISOString();
  }
  return query;
}

async function handleExport() {
  if (exporting.value) return;
  exporting.value = true;
  try {
    const result = await downloadMerchantCsv('orders', buildExportQuery());
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

async function load() {
  loading.value = true;
  try {
    const res = await merchantApi.orders(buildQuery());
    orders.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function handleSearch() {
  page.value = 1;
  void load();
}

function handleReset() {
  filters.status = null;
  filters.payment_type = null;
  filters.keyword = '';
  filters.range = null;
  page.value = 1;
  void load();
}

function handlePageChange(value: number) {
  page.value = value;
  void load();
}

function handlePageSizeChange(value: number) {
  pageSize.value = value;
  page.value = 1;
  void load();
}

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="page">
    <PageHeader title="我的订单" subtitle="按状态、支付方式、时间范围筛选订单，已支付订单可直接申请退款">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
        <n-button size="small" type="primary" :loading="exporting" @click="handleExport">导出 CSV</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="table-card">
      <div class="toolbar">
        <n-select
          v-model:value="filters.status"
          :options="statusOptions"
          placeholder="订单状态"
          clearable
          style="width: 140px"
        />
        <n-select
          v-model:value="filters.payment_type"
          :options="paymentTypeOptions"
          placeholder="支付方式"
          clearable
          style="width: 150px"
        />
        <n-input
          v-model:value="filters.keyword"
          placeholder="订单号 / 商品名称"
          clearable
          style="width: 220px"
          @keyup.enter="handleSearch"
        />
        <n-date-picker
          v-model:value="filters.range"
          type="daterange"
          clearable
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 260px"
        />
        <n-button type="primary" @click="handleSearch">查询</n-button>
        <n-button quaternary @click="handleReset">重置</n-button>
      </div>

      <n-data-table
        remote
        :columns="columns"
        :data="orders"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1180"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      />
    </n-card>

    <n-drawer v-model:show="detailVisible" :width="460" placement="right">
      <n-drawer-content title="订单详情" closable>
        <n-descriptions v-if="detailOrder" :column="1" label-placement="left" bordered size="small">
          <n-descriptions-item label="平台订单号">
            <span class="mono-cell">{{ detailOrder.trade_no }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="商户订单号">
            <span class="mono-cell">{{ detailOrder.out_trade_no }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="商品名称">{{ detailOrder.name }}</n-descriptions-item>
          <n-descriptions-item label="订单金额">{{ formatMoney(detailOrder.amount) }}</n-descriptions-item>
          <n-descriptions-item label="币种">{{ detailOrder.currency }}</n-descriptions-item>
          <n-descriptions-item label="支付方式">{{ paymentTypeText(detailOrder.payment_type) }}</n-descriptions-item>
          <n-descriptions-item label="订单状态">
            <StatusTag :status="detailOrder.status" />
          </n-descriptions-item>
          <n-descriptions-item label="渠道单号">
            <span class="mono-cell">{{ detailOrder.channel_trade_no || '-' }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="异步通知地址">
            <span class="break-all">{{ detailOrder.notify_url || '-' }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="同步跳转地址">
            <span class="break-all">{{ detailOrder.return_url || '-' }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="附加参数">{{ detailOrder.attach || '-' }}</n-descriptions-item>
          <n-descriptions-item label="客户端 IP">{{ detailOrder.client_ip || '-' }}</n-descriptions-item>
          <n-descriptions-item label="创建时间">{{ formatDateTime(detailOrder.created_at) }}</n-descriptions-item>
          <n-descriptions-item label="支付时间">{{ formatDateTime(detailOrder.paid_at) }}</n-descriptions-item>
          <n-descriptions-item label="过期时间">{{ formatDateTime(detailOrder.expires_at) }}</n-descriptions-item>
        </n-descriptions>
        <template #footer>
          <n-button
            v-if="detailOrder?.status === 'paid'"
            type="warning"
            secondary
            @click="refundFromDetail"
          >
            申请退款
          </n-button>
        </template>
      </n-drawer-content>
    </n-drawer>

    <n-modal v-model:show="refundVisible" preset="card" title="申请退款" style="max-width: 460px">
      <n-alert type="info" :bordered="false" class="refund-alert">
        退款将进入平台审核流程，审核通过后按原路退回。剩余可退金额
        <strong>{{ formatMoney(refundRemaining) }}</strong>。
      </n-alert>
      <n-form label-placement="top" class="refund-form">
        <n-form-item label="退款金额（元）">
          <n-input v-model:value="refundForm.amountYuan" placeholder="0.00">
            <template #prefix>¥</template>
          </n-input>
        </n-form-item>
        <n-form-item label="退款原因">
          <n-input
            v-model:value="refundForm.reason"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="请填写退款原因，便于平台审核"
          />
        </n-form-item>
      </n-form>
      <div class="refund-meta">
        <span>订单号：<span class="mono-cell">{{ refundTarget?.trade_no }}</span></span>
        <span>实付金额：{{ formatMoney(refundTarget?.amount) }}</span>
      </div>
      <template #footer>
        <div class="modal-footer">
          <n-button @click="refundVisible = false">取消</n-button>
          <n-button type="primary" :loading="refundSubmitting" @click="submitRefund">提交退款申请</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.table-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
:deep(.muted-cell) {
  color: #64748b;
}
:deep(.action-cell) {
  display: flex;
  gap: 10px;
  align-items: center;
}
:deep(.break-all) {
  word-break: break-all;
}
.refund-alert {
  margin-bottom: 16px;
}
.refund-form {
  margin-bottom: 4px;
}
.refund-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12.5px;
  color: #64748b;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
@media (max-width: 768px) {
  .toolbar :deep(.n-select),
  .toolbar :deep(.n-input),
  .toolbar :deep(.n-date-picker) {
    width: 100% !important;
  }
}
</style>
