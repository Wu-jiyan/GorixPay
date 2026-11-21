<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { RefreshOutline, ReloadOutline, SearchOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { groupPurchasesApi } from '@/api/groupPurchases';
import type { GroupPlan, GroupPurchase } from '@/api/groupPurchases';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney } from '@/utils/format';
import { asRow, itemsOf, numberOr, useNarrow, useServerPaging } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<GroupPurchase[]>([]);
const loading = ref(false);
const merchantId = ref('');
const status = ref<string | null>(null);
const refundingId = ref('');

const plans = ref<GroupPlan[]>([]);
const plansLoading = ref(false);

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

/** 套餐目录：GET /admin/group-plans，只读；改价走「用户组与费率」页 */
async function loadPlans() {
  plansLoading.value = true;
  try {
    plans.value = itemsOf<GroupPlan>(await groupPurchasesApi.plans());
  } catch (error) {
    plans.value = [];
    message.error(errorMessage(error));
  } finally {
    plansLoading.value = false;
  }
}

/** rate_bp 是万分比，展示成百分比 */
function rateText(rateBp?: number): string {
  const value = numberOr(rateBp, 0);
  return `${(value / 100).toFixed(2)}%`;
}

const planColumns: DataTableColumns<GroupPlan> = [
  {
    title: '套餐 ID',
    key: 'id',
    width: 240,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.id || '-'),
  },
  { title: '名称', key: 'name', minWidth: 160, ellipsis: { tooltip: true }, render: (row) => row.name || '-' },
  {
    title: '费率',
    key: 'rate_bp',
    width: 110,
    align: 'right',
    render: (row) => rateText(row.rate_bp),
  },
  {
    title: '买断价',
    key: 'price_cents',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(numberOr(row.price_cents, 0))),
  },
  {
    title: '结算方式',
    key: 'settle_mode',
    width: 130,
    render: (row) => row.settle_mode || '-',
  },
  { title: '状态', key: 'status', width: 110, render: (row) => statusTag(row.status) },
  {
    title: '更新时间',
    key: 'updated_at',
    width: 180,
    render: (row) => formatDateTime(row.updated_at ?? row.created_at),
  },
];

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

onMounted(() => {
  void load();
  void loadPlans();
});
</script>

<template>
  <admin-page title="用户组购买记录" subtitle="商户买断用户组的支付与退款记录">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
        <n-button :loading="plansLoading" secondary @click="loadPlans">
          <template #icon><n-icon><ReloadOutline /></n-icon></template>
          刷新套餐目录
        </n-button>
      </n-space>
    </template>

    <n-card size="small" :bordered="false" class="gp-section-gap">
      <template #header>套餐目录</template>
      <template #header-extra>
        <n-text depth="3" class="gp-hint">只读；改价请到「用户组与费率」页</n-text>
      </template>
      <n-data-table
        :columns="planColumns"
        :data="plans"
        :loading="plansLoading"
        :bordered="false"
        :pagination="false"
        :row-key="(row: GroupPlan) => row.id"
        :scroll-x="1100"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="暂无用户组套餐" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-card size="small" :bordered="false">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
        <n-gi>
          <n-input
            v-model:value="merchantId"
            placeholder="商户 ID"
            clearable
            style="width: 100%"
            @keyup.enter="search"
          />
        </n-gi>
        <n-gi>
          <n-select
            v-model:value="status"
            :options="statusOptions"
            placeholder="全部状态"
            clearable
            style="width: 100%"
          />
        </n-gi>
        <n-gi>
          <n-space :wrap="true">
            <n-button type="primary" @click="search">
              <template #icon><n-icon><SearchOutline /></n-icon></template>
              查询
            </n-button>
            <n-button @click="resetFilters">
              <template #icon><n-icon><ReloadOutline /></n-icon></template>
              重置
            </n-button>
          </n-space>
        </n-gi>
      </n-grid>

      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: GroupPurchase) => row.id"
        :bordered="false"
        :single-line="false"
        :scroll-x="1200"
        :size="isNarrow ? 'small' : 'medium'"
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
