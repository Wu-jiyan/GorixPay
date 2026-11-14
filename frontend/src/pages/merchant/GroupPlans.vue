<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NTag, useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { Merchant } from '@/api/types';
import { merchantGroupPlanApi, type GroupPlan } from '@/api/merchantGroupPlans';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatMoney, paymentTypeText } from '@/utils/format';

// 商户表可能已经带了用户组字段（M1 加的），类型文件不归我改，这里做可选扩展。
interface ProfileWithGroup extends Merchant {
  group_id?: string | null;
  group_name?: string | null;
  rate_bp?: number | null;
  settle_mode?: string | null;
}

const message = useMessage();

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

const columns: DataTableColumns<GroupPlan> = [  { title: '用户组', key: 'name', minWidth: 160 },
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
    render: (row) => payTypesText(row),
  },
  {
    title: '价格',
    key: 'price_cents',
    width: 130,
    align: 'right',
    render: (row) =>
      row.price_cents > 0
        ? h(MoneyText, { amount: row.price_cents })
        : h(NTag, { size: 'small', type: 'success', bordered: false }, () => '免费'),
  },
  {
    title: '操作',
    key: 'actions',
    width: 120,
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
  <div class="page">
    <PageHeader title="用户组" subtitle="按费率与支付方式选择用户组，购买后立即生效">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="info-card" title="当前用户组">
      <n-spin :show="loading">
        <div class="info-grid">
          <div class="info-item">
            <span class="info-label">用户组</span>
            <span class="info-value">{{ currentGroupName }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">费率</span>
            <span class="info-value">{{ rateText }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">结算模式</span>
            <span class="info-value">{{ settleModeText }}</span>
          </div>
        </div>
        <p v-if="!hasGroupInfo" class="info-tip">
          平台暂未返回当前用户组信息，以下只展示可购买的用户组。
        </p>
      </n-spin>
    </n-card>

    <n-card :bordered="false" class="table-card" title="可购买用户组">
      <n-data-table
        :columns="columns"
        :data="plans"
        :loading="loading"
        :bordered="false"
        :row-key="rowKey"
        :pagination="false"
        :scroll-x="820"
        size="small"
      >
        <template #empty>
          <n-empty description="暂无可购买的用户组" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
.info-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.info-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
}
.info-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.info-label {
  font-size: 12.5px;
  color: #94a3b8;
}
.info-value {
  font-size: 16px;
  font-weight: 600;
  color: #1e293b;
}
.info-tip {
  margin: 14px 0 0;
  font-size: 12px;
  color: #94a3b8;
}
.table-card {
  margin-top: 16px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
@media (max-width: 768px) {
  .info-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 12px;
  }
}
</style>
