<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NCode, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { FunnelOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { PageQuery } from '@/api';
import { errorMessage } from '@/api/client';
import { formatDateTime } from '@/utils/format';
import { prettyJson, useNarrow, useServerPaging } from './shared';

interface AuditRow {
  id: string;
  event_type: string;
  actor_type: string;
  actor_id?: string;
  payload: string;
  created_at: string;
}

const message = useMessage();
const isNarrow = useNarrow();

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

function payloadPreview(payload: string): string {
  return prettyJson(payload).replace(/\s+/g, ' ').slice(0, 120);
}

const columns = computed<DataTableColumns<AuditRow>>(() => [
  {
    type: 'expand',
    width: 40,
    renderExpand: (row) =>
      h('div', { style: 'padding: 4px 12px 8px' }, [h(NCode, { code: prettyJson(row.payload), language: 'json', wordWrap: true })]),
  },
  {
    title: 'ID',
    key: 'id',
    width: 260,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono' }, row.id || '-'),
  },
  {
    title: '事件类型',
    key: 'event_type',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono' }, row.event_type || '-'),
  },
  { title: '操作者类型', key: 'actor_type', width: 120, render: (row) => row.actor_type || '-' },
  {
    title: '操作者 ID',
    key: 'actor_id',
    width: 240,
    ellipsis: { tooltip: true },
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
    minWidth: 260,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'muted mono' }, payloadPreview(row.payload)),
  },
]);

onMounted(load);
</script>

<template>
  <admin-page title="审计日志" subtitle="管理端写操作审计事件，展开行查看格式化 JSON 载荷">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-card size="small" :bordered="false">
      <n-grid cols="1 s:2 m:4" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 12px">
        <n-gi>
          <n-input
            v-model:value="keyword"
            placeholder="关键字（路径 / 资源 ID）"
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
          <n-input
            v-model:value="eventType"
            placeholder="事件类型，例如 admin.http.patch"
            clearable
            style="width: 100%"
            @keyup.enter="search"
          >
            <template #prefix>
              <n-icon :component="FunnelOutline" />
            </template>
          </n-input>
        </n-gi>
        <n-gi>
          <n-space :wrap="true">
            <n-button type="primary" @click="search">查询</n-button>
            <n-button @click="resetFilters">重置</n-button>
          </n-space>
        </n-gi>
      </n-grid>

      <n-text depth="3" class="gp-hint" style="display: block; margin-bottom: 10px">
        点行首箭头展开完整 payload；解析失败时展示原始文本。
      </n-text>

      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: AuditRow) => row.id"
        :scroll-x="1400"
        :size="isNarrow ? 'small' : 'medium'"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无审计事件" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
