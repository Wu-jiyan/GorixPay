<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import dayjs from 'dayjs';
import { adminApi } from '@/api';
import type { Channel } from '@/api';
import { payRollsApi } from '@/api/payTypes';
import type { PayRoll } from '@/api/payTypes';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { asRow, itemsOf } from './shared';

interface RollForm {
  channel_id: string;
  roll_date: number | null;
  weight: number | null;
  status: string;
}

const message = useMessage();

const rows = ref<PayRoll[]>([]);
const channels = ref<Channel[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);

const showGenerate = ref(false);
const generating = ref(false);
const generateForm = reactive<{ channel_ids: string[]; range: [number, number] | null }>({
  channel_ids: [],
  range: null,
});

const form = reactive<RollForm>({
  channel_id: '',
  roll_date: null,
  weight: 1,
  status: 'active',
});

const statusOptions: SelectOption[] = [
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'disabled' },
];

const channelOptions = computed<SelectOption[]>(() =>
  channels.value.map((item) => ({ label: item.name ? `${item.name}（${item.plugin_code}）` : item.id, value: item.id })),
);

const channelName = computed(() => {
  const map = new Map<string, string>();
  channels.value.forEach((item) => map.set(item.id, item.name || item.id));
  return map;
});

const rules: FormRules = {
  channel_id: { required: true, message: '请选择支付渠道', trigger: ['change', 'blur'] },
  roll_date: { required: true, type: 'number', message: '请选择轮询日期', trigger: ['change', 'blur'] },
  weight: { required: true, type: 'number', message: '请输入权重', trigger: ['change', 'blur'] },
};

const isEditing = computed(() => editingId.value !== null);

async function load() {
  loading.value = true;
  try {
    rows.value = itemsOf<PayRoll>(await payRollsApi.list());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

async function loadChannels() {
  try {
    channels.value = itemsOf<Channel>(await adminApi.channels());
  } catch {
    channels.value = [];
  }
}

function openCreate() {
  editingId.value = null;
  form.channel_id = channels.value[0]?.id ?? '';
  form.roll_date = null;
  form.weight = 1;
  form.status = 'active';
  showForm.value = true;
}

function openEdit(row: PayRoll) {
  editingId.value = row.id;
  form.channel_id = row.channel_id || '';
  const parsed = dayjs(row.roll_date);
  form.roll_date = parsed.isValid() ? parsed.valueOf() : null;
  form.weight = typeof row.weight === 'number' ? row.weight : 1;
  form.status = row.status || 'active';
  showForm.value = true;
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  if (form.roll_date === null) {
    message.warning('请选择轮询日期');
    return;
  }
  submitting.value = true;
  try {
    const payload = {
      channel_id: form.channel_id,
      roll_date: dayjs(form.roll_date).format('YYYY-MM-DD'),
      weight: form.weight ?? 1,
      status: form.status,
    };
    if (editingId.value) {
      await payRollsApi.update(editingId.value, payload);
      message.success('轮询配置已更新');
    } else {
      await payRollsApi.create(payload);
      message.success('轮询配置已创建');
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function remove(row: PayRoll) {
  try {
    await payRollsApi.remove(row.id);
    message.success('轮询配置已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
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

const columns = computed<DataTableColumns<PayRoll>>(() => [
  {
    title: '支付渠道',
    key: 'channel_id',
    minWidth: 260,
    ellipsis: true,
    render: (row) => {
      const name = channelName.value.get(row.channel_id);
      if (!name) return h('span', { class: 'mono' }, row.channel_id || '-');
      return h('div', [
        h('div', name),
        h('div', { class: 'mono', style: 'font-size: 12px; color: #94a3b8' }, row.channel_id),
      ]);
    },
  },
  { title: '轮询日期', key: 'roll_date', width: 140, render: (row) => row.roll_date || '-' },
  {
    title: '权重',
    key: 'weight',
    width: 100,
    align: 'right',
    render: (row) => (typeof row.weight === 'number' ? row.weight : '-'),
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 4, wrap: false },
        {
          default: () => [
            h(
              NButton,
              { size: 'tiny', quaternary: true, type: 'info', onClick: () => openEdit(row) },
              { default: () => '编辑' },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => remove(row) },
              {
                trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                default: () => '确认删除该轮询配置？',
              },
            ),
          ],
        },
      ),
  },
]);

onMounted(() => {
  void load();
  void loadChannels();
});
</script>

<template>
  <div class="page">
    <PageHeader title="按日轮询" subtitle="按日期给渠道分配权重，收银台据此轮询可用通道">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button secondary type="primary" @click="openGenerate">批量生成</n-button>
        <n-button type="primary" @click="openCreate">新建轮询</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 20 }"
        :row-key="(row: PayRoll) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
      >
        <template #empty>
          <n-empty description="暂无轮询配置" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑轮询配置' : '新建轮询配置'"
      style="width: 580px; max-width: 94vw"
    >
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="110">
        <n-form-item label="支付渠道" path="channel_id">
          <n-select
            v-model:value="form.channel_id"
            :options="channelOptions"
            filterable
            placeholder="选择渠道；渠道列表为空时可直接填 ID"
            tag
          />
        </n-form-item>
        <n-form-item label="轮询日期" path="roll_date">
          <n-date-picker v-model:value="form.roll_date" type="date" clearable style="width: 200px" />
        </n-form-item>
        <n-form-item label="权重" path="weight">
          <n-input-number v-model:value="form.weight" :min="1" :step="1" style="width: 160px" />
        </n-form-item>
        <n-form-item label="状态">
          <n-select v-model:value="form.status" :options="statusOptions" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showGenerate" preset="card" title="批量生成轮询" style="width: 580px; max-width: 94vw">
      <n-form label-placement="left" label-width="110">
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
          <n-date-picker v-model:value="generateForm.range" type="daterange" clearable style="width: 260px" />
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
  </div>
</template>
