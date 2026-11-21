<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminFundingApi } from '@/api/adminFunding';
import type { FundingOrder } from '@/api/adminFunding';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney, orderStatus, paymentTypeText } from '@/utils/format';
import { useNarrow } from './shared';

/**
 * 资金单据（充值 / 保证金）。
 * 后端只有两条只读列表：GET /admin/funding/recharge、GET /admin/funding/deposit，
 * 都只接受 page / page_size / merchant_id，资金动作在商户端发起。
 */
const message = useMessage();
const isNarrow = useNarrow();

const tab = ref<'recharge' | 'deposit'>('recharge');

const rechargeRows = ref<FundingOrder[]>([]);
const depositRows = ref<FundingOrder[]>([]);
const rechargeTotal = ref(0);
const depositTotal = ref(0);
const rechargePage = ref(1);
const depositPage = ref(1);
const pageSize = ref(20);
const rechargeLoading = ref(false);
const depositLoading = ref(false);

const merchantId = ref('');
const keyword = ref('');

const loading = computed(() => (tab.value === 'recharge' ? rechargeLoading.value : depositLoading.value));
const totalCount = computed(() => (tab.value === 'recharge' ? rechargeTotal.value : depositTotal.value));
const currentPage = computed(() => (tab.value === 'recharge' ? rechargePage.value : depositPage.value));

const pagination = computed(() => ({
  page: currentPage.value,
  pageSize: pageSize.value,
  itemCount: totalCount.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  prefix: () => `共 ${totalCount.value} 条`,
}));

/** 关键字只在本地过滤：后端 funding 列表没有 keyword 参数 */
function filterRows(rows: FundingOrder[]): FundingOrder[] {
  const keywordText = keyword.value.trim().toLowerCase();
  if (keywordText === '') return rows;
  return rows.filter((row) =>
    [row.trade_no, row.out_trade_no, row.name, row.channel_trade_no, row.attach]
      .filter((value): value is string => typeof value === 'string')
      .some((value) => value.toLowerCase().includes(keywordText)),
  );
}

const rechargeData = computed(() => filterRows(rechargeRows.value));
const depositData = computed(() => filterRows(depositRows.value));

async function loadRecharge() {
  rechargeLoading.value = true;
  try {
    const data = await adminFundingApi.recharge({
      page: rechargePage.value,
      page_size: pageSize.value,
      merchant_id: merchantId.value.trim() || undefined,
    });
    rechargeRows.value = data.items ?? [];
    rechargeTotal.value = data.total ?? rechargeRows.value.length;
  } catch (error) {
    rechargeRows.value = [];
    rechargeTotal.value = 0;
    message.error(errorMessage(error));
  } finally {
    rechargeLoading.value = false;
  }
}

async function loadDeposit() {
  depositLoading.value = true;
  try {
    const data = await adminFundingApi.deposit({
      page: depositPage.value,
      page_size: pageSize.value,
      merchant_id: merchantId.value.trim() || undefined,
    });
    depositRows.value = data.items ?? [];
    depositTotal.value = data.total ?? depositRows.value.length;
  } catch (error) {
    depositRows.value = [];
    depositTotal.value = 0;
    message.error(errorMessage(error));
  } finally {
    depositLoading.value = false;
  }
}

function loadCurrent() {
  if (tab.value === 'recharge') return loadRecharge();
  return loadDeposit();
}

function handleTab(value: string) {
  tab.value = value === 'deposit' ? 'deposit' : 'recharge';
  keyword.value = '';
  void loadCurrent();
}

function search() {
  if (tab.value === 'recharge') rechargePage.value = 1;
  else depositPage.value = 1;
  void loadCurrent();
}

function resetFilters() {
  merchantId.value = '';
  keyword.value = '';
  rechargePage.value = 1;
  depositPage.value = 1;
  void loadCurrent();
}

function onPage(value: number) {
  if (tab.value === 'recharge') rechargePage.value = value;
  else depositPage.value = value;
  void loadCurrent();
}

function onPageSize(value: number) {
  pageSize.value = value;
  rechargePage.value = 1;
  depositPage.value = 1;
  void loadCurrent();
}

const columns: DataTableColumns<FundingOrder> = [
  {
    title: '订单号',
    key: 'trade_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.trade_no || '-'),
  },
  {
    title: '商户订单号',
    key: 'out_trade_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono', depth: 3 }, () => row.out_trade_no || '-'),
  },
  { title: '商品名称', key: 'name', minWidth: 160, ellipsis: { tooltip: true }, render: (row) => row.name || '-' },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  { title: '币种', key: 'currency', width: 80, render: (row) => row.currency || '-' },
  {
    title: '支付方式',
    key: 'payment_type',
    width: 120,
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
    title: '通道单号',
    key: 'channel_trade_no',
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono', depth: 3 }, () => row.channel_trade_no || '-'),
  },
  {
    title: '支付时间',
    key: 'paid_at',
    width: 170,
    render: (row) => formatDateTime(row.paid_at),
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

onMounted(() => {
  void loadRecharge();
});
</script>

<template>
  <admin-page title="资金单据" subtitle="商户充值单与保证金单据（只读，资金动作在商户端发起）">
    <template #actions>
      <n-button :loading="loading" @click="loadCurrent">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新当前页签
      </n-button>
    </template>

    <n-card size="small" :bordered="false">
      <n-form :show-feedback="false" :label-placement="isNarrow ? 'top' : 'left'">
        <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12">
          <n-gi>
            <n-form-item label="商户 ID">
              <n-input
                v-model:value="merchantId"
                placeholder="按商户筛选"
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
            <n-form-item label="关键字">
              <n-input
                v-model:value="keyword"
                placeholder="订单号 / 商品名（当前页内过滤）"
                clearable
                style="width: 100%"
                @keyup.enter="search"
              />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item :show-label="false">
              <n-space :wrap="true">
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

      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="recharge" tab="充值单据">
          <n-data-table
            remote
            :columns="columns"
            :data="rechargeData"
            :loading="rechargeLoading"
            :pagination="pagination"
            :bordered="false"
            :row-key="(row: FundingOrder) => row.id"
            :scroll-x="1640"
            :size="isNarrow ? 'small' : 'medium'"
            @update:page="onPage"
            @update:page-size="onPageSize"
          >
            <template #empty>
              <n-empty description="暂无充值单据" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="deposit" tab="保证金单据">
          <n-data-table
            remote
            :columns="columns"
            :data="depositData"
            :loading="depositLoading"
            :pagination="pagination"
            :bordered="false"
            :row-key="(row: FundingOrder) => row.id"
            :scroll-x="1640"
            :size="isNarrow ? 'small' : 'medium'"
            @update:page="onPage"
            @update:page-size="onPageSize"
          >
            <template #empty>
              <n-empty description="暂无保证金单据" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>

      <n-text depth="3" class="gp-hint" style="display: block; margin-top: 10px">
        两条列表都只读；金额单位是分。关键字只在当前页内过滤，跨页搜索请用商户 ID。
      </n-text>
    </n-card>
  </admin-page>
</template>