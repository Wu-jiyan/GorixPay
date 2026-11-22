<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NForm, NFormItem, NInput, NInputNumber, NModal, NPopconfirm, NSpace, NSwitch, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, DataTableRowKey, SelectOption } from 'naive-ui';
import { CloseOutline, DownloadOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { adminApi } from '@/api';
import { exportCsvUrl } from '@/api/reports';
import type { Order } from '@/api';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime, formatMoney, orderStatus, paymentTypeText } from '@/utils/format';
import { numberOr, toISOString, useNarrow, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<Order[]>([]);
const loading = ref(false);

const status = ref<string | null>(null);
const paymentType = ref<string | null>(null);
const keyword = ref('');
const dateRange = ref<[number, number] | null>(null);
/** 资金冻结筛选：null=全部、'true'=仅已冻结、'false'=仅未冻结 */
const frozen = ref<'true' | 'false' | null>(null);

const showDetail = ref(false);
const detail = ref<Order | null>(null);

/* 批量关单：勾选行 + 二次确认 */
const checkedKeys = ref<DataTableRowKey[]>([]);
const batchClosing = ref(false);
const selectedCount = computed(() => checkedKeys.value.length);

/* 手动补单：走渠道回调同一条完成路径 */
const showComplete = ref(false);
const completeTarget = ref<Order | null>(null);
const completeChannelTradeNo = ref('');
const completing = ref(false);

/* 补发通知：行级 in-flight 锁 */
const notifyingId = ref('');

/* 冻结/解冻：同一时刻只允许一行在飞 */
const freezingId = ref('');

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

// 选「全部」是 null，clean() 会把它从 query 里剔掉
const frozenOptions: SelectOption[] = [
  { label: '仅已冻结', value: 'true' },
  { label: '仅未冻结', value: 'false' },
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
      frozen: frozen.value ?? undefined,
      created_from: range ? toISOString(range[0]) : undefined,
      created_to: range ? toISOString(range[1]) : undefined,
    });
    rows.value = data.items;
    total.value = data.total;
    // 勾选只对当前页有效：翻页/换筛选后把已经不在列表里的行剔掉，避免对看不见的行下操作
    const visible = new Set(rows.value.map((row) => row.id));
    checkedKeys.value = checkedKeys.value.filter((key) => visible.has(String(key)));
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
      frozen: frozen.value ?? undefined,
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
  frozen.value = null;
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

/** 批量关单：后端只关 pending/processing，跳过的笔数 = requested - closed */
async function batchClose() {
  const ids = checkedKeys.value.map((key) => String(key));
  if (ids.length === 0) {
    message.warning('先勾选要关闭的订单');
    return;
  }
  batchClosing.value = true;
  try {
    const result = await adminApi.batchCloseOrders(ids);
    const closed = numberOr(result.closed, 0);
    const requested = numberOr(result.requested, ids.length);
    const skipped = Math.max(0, requested - closed);
    message.success(`已关闭 ${closed} 笔，跳过 ${skipped} 笔`);
    checkedKeys.value = [];
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    batchClosing.value = false;
  }
}

function openComplete(row: Order) {
  completeTarget.value = row;
  completeChannelTradeNo.value = '';
  showComplete.value = true;
}

/** 手动补单：留空通道单号时后端会自己生成 MANUAL-<trade_no> */
async function submitComplete() {
  const target = completeTarget.value;
  if (!target) return;
  completing.value = true;
  try {
    const result = await adminApi.completeOrder(target.id, completeChannelTradeNo.value.trim());
    message.success(result.applied ? '补单完成：已按已支付记账并通知商户' : '该订单此前已完成，本次没有重复记账');
    showComplete.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    completing.value = false;
  }
}

/** 补发通知：requeued=0 说明商户已经确认过，没有待重发的消息 */
async function resendNotify(row: Order) {
  notifyingId.value = row.id;
  try {
    const result = await adminApi.resendOrderNotify(row.id);
    const count = numberOr(result.requeued, 0);
    if (count === 0) message.warning('该订单没有待重发的通知');
    else message.success(`已重新排队 ${count} 条通知`);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    notifyingId.value = '';
  }
}

/** frozen_at 有值即已冻结 */
function isFrozen(row: Order): boolean {
  return typeof row.frozen_at === 'string' && row.frozen_at !== '';
}

/**
 * 冻结 / 解冻订单：金额在商户「可用余额」和「冻结余额」之间挪，订单状态不变。
 * 后端把整条订单回给我们，直接覆盖当前行和已打开的详情弹窗，不用整页重载。
 */
