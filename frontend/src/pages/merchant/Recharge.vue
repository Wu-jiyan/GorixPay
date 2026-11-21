<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import axios from 'axios';
import { NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { CardOutline, CopyOutline, LockClosedOutline, OpenOutline, RefreshOutline, WalletOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { DepositInfo, Order } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { copyText, formatDateTime, formatMoney, orderStatus, yuanToCents } from '@/utils/format';
import { useNarrow } from './shared';

interface FundingResult {
  trade_no: string;
  amount: number;
  cashier_url: string;
}

const message = useMessage();
const isNarrow = useNarrow();

const activeTab = ref<'recharge' | 'deposit'>('recharge');

const rechargeNotEnabled = ref(false);
const rechargeAmount = ref<number | null>(null);
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
const depositAmount = ref<number | null>(null);
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
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.trade_no || '-'),
  },
  { title: '商品名称', key: 'name', minWidth: 160, ellipsis: { tooltip: true } },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
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
  const cents = yuanToCents(rechargeAmount.value ?? 0);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的充值金额');
    return;
  }
  rechargeSubmitting.value = true;
  try {
    const res = await merchantApi.recharge(cents);
    rechargeResult.value = res;
    rechargeAmount.value = null;
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
  const cents = yuanToCents(depositAmount.value ?? 0);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的保证金金额');
    return;
  }
  depositSubmitting.value = true;
  try {
    const res = await merchantApi.rechargeDeposit(cents);
    depositResult.value = res;
    depositAmount.value = null;
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
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
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
  <admin-page title="余额充值" subtitle="充值可用余额或缴纳保证金，支付成功后自动入账">
    <template #actions>
      <n-button :loading="rechargeLoading || depositLoading" @click="refreshAll">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-card size="small" :bordered="false">
      <n-tabs v-model:value="activeTab" type="line" animated>
        <n-tab-pane name="recharge" tab="余额充值">
          <n-alert v-if="rechargeNotEnabled" type="warning" :bordered="false" style="margin-bottom: 16px">
            充值接口未启用：当前平台未开放自助充值，可稍后再试或联系平台运营。
          </n-alert>

          <template v-if="!rechargeNotEnabled">
            <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16">
              <n-gi>
                <n-card size="small" :bordered="false" title="充值金额">
                  <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
                    <n-form-item label="充值金额（元）">
                      <n-input-number
                        v-model:value="rechargeAmount"
                        :precision="2"
                        :min="0.01"
                        placeholder="请输入充值金额，最低 0.01 元"
                        style="width: 100%"
                      >
                        <template #prefix>¥</template>
                      </n-input-number>
                    </n-form-item>
                    <n-form-item label="快捷金额">
                      <n-space :wrap="true">
                        <n-button
                          v-for="item in quickAmounts"
                          :key="item"
                          size="small"
                          secondary
                          @click="rechargeAmount = item"
                        >
                          ¥{{ item }}
                        </n-button>
                      </n-space>
                    </n-form-item>
                    <n-form-item>
                      <n-button type="primary" :loading="rechargeSubmitting" @click="submitRecharge">
                        <template #icon>
                          <n-icon :component="CardOutline" />
                        </template>
                        创建充值订单
                      </n-button>
                    </n-form-item>
                  </n-form>

                  <n-space v-if="rechargeResult" vertical :size="12">
                    <n-alert type="success" :bordered="false" title="充值订单已创建">
                      订单号 <n-text class="mono">{{ rechargeResult.trade_no }}</n-text>，金额
                      {{ formatMoney(rechargeResult.amount) }}，请在收银台完成支付。
                    </n-alert>
                    <n-input :value="rechargeResult.cashier_url" readonly>
                      <template #suffix>
                        <n-button text type="primary" @click="handleCopy(rechargeResult.cashier_url)">
                          <template #icon>
                            <n-icon :component="CopyOutline" />
                          </template>
                          复制
                        </n-button>
                      </template>
                    </n-input>
                    <n-space :wrap="true">
                      <n-button
                        type="primary"
                        tag="a"
                        :href="rechargeResult.cashier_url"
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        <template #icon>
                          <n-icon :component="OpenOutline" />
                        </template>
                        打开收银台支付
                      </n-button>
                      <n-button secondary @click="handleCopy(rechargeResult.cashier_url)">复制支付链接</n-button>
                    </n-space>
                  </n-space>
                </n-card>
              </n-gi>

              <n-gi>
                <n-card size="small" :bordered="false" title="充值说明">
                  <n-list>
                    <n-list-item>充值订单通过平台收银台支付，支付成功后可立即用于结算、代付或认证费用。</n-list-item>
                    <n-list-item>充值金额以订单创建时的金额为准，创建后不可修改。</n-list-item>
                    <n-list-item>未支付的充值订单可以关闭或等待超时，不会产生扣款。</n-list-item>
                    <n-list-item>充值订单不支持退款，如需调整请联系平台运营。</n-list-item>
                  </n-list>
                </n-card>
              </n-gi>
            </n-grid>

            <n-card size="small" :bordered="false" title="充值订单" style="margin-top: 16px">
              <n-data-table
                remote
                :columns="orderColumns"
                :data="rechargeOrders"
                :loading="rechargeLoading"
                :pagination="rechargePagination"
                :bordered="false"
                :row-key="rowKey"
                :scroll-x="940"
                :size="isNarrow ? 'small' : 'medium'"
                @update:page="handleRechargePageChange"
                @update:page-size="handleRechargePageSizeChange"
              >
                <template #empty>
                  <n-empty description="暂无充值订单" size="small" />
                </template>
              </n-data-table>
            </n-card>
          </template>
        </n-tab-pane>

        <n-tab-pane name="deposit" tab="保证金">
          <n-alert v-if="depositNotEnabled" type="warning" :bordered="false" style="margin-bottom: 16px">
            保证金接口未启用：当前平台未开放保证金功能，可稍后再试或联系平台运营。
          </n-alert>
          <n-alert
            v-else-if="depositInfo && depositInfo.enabled === false"
            type="info"
            :bordered="false"
            style="margin-bottom: 16px"
          >
            保证金缴纳当前未开启，如需缴纳请联系平台运营。
          </n-alert>

          <template v-if="!depositNotEnabled">
            <n-spin :show="depositLoading">
              <n-grid cols="1 s:2" responsive="screen" :x-gap="16" :y-gap="16">
                <n-gi>
                  <n-card size="small" :bordered="false">
                    <n-statistic label="保证金余额" :value="formatMoney(depositInfo?.deposit_balance)">
                      <template #prefix>
                        <n-icon :component="WalletOutline" />
                      </template>
                    </n-statistic>
                    <n-text depth="3" class="gp-hint">已入账的保证金合计</n-text>
                  </n-card>
                </n-gi>
                <n-gi>
                  <n-card size="small" :bordered="false">
                    <n-statistic label="保证金要求" :value="formatMoney(depositInfo?.required_cents)">
                      <template #prefix>
                        <n-icon :component="LockClosedOutline" />
                      </template>
                    </n-statistic>
                    <n-text depth="3" class="gp-hint">
                      {{ depositShortfall > 0 ? `还需缴纳 ${formatMoney(depositShortfall)}` : '已满足保证金要求' }}
                    </n-text>
                  </n-card>
                </n-gi>
              </n-grid>
            </n-spin>

            <n-alert
              v-if="depositShortfall > 0"
              type="warning"
              :bordered="false"
              style="margin-top: 16px"
            >
              保证金不足：当前还需缴纳 {{ formatMoney(depositShortfall) }}，未满足要求可能影响结算与代付。
            </n-alert>

            <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16" style="margin-top: 16px">
              <n-gi>
                <n-card size="small" :bordered="false" title="缴纳保证金">
                  <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
                    <n-form-item label="缴纳金额（元）">
                      <n-input-number
                        v-model:value="depositAmount"
                        :precision="2"
                        :min="0.01"
                        placeholder="请输入缴纳金额"
                        style="width: 100%"
                      >
                        <template #prefix>¥</template>
                      </n-input-number>
                    </n-form-item>
                    <n-form-item>
                      <n-button type="primary" :loading="depositSubmitting" :disabled="depositDisabled" @click="submitDeposit">
                        创建保证金订单
                      </n-button>
                    </n-form-item>
                  </n-form>

                  <n-space v-if="depositResult" vertical :size="12">
                    <n-alert type="success" :bordered="false" title="保证金订单已创建">
                      订单号 <n-text class="mono">{{ depositResult.trade_no }}</n-text>，金额
                      {{ formatMoney(depositResult.amount) }}，请在收银台完成支付。
                    </n-alert>
                    <n-input :value="depositResult.cashier_url" readonly>
                      <template #suffix>
                        <n-button text type="primary" @click="handleCopy(depositResult.cashier_url)">
                          <template #icon>
                            <n-icon :component="CopyOutline" />
                          </template>
                          复制
                        </n-button>
                      </template>
                    </n-input>
                    <n-space :wrap="true">
                      <n-button
                        type="primary"
                        tag="a"
                        :href="depositResult.cashier_url"
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        <template #icon>
                          <n-icon :component="OpenOutline" />
                        </template>
                        打开收银台支付
                      </n-button>
                      <n-button secondary @click="handleCopy(depositResult.cashier_url)">复制支付链接</n-button>
                    </n-space>
                  </n-space>
                </n-card>
              </n-gi>

              <n-gi>
                <n-card size="small" :bordered="false" title="保证金说明">
                  <n-list>
                    <n-list-item>保证金独立记账，不计入可用余额，用于风险保障。</n-list-item>
                    <n-list-item>保证金订单同样走收银台支付，支付成功后由平台入账。</n-list-item>
                    <n-list-item>保证金要求由平台统一配置，页面展示的金额为当前要求下限。</n-list-item>
                    <n-list-item>保证金订单不支持退款，如需调整请联系平台运营。</n-list-item>
                  </n-list>
                </n-card>
              </n-gi>
            </n-grid>

            <n-card size="small" :bordered="false" title="保证金订单" style="margin-top: 16px">
              <n-data-table
                remote
                :columns="orderColumns"
                :data="depositOrders"
                :loading="depositOrdersLoading"
                :pagination="depositPagination"
                :bordered="false"
                :row-key="rowKey"
                :scroll-x="940"
                :size="isNarrow ? 'small' : 'medium'"
                @update:page="handleDepositPageChange"
                @update:page-size="handleDepositPageSizeChange"
              >
                <template #empty>
                  <n-empty description="暂无保证金订单" size="small" />
                </template>
              </n-data-table>
            </n-card>
          </template>
        </n-tab-pane>
      </n-tabs>
    </n-card>
  </admin-page>
</template>
