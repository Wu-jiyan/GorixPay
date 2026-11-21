<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NForm, NFormItem, NInput, NInputNumber, NModal, NPopconfirm, NSpace, NSwitch, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { DownloadOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { adminApi } from '@/api';
import { exportCsvUrl } from '@/api/reports';
import type { Order } from '@/api';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime, formatMoney, orderStatus, paymentTypeText } from '@/utils/format';
import { toISOString, useNarrow, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<Order[]>([]);
const loading = ref(false);

const status = ref<string | null>(null);
const paymentType = ref<string | null>(null);
const keyword = ref('');
const dateRange = ref<[number, number] | null>(null);

const showDetail = ref(false);
const detail = ref<Order | null>(null);

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

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
  { label: '收银台', value: 'cashier' },
  { label: '沙箱支付', value: 'sandbox' },
];

async function load() {
  loading.value = true;
  try {
    const range = dateRange.value;
    const data = await adminApi.orders({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
      payment_type: paymentType.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
      created_from: range ? toISOString(range[0]) : undefined,
      created_to: range ? toISOString(range[1]) : undefined,
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

function exportCsv() {
  const range = dateRange.value;
  window.open(
    exportCsvUrl('orders', {
      status: status.value ?? undefined,
      payment_type: paymentType.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
      created_from: range ? toISOString(range[0]) : undefined,
      created_to: range ? toISOString(range[1]) : undefined,
    }),
    '_blank',
  );
}

function resetFilters() {
  status.value = null;
  paymentType.value = null;
  keyword.value = '';
  dateRange.value = null;
  resetPage();
  void load();
}

function openDetail(row: Order) {
  detail.value = row;
  showDetail.value = true;
}

async function close(row: Order) {
  try {
    await adminApi.closeOrder(row.id);
    message.success('订单已关闭');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function closable(row: Order): boolean {
  return row.status === 'pending' || row.status === 'processing';
}

// 退款：默认整单全额，金额按元填写，提交时换回分。
const showRefund = ref(false);
const refundTarget = ref<Order | null>(null);
const refundAmount = ref<number | null>(null);
const refundReason = ref('');
const refundExecute = ref(true);
const refunding = ref(false);

function refundable(row: Order): boolean {
  return row.status === 'paid';
}

function openRefund(row: Order) {
  refundTarget.value = row;
  refundAmount.value = row.amount / 100;
  refundReason.value = '';
  refundExecute.value = true;
  showRefund.value = true;
}

async function submitRefund() {
  const target = refundTarget.value;
  if (!target) return;
  const yuan = refundAmount.value;
  if (yuan === null || yuan <= 0) {
    message.error('退款金额需大于 0');
    return;
  }
  refunding.value = true;
  try {
    const result = await adminApi.refundOrder(target.id, {
      amount: Math.round(yuan * 100),
      reason: refundReason.value.trim() || undefined,
      execute: refundExecute.value,
    });
    if (result.channel === 'refunded') {
      message.success('退款已提交并完成通道退款');
    } else {
      message.success(result.message || '退款申请已提交，等待审核');
    }
    showRefund.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
    await load();
  } finally {
    refunding.value = false;
  }
}

const columns = computed<DataTableColumns<Order>>(() => [
  {
    title: '平台订单号',
    key: 'trade_no',
    width: 210,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.trade_no || '-'),
  },
  {
    title: '商户订单号',
    key: 'out_trade_no',
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.out_trade_no || '-'),
  },
  {
    title: '商品名称',
    key: 'name',
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) => row.name || '-',
  },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  {
    title: '支付方式',
    key: 'payment_type',
    width: 110,
    render: (row) => paymentTypeText(row.payment_type),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: orderStatus(row.status).type }, () =>
        orderStatus(row.status).text,
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
    width: 190,
    fixed: 'right',
    render: (row) =>
      h(NSpace, { size: 8, wrap: false }, () => [
        h(
          NButton,
          { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) },
          () => '详情',
        ),
        refundable(row)
          ? h(
              NButton,
              { size: 'small', text: true, type: 'warning', onClick: () => openRefund(row) },
              () => '退款',
            )
          : null,
        closable(row)
          ? h(
              NPopconfirm,
              { onPositiveClick: () => close(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'small', text: true, type: 'error' },
                    () => '关闭',
                  ),
                default: () => '确认关闭该订单？关闭后不可再支付。',
              },
            )
          : null,
      ]),
  },
]);

