<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NGridItem, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { CopyOutline, RefreshOutline, SyncOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import { errorMessage } from '@/api/client';
import { copyText, formatDateTime } from '@/utils/format';
import {
  fieldDateTime,
  fieldText,
  pick,
  previewJson,
  prettyJson,
  useNarrow,
  useServerPaging,
} from './shared';

type Row = Record<string, unknown>;

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<Row[]>([]);
const loading = ref(false);
const requeueing = ref(false);
const status = ref<string | null>(null);
const stats = ref<Record<string, number>>({});
const statsLoading = ref(false);

const showDetail = ref(false);
const detailRow = ref<Row | null>(null);

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待发送', value: 'pending' },
  { label: '处理中', value: 'processing' },
  { label: '已完成', value: 'completed' },
  { label: '死信', value: 'dead' },
];

const statLabels: Record<string, string> = {
  pending: '待发送',
  processing: '处理中',
  completed: '已完成',
  dead: '死信',
  total: '消息总数',
  failed: '失败',
  delivered: '已投递',
};

const statCards = computed(() =>
  Object.entries(stats.value).map(([key, value]) => ({
    key,
    label: statLabels[key] ?? key,
    value: typeof value === 'number' ? value : Number(value) || 0,
  })),
);

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.outboxMessages({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
    });
    rows.value = data.items;
    total.value = data.total;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

async function loadStats() {
  statsLoading.value = true;
  try {
    stats.value = await adminApi.outboxStats();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    statsLoading.value = false;
  }
}

function search() {
  resetPage();
  void load();
}

function resetFilters() {
  status.value = null;
  resetPage();
  void load();
}

async function requeueDead() {
  requeueing.value = true;
  try {
    const result = await adminApi.requeueDead();
    message.success(`已重投 ${result.requeued} 条死信`);
    await Promise.all([load(), loadStats()]);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    requeueing.value = false;
  }
}

async function refreshAll() {
  await Promise.all([load(), loadStats()]);
}

const outboxStatusMap: Record<string, { text: string; type: 'default' | 'success' | 'warning' | 'error' | 'info' }> = {
  pending: { text: '待发送', type: 'warning' },
  processing: { text: '处理中', type: 'info' },
  completed: { text: '已完成', type: 'success' },
  dead: { text: '死信', type: 'error' },
};

function statusMeta(row: Row | null) {
  const current = fieldText(row, 'status', 'Status');
  return outboxStatusMap[current] ?? { text: current, type: 'default' as const };
}

function outboxStatusTag(row: Row) {
  const resolved = statusMeta(row);
  return h(NTag, { size: 'small', round: true, bordered: false, type: resolved.type }, {
    default: () => resolved.text,
  });
}

function openDetail(row: Row) {
  detailRow.value = row;
  showDetail.value = true;
}

function payloadOf(row: Row | null): string {
  if (row === null) return '-';
  return prettyJson(pick(row, 'payload', 'Payload'));
}

async function copyPayload() {
  const ok = await copyText(payloadOf(detailRow.value));
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
}

const detailPayload = computed(() => payloadOf(detailRow.value));
const detailStatus = computed(() => statusMeta(detailRow.value));
const detailCreatedAt = computed(() => {
  const value = pick(detailRow.value, 'created_at', 'CreatedAt');
  return typeof value === 'string' ? formatDateTime(value) : '-';
});

