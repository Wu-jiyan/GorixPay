<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import axios from 'axios';
import { NText, useMessage, type DataTableColumns } from 'naive-ui';
import { CopyOutline, LinkOutline, PeopleOutline, RefreshOutline, TrendingUpOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { InviteInfo, InviteRecord, PageQuery } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { copyText, formatDateTime, formatMoney } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const infoLoading = ref(true);
const info = ref<InviteInfo | null>(null);
const notEnabled = ref(false);

const recordsLoading = ref(false);
const records = ref<InviteRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const ratePercent = computed(() => (info.value?.rate_bp ?? 0) / 100);
const rowKey = (row: InviteRecord) => row.id;

const columns: DataTableColumns<InviteRecord> = [
  {
    title: '被邀请商户',
    key: 'invitee_name',
    minWidth: 170,
    render: (row) => row.invitee_name || row.invitee_id || '-',
  },
  {
    title: '订单号',
    key: 'trade_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.trade_no || '-'),
  },
  {
    title: '订单金额',
    key: 'order_amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.order_amount)),
  },
  {
    title: '返佣金额',
    key: 'rebate_amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount', type: 'success' }, () => formatMoney(row.rebate_amount)),
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
  if (ok) message.success('已复制到剪贴板');
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
  <admin-page title="邀请返佣" subtitle="邀请商户注册，被邀请商户每笔成功订单按比例返佣到可用余额">
    <template #actions>
      <n-button :loading="infoLoading" @click="loadInfo">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-alert v-if="notEnabled" type="warning" :bordered="false" style="margin-bottom: 16px">
      邀请返佣接口未启用：当前平台未开放邀请返佣，可稍后再试或联系平台运营。
    </n-alert>
    <n-alert v-else-if="info && !info.enabled" type="info" :bordered="false" style="margin-bottom: 16px">
      邀请返佣当前未开启：邀请码仍可分享，但平台开启返佣前不会产生收益。
    </n-alert>

    <n-spin :show="infoLoading">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="邀请人数" :value="info?.invited_count ?? 0">
              <template #prefix>
                <n-icon :component="PeopleOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">通过你的邀请码注册的商户</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="返佣比例" :value="`${ratePercent.toFixed(2)}%`">
              <template #prefix>
                <n-icon :component="TrendingUpOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">按被邀请商户订单金额计算</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="今日收益" :value="formatMoney(info?.today_income)">
              <template #prefix>
                <n-icon :component="TrendingUpOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">今日支付的订单返佣</n-text>
          </n-card>
        </n-gi>
        <n-gi>
          <n-card size="small" :bordered="false">
            <n-statistic label="累计收益" :value="formatMoney(info?.total_income)">
              <template #prefix>
                <n-icon :component="TrendingUpOutline" />
              </template>
            </n-statistic>
            <n-text depth="3" class="gp-hint">历史返佣合计</n-text>
          </n-card>
        </n-gi>
      </n-grid>
    </n-spin>

    <n-card :bordered="false" title="邀请链接" class="gp-section-gap">
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="邀请码">
          <n-input :value="info?.invite_code || ''" readonly placeholder="暂未生成邀请码">
            <template #prefix>
              <n-icon :component="LinkOutline" />
            </template>
            <template #suffix>
              <n-button text type="primary" @click="handleCopy(info?.invite_code ?? '', '邀请码')">
                <template #icon>
                  <n-icon :component="CopyOutline" />
                </template>
                复制
              </n-button>
            </template>
          </n-input>
        </n-form-item>
        <n-form-item label="邀请链接">
          <n-input :value="info?.invite_url || ''" readonly placeholder="暂未生成邀请链接">
            <template #prefix>
              <n-icon :component="LinkOutline" />
            </template>
            <template #suffix>
              <n-button text type="primary" @click="handleCopy(info?.invite_url ?? '', '邀请链接')">
                <template #icon>
                  <n-icon :component="CopyOutline" />
                </template>
                复制
              </n-button>
            </template>
          </n-input>
        </n-form-item>
      </n-form>
      <n-text depth="2" style="font-size: 12.5px">
        把邀请链接发给朋友，对方注册时自动绑定邀请关系；被邀请商户订单支付成功后，按比例返佣到你的可用余额。
      </n-text>
    </n-card>

    <n-card :bordered="false" title="返佣记录" class="gp-section-gap">
      <n-data-table
        remote
        :columns="columns"
        :data="records"
        :loading="recordsLoading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="980"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无返佣记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
