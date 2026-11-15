<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { RefreshOutline, ReloadOutline, SearchOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { groupPurchasesApi } from '@/api/groupPurchases';
import type { GroupPurchase } from '@/api/groupPurchases';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

const message = useMessage();

const rows = ref<GroupPurchase[]>([]);
const loading = ref(false);
const merchantId = ref('');
const status = ref<string | null>(null);
const refundingId = ref('');

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待支付', value: 'pending' },
  { label: '已支付', value: 'paid' },
  { label: '已退款', value: 'refunded' },
];

const statusMap: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  pending: { text: '待支付', type: 'warning' },
  paid: { text: '已支付', type: 'success' },
  refunded: { text: '已退款', type: 'default' },
  failed: { text: '失败', type: 'error' },
};

async function load() {
  loading.value = true;
  try {
    const data = await groupPurchasesApi.list({
      page: page.value,
      page_size: pageSize.value,
      merchant_id: merchantId.value.trim() || undefined,
      status: status.value ?? undefined,
    });
    const items = itemsOf<GroupPurchase>(data);
    rows.value = items;
    total.value = numberOr(asRow(data).total, items.length);
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

function resetFilters() {
  merchantId.value = '';
  status.value = null;
  resetPage();
  void load();
}

async function refund(row: GroupPurchase) {
  refundingId.value = row.id;
  try {
    await groupPurchasesApi.refund(row.id);
    message.success('已提交退款');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    refundingId.value = '';
  }
}

function statusTag(value?: string) {
  const resolved = statusMap[value ?? ''] ?? { text: value || '-', type: 'default' as const };
  return h(
    NTag,
    { size: 'small', round: true, bordered: false, type: resolved.type },
    { default: () => resolved.text },
  );
}

const columns = computed<DataTableColumns<GroupPurchase>>(() => {
  const refunding = refundingId.value;
  return [
    {
      title: '商户 ID',
      key: 'merchant_id',
      width: 260,
      ellipsis: true,
      render: (row) => h(NText, { code: true }, { default: () => row.merchant_id || '-' }),
    },
    {
      title: '用户组 ID',
      key: 'group_id',
      width: 260,
      ellipsis: true,
      render: (row) => h(NText, { code: true }, { default: () => row.group_id || '-' }),
    },
    {
      title: '买断价',
      key: 'price_cents',
      width: 140,
      align: 'right',
      render: (row) =>
        h(NText, { strong: true }, { default: () => formatMoney(numberOr(row.price_cents, 0)) }),
    },
    { title: '状态', key: 'status', width: 110, render: (row) => statusTag(row.status) },
    {
      title: '购买时间',
      key: 'created_at',
      width: 180,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      fixed: 'right',
      render: (row) =>
        h(
          NPopconfirm,
          { onPositiveClick: () => refund(row) },
          {
            trigger: () =>
              h(
                NButton,
                {
                  size: 'tiny',
                  quaternary: true,
                  type: 'error',
                  loading: refunding === row.id,
                  disabled: row.status === 'refunded',
                },
                { default: () => '退款' },
              ),
            default: () => '确认退款该笔购买？退款后用户组关系会被撤销。',
          },
        ),
    },
  ];
});

onMounted(load);
</script>

<template>
  <admin-page title="用户组购买记录" subtitle="商户买断用户组的支付与退款记录">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        刷新
      </n-button>
    </template>

    <n-card size="small" :bordered="false">
      <n-space align="center" :size="12" style="margin-bottom: 12px">
        <n-input
          v-model:value="merchantId"
          placeholder="商户 ID"
          clearable
          style="width: 280px"
          @keyup.enter="search"
        />
        <n-select
          v-model:value="status"
          :options="statusOptions"
          placeholder="全部状态"
          clearable
          style="width: 160px"
        />
        <n-button type="primary" @click="search">
          <template #icon><n-icon><SearchOutline /></n-icon></template>
          查询
        </n-button>
        <n-button @click="resetFilters">
          <template #icon><n-icon><ReloadOutline /></n-icon></template>
          重置
        </n-button>
        <n-text depth="3">共 {{ total }} 条</n-text>
      </n-space>

      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: GroupPurchase) => row.id"
        :bordered="false"
        :single-line="false"
        :scroll-x="1200"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无购买记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