const columns = computed<DataTableColumns<Row>>(() => [
  {
    title: '消息 ID',
    key: 'id',
    width: 250,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono' }, fieldText(row, 'id', 'ID')),
  },
  {
    title: 'Topic',
    key: 'topic',
    width: 180,
    ellipsis: { tooltip: true },
    render: (row) => fieldText(row, 'topic', 'Topic'),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => outboxStatusTag(row),
  },
  {
    title: '重试次数',
    key: 'attempts',
    width: 100,
    align: 'right',
    render: (row) => fieldText(row, 'attempts', 'Attempts'),
  },
  {
    title: '下次可投递',
    key: 'available_at',
    width: 170,
    render: (row) => fieldDateTime(row, 'available_at', 'AvailableAt'),
  },
  {
    title: '最近错误',
    key: 'last_error',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render: (row) => {
      const text = fieldText(row, 'last_error', 'LastError');
      return text === '-' ? text : h('span', { style: 'color: var(--gp-danger)' }, previewJson(text, 80));
    },
  },
  {
    title: '载荷',
    key: 'payload',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => {
      const value = pick(row, 'payload', 'Payload');
      return value === undefined ? '-' : h('span', { class: 'muted' }, previewJson(value, 60));
    },
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => fieldDateTime(row, 'created_at', 'CreatedAt'),
  },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right',
    render: (row) =>
      h(NButton, { text: true, size: 'small', type: 'primary', onClick: () => openDetail(row) }, {
        default: () => '详情',
      }),
  },
]);

onMounted(() => {
  void load();
  void loadStats();
});
</script>

<template>
  <admin-page title="消息队列" subtitle="商户通知 Outbox：投递状态、重试与死信重投">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading || statsLoading" @click="refreshAll">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-popconfirm @positive-click="requeueDead">
          <template #trigger>
            <n-button type="warning" :loading="requeueing">
              <template #icon>
                <n-icon :component="SyncOutline" />
              </template>
              重投死信
            </n-button>
          </template>
          确认把所有 dead 状态的消息重新入队？
        </n-popconfirm>
      </n-space>
    </template>

    <n-spin :show="statsLoading">
      <n-empty v-if="statCards.length === 0" description="暂无队列统计" size="small" />
      <n-grid v-else cols="2 s:3 m:5" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 16px">
        <n-grid-item v-for="card in statCards" :key="card.key">
          <n-card size="small" :bordered="false">
            <n-statistic :label="card.label" :value="card.value" />
          </n-card>
        </n-grid-item>
      </n-grid>
    </n-spin>

    <n-card size="small" :bordered="false">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
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
            <n-button type="primary" @click="search">查询</n-button>
            <n-button @click="resetFilters">重置</n-button>
          </n-space>
        </n-gi>
      </n-grid>

      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Row) => String(row.id ?? row.ID ?? '')"
        :scroll-x="1500"
        :size="isNarrow ? 'small' : 'medium'"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无队列消息" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showDetail" :width="isNarrow ? '92vw' : 640" placement="right">
      <n-drawer-content title="消息详情" closable>
        <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
          <n-descriptions-item label="消息 ID">
            <span class="mono">{{ fieldText(detailRow, 'id', 'ID') }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="Topic">
            {{ fieldText(detailRow, 'topic', 'Topic') }}
          </n-descriptions-item>
          <n-descriptions-item label="状态">
            <n-tag size="small" round :bordered="false" :type="detailStatus.type">{{ detailStatus.text }}</n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="重试次数">
            {{ fieldText(detailRow, 'attempts', 'Attempts') }}
          </n-descriptions-item>
          <n-descriptions-item label="下次可投递">
            {{ fieldDateTime(detailRow, 'available_at', 'AvailableAt') }}
          </n-descriptions-item>
          <n-descriptions-item label="创建时间">
            {{ detailCreatedAt }}
          </n-descriptions-item>
          <n-descriptions-item label="最近错误" :span="isNarrow ? 1 : 2">
            <n-text type="error">{{ fieldText(detailRow, 'last_error', 'LastError') }}</n-text>
          </n-descriptions-item>
        </n-descriptions>

        <n-divider title-placement="left">
          载荷
        </n-divider>
        <n-space justify="end" style="margin-bottom: 8px">
          <n-button text type="primary" size="small" @click="copyPayload">
            <template #icon>
              <n-icon :component="CopyOutline" />
            </template>
            复制
          </n-button>
        </n-space>
        <n-code :code="detailPayload" language="json" word-wrap />

        <template #footer>
          <n-space justify="end">
            <n-button @click="showDetail = false">关闭</n-button>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>
