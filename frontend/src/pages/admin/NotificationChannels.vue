<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { notificationChannelsApi } from '@/api/notificationChannels';
import type { NotificationChannel } from '@/api/notificationChannels';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

interface ConfigRow {
  key: string;
  value: string;
}

interface ChannelForm {
  kind: string;
  provider: string;
  status: string;
}

const message = useMessage();

const rows = ref<NotificationChannel[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);
const configRows = ref<ConfigRow[]>([]);

const showTest = ref(false);
const testTarget = ref<NotificationChannel | null>(null);
const testValue = ref('');
const testing = ref(false);

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const form = reactive<ChannelForm>({
  kind: 'sms',
  provider: '',
  status: 'active',
});

const kindOptions: SelectOption[] = [
  { label: '短信（sms）', value: 'sms' },
  { label: '邮件（mail）', value: 'mail' },
];

const statusOptions: SelectOption[] = [
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'disabled' },
];

const kindLabels: Record<string, string> = { sms: '短信', mail: '邮件' };

const sampleKeys: Record<string, string[]> = {
  sms: ['access_key', 'secret', 'sign_name', 'template_code'],
  mail: ['host', 'port', 'username', 'password', 'from'],
};

const rules: FormRules = {
  kind: { required: true, message: '请选择通知类型', trigger: ['change', 'blur'] },
  provider: { required: true, message: '请输入服务商名称', trigger: ['blur', 'input'] },
};

const isEditing = computed(() => editingId.value !== null);

