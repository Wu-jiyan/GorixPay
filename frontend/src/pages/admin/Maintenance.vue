<script setup lang="ts">
import { computed, h, ref, watch } from 'vue';
import { NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import dayjs from 'dayjs';
import { PlayOutline, TrashOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { maintenanceApi } from '@/api/maintenance';
import type { CleanupResultRow } from '@/api/maintenance';
import { errorMessage } from '@/api/client';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

/** 操作员要在二次确认弹窗里手输的文字 */
const CONFIRM_WORD = 'DELETE';
/** 后端固定校验的执行令牌：internal/device/http.go 要求 confirm 必须是 DELETE */
const API_CONFIRM_TOKEN = 'DELETE';

const targets = ref<string[]>(['audit_events']);
const retentionDays = ref<number | null>(30);
const running = ref(false);
const results = ref<CleanupResultRow[]>([]);
const lastMode = ref<'dry' | 'real' | ''>('');
const dryRunDone = ref(false);

const showConfirm = ref(false);
const confirmText = ref('');
const confirming = ref(false);

const targetOptions: SelectOption[] = [
  { label: '审计事件（audit_events）', value: 'audit_events' },
  { label: '通知发件箱（outbox_messages）', value: 'outbox_messages' },
  { label: '支付尝试（payment_attempts）', value: 'payment_attempts' },
  { label: '支付回调（payment_callbacks）', value: 'payment_callbacks' },
];

const canRun = computed(
  () => targets.value.length > 0 && typeof retentionDays.value === 'number' && retentionDays.value > 0,
);

/** 保留天数 → 截止日期：清理该日期之前的数据 */
const beforeText = computed(() =>
  dayjs()
    .subtract(retentionDays.value ?? 0, 'day')
    .format('YYYY-MM-DD'),
);

const matchedRows = computed(() =>
  results.value.reduce((acc, row) => acc + (typeof row.matched === 'number' ? row.matched : 0), 0),
);

const deletedRows = computed(() =>
  results.value.reduce((acc, row) => acc + (typeof row.deleted === 'number' ? row.deleted : 0), 0),
);

const targetNames = computed(() =>
  targets.value.map((value) => targetOptions.find((option) => option.value === value)?.label ?? value),
);

// 条件一变，之前的预演结果就不再代表这次要删的数据
watch([targets, retentionDays], () => {
  dryRunDone.value = false;
});

function scopeText(): string {
  return targetNames.value.length > 0 ? targetNames.value.join('、') : '未选择目标';
}

async function run(dryRun: boolean, confirm?: string) {
  if (!canRun.value) {
    message.warning('请选择清理目标和保留天数');
    return;
  }
  const response = await maintenanceApi.cleanup({
    targets: targets.value,
    before: beforeText.value,
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
  if (!canRun.value) {
    message.warning('请选择清理目标和保留天数');
    return;
  }
  running.value = true;
  try {
    await run(true);
    dryRunDone.value = true;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    running.value = false;
  }
}

function openConfirm() {
  if (!canRun.value) {
    message.warning('请选择清理目标和保留天数');
    return;
  }
  if (!dryRunDone.value) {
    message.warning('请先预演一次，确认命中行数后再执行');
    return;
  }
  confirmText.value = '';
  showConfirm.value = true;
}

async function execute() {
  if (confirmText.value !== CONFIRM_WORD) {
    message.warning(`请输入 ${CONFIRM_WORD} 才会执行`);
    return;
  }
  confirming.value = true;
  try {
    await run(false, API_CONFIRM_TOKEN);
    showConfirm.value = false;
    dryRunDone.value = false;
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
    minWidth: 240,
    render: (row) => h(NText, { code: true }, { default: () => row.target || '-' }),
  },
  {
    title: '匹配行数',
    key: 'matched',
    width: 140,
    align: 'right',
    render: (row) => (typeof row.matched === 'number' ? String(row.matched) : '-'),
  },
  {
    title: '删除行数',
    key: 'deleted',
    width: 140,
    align: 'right',
    render: (row) => (typeof row.deleted === 'number' ? String(row.deleted) : '-'),
  },
]);
</script>

<template>
  <admin-page title="数据清理" subtitle="按截止日期清理历史数据，先预演确认命中行数，再执行删除">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="running" @click="dryRun">
          <template #icon><n-icon><PlayOutline /></n-icon></template>
          预演
        </n-button>
        <n-button type="error" :loading="running" @click="openConfirm">
          <template #icon><n-icon><TrashOutline /></n-icon></template>
          执行清理
        </n-button>
      </n-space>
    </template>

    <n-alert type="warning" :bordered="false" :show-icon="true" style="margin-bottom: 16px">
      删除不可恢复。先点「预演」看每个目标的匹配行数，确认无误后再执行；执行时必须手动输入
      {{ CONFIRM_WORD }}。
    </n-alert>

    <n-card size="small" :bordered="false" title="清理条件" style="margin-bottom: 16px">
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="清理目标">
          <n-select
            v-model:value="targets"
            multiple
            :options="targetOptions"
            placeholder="选择要清理的表"
            style="width: 100%; max-width: 460px"
          />
        </n-form-item>
        <n-form-item label="保留天数">
          <n-space align="center" :size="12" :wrap="true">
            <n-input-number
              v-model:value="retentionDays"
              :min="1"
              :max="3650"
              :precision="0"
              style="width: 100%; max-width: 160px"
            />
            <n-text depth="3">
              保留最近 {{ retentionDays ?? 0 }} 天，清理 {{ beforeText }} 之前的数据
            </n-text>
          </n-space>
        </n-form-item>
        <n-form-item label="预演状态">
          <n-space align="center" :size="10" :wrap="true">
            <n-tag v-if="dryRunDone" size="small" type="success" round :bordered="false">
              已预演：匹配 {{ matchedRows }} 行
            </n-tag>
            <n-tag v-else size="small" round :bordered="false">未预演</n-tag>
            <n-text depth="3">执行前必须先预演一次；改动清理目标或保留天数后需要重新预演。</n-text>
          </n-space>
        </n-form-item>
      </n-form>

      <n-alert type="info" :bordered="false" style="margin-top: 4px">
        本次范围：{{ scopeText() }}。
      </n-alert>
    </n-card>

    <n-card :bordered="false">
      <template #header>
        <n-space align="center" :size="8">
          <n-text>执行结果</n-text>
          <n-tag v-if="lastMode === 'dry'" size="small" round type="info" :bordered="false">预演</n-tag>
          <n-tag v-else-if="lastMode === 'real'" size="small" round type="error" :bordered="false">
            已执行
          </n-tag>
        </n-space>
      </template>

      <n-alert
        v-if="results.length > 0"
        :type="lastMode === 'dry' ? 'warning' : 'error'"
        :bordered="false"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        <template v-if="lastMode === 'dry'">
          预演结果：{{ beforeText }} 之前的数据中，命中 {{ matchedRows }} 行，未删除任何数据。
        </template>
        <template v-else>
          执行结果：{{ beforeText }} 之前的数据中，匹配 {{ matchedRows }} 行，已删除
          {{ deletedRows }} 行，删除不可恢复。
        </template>
      </n-alert>

      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="results"
        :row-key="(row: CleanupResultRow) => row.target ?? ''"
        :scroll-x="620"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="还没有执行记录，点右上角「预演」开始" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showConfirm"
      preset="card"
      title="确认执行清理"
      style="width: 560px; max-width: 94vw"
    >
      <n-alert type="warning" :bordered="false" :show-icon="true" style="margin-bottom: 16px">
        即将删除 {{ beforeText }} 之前、所选目标（{{ scopeText() }}）中的全部数据：预演命中
        {{ matchedRows }} 行。删除后不可恢复。
      </n-alert>

      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="输入确认文字">
          <n-input
            v-model:value="confirmText"
            :placeholder="CONFIRM_WORD"
            :status="confirmText === CONFIRM_WORD ? 'success' : undefined"
          />
        </n-form-item>
      </n-form>
      <n-text depth="3">输入 {{ CONFIRM_WORD }} 后「确认删除」才会亮起。</n-text>

      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="showConfirm = false">取消</n-button>
          <n-button
            type="error"
            :loading="confirming"
            :disabled="confirmText !== CONFIRM_WORD"
            @click="execute"
          >
            <template #icon><n-icon><TrashOutline /></n-icon></template>
            确认删除
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
