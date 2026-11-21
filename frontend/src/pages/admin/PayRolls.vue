<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import {
  NButton,
  NInputNumber,
  NPopconfirm,
  NSpace,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { CalendarOutline, RefreshOutline } from '@vicons/ionicons5';
import dayjs from 'dayjs';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { Channel } from '@/api';
import { payRollsApi } from '@/api/payTypes';
import type { PayRoll } from '@/api/payTypes';
import type { PageQuery } from '@/api/types';
import { errorMessage } from '@/api/client';
import { activeStatus } from '@/utils/format';
import { asRow, itemsOf, useNarrow } from './shared';

/** 后端 pay-rolls 支持 from / to 日期过滤，但 PageQuery 类型里没有这两个字段 */
type RollQuery = PageQuery & { from?: string; to?: string };

interface RollRow {
  channel_id: string;
  name: string;
  channel_status: string;
  roll_id: string;
  configured: boolean;
  weight: number | null;
  status: string;
}

const message = useMessage();
const isNarrow = useNarrow();

const day = ref<number>(dayjs().valueOf());
const channels = ref<Channel[]>([]);
const rolls = ref<PayRoll[]>([]);
const loading = ref(false);
const savingId = ref('');
const pendingStatusId = ref('');
const weights = ref<Record<string, number | null>>({});

const showGenerate = ref(false);
const generating = ref(false);
const generateForm = reactive<{ channel_ids: string[]; range: [number, number] | null }>({
  channel_ids: [],
  range: null,
});

const dayText = computed(() => dayjs(day.value).format('YYYY-MM-DD'));

const rows = computed<RollRow[]>(() => {
  const byChannel = new Map<string, PayRoll>();
  rolls.value.forEach((roll) => byChannel.set(roll.channel_id, roll));
  const list: RollRow[] = channels.value.map((channel) => {
    const roll = byChannel.get(channel.id);
    byChannel.delete(channel.id);
    return {
      channel_id: channel.id,
      name: channel.name || channel.id,
      channel_status: channel.status || '',
      roll_id: roll?.id ?? '',
      configured: Boolean(roll),
      weight: typeof roll?.weight === 'number' ? roll.weight : null,
      status: roll?.status ?? '',
    };
  });
  // 通道已被删除但当天还留着轮询行：照样列出来，避免脏数据看不到
  byChannel.forEach((roll, channelId) => {
    list.push({
      channel_id: channelId,
      name: '(已删除的渠道)',
      channel_status: '',
      roll_id: roll.id,
      configured: true,
      weight: typeof roll.weight === 'number' ? roll.weight : null,
      status: roll.status || '',
    });
  });
  return list;
});

const configuredCount = computed(() => rows.value.filter((row) => row.configured).length);

const totalWeight = computed(() =>
  rows.value.reduce((sum, row) => {
    if (!row.configured) return sum;
    const draft = weights.value[row.channel_id];
    return sum + (typeof draft === 'number' ? draft : 0);
  }, 0),
);

function shareOf(row: RollRow): number {
  const draft = weights.value[row.channel_id];
  if (!row.configured || typeof draft !== 'number' || totalWeight.value <= 0) return 0;
  return Math.round((draft / totalWeight.value) * 1000) / 10;
}

function isDirty(row: RollRow): boolean {
  return weights.value[row.channel_id] !== row.weight;
}

function syncWeights() {
  const next: Record<string, number | null> = {};
  for (const row of rows.value) next[row.channel_id] = row.weight;
  weights.value = next;
}

/** 保存成功后只回写这一行，避免把其它行的未保存草稿冲掉 */
function applyRoll(roll: PayRoll) {
  rolls.value = [...rolls.value.filter((item) => item.id !== roll.id), roll];
  weights.value = { ...weights.value, [roll.channel_id]: roll.weight };
}

function dropRoll(rollId: string, channelId: string) {
  rolls.value = rolls.value.filter((item) => item.id !== rollId);
  weights.value = { ...weights.value, [channelId]: null };
}

async function load() {
  loading.value = true;
  try {
    const query: RollQuery = { page: 1, page_size: 200, from: dayText.value, to: dayText.value };
    const data = await payRollsApi.list(query);
    rolls.value = itemsOf<PayRoll>(data);
    syncWeights();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

async function loadChannels() {
  try {
    channels.value = itemsOf<Channel>(await adminApi.channels());
    syncWeights();
  } catch (error) {
    channels.value = [];
    message.error(errorMessage(error));
  }
}

function reloadDay() {
  void load();
}

function openGenerate() {
  generateForm.channel_ids = [];
  generateForm.range = null;
  showGenerate.value = true;
}

function countOf(result: unknown): number {
  if (typeof result === 'number') return result;
  const row = asRow(result);
  for (const key of ['generated', 'count', 'created']) {
    const value = row[key];
    if (typeof value === 'number' && Number.isFinite(value)) return value;
  }
  return 0;
}

async function runGenerate() {
  if (generateForm.channel_ids.length === 0) {
    message.warning('请选择至少一个渠道');
    return;
  }
  if (!generateForm.range) {
    message.warning('请选择日期区间');
    return;
  }
  generating.value = true;
  try {
    const result = await payRollsApi.generate({
      channel_ids: generateForm.channel_ids,
      from: dayjs(generateForm.range[0]).format('YYYY-MM-DD'),
      to: dayjs(generateForm.range[1]).format('YYYY-MM-DD'),
    });
    message.success(`批量生成完成，本次生成 ${countOf(result)} 条轮询配置`);
    showGenerate.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    generating.value = false;
  }
}

async function saveWeight(row: RollRow) {
  const weight = weights.value[row.channel_id];
  if (typeof weight !== 'number' || !Number.isFinite(weight)) {
    message.warning('请输入权重');
    return;
  }
  savingId.value = row.channel_id;
  try {
    if (row.configured) {
      const updated = await payRollsApi.update(row.roll_id, {
        channel_id: row.channel_id,
        roll_date: dayText.value,
        weight,
        status: row.status || 'active',
      });
      message.success('权重已保存');
      applyRoll(updated);
    } else {
      const created = await payRollsApi.create({
        channel_id: row.channel_id,
        roll_date: dayText.value,
        weight,
        status: 'active',
      });
      message.success('已为当天新增轮询配置');
      applyRoll(created);
    }
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    savingId.value = '';
  }
}

async function toggleStatus(row: RollRow) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  pendingStatusId.value = row.channel_id;
  try {
    const updated = await payRollsApi.update(row.roll_id, {
      channel_id: row.channel_id,
      roll_date: dayText.value,
      weight: row.weight ?? 1,
      status: next,
    });
    message.success(next === 'active' ? '已启用' : '已停用');
    applyRoll(updated);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    pendingStatusId.value = '';
  }
}

async function remove(row: RollRow) {
  try {
    await payRollsApi.remove(row.roll_id);
    message.success('轮询配置已删除');
    dropRoll(row.roll_id, row.channel_id);
  } catch (error) {
    message.error(errorMessage(error));
  }
}

const channelOptions = computed(() =>
  channels.value.map((item) => ({
    label: item.name ? `${item.name}（${item.plugin_code}）` : item.id,
    value: item.id,
  })),
);

const columns = computed<DataTableColumns<RollRow>>(() => [
  {
    title: '支付渠道',
    key: 'channel_id',
    minWidth: 260,
    render: (row) =>
      h('div', [
        h('div', row.name),
        h(
          NText,
          { depth: 3, code: true, class: 'gp-hint' },
          { default: () => row.channel_id },
        ),
      ]),
  },
  {
    title: '渠道状态',
    key: 'channel_status',
    width: 110,
    render: (row) => {
      const meta = activeStatus(row.channel_status);
      return h(
        NTag,
        { size: 'small', round: true, type: meta.type, bordered: false },
        { default: () => meta.text },
      );
    },
  },
  {
    title: '当日权重',
    key: 'weight',
    width: 150,
    render: (row) =>
      h(NInputNumber, {
        value: weights.value[row.channel_id],
        min: 0,
        max: 10000,
        step: 1,
        placeholder: '未配置',
        size: 'small',
        style: 'width: 110px',
        'onUpdate:value': (value: number | null) => {
          weights.value[row.channel_id] = value;
        },
      }),
  },
  {
    title: '权重占比',
    key: 'share',
    width: 110,
    align: 'right',
    render: (row) =>
      h(
        NText,
        { depth: row.configured ? 1 : 3 },
        { default: () => (row.configured ? `${shareOf(row)}%` : '-') },
      ),
  },
  {
    title: '轮询状态',
    key: 'status',
    width: 120,
    render: (row) => {
      if (!row.configured) {
        return h(
          NTag,
          { size: 'small', round: true, type: 'warning', bordered: false },
          { default: () => '未配置' },
        );
      }
      return h(
        NPopconfirm,
        {
          positiveText: '确定',
          negativeText: '取消',
          onPositiveClick: () => toggleStatus(row),
        },
        {
          trigger: () =>
            h(
              NSwitch,
              {
                value: row.status === 'active',
                size: 'small',
                loading: pendingStatusId.value === row.channel_id,
              },
              { checked: () => '启用', unchecked: () => '停用' },
            ),
          default: () =>
            row.status === 'active' ? '确认停用该渠道当天的轮询？' : '确认启用该渠道当天的轮询？',
        },
      );
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 12, wrap: false },
        {
          default: () => [
            h(
              NButton,
              {
                text: true,
                size: 'small',
                type: row.configured ? 'primary' : 'success',
                disabled: row.configured && !isDirty(row),
                loading: savingId.value === row.channel_id,
                onClick: () => saveWeight(row),
              },
              { default: () => (row.configured ? '保存' : '新增配置') },
            ),
            row.configured
              ? h(
                  NPopconfirm,
                  { positiveText: '确定', negativeText: '取消', onPositiveClick: () => remove(row) },
                  {
                    trigger: () =>
                      h(
                        NButton,
                        { text: true, size: 'small', type: 'error' },
                        { default: () => '删除' },
                      ),
                    default: () => '确认删除该轮询配置？',
                  },
                )
              : null,
          ].filter(Boolean),
        },
      ),
  },
]);

onMounted(() => {
  void loadChannels();
  void load();
});
</script>

<template>
  <admin-page title="按日轮询" subtitle="按日期给渠道分配权重，收银台据此轮询可用通道">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button secondary type="primary" @click="openGenerate">
          <template #icon>
            <n-icon :component="CalendarOutline" />
          </template>
          批量生成
        </n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      收银台按当天的权重在渠道之间轮询：当天没配置的渠道沿用自身权重，配置了但停用的同样按未配置处理。
    </n-alert>

    <n-card size="small" :bordered="false">
      <template #header>
        <n-grid cols="1 s:2" responsive="screen" :x-gap="12" :y-gap="8" style="width: 100%">
          <n-gi>
            <n-date-picker
              v-model:value="day"
              type="date"
              :clearable="false"
              style="width: 100%; max-width: 200px"
              @update:value="reloadDay"
            />
          </n-gi>
          <n-gi>
            <n-text depth="3" class="gp-hint">
              当日已配置 {{ configuredCount }} 个渠道，权重合计 {{ totalWeight }}
            </n-text>
          </n-gi>
        </n-grid>
      </template>

      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 20 }"
        :row-key="(row: RollRow) => row.channel_id"
        :scroll-x="1100"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="暂无轮询配置" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="showGenerate" preset="card" title="批量生成轮询" style="width: 640px; max-width: 94vw">
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="支付渠道">
          <n-select
            v-model:value="generateForm.channel_ids"
            :options="channelOptions"
            multiple
            filterable
            placeholder="选择要生成轮询的渠道"
          />
        </n-form-item>
        <n-form-item label="日期区间">
          <n-date-picker v-model:value="generateForm.range" type="daterange" clearable style="width: 100%; max-width: 320px" />
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        按所选渠道与日期逐天生成轮询配置，已存在的日期会由后端按幂等规则跳过。
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showGenerate = false">取消</n-button>
          <n-button type="primary" :loading="generating" @click="runGenerate">开始生成</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