async function load() {
  loading.value = true;
  try {
    const data = await notificationChannelsApi.list({ page: page.value, page_size: pageSize.value });
    const items = itemsOf<NotificationChannel>(data);
    rows.value = items;
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function addRow(key = '', value = '') {
  configRows.value = [...configRows.value, { key, value }];
}

function removeRow(index: number) {
  configRows.value = configRows.value.filter((_, i) => i !== index);
}

function fillSampleKeys() {
  const keys = sampleKeys[form.kind] ?? [];
  const existing = new Set(configRows.value.map((row) => row.key.trim()));
  const missing = keys.filter((key) => !existing.has(key));
  if (missing.length === 0) {
    message.info('示例键已经都在列表里了');
    return;
  }
  configRows.value = [...configRows.value, ...missing.map((key) => ({ key, value: '' }))];
}

function openCreate() {
  editingId.value = null;
  form.kind = 'sms';
  form.provider = '';
  form.status = 'active';
  configRows.value = [];
  for (const key of sampleKeys.sms) addRow(key, '');
  showForm.value = true;
}

function openEdit(row: NotificationChannel) {
  editingId.value = row.id;
  form.kind = row.kind || 'sms';
  form.provider = row.provider || '';
  form.status = row.status || 'active';
  // 后端只回键名，值一律留空表示保持原值
  const keys = (row.config_keys ?? []).filter((key) => typeof key === 'string' && key !== '');
  configRows.value = keys.length > 0 ? keys.map((key) => ({ key, value: '' })) : [];
  showForm.value = true;
}

function buildConfig(includeEmpty: boolean): Record<string, string> {
  const config: Record<string, string> = {};
  configRows.value.forEach((row) => {
    const key = row.key.trim();
    if (key === '') return;
    const value = row.value.trim();
    if (value === '' && !includeEmpty) return;
    config[key] = value;
  });
  return config;
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  const keys = configRows.value.map((row) => row.key.trim()).filter((key) => key !== '');
  if (keys.length === 0) {
    message.warning('至少配置一个配置项');
    return;
  }
  submitting.value = true;
  try {
    const payload = {
      kind: form.kind,
      provider: form.provider.trim(),
      status: form.status,
      // 新建时把空值也提交，编辑时留空表示保持原值
      config: buildConfig(!editingId.value),
    };
    if (editingId.value) {
      await notificationChannelsApi.update(editingId.value, payload);
      message.success('通知通道已更新');
    } else {
      await notificationChannelsApi.create(payload);
      message.success('通知通道已创建');
      // 新建后回到第一页，避免新记录在别的页看不到
      resetPage();
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function remove(row: NotificationChannel) {
  try {
    await notificationChannelsApi.remove(row.id);
    message.success('通知通道已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function openTest(row: NotificationChannel) {
  testTarget.value = row;
  testValue.value = '';
  showTest.value = true;
}

async function runTest() {
  const target = testTarget.value;
  if (!target) return;
  const value = testValue.value.trim();
  if (value === '') {
    message.warning('请输入测试接收地址');
    return;
  }
  testing.value = true;
  try {
    const result = await notificationChannelsApi.test(target.id, value);
    if (result?.sent === false) {
      message.error(result.message || '测试消息发送失败');
    } else {
      message.success('测试消息已发送');
    }
    showTest.value = false;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    testing.value = false;
  }
}

function kindTag(kind?: string) {
  const sms = (kind ?? '') === 'sms';
  return h(
    NTag,
    { size: 'small', round: true, type: sms ? 'info' : 'success', bordered: false },
    { default: () => kindLabels[kind ?? ''] ?? kind ?? '-' },
  );
}

const columns = computed<DataTableColumns<NotificationChannel>>(() => [
  { title: '类型', key: 'kind', width: 100, render: (row) => kindTag(row.kind) },
  {
    title: '服务商',
    key: 'provider',
    width: 160,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.provider || '-'),
  },
  {
    title: '配置键',
    key: 'config_keys',
    minWidth: 260,
    render: (row) => {
      const keys = (row.config_keys ?? []).filter((key) => typeof key === 'string' && key !== '');
      if (keys.length === 0) return h('span', { class: 'muted' }, '-');
      return h(
        'div',
        { class: 'tag-cell' },
        keys.map((key) => h(NTag, { size: 'small', round: true, bordered: false }, { default: () => key })),
      );
    },
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '更新时间',
    key: 'updated_at',
    width: 170,
    render: (row) => formatDateTime(row.updated_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
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
              NButton,
              { size: 'tiny', quaternary: true, type: 'primary', onClick: () => openTest(row) },
              { default: () => '测试' },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => remove(row) },
              {
                trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                default: () => '确认删除该通知通道？',
              },
            ),
          ],
        },
      ),
  },
]);

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="通知通道" subtitle="短信与邮件的服务商配置，配置键值由后端加密保存">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="openCreate">新建通道</n-button>
      </template>
    </PageHeader>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      没有接真实短信/邮件网关时，测试消息只会打到服务端日志，不会真的发出去。
    </n-alert>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: NotificationChannel) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无通知通道，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑通知通道' : '新建通知通道'"
      style="width: 700px; max-width: 94vw"
    >
      <n-alert v-if="isEditing" type="warning" :bordered="false" style="margin-bottom: 16px">
        后端只回配置键名、不回具体值：这里列出的是已有键，值留空表示保持原值，填写才会覆盖。
      </n-alert>
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="110">
        <n-form-item label="类型" path="kind">
          <n-select v-model:value="form.kind" :options="kindOptions" />
        </n-form-item>
        <n-form-item label="服务商" path="provider">
          <n-input v-model:value="form.provider" placeholder="例如 aliyun / tencent / smtp" />
        </n-form-item>
        <n-form-item label="状态">
          <n-select v-model:value="form.status" :options="statusOptions" />
        </n-form-item>
        <n-form-item label="配置项">
          <div class="config-box">
            <div class="config-hint muted small">
              {{ form.kind === 'sms'
                ? '短信常见键：access_key、secret、sign_name、template_code'
                : '邮件常见键：host、port、username、password、from' }}
            </div>
            <div v-for="(row, index) in configRows" :key="index" class="kv-row">
              <n-input v-model:value="row.key" placeholder="配置键" style="width: 220px" />
              <n-input
                v-model:value="row.value"
                :placeholder="isEditing ? '留空表示保持原值' : '配置值'"
                style="flex: 1; min-width: 180px"
              />
              <n-button size="small" quaternary type="error" @click="removeRow(index)">删除</n-button>
            </div>
            <div class="kv-actions">
              <n-button size="small" dashed @click="addRow()">添加配置项</n-button>
              <n-button size="small" quaternary type="primary" @click="fillSampleKeys">插入示例键</n-button>
            </div>
          </div>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showTest" preset="card" title="发送测试消息" style="width: 520px; max-width: 94vw">
      <n-descriptions v-if="testTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="类型">{{ kindLabels[testTarget.kind] ?? testTarget.kind }}</n-descriptions-item>
        <n-descriptions-item label="服务商">{{ testTarget.provider || '-' }}</n-descriptions-item>
      </n-descriptions>
      <n-form-item label="接收目标" style="margin-top: 16px">
        <n-input
          v-model:value="testValue"
          :placeholder="testTarget?.kind === 'mail' ? '收件邮箱，例如 ops@example.com' : '手机号，例如 13800000000'"
        />
      </n-form-item>
      <n-alert type="info" :bordered="false">
        未配置真实网关时，这条测试消息只会写入服务端日志。
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showTest = false">取消</n-button>
          <n-button type="primary" :loading="testing" @click="runTest">发送测试</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.small {
  font-size: 12px;
}
.tag-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.config-box {
  width: 100%;
}
.config-hint {
  margin-bottom: 8px;
}
.kv-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}
.kv-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
