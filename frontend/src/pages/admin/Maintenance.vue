<script setup lang="ts">
import { computed, h, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import dayjs from 'dayjs';
import { maintenanceApi } from '@/api/maintenance';
import type { CleanupResultRow } from '@/api/maintenance';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';

const message = useMessage();

const targets = ref<string[]>(['audit_events']);
const before = ref<number | null>(dayjs().subtract(30, 'day').valueOf());
const running = ref(false);
const results = ref<CleanupResultRow[]>([]);
const lastMode = ref<'dry' | 'real' | ''>('');

const showConfirm = ref(false);
const confirmText = ref('');
const confirming = ref(false);

const targetOptions: SelectOption[] = [
  { label: '审计事件（audit_events）', value: 'audit_events' },
  { label: '通知发件箱（outbox_messages）', value: 'outbox_messages' },
  { label: '支付尝试（payment_attempts）', value: 'payment_attempts' },
  { label: '支付回调（payment_callbacks）', value: 'payment_callbacks' },
];

const canRun = computed(() => targets.value.length > 0 && before.value !== null);

function beforeText(): string {
  return dayjs(before.value ?? Date.now()).format('YYYY-MM-DD');
}

async function run(dryRun: boolean, confirm?: string) {
  if (!canRun.value) {
    message.warning('请选择清理目标和截止日期');
    return;
  }
  const response = await maintenanceApi.cleanup({
    targets: targets.value,
    before: beforeText(),
    dry_run: dryRun,
    confirm,
  });
  results.value = response?.results ?? [];
  lastMode.value = dryRun ? 'dry' : 'real';
  const matched = results.value.reduce((acc, row) => acc + (row.matched ?? 0), 0);
  const deleted = results.value.reduce((acc, row) => acc + (row.deleted ?? 0), 0);
  if (dryRun) message.success(`预演完成：匹配 ${matched} 行，未删除任何数据`);
  else message.success(`清理完成：删除 ${deleted} 行`);
}

async function dryRun() {
  running.value = true;
  try {
    await run(true);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    running.value = false;
  }
}

function openConfirm() {
  if (!canRun.value) {
    message.warning('请选择清理目标和截止日期');
    return;
  }
  confirmText.value = '';
  showConfirm.value = true;
}

async function execute() {
  if (confirmText.value !== 'DELETE') {
    message.warning('请输入 DELETE 才会执行');
    return;
  }
  confirming.value = true;
  try {
    await run(false, 'DELETE');
    showConfirm.value = false;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    confirming.value = false;
  }
}

const columns = computed<DataTableColumns<CleanupResultRow>>(() => [
  {
    title: '目标',
    key: 'target',
    minWidth: 220,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.target || '-'),
  },
  {
    title: '匹配行数',
    key: 'matched',
    width: 140,
    align: 'right',
    render: (row) => (typeof row.matched === 'number' ? row.matched : '-'),
  },
  {
    title: '删除行数',
    key: 'deleted',
    width: 140,
    align: 'right',
    render: (row) => (typeof row.deleted === 'number' ? row.deleted : '-'),
  },
]);

</script>

<template>
  <div class="page">
    <PageHeader title="数据清理" subtitle="按截止日期清理历史数据，先预演确认命中行数，再执行删除">
      <template #actions>
        <n-button secondary :loading="running" @click="dryRun">预演</n-button>
        <n-button type="error" :loading="running" @click="openConfirm">执行清理</n-button>
      </template>
    </PageHeader>

    <n-alert type="warning" :bordered="false" style="margin-bottom: 16px">
      删除不可恢复。先点「预演」看每个目标的匹配行数，确认无误后再执行；执行时必须手动输入 DELETE。
    </n-alert>

    <n-card :bordered="false" title="清理条件" class="section">
      <n-form label-placement="left" label-width="110">
        <n-form-item label="清理目标">
          <n-select v-model:value="targets" multiple :options="targetOptions" placeholder="选择要清理的表" />
        </n-form-item>
        <n-form-item label="截止日期">
          <n-date-picker v-model:value="before" type="date" clearable style="width: 220px" />
          <span class="muted small" style="margin-left: 10px">清理该日期之前的数据</span>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card :bordered="false" class="section">
      <template #header>
        <span>执行结果</span>
        <n-tag v-if="lastMode === 'dry'" size="small" round type="info" style="margin-left: 8px">预演</n-tag>
        <n-tag v-else-if="lastMode === 'real'" size="small" round type="error" style="margin-left: 8px">已执行</n-tag>
      </template>
      <n-data-table
        :columns="columns"
        :data="results"
        :bordered="false"
        :single-line="false"
        size="small"
        :row-key="(row: CleanupResultRow) => row.target ?? ''"
      >
        <template #empty>
          <n-empty description="还没有执行记录，点右上角「预演」开始" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="showConfirm" preset="card" title="确认执行清理" style="width: 520px; max-width: 94vw">
      <n-alert type="error" :bordered="false" style="margin-bottom: 14px">
        即将删除 {{ beforeText() }} 之前、所选目标中的全部数据，删除后不可恢复。
      </n-alert>
      <div class="field">
        <div class="field-label">
          输入 <span class="mono strong">DELETE</span> 以确认
        </div>
        <n-input v-model:value="confirmText" placeholder="DELETE" />
      </div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showConfirm = false">取消</n-button>
          <n-button
            type="error"
            :loading="confirming"
            :disabled="confirmText !== 'DELETE'"
            @click="execute"
          >
            确认删除
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.section {
  margin-bottom: 16px;
}
.section:last-child {
  margin-bottom: 0;
}
.small {
  font-size: 12px;
}
.field {
  margin-top: 8px;
}
.field-label {
  font-size: 13px;
  color: #4b5563;
  margin-bottom: 6px;
}
.strong {
  font-weight: 700;
  color: #b91c1c;
}
</style>
