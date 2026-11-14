<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import { exportCsvUrl } from '@/api/reports';
import type { Order } from '@/api';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime, formatMoney, paymentTypeText } from '@/utils/format';
import { toISOString, useServerPaging } from './shared';

const message = useMessage();

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

const columns = computed<DataTableColumns<Order>>(() => [
  {
    title: '平台订单号',
    key: 'trade_no',
    width: 200,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.trade_no || '-'),
  },
  {
    title: '商户订单号',
    key: 'out_trade_no',
    width: 180,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.out_trade_no || '-'),
  },
  {
    title: '商品名称',
    key: 'name',
    minWidth: 150,
    ellipsis: true,
    render: (row) => row.name || '-',
  },
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
    width: 110,
    render: (row) => paymentTypeText(row.payment_type),
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'order' }),
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
              { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) },
              { default: () => '详情' },
            ),
            closable(row)
              ? h(
                  NPopconfirm,
                  { onPositiveClick: () => close(row) },
                  {
                    trigger: () =>
                      h(
                        NButton,
                        { size: 'tiny', quaternary: true, type: 'error' },
                        { default: () => '关闭' },
                      ),
                    default: () => '确认关闭该订单？关闭后不可再支付。',
                  },
                )
              : null,
          ],
        },
      ),
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
    { label: '客户端 IP', value: order.client_ip || '-' },
    { label: '支付时间', value: formatDateTime(order.paid_at) },
    { label: '过期时间', value: formatDateTime(order.expires_at) },
    { label: '创建时间', value: formatDateTime(order.created_at) },
    { label: '更新时间', value: formatDateTime(order.updated_at) },
  ];
});

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="订单管理" subtitle="全平台支付订单查询、明细查看与未支付关单">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button secondary @click="exportCsv">导出 CSV</n-button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <n-input
        v-model:value="keyword"
        placeholder="订单号 / 商品名"
        clearable
        style="width: 220px"
        @keyup.enter="search"
      />
      <n-select v-model:value="status" :options="statusOptions" placeholder="全部状态" clearable style="width: 140px" />
      <n-select
        v-model:value="paymentType"
        :options="paymentTypeOptions"
        placeholder="全部支付方式"
        clearable
        style="width: 150px"
      />
      <n-date-picker
        v-model:value="dateRange"
        type="daterange"
        clearable
        start-placeholder="开始日期"
        end-placeholder="结束日期"
        style="width: 260px"
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
        :row-key="(row: Order) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-modal v-model:show="showDetail" preset="card" title="订单详情" style="width: 720px; max-width: 94vw">
      <n-empty v-if="!detail" description="暂无订单数据" />
      <n-descriptions v-else :column="1" label-placement="left" bordered size="small">
        <n-descriptions-item v-for="field in detailFields" :key="field.label" :label="field.label">
          <span :class="{ mono: field.mono }">{{ field.value }}</span>
        </n-descriptions-item>
      </n-descriptions>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDetail = false">关闭窗口</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>
