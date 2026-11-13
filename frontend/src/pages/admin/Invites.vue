<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { adminApi } from '@/api';
import type { InviteRecord } from '@/api';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import { formatDateTime } from '@/utils/format';
import { httpStatus, itemsOf, numberOr, useServerPaging } from './shared';

const message = useMessage();

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

const columns = computed<DataTableColumns<InviteRecord>>(() => [
  {
    title: '邀请人 ID',
    key: 'inviter_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.inviter_id || '-'),
  },
  {
    title: '被邀请人 ID',
    key: 'invitee_id',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.invitee_id || '-'),
  },
  {
    title: '被邀请人',
    key: 'invitee_name',
    minWidth: 140,
    ellipsis: true,
    render: (row) => row.invitee_name || '-',
  },
  {
    title: '订单号',
    key: 'trade_no',
    width: 200,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.trade_no || '-'),
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
    render: (row) => h(MoneyText, { amount: row.rebate_amount, color: '#0f766e' }),
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
  <div class="page">
    <PageHeader title="返佣记录" subtitle="邀请返佣明细：按订单金额与万分比比例计算">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="unavailable" type="warning" :bordered="false" style="margin-bottom: 16px">
      返佣接口未启用：后端 <span class="mono">/admin/invites</span> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <div class="toolbar">
      <n-input
        v-model:value="inviterId"
        placeholder="邀请人商户 ID"
        clearable
        style="width: 280px"
        @keyup.enter="search"
      />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
      <span class="muted" style="font-size: 12px">共 {{ total }} 条</span>
    </div>

    <n-card :bordered="false">
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
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无返佣记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>
