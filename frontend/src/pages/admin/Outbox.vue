<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import { errorMessage } from '@/api/client';
import StatCard from '@/components/StatCard.vue';
import PageHeader from '@/components/PageHeader.vue';
import { fieldDateTime, fieldText, previewJson, useServerPaging } from './shared';

type Row = Record<string, unknown>;

const message = useMessage();

const rows = ref<Row[]>([]);
const loading = ref(false);
const requeueing = ref(false);
const status = ref<string | null>(null);
const stats = ref<Record<string, number>>({});
const statsLoading = ref(false);

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

const statAccent: Record<string, string> = {
  pending: '#b45309',
  processing: '#2563eb',
  completed: '#0f766e',
  dead: '#b91c1c',
  total: '#334155',
};

const statCards = computed(() =>
  Object.entries(stats.value).map(([key, value]) => ({
    key,
    label: statLabels[key] ?? key,
    value: typeof value === 'number' ? value : Number(value) || 0,
    accent: statAccent[key] ?? '#334155',
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

function outboxStatusTag(row: Row) {
  const status = fieldText(row, 'status', 'Status');
  const map: Record<string, { text: string; type: 'default' | 'success' | 'warning' | 'error' | 'info' }> = {
    pending: { text: '待发送', type: 'warning' },
    processing: { text: '处理中', type: 'info' },
    completed: { text: '已完成', type: 'success' },
    dead: { text: '死信', type: 'error' },
  };
  const resolved = map[status] ?? { text: status, type: 'default' as const };
  return h(NTag, { size: 'small', round: true, type: resolved.type }, { default: () => resolved.text });
}

const columns = computed<DataTableColumns<Row>>(() => [
  {
    title: '消息 ID',
    key: 'id',
    width: 250,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, fieldText(row, 'id', 'ID')),
  },
  {
    title: 'Topic',
    key: 'topic',
    width: 190,
    ellipsis: true,
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
    ellipsis: true,
    render: (row) => {
      const text = fieldText(row, 'last_error', 'LastError');
      return text === '-' ? text : h('span', { title: text, style: 'color: #b91c1c' }, previewJson(text, 80));
    },
  },
  {
    title: '载荷',
    key: 'payload',
    minWidth: 200,
    ellipsis: true,
    render: (row) => {
      const text = fieldText(row, 'payload', 'Payload');
      return text === '-' ? text : h('span', { title: text, class: 'muted' }, previewJson(text, 60));
    },
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => fieldDateTime(row, 'created_at', 'CreatedAt'),
  },
]);

onMounted(() => {
  void load();
  void loadStats();
});
</script>

<template>
  <div class="page">
    <PageHeader title="消息队列" subtitle="商户通知 Outbox：投递状态、重试与死信重投">
      <template #actions>
        <n-button secondary :loading="loading || statsLoading" @click="refreshAll">刷新</n-button>
        <n-popconfirm @positive-click="requeueDead">
          <template #trigger>
            <n-button type="warning" :loading="requeueing">重投死信</n-button>
          </template>
          确认把所有 dead 状态的消息重新入队？
        </n-popconfirm>
      </template>
    </PageHeader>

    <n-spin :show="statsLoading">
      <n-empty v-if="statCards.length === 0" description="暂无队列统计" />
      <n-grid v-else cols="2 s:3 m:5" responsive="screen" :x-gap="16" :y-gap="16">
        <n-gi v-for="card in statCards" :key="card.key">
          <StatCard :label="card.label" :value="card.value" :accent="card.accent" />
        </n-gi>
      </n-grid>
    </n-spin>

    <div class="toolbar" style="margin-top: 16px">
      <n-select v-model:value="status" :options="statusOptions" placeholder="全部状态" clearable style="width: 160px" />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
      <span class="muted mono" style="font-size: 12px">共 {{ total }} 条</span>
    </div>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Row) => String(row.id ?? row.ID ?? '')"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无队列消息" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
:deep(.stat-card .stat-value) {
  word-break: keep-all;
}
</style>
