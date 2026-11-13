<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { adminApi } from '@/api';
import type { PageQuery } from '@/api';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { formatDateTime } from '@/utils/format';
import { prettyJson, useServerPaging } from './shared';

interface AuditRow {
  id: string;
  event_type: string;
  actor_type: string;
  actor_id?: string;
  payload: string;
  created_at: string;
}

const message = useMessage();

const rows = ref<AuditRow[]>([]);
const loading = ref(false);
const keyword = ref('');
const eventType = ref('');

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

async function load() {
  loading.value = true;
  try {
    // 后端 /admin/audit-events 支持 event_type，PageQuery 未声明该字段，这里扩展后传入
    const query: PageQuery & { event_type?: string } = {
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value.trim() || undefined,
      event_type: eventType.value.trim() || undefined,
    };
    const data = await adminApi.auditEvents(query);
    rows.value = data.items;
    total.value = data.total;
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
  keyword.value = '';
  eventType.value = '';
  resetPage();
  void load();
}

const columns = computed<DataTableColumns<AuditRow>>(() => [
  {
    type: 'expand',
    renderExpand: (row) => h('pre', { class: 'json' }, prettyJson(row.payload)),
  },
  {
    title: 'ID',
    key: 'id',
    width: 280,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.id || '-'),
  },
  {
    title: '事件类型',
    key: 'event_type',
    minWidth: 200,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.event_type || '-'),
  },
  { title: '操作者类型', key: 'actor_type', width: 130, render: (row) => row.actor_type || '-' },
  {
    title: '操作者 ID',
    key: 'actor_id',
    width: 280,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.actor_id || '-'),
  },
  {
    title: '时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '载荷',
    key: 'payload',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'muted mono' }, prettyJson(row.payload).replace(/\s+/g, ' ').slice(0, 80)),
  },
]);

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="审计日志" subtitle="管理端写操作审计事件，展开行查看格式化 JSON 载荷">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <n-input
        v-model:value="keyword"
        placeholder="关键字（路径 / 资源 ID）"
        clearable
        style="width: 260px"
        @keyup.enter="search"
      />
      <n-input
        v-model:value="eventType"
        placeholder="事件类型，例如 admin.http.patch"
        clearable
        style="width: 260px"
        @keyup.enter="search"
      />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
    </div>

    <n-card :bordered="false">
      <p class="muted hint">点击行首箭头展开查看完整 payload；解析失败时展示原始文本。</p>
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: AuditRow) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无审计事件" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
.hint {
  font-size: 12px;
  margin: 0 0 12px;
}
.json {
  margin: 0;
  max-height: 320px;
  overflow: auto;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 10px;
  padding: 12px 14px;
  font-size: 12px;
  line-height: 1.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
