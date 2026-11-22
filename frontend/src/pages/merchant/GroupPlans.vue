<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { PricetagsOutline, RefreshOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { Merchant } from '@/api/types';
import { merchantGroupPlanApi, type GroupPlan } from '@/api/merchantGroupPlans';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatMoney, paymentTypeText } from '@/utils/format';
import { useNarrow } from './shared';

// 商户表可能已经带了用户组字段（M1 加的），类型文件不归我改，这里做可选扩展。
interface ProfileWithGroup extends Merchant {
  group_id?: string | null;
  group_name?: string | null;
  rate_bp?: number | null;
  settle_mode?: string | null;
}

const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(true);
const plans = ref<GroupPlan[]>([]);
const profile = ref<ProfileWithGroup | null>(null);
const buying = ref('');

const currentGroupName = computed(() => {
  const data = profile.value;
  if (!data) return '-';
  return data.group_name || (data.group_id ? '已分配用户组' : '未分配用户组');
});

const rateText = computed(() => {
  const rate = profile.value?.rate_bp;
  if (rate === null || rate === undefined) return '-';
  return `${(rate / 100).toFixed(2)}%`;
});

const settleModeText = computed(() => {
  const mode = profile.value?.settle_mode;
  if (mode === 'direct') return '直连结算';
  if (mode === 'normal') return '普通结算';
  return '-';
});

const hasGroupInfo = computed(() => {
  const data = profile.value;
  return Boolean(data && (data.group_id || data.group_name || data.rate_bp !== undefined));
});

function payTypesText(plan: GroupPlan): string {
  const types = plan.pay_types ?? [];
  if (types.length === 0) return '不限';
  return types.map((type) => paymentTypeText(type)).join(' / ');
}

async function load() {
  loading.value = true;
  try {
    const [planList, profileData] = await Promise.all([
      merchantGroupPlanApi.plans(),
      merchantApi.profile().catch(() => null),
    ]);
    plans.value = planList;
    profile.value = (profileData as ProfileWithGroup | null) ?? null;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function purchase(plan: GroupPlan) {
  if (buying.value) return;
  buying.value = plan.group_id;
  try {
    await merchantGroupPlanApi.purchase(plan.group_id);
    message.success(`已购买「${plan.name}」，费率已更新`);
    await load();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    buying.value = '';
  }
}

const rowKey = (row: GroupPlan) => row.group_id;

const columns: DataTableColumns<GroupPlan> = [
  { title: '用户组', key: 'name', minWidth: 160 },
  {
    title: '费率',
    key: 'rate_bp',
    width: 110,
    align: 'right',
    render: (row) => `${((row.rate_bp ?? 0) / 100).toFixed(2)}%`,
  },
  {
    title: '可用支付方式',
    key: 'pay_types',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) => payTypesText(row),
  },
  {
    title: '价格',
    key: 'price_cents',
    width: 130,
    align: 'right',
    render: (row) =>
      row.price_cents > 0
        ? h(NText, { class: 'amount' }, () => formatMoney(row.price_cents))
        : h(NTag, { size: 'small', type: 'success', bordered: false }, () => '免费'),
  },
  {
    title: '操作',
    key: 'actions',
    width: 130,
    fixed: 'right',
    render: (row) => {
      if (profile.value?.group_id && profile.value.group_id === row.group_id) {
        return h(NTag, { size: 'small', type: 'info', bordered: false }, () => '当前用户组');
      }
      return h(
        NPopconfirm,
        { onPositiveClick: () => void purchase(row), positiveText: '购买', negativeText: '取消' },
        {
          trigger: () =>
            h(
              NButton,
              { size: 'small', type: 'primary', loading: buying.value === row.group_id },
              () => '购买',
            ),
          default: () =>
            `确认购买「${row.name}」？费用 ${row.price_cents > 0 ? formatMoney(row.price_cents) : '免费'}，将从可用余额扣除。`,
        },
      );
    },
  },
];

onMounted(() => {
  void load();
});
</script>

<template>
  <admin-page title="用户组" subtitle="按费率与支付方式选择用户组，购买后立即生效">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-card :bordered="false" title="当前用户组">
      <n-spin :show="loading">
        <n-descriptions :column="isNarrow ? 1 : 3" bordered size="small" label-placement="top">
          <n-descriptions-item label="用户组">{{ currentGroupName }}</n-descriptions-item>
          <n-descriptions-item label="费率">{{ rateText }}</n-descriptions-item>
          <n-descriptions-item label="结算模式">{{ settleModeText }}</n-descriptions-item>
        </n-descriptions>
        <n-text v-if="!hasGroupInfo" depth="3" class="gp-hint" style="display: block; margin-top: 12px">
          平台暂未返回当前用户组信息，以下只展示可购买的用户组。
        </n-text>
      </n-spin>
    </n-card>

    <n-card :bordered="false" title="可购买用户组" class="gp-section-gap">
      <n-data-table
        :columns="columns"
        :data="plans"
        :loading="loading"
        :bordered="false"
        :row-key="rowKey"
        :pagination="false"
        :scroll-x="880"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="暂无可购买的用户组" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
