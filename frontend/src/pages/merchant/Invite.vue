<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import axios from 'axios';
import { useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { InviteInfo, InviteRecord, PageQuery } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatCard from '@/components/StatCard.vue';
import MoneyText from '@/components/MoneyText.vue';
import { copyText, formatDateTime, formatMoney } from '@/utils/format';

const message = useMessage();

const infoLoading = ref(true);
const info = ref<InviteInfo | null>(null);
const notEnabled = ref(false);

const recordsLoading = ref(false);
const records = ref<InviteRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const ratePercent = computed(() => (((info.value?.rate_bp ?? 0) / 100).toFixed(2)));
const rowKey = (row: InviteRecord) => row.id;

const columns: DataTableColumns<InviteRecord> = [
  {
    title: '被邀请商户',
    key: 'invitee_name',
    minWidth: 160,
    render: (row) => row.invitee_name || row.invitee_id || '-',
  },
  {
    title: '订单号',
    key: 'trade_no',
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.trade_no),
  },
  {
    title: '订单金额',
    key: 'order_amount',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.order_amount }),
  },
  {
    title: '返佣金额',
    key: 'rebate_amount',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.rebate_amount, color: '#059669' }),
  },
  {
    title: '返佣比例',
    key: 'rate_bp',
    width: 110,
    render: (row) => `${(row.rate_bp / 100).toFixed(2)}%`,
  },
  {
    title: '时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
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

async function loadInfo() {
  infoLoading.value = true;
  try {
    info.value = await merchantApi.invite();
    notEnabled.value = false;
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      notEnabled.value = true;
      info.value = null;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    infoLoading.value = false;
  }
}

async function loadRecords() {
  recordsLoading.value = true;
  try {
    const query: PageQuery = { page: page.value, page_size: pageSize.value };
    const res = await merchantApi.inviteRecords(query);
    records.value = res.items;
    total.value = res.total;
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      records.value = [];
      total.value = 0;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    recordsLoading.value = false;
  }
}

async function handleCopy(text: string, label: string) {
  if (!text) {
    message.warning(`${label}为空，暂时无法复制`);
    return;
  }
  const ok = await copyText(text);
  if (ok) message.success(`${label}已复制`);
  else message.error('复制失败，请手动选择文本复制');
}

function handlePageChange(value: number) {
  page.value = value;
  void loadRecords();
}

function handlePageSizeChange(value: number) {
  pageSize.value = value;
  page.value = 1;
  void loadRecords();
}

onMounted(() => {
  void loadInfo();
  void loadRecords();
});
</script>

<template>
  <div class="page">
    <PageHeader title="邀请返佣" subtitle="邀请商户注册，被邀请商户每笔成功订单按比例返佣到可用余额">
      <template #actions>
        <n-button size="small" :loading="infoLoading" @click="loadInfo">刷新</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="notEnabled" type="warning" :bordered="false" class="tips-alert">
      邀请返佣接口未启用：当前平台未开放邀请返佣，可稍后再试或联系平台运营。
    </n-alert>
    <n-alert v-else-if="info && !info.enabled" type="info" :bordered="false" class="tips-alert">
      邀请返佣当前未开启：邀请码仍可分享，但平台开启返佣前不会产生收益。
    </n-alert>

    <div class="stat-grid">
      <StatCard label="邀请人数" :value="info?.invited_count ?? 0" hint="通过你的邀请码注册的商户" />
      <StatCard label="返佣比例" :value="`${ratePercent}%`" hint="按被邀请商户订单金额计算" accent="#2563eb" />
      <StatCard label="今日收益" :value="formatMoney(info?.today_income)" hint="今日支付的订单返佣" accent="#059669" />
      <StatCard label="累计收益" :value="formatMoney(info?.total_income)" hint="历史返佣合计" accent="#7c3aed" />
    </div>

    <n-card :bordered="false" class="invite-card" title="邀请链接">
      <n-spin :show="infoLoading">
        <div class="invite-rows">
          <div class="invite-row">
            <span class="invite-label">邀请码</span>
            <div class="invite-value">
              <span class="mono invite-code">{{ info?.invite_code || '-' }}</span>
              <n-button size="tiny" secondary @click="handleCopy(info?.invite_code ?? '', '邀请码')">复制</n-button>
            </div>
          </div>
          <div class="invite-row">
            <span class="invite-label">邀请链接</span>
            <div class="invite-value">
              <span class="invite-url">{{ info?.invite_url || '-' }}</span>
              <n-button size="tiny" secondary @click="handleCopy(info?.invite_url ?? '', '邀请链接')">复制</n-button>
            </div>
          </div>
        </div>
        <p class="invite-tip">
          把邀请链接发给朋友，对方注册时自动绑定邀请关系；被邀请商户订单支付成功后，按比例返佣到你的可用余额。
        </p>
      </n-spin>
    </n-card>

    <n-card :bordered="false" class="table-card" title="返佣记录">
      <n-data-table
        remote
        :columns="columns"
        :data="records"
        :loading="recordsLoading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="980"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无返佣记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
.tips-alert {
  margin-bottom: 16px;
}
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}
.invite-card {
  margin-top: 16px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.invite-rows {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.invite-row {
  display: flex;
  gap: 16px;
  align-items: center;
  flex-wrap: wrap;
}
.invite-label {
  width: 70px;
  flex: none;
  color: #94a3b8;
  font-size: 12.5px;
}
.invite-value {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
}
.invite-code {
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 1px;
  color: #1d4ed8;
}
.invite-url {
  font-size: 12.5px;
  color: #334155;
  word-break: break-all;
}
.invite-tip {
  margin: 14px 0 0;
  font-size: 12.5px;
  color: #64748b;
  line-height: 1.8;
}
.table-card {
  margin-top: 16px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
@media (max-width: 1100px) {
  .stat-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .invite-label {
    width: auto;
  }
}
</style>
