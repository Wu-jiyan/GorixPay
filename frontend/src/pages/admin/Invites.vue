<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NText, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { LinkOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { InviteRecord } from '@/api';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney } from '@/utils/format';
import { httpStatus, itemsOf, numberOr, useNarrow, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<InviteRecord[]>([]);
const loading = ref(false);
const unavailable = ref(false);
const inviterId = ref('');

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.invites({
      page: page.value,
      page_size: pageSize.value,
      inviter_id: inviterId.value.trim() || undefined,
    });
    const items = itemsOf<InviteRecord>(data.items);
    rows.value = items;
    total.value = numberOr(data.total, items.length);
    unavailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      rows.value = [];
      total.value = 0;
      unavailable.value = true;
      return;
    }
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function search() {
  resetPage();
  void load();
}

function resetFilters() {
  inviterId.value = '';
  resetPage();
  void load();
}

function rateText(rateBp?: number): string {
  if (typeof rateBp !== 'number' || !Number.isFinite(rateBp)) return '-';
  return `${(rateBp / 100).toFixed(2)}%`;
}

function monoNode(value: string) {
  return h('span', { class: 'mono' }, value || '-');
}

function moneyNode(amount: number | null, type?: 'success' | 'error') {
  return h(NText, { class: 'amount', type }, { default: () => formatMoney(amount) });
}

const columns = computed<DataTableColumns<InviteRecord>>(() => [
  {
    title: '邀请人 ID',
    key: 'inviter_id',
    width: 250,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(row.inviter_id),
  },
  {
    title: '被邀请人 ID',
    key: 'invitee_id',
    width: 250,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(row.invitee_id),
  },
  {
    title: '被邀请人',
    key: 'invitee_name',
    minWidth: 140,
    ellipsis: { tooltip: true },
    render: (row) => row.invitee_name || '-',
  },
  {
    title: '订单号',
    key: 'trade_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => monoNode(row.trade_no),
  },
  {
    title: '订单金额',
    key: 'order_amount',
    width: 140,
    align: 'right',
    render: (row) => moneyNode(typeof row.order_amount === 'number' ? row.order_amount : null),
  },
  {
    title: '返佣金额',
    key: 'rebate_amount',
    width: 140,
    align: 'right',
    render: (row) => moneyNode(typeof row.rebate_amount === 'number' ? row.rebate_amount : null, 'success'),
  },
  {
    title: '返佣比例',
    key: 'rate_bp',
    width: 110,
    align: 'right',
    render: (row) => rateText(row.rate_bp),
  },
  {
    title: '返佣时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
]);

onMounted(load);
</script>

<template>
  <admin-page title="返佣记录" subtitle="邀请返佣明细：按订单金额与万分比比例计算">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-alert v-if="unavailable" type="warning" :bordered="false" style="margin-bottom: 16px">
      返佣接口未启用：后端 <span class="mono">/admin/invites</span> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <n-card size="small" :bordered="false">
      <template #header>
        <n-space :size="6" align="center">
          <n-icon :component="LinkOutline" />
          <span>邀请返佣明细</span>
        </n-space>
      </template>

      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
        <n-gi>
          <n-input
            v-model:value="inviterId"
            placeholder="邀请人商户 ID"
            clearable
            style="width: 100%"
            @keyup.enter="search"
          >
            <template #prefix>
              <n-icon :component="SearchOutline" />
            </template>
          </n-input>
        </n-gi>
        <n-gi>
          <n-space :wrap="true">
            <n-button type="primary" @click="search">查询</n-button>
            <n-button @click="resetFilters">重置</n-button>
          </n-space>
        </n-gi>
        <n-gi>
          <n-text depth="3" style="font-size: 12px">共 {{ total }} 条</n-text>
        </n-gi>
      </n-grid>

      <n-empty v-if="unavailable" description="接口未启用" size="large">
        <template #extra>
          <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
        </template>
      </n-empty>
      <n-data-table
        v-else
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: InviteRecord) => row.id"
        :scroll-x="1560"
        :size="isNarrow ? 'small' : 'medium'"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无返佣记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