async function toggleFreeze(row: Order, freeze: boolean) {
  freezingId.value = row.id;
  try {
    const updated = freeze ? await adminApi.freezeOrder(row.id) : await adminApi.unfreezeOrder(row.id);
    const index = rows.value.findIndex((item) => item.id === row.id);
    if (index >= 0) rows.value[index] = { ...rows.value[index], ...updated };
    if (detail.value && detail.value.id === row.id) detail.value = { ...detail.value, ...updated };
    message.success(freeze ? '订单金额已转入商户冻结余额' : '冻结金额已退回商户可用余额');
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    freezingId.value = '';
  }
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
    // 只有未支付订单能被批量关单，已支付的勾了也没用，直接从选择列禁掉
    type: 'selection',
    disabled: (row: Order) => !closable(row),
  },
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
    width: 160,
    render: (row) =>
      h(NSpace, { size: 6, wrap: false, align: 'center' }, () => [
        h(NTag, { size: 'small', bordered: false, type: orderStatus(row.status).type }, () =>
          orderStatus(row.status).text,
        ),
        // 冻结不改订单状态，另挂一个标记，避免和"已支付"混成一句话
        isFrozen(row)
          ? h(NTag, { size: 'small', type: 'warning', bordered: false }, () => '已冻结')
          : null,
      ]),
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
    width: 390,
    fixed: 'right',
    render: (row) =>
      h(NSpace, { size: 8, wrap: false }, () => [
        h(
          NButton,
          { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) },
          () => '详情',
        ),
        closable(row)
          ? h(
              NButton,
              { size: 'small', text: true, type: 'primary', onClick: () => openComplete(row) },
              () => '补单',
            )
          : null,
        refundable(row)
          ? h(
              NButton,
              { size: 'small', text: true, type: 'warning', onClick: () => openRefund(row) },
              () => '退款',
            )
          : null,
        row.status === 'paid'
          ? h(
              NButton,
              {
                size: 'small',
                text: true,
                type: 'info',
                loading: notifyingId.value === row.id,
                disabled: notifyingId.value === row.id,
                onClick: () => resendNotify(row),
              },
              () => '补发通知',
            )
          : null,
        row.status === 'paid'
          ? h(
              NPopconfirm,
              {
                onPositiveClick: () => toggleFreeze(row, !isFrozen(row)),
              },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      size: 'small',
                      text: true,
                      type: isFrozen(row) ? 'info' : 'warning',
                      loading: freezingId.value === row.id,
                      disabled: freezingId.value === row.id,
                    },
                    () => (isFrozen(row) ? '解冻' : '冻结'),
                  ),
                default: () =>
                  isFrozen(row)
                    ? '解冻后这笔金额会退回商户可用余额。'
                    : '该订单已入账金额会转入商户冻结余额。',
              },
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
  const fields: DetailField[] = [
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
  // 只有被冻结过的订单才多这一条，避免正常订单里多一行 "-"
  if (typeof order.frozen_at === 'string' && order.frozen_at !== '') {
    fields.splice(14, 0, { label: '冻结时间', value: formatDateTime(order.frozen_at) });
  }
  return fields;
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
        <n-popconfirm :disabled="selectedCount === 0" @positive-click="batchClose">
          <template #trigger>
            <n-button
              type="error"
              secondary
              :disabled="selectedCount === 0"
              :loading="batchClosing"
            >
              <template #icon>
                <n-icon :component="CloseOutline" />
              </template>
              批量关单<span v-if="selectedCount > 0">（{{ selectedCount }}）</span>
            </n-button>
          </template>
          确认关闭选中的 {{ selectedCount }} 笔未支付订单？已支付 / 已关闭的会被跳过。
        </n-popconfirm>
      </n-space>
    </template>

    <n-card :bordered="false">
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
            <n-form-item label="资金冻结">
              <n-select
                v-model:value="frozen"
                :options="frozenOptions"
                placeholder="全部"
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
        :bordered="false"
        v-model:checked-row-keys="checkedKeys"
        :remote="true"
        :loading="loading"
        :columns="columns"
        :data="rows"
        :pagination="pagination"
        :row-key="(row: Order) => row.id"
        :scroll-x="1470"
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
      <n-empty v-else description="暂无订单数据" size="small" />

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
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :show-feedback="false">
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
            <n-text depth="3" class="gp-hint">
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

    <n-modal v-model:show="showComplete" preset="card" title="手动补单" style="width: 560px; max-width: 94vw">
      <n-alert type="warning" :bordered="false" style="margin-bottom: 14px">
        补单会按「已支付」记账（账本 + 钩子 + 通知），走的是和渠道回调完全相同的完成路径，每笔订单只生效一次。
      </n-alert>

      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="订单号">
          <n-text class="mono">{{ completeTarget?.trade_no || '-' }}</n-text>
        </n-form-item>
        <n-form-item label="订单金额">
          <n-text class="amount">{{ completeTarget ? formatMoney(completeTarget.amount) : '-' }}</n-text>
        </n-form-item>
        <n-form-item label="通道单号">
          <n-input
            v-model:value="completeChannelTradeNo"
            placeholder="选填；留空后端生成 MANUAL-<平台订单号>"
            class="mono"
          />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showComplete = false">取消</n-button>
          <n-button type="primary" :loading="completing" @click="submitComplete">确认补单</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
