<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import axios from 'axios';
import { useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { DepositInfo, Order } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatCard from '@/components/StatCard.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { copyText, formatDateTime, formatMoney, yuanToCents } from '@/utils/format';

interface FundingResult {
  trade_no: string;
  amount: number;
  cashier_url: string;
}

const message = useMessage();

const activeTab = ref<'recharge' | 'deposit'>('recharge');

const rechargeNotEnabled = ref(false);
const rechargeAmount = ref('');
const rechargeSubmitting = ref(false);
const rechargeResult = ref<FundingResult | null>(null);
const rechargeLoading = ref(true);
const rechargeOrders = ref<Order[]>([]);
const rechargeTotal = ref(0);
const rechargePage = ref(1);
const rechargePageSize = ref(10);

const depositNotEnabled = ref(false);
const depositInfo = ref<DepositInfo | null>(null);
const depositLoading = ref(true);
const depositAmount = ref('');
const depositSubmitting = ref(false);
const depositResult = ref<FundingResult | null>(null);
const depositOrdersLoading = ref(true);
const depositOrders = ref<Order[]>([]);
const depositTotal = ref(0);
const depositPage = ref(1);
const depositPageSize = ref(10);

const quickAmounts = [100, 500, 1000, 5000, 10000];

const depositShortfall = computed(() => {
  const balance = depositInfo.value?.deposit_balance ?? 0;
  const required = depositInfo.value?.required_cents ?? 0;
  return Math.max(0, required - balance);
});

const depositDisabled = computed(
  () => depositNotEnabled.value || (depositInfo.value ? depositInfo.value.enabled === false : false),
);

const rowKey = (row: Order) => row.id;

const orderColumns: DataTableColumns<Order> = [
  {
    title: '订单号',
    key: 'trade_no',
    width: 195,
    render: (row) => h('span', { class: 'mono-cell' }, row.trade_no),
  },
  { title: '商品名称', key: 'name', minWidth: 150, ellipsis: { tooltip: true } },
  {
    title: '金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => h(StatusTag, { status: row.status }),
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
];

const rechargePagination = computed(() => ({
  page: rechargePage.value,
  pageSize: rechargePageSize.value,
  itemCount: rechargeTotal.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

const depositPagination = computed(() => ({
  page: depositPage.value,
  pageSize: depositPageSize.value,
  itemCount: depositTotal.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

async function loadRechargeOrders() {
  rechargeLoading.value = true;
  try {
    const res = await merchantApi.rechargeOrders({ page: rechargePage.value, page_size: rechargePageSize.value });
    rechargeOrders.value = res.items;
    rechargeTotal.value = res.total;
    rechargeNotEnabled.value = false;
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      rechargeNotEnabled.value = true;
      rechargeOrders.value = [];
      rechargeTotal.value = 0;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    rechargeLoading.value = false;
  }
}

async function loadDepositInfo() {
  depositLoading.value = true;
  try {
    depositInfo.value = await merchantApi.deposit();
    depositNotEnabled.value = false;
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      depositNotEnabled.value = true;
      depositInfo.value = null;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    depositLoading.value = false;
  }
}

async function loadDepositOrders() {
  depositOrdersLoading.value = true;
  try {
    const res = await merchantApi.depositOrders({ page: depositPage.value, page_size: depositPageSize.value });
    depositOrders.value = res.items;
    depositTotal.value = res.total;
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      depositOrders.value = [];
      depositTotal.value = 0;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    depositOrdersLoading.value = false;
  }
}

async function submitRecharge() {
  const cents = yuanToCents(rechargeAmount.value);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的充值金额');
    return;
  }
  rechargeSubmitting.value = true;
  try {
    const res = await merchantApi.recharge(cents);
    rechargeResult.value = res;
    rechargeAmount.value = '';
    message.success('充值订单已创建，请在收银台完成支付');
    rechargePage.value = 1;
    await loadRechargeOrders();
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      rechargeNotEnabled.value = true;
      message.error('充值接口未启用，请联系平台运营');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    rechargeSubmitting.value = false;
  }
}

async function submitDeposit() {
  const cents = yuanToCents(depositAmount.value);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的保证金金额');
    return;
  }
  depositSubmitting.value = true;
  try {
    const res = await merchantApi.rechargeDeposit(cents);
    depositResult.value = res;
    depositAmount.value = '';
    message.success('保证金订单已创建，请在收银台完成支付');
    depositPage.value = 1;
    await Promise.all([loadDepositOrders(), loadDepositInfo()]);
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      depositNotEnabled.value = true;
      message.error('保证金接口未启用，请联系平台运营');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    depositSubmitting.value = false;
  }
}

async function handleCopy(url: string) {
  if (!url) return;
  const ok = await copyText(url);
  if (ok) message.success('收银台链接已复制');
  else message.error('复制失败，请手动选择链接复制');
}

function handleRechargePageChange(value: number) {
  rechargePage.value = value;
  void loadRechargeOrders();
}

function handleRechargePageSizeChange(value: number) {
  rechargePageSize.value = value;
  rechargePage.value = 1;
  void loadRechargeOrders();
}

function handleDepositPageChange(value: number) {
  depositPage.value = value;
  void loadDepositOrders();
}

function handleDepositPageSizeChange(value: number) {
  depositPageSize.value = value;
  depositPage.value = 1;
  void loadDepositOrders();
}

function refreshAll() {
  void loadRechargeOrders();
  void loadDepositInfo();
  void loadDepositOrders();
}

onMounted(() => {
  refreshAll();
});
</script>

<template>
  <div class="page">
    <PageHeader title="余额充值" subtitle="充值可用余额或缴纳保证金，支付成功后自动入账">
      <template #actions>
        <n-button size="small" :loading="rechargeLoading || depositLoading" @click="refreshAll">
          刷新
        </n-button>
      </template>
    </PageHeader>

    <n-tabs v-model:value="activeTab" type="line" animated>
      <n-tab-pane name="recharge" tab="余额充值">
        <n-alert v-if="rechargeNotEnabled" type="warning" :bordered="false" class="tips-alert">
          充值接口未启用：当前平台未开放自助充值，可稍后再试或联系平台运营。
        </n-alert>

        <div v-if="!rechargeNotEnabled" class="funding-grid">
          <n-card :bordered="false" class="panel-card" title="充值金额">
            <n-form label-placement="top">
              <n-form-item label="充值金额（元）">
                <n-input v-model:value="rechargeAmount" placeholder="请输入充值金额，最低 0.01 元">
                  <template #prefix>¥</template>
                </n-input>
              </n-form-item>
              <div class="quick-row">
                <n-button
                  v-for="item in quickAmounts"
                  :key="item"
                  size="small"
                  quaternary
                  @click="rechargeAmount = String(item)"
                >
                  ¥{{ item }}
                </n-button>
              </div>
            </n-form>
            <div class="submit-row">
              <n-button
                type="primary"
                :loading="rechargeSubmitting"
                :disabled="rechargeNotEnabled"
                @click="submitRecharge"
              >
                创建充值订单
              </n-button>
            </div>

            <div v-if="rechargeResult" class="result-box">
              <n-alert type="success" :bordered="false" title="充值订单已创建">
                订单号 <span class="mono-cell">{{ rechargeResult.trade_no }}</span>，金额
                {{ formatMoney(rechargeResult.amount) }}，请在收银台完成支付。
              </n-alert>
              <div class="result-actions">
                <n-button
                  type="primary"
                  tag="a"
                  :href="rechargeResult.cashier_url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  打开收银台支付
                </n-button>
                <n-button secondary @click="handleCopy(rechargeResult.cashier_url)">复制支付链接</n-button>
              </div>
              <pre class="url-text">{{ rechargeResult.cashier_url }}</pre>
            </div>
          </n-card>

          <n-card :bordered="false" class="panel-card" title="充值说明">
            <ul class="note-list">
              <li>充值订单通过平台收银台支付，支付成功后可立即用于结算、代付或认证费用。</li>
              <li>充值金额以订单创建时的金额为准，创建后不可修改。</li>
              <li>未支付的充值订单可以关闭或等待超时，不会产生扣款。</li>
              <li>充值订单不支持退款，如需调整请联系平台运营。</li>
            </ul>
          </n-card>
        </div>

        <n-card v-if="!rechargeNotEnabled" :bordered="false" class="table-card" title="充值订单">
          <n-data-table
            remote
            :columns="orderColumns"
            :data="rechargeOrders"
            :loading="rechargeLoading"
            :pagination="rechargePagination"
            :bordered="false"
            :row-key="rowKey"
            :scroll-x="900"
            size="small"
            @update:page="handleRechargePageChange"
            @update:page-size="handleRechargePageSizeChange"
          >
            <template #empty>
              <n-empty description="暂无充值订单" size="small" />
            </template>
          </n-data-table>
        </n-card>
      </n-tab-pane>

      <n-tab-pane name="deposit" tab="保证金">
        <n-alert v-if="depositNotEnabled" type="warning" :bordered="false" class="tips-alert">
          保证金接口未启用：当前平台未开放保证金功能，可稍后再试或联系平台运营。
        </n-alert>
        <n-alert
          v-else-if="depositInfo && depositInfo.enabled === false"
          type="info"
          :bordered="false"
          class="tips-alert"
        >
          保证金缴纳当前未开启，如需缴纳请联系平台运营。
        </n-alert>

        <n-spin v-if="!depositNotEnabled" :show="depositLoading">
          <div class="stat-grid">
            <StatCard
              label="保证金余额"
              :value="formatMoney(depositInfo?.deposit_balance)"
              hint="已入账的保证金合计"
              accent="#2563eb"
            />
            <StatCard
              label="保证金要求"
              :value="formatMoney(depositInfo?.required_cents)"
              :hint="depositShortfall > 0 ? `还需缴纳 ${formatMoney(depositShortfall)}` : '已满足保证金要求'"
              :accent="depositShortfall > 0 ? '#dc2626' : '#059669'"
            />
          </div>
        </n-spin>

        <n-alert v-if="!depositNotEnabled && depositShortfall > 0" type="warning" :bordered="false" class="tips-alert deposit-tip">
          保证金不足：当前还需缴纳 {{ formatMoney(depositShortfall) }}，未满足要求可能影响结算与代付。
        </n-alert>

        <div v-if="!depositNotEnabled" class="funding-grid">
          <n-card :bordered="false" class="panel-card" title="缴纳保证金">
            <n-form label-placement="top">
              <n-form-item label="缴纳金额（元）">
                <n-input v-model:value="depositAmount" placeholder="请输入缴纳金额">
                  <template #prefix>¥</template>
                </n-input>
              </n-form-item>
            </n-form>
            <div class="submit-row">
              <n-button
                type="primary"
                :loading="depositSubmitting"
                :disabled="depositDisabled"
                @click="submitDeposit"
              >
                创建保证金订单
              </n-button>
            </div>

            <div v-if="depositResult" class="result-box">
              <n-alert type="success" :bordered="false" title="保证金订单已创建">
                订单号 <span class="mono-cell">{{ depositResult.trade_no }}</span>，金额
                {{ formatMoney(depositResult.amount) }}，请在收银台完成支付。
              </n-alert>
              <div class="result-actions">
                <n-button
                  type="primary"
                  tag="a"
                  :href="depositResult.cashier_url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  打开收银台支付
                </n-button>
                <n-button secondary @click="handleCopy(depositResult.cashier_url)">复制支付链接</n-button>
              </div>
              <pre class="url-text">{{ depositResult.cashier_url }}</pre>
            </div>
          </n-card>

          <n-card :bordered="false" class="panel-card" title="保证金说明">
            <ul class="note-list">
              <li>保证金独立记账，不计入可用余额，用于风险保障。</li>
              <li>保证金订单同样走收银台支付，支付成功后由平台入账。</li>
              <li>保证金要求由平台统一配置，页面展示的金额为当前要求下限。</li>
              <li>保证金订单不支持退款，如需调整请联系平台运营。</li>
            </ul>
          </n-card>
        </div>

        <n-card v-if="!depositNotEnabled" :bordered="false" class="table-card" title="保证金订单">
          <n-data-table
            remote
            :columns="orderColumns"
            :data="depositOrders"
            :loading="depositOrdersLoading"
            :pagination="depositPagination"
            :bordered="false"
            :row-key="rowKey"
            :scroll-x="900"
            size="small"
            @update:page="handleDepositPageChange"
            @update:page-size="handleDepositPageSizeChange"
          >
            <template #empty>
              <n-empty description="暂无保证金订单" size="small" />
            </template>
          </n-data-table>
        </n-card>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<style scoped>
.tips-alert {
  margin-bottom: 16px;
}
.deposit-tip {
  margin-top: 16px;
}
.funding-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.panel-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.stat-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.quick-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: -8px;
}
.submit-row {
  margin-top: 16px;
}
.result-box {
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px dashed #e2e8f0;
}
.result-actions {
  margin-top: 12px;
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.url-text {
  margin: 12px 0 0;
  padding: 10px 12px;
  background: #f1f5f9;
  border-radius: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11.5px;
  color: #334155;
  white-space: pre-wrap;
  word-break: break-all;
}
.note-list {
  margin: 0;
  padding-left: 18px;
  color: #64748b;
  font-size: 13px;
  line-height: 1.9;
}
.note-list li {
  margin-bottom: 6px;
}
.table-card {
  margin-top: 16px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
@media (max-width: 1000px) {
  .funding-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
