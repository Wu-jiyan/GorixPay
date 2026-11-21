<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { RefreshOutline, ShieldCheckmarkOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { RiskEvent } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime } from '@/utils/format';
import { fieldText, useNarrow } from '../admin/shared';

/**
 * 商户风险记录：GET /merchant/risk-events（后端只回本商户的事件）。
 * 字段对齐 internal/risk/model.go 的 Event：
 * id / merchant_id / rule_code / detail / client_ip / created_at。
 * 后端没有 level、也没有订单号字段，所以「等级」按规则码推断（见 levelOf），
 * 「订单号」取行上可能存在的 order_* 字段，取不到就是 "-"。
 */
const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(false);
const items = ref<RiskEvent[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);

type TagType = 'default' | 'info' | 'success' | 'warning' | 'error';

interface Level {
  text: string;
  type: TagType;
}

/**
 * 规则码 → 等级。当前后端会写的规则码只有两类：
 *   blacklist.<entry_type>（命中黑名单，订单被拦下）→ 高
 *   risk.daily_amount_limit / risk.daily_order_limit / risk.ip_daily_limit → 中
 * 其余未知规则码按低处理，不猜。
 */
function levelOf(ruleCode?: string): Level {
  const code = (ruleCode ?? '').toLowerCase();
  if (code.startsWith('blacklist.')) return { text: '高', type: 'error' };
  if (code.includes('limit')) return { text: '中', type: 'warning' };
  return { text: '低', type: 'default' };
}

function levelTag(row: RiskEvent) {
  const level = levelOf(row.rule_code);
  return h(NTag, { size: 'small', type: level.type, bordered: false }, () => level.text);
}

/** 订单号：当前后端事件里没有，真有 order_* 字段时直接显示 */
function orderOf(row: RiskEvent): string {
  return fieldText(row, 'order_id', 'order_no', 'trade_no', 'order_trade_no');
}

const columns: DataTableColumns<RiskEvent> = [
  {
    title: '时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '规则码',
    key: 'rule_code',
    width: 220,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.rule_code || '-'),
  },
  {
    title: '等级',
    key: 'level',
    width: 90,
    render: (row) => levelTag(row),
  },
  {
    title: '说明',
    key: 'detail',
    minWidth: 240,
    ellipsis: { tooltip: true },
    render: (row) => row.detail || '-',
  },
  {
    title: '订单号',
    key: 'order_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) =>
      h(NText, { class: 'mono', depth: 3 }, () => orderOf(row)),
  },
  {
    title: '客户端 IP',
    key: 'client_ip',
    width: 150,
    render: (row) => h(NText, { class: 'mono', depth: 3 }, () => row.client_ip || '-'),
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

async function load() {
  loading.value = true;
  try {
    const res = await merchantApi.riskEvents({ page: page.value, page_size: pageSize.value });
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function handlePageChange(value: number) {
  page.value = value;
  void load();
}

function handlePageSizeChange(value: number) {
  pageSize.value = value;
  page.value = 1;
  void load();
}

onMounted(() => {
  void load();
});
</script>

<template>
  <admin-page title="风险记录" subtitle="本商户触发过的风控规则，命中黑名单或超出限额时系统会在这里留档">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      命中黑名单（<n-text class="mono">blacklist.*</n-text>）会拦下下单请求；超出日限额（
      <n-text class="mono">*_limit</n-text>）会拒绝新建支付订单。反复出现请检查接入域名与来源 IP。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="(row: RiskEvent) => row.id"
        :scroll-x="1100"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无风险记录" size="small">
            <template #extra>
              <n-space align="center" :size="6">
                <n-icon :component="ShieldCheckmarkOutline" />
                <n-text depth="3" class="gp-hint">最近没有触发任何风控规则</n-text>
              </n-space>
            </template>
          </n-empty>
        </template>
      </n-data-table>

      <n-text depth="3" class="gp-hint" style="display: block; margin-top: 10px">
        等级按规则码推断：blacklist.* 为高，*_limit 为中，其余为低。
      </n-text>
    </n-card>
  </admin-page>
</template>