interface DetailField {
  label: string;
  value: string;
  mono?: boolean;
}

const detailFields = computed<DetailField[]>(() => {
  const order = detail.value;
  if (!order) return [];
  return [
    { label: '订单 ID', value: order.id || '-', mono: true },
    { label: '平台订单号', value: order.trade_no || '-', mono: true },
    { label: '商户订单号', value: order.out_trade_no || '-', mono: true },
    { label: '商品名称', value: order.name || '-' },
    { label: '订单金额', value: formatMoney(order.amount) },
    { label: '币种', value: order.currency || '-' },
    { label: '支付方式', value: paymentTypeText(order.payment_type) },
    { label: '订单状态', value: order.status || '-' },
    { label: '渠道订单号', value: order.channel_trade_no || '-', mono: true },
    { label: '回调地址', value: order.notify_url || '-' },
    { label: '返回地址', value: order.return_url || '-' },
    { label: '附加参数', value: order.attach || '-' },
    { label: '客户端 IP', value: order.client_ip || '-', mono: true },
    { label: '支付时间', value: formatDateTime(order.paid_at) },
    { label: '过期时间', value: formatDateTime(order.expires_at) },
    { label: '创建时间', value: formatDateTime(order.created_at) },
    { label: '更新时间', value: formatDateTime(order.updated_at) },
  ];
});

onMounted(load);
</script>

<template>
  <admin-page title="订单管理" subtitle="全平台支付订单查询、明细查看与未支付关单">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button @click="exportCsv">
          <template #icon>
            <n-icon :component="DownloadOutline" />
          </template>
          导出 CSV
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
                placeholder="订单号 / 商品名"
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
            <n-form-item label="支付方式">
              <n-select
                v-model:value="paymentType"
                :options="paymentTypeOptions"
                placeholder="全部支付方式"
                clearable
                style="width: 100%"
              />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="创建时间">
              <n-date-picker
                v-model:value="dateRange"
                type="daterange"
                clearable
                start-placeholder="开始日期"
                end-placeholder="结束日期"
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
        :row-key="(row: Order) => row.id"
        :scroll-x="1200"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-modal v-model:show="showDetail" preset="card" title="订单详情" style="width: 760px; max-width: 94vw">
      <n-descriptions v-if="detail" :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left">
        <n-descriptions-item v-for="field in detailFields" :key="field.label" :label="field.label">
          <n-text :class="{ mono: field.mono }" style="word-break: break-all">{{ field.value }}</n-text>
        </n-descriptions-item>
      </n-descriptions>
      <n-empty v-else description="暂无订单数据" />

      <template #footer>
        <n-space justify="end">
          <n-button
            v-if="detail && refundable(detail)"
            type="warning"
            secondary
            @click="showDetail = false; openRefund(detail)"
          >
            退款
          </n-button>
          <n-button @click="showDetail = false">关闭窗口</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showRefund" preset="card" title="订单退款" style="width: 480px; max-width: 94vw">
      <n-form label-placement="top" :show-feedback="false">
        <n-form-item label="订单号">
          <n-text class="mono">{{ refundTarget?.trade_no || '-' }}</n-text>
        </n-form-item>
        <n-form-item label="订单金额">
          <n-text>{{ refundTarget ? formatMoney(refundTarget.amount) : '-' }}</n-text>
        </n-form-item>
        <n-form-item label="退款金额（元）">
          <n-input-number
            v-model:value="refundAmount"
            :min="0.01"
            :max="refundTarget ? refundTarget.amount / 100 : undefined"
            :precision="2"
            style="width: 100%"
          />
        </n-form-item>
        <n-form-item label="退款原因">
          <n-input v-model:value="refundReason" type="textarea" :rows="2" placeholder="选填，会记录在退款单上" />
        </n-form-item>
        <n-form-item label="立即向通道退款">
          <n-space vertical :size="4">
            <n-switch v-model:value="refundExecute" />
            <n-text depth="3" style="font-size: 12px">
              开启：立刻调用捕获这笔款的通道退款接口并记账；关闭：只生成退款单，等待在「退款管理」里审核。
            </n-text>
          </n-space>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRefund = false">取消</n-button>
          <n-button type="warning" :loading="refunding" @click="submitRefund">确认退款</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
