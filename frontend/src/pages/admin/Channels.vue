<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import type { Channel, PluginConfigField, PluginMeta } from '@/api';
import { errorMessage, http } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { paymentTypeText } from '@/utils/format';
import { asRow, itemsOf, prettyJson } from './shared';

interface ChannelForm {
  name: string;
  plugin_code: string;
  payment_type: string;
  priority: number | null;
  weight: number | null;
  status: string;
  api_url: string;
  pid: string;
  key: string;
  config: Record<string, string>;
}

interface ChannelPayload {
  name: string;
  plugin_code: string;
  payment_type: string;
  priority: number;
  weight: number;
  status: string;
  api_url: string;
  pid: string;
  key: string;
  config: Record<string, string>;
}

interface TestResult {
  channelName: string;
  ok: boolean;
  pay_type: string;
  pay_info: string;
  error: string;
}

const message = useMessage();

const rows = ref<Channel[]>([]);
const plugins = ref<PluginMeta[]>([]);
const pluginsReady = ref(false);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);

const testingId = ref('');
const showTest = ref(false);
const testResult = ref<TestResult | null>(null);

/** 内置插件编码：插件注册表未就绪时也能选择 */
const KNOWN_PLUGIN_CODES = [
  'epay',
  'sandbox',
  'alipay',
  'wechatpayv3',
  'qqpay',
  'unionpay',
  'jeepay',
  'generic',
  'usdt',
];

/** select 类型字段的固定选项；未声明的 select 字段退化为文本输入 */
const SELECT_OPTIONS: Record<string, Record<string, SelectOption[]>> = {
  generic: {
    sign_type: [
      { label: 'MD5 签名', value: 'md5' },
      { label: '不签名', value: 'none' },
    ],
  },
  usdt: {
    action: [
      { label: '跳转支付', value: 'jump' },
      { label: '二维码', value: 'qrcode' },
    ],
  },
  unionpay: {
    submit_mode: [
      { label: 'HTML 表单', value: 'html' },
      { label: '跳转', value: 'jump' },
    ],
  },
};

const form = reactive<ChannelForm>({
  name: '',
  plugin_code: 'epay',
  payment_type: 'alipay',
  priority: 0,
  weight: 1,
  status: 'active',
  api_url: '',
  pid: '',
  key: '',
  config: {},
});

const pluginOptions = computed<SelectOption[]>(() => {
  const metaByCode = new Map(plugins.value.map((item) => [item.code, item]));
  const codes = [...KNOWN_PLUGIN_CODES];
  plugins.value.forEach((item) => {
    if (!codes.includes(item.code)) codes.push(item.code);
  });
  return codes.map((code) => {
    const meta = metaByCode.get(code);
    return { label: meta && meta.name ? `${meta.name}（${code}）` : code, value: code };
  });
});

const selectedPlugin = computed<PluginMeta | null>(
  () => plugins.value.find((item) => item.code === form.plugin_code) ?? null,
);

const configFields = computed<PluginConfigField[]>(() => selectedPlugin.value?.config_fields ?? []);
const isEpay = computed(() => form.plugin_code === 'epay');
const isSandbox = computed(() => form.plugin_code === 'sandbox');
const isEditing = computed(() => editingId.value !== null);

const paymentTypeOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信支付', value: 'wxpay' },
  { label: 'QQ 钱包', value: 'qqpay' },
  { label: '银联', value: 'unionpay' },
  { label: '网银', value: 'bank' },
  { label: '收银台', value: 'cashier' },
  { label: '沙箱支付', value: 'sandbox' },
  { label: 'USDT', value: 'usdt' },
];

const statusOptions: SelectOption[] = [
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'disabled' },
];

const rules = computed<FormRules>(() => {
  const base: FormRules = {
    name: { required: true, message: '请输入渠道名称', trigger: ['blur', 'input'] },
    plugin_code: { required: true, message: '请选择插件', trigger: ['change', 'blur'] },
    payment_type: { required: true, message: '请选择支付方式', trigger: ['change', 'blur'] },
    status: { required: true, message: '请选择状态', trigger: ['change', 'blur'] },
  };
  if (isEpay.value) {
    base.api_url = { required: true, message: '请输入上游接口地址', trigger: ['blur', 'input'] };
    base.pid = { required: true, message: '请输入上游商户号', trigger: ['blur', 'input'] };
    base.key = { required: true, message: '请输入上游密钥', trigger: ['blur', 'input'] };
  }
  configFields.value.forEach((field) => {
    if (!field.required) return;
    base[`config.${field.key}`] = {
      required: true,
      message: `请填写 ${field.label || field.key}`,
      trigger: ['blur', 'input', 'change'],
    };
  });
  return base;
});

function optionsFor(code: string, field: PluginConfigField): SelectOption[] | null {
  if (field.type !== 'select') return null;
  const perPlugin = SELECT_OPTIONS[code];
  const options = perPlugin ? perPlugin[field.key] : undefined;
  return options && options.length > 0 ? options : null;
}

function fieldOptions(field: PluginConfigField): SelectOption[] | null {
  return optionsFor(form.plugin_code, field);
}

function inputTypeOf(field: PluginConfigField): 'text' | 'password' | 'textarea' {
  if (field.type === 'password') return 'password';
  if (field.type === 'textarea') return 'textarea';
  return 'text';
}

function fieldPlaceholder(field: PluginConfigField): string {
  if (field.type === 'password' || field.secret) {
    return isEditing.value ? '编辑需重新填写密钥（后端不回显）' : '请输入密钥';
  }
  if (field.type === 'url') return 'https://example.com';
  if (field.type === 'number') return '请输入数字';
  return `请输入 ${field.label || field.key}`;
}

function initConfig(code: string, useDefaults: boolean) {
  const next: Record<string, string> = {};
  const meta = plugins.value.find((item) => item.code === code);
  (meta?.config_fields ?? []).forEach((field) => {
    const options = optionsFor(code, field);
    next[field.key] = useDefaults && options ? String(options[0].value ?? '') : '';
  });
  form.config = next;
}

async function load() {
  loading.value = true;
  try {
    rows.value = itemsOf<Channel>(await adminApi.channels());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

async function loadPlugins() {
  try {
    plugins.value = itemsOf<PluginMeta>(await adminApi.plugins());
    pluginsReady.value = true;
  } catch {
    plugins.value = [];
    pluginsReady.value = false;
  }
}

function resetForm() {
  form.name = '';
  form.plugin_code = 'epay';
  form.payment_type = 'alipay';
  form.priority = 0;
  form.weight = 1;
  form.status = 'active';
  form.api_url = '';
  form.pid = '';
  form.key = '';
  form.config = {};
}

function openCreate() {
  editingId.value = null;
  resetForm();
  initConfig('epay', true);
  showForm.value = true;
}

function openEdit(row: Channel) {
  editingId.value = row.id;
  form.name = row.name ?? '';
  form.plugin_code = row.plugin_code || 'epay';
  form.payment_type = row.payment_type ?? 'alipay';
  form.priority = typeof row.priority === 'number' ? row.priority : 0;
  form.weight = typeof row.weight === 'number' ? row.weight : 1;
  form.status = row.status ?? 'active';
  // 后端不回显加密配置，编辑时一律重新填写
  form.api_url = '';
  form.pid = '';
  form.key = '';
  initConfig(form.plugin_code, false);
  showForm.value = true;
}

function onPluginChange(code: string) {
  form.plugin_code = code;
  form.config = {};
  initConfig(code, true);
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const config: Record<string, string> = {};
    configFields.value.forEach((field) => {
      const raw = form.config[field.key];
      config[field.key] = typeof raw === 'string' ? raw.trim() : '';
    });
    const payload: ChannelPayload = {
      name: form.name.trim(),
      plugin_code: form.plugin_code,
      payment_type: form.payment_type,
      priority: form.priority ?? 0,
      weight: form.weight ?? 1,
      status: form.status,
      api_url: isEpay.value ? form.api_url.trim() : '',
      pid: isEpay.value ? form.pid.trim() : '',
      key: isEpay.value ? form.key.trim() : '',
      config,
    };
    if (isEpay.value) {
      // epay 旧字段同时映射进 config，后端两种读法都能拿到
      config.api_url = payload.api_url;
      config.pid = payload.pid;
      config.key = payload.key;
    }
    if (editingId.value) {
      await adminApi.updateChannel(editingId.value, payload);
      message.success('渠道已更新');
    } else {
      await adminApi.createChannel(payload);
      message.success('渠道已创建');
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: Channel) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  try {
    await adminApi.setChannelStatus(row.id, next);
    message.success(next === 'active' ? '渠道已启用' : '渠道已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

/** 渠道测试：失败也只在弹窗里展示，不抛出异常 */
async function testChannel(row: Channel) {
  testingId.value = row.id;
  try {
    const { data } = await http.post<{
      ok?: boolean;
      pay_type?: string;
      pay_info?: unknown;
      error?: string;
    }>(`/admin/channels/${row.id}/test`, {
      payment_type: row.payment_type,
      amount: 100,
    });
    testResult.value = {
      channelName: row.name || row.id,
      ok: data.ok === true,
      pay_type: data.pay_type || '-',
      pay_info: data.pay_info === undefined || data.pay_info === null || data.pay_info === '' ? '-' : prettyJson(data.pay_info),
      error: data.error || '',
    };
  } catch (error) {
    testResult.value = {
      channelName: row.name || row.id,
      ok: false,
      pay_type: '-',
      pay_info: '-',
      error: errorMessage(error),
    };
  } finally {
    testingId.value = '';
    showTest.value = true;
  }
}

function configuredTag(row: Channel) {
  const value = asRow(row).configured;
  const configured = value === true || value === 1 || value === 'true';
  return h(NTag, { size: 'small', round: true, type: configured ? 'success' : 'warning' }, {
    default: () => (configured ? '已配置' : '未配置'),
  });
}

const columns = computed<DataTableColumns<Channel>>(() => {
  const testing = testingId.value;
  return [
    { title: '渠道名称', key: 'name', minWidth: 150, render: (row) => row.name || '-' },
    {
      title: '插件',
      key: 'plugin_code',
      width: 130,
      render: (row) => h('span', { class: 'mono' }, row.plugin_code || '-'),
    },
    { title: '支付方式', key: 'payment_type', width: 110, render: (row) => paymentTypeText(row.payment_type) },
    { title: '优先级', key: 'priority', width: 90, align: 'right', render: (row) => row.priority ?? '-' },
    { title: '权重', key: 'weight', width: 90, align: 'right', render: (row) => row.weight ?? '-' },
    {
      title: '状态',
      key: 'status',
      width: 90,
      render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
    },
    { title: '配置', key: 'configured', width: 100, render: (row) => configuredTag(row) },
    {
      title: '操作',
      key: 'actions',
      width: 220,
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
                {
                  size: 'tiny',
                  quaternary: true,
                  loading: testing === row.id,
                  onClick: () => testChannel(row),
                },
                { default: () => '测试' },
              ),
              h(
                NPopconfirm,
                { onPositiveClick: () => toggleStatus(row) },
                {
                  trigger: () =>
                    h(
                      NButton,
                      { size: 'tiny', quaternary: true, type: row.status === 'active' ? 'warning' : 'primary' },
                      { default: () => (row.status === 'active' ? '禁用' : '启用') },
                    ),
                  default: () => (row.status === 'active' ? '确认禁用该渠道？' : '确认启用该渠道？'),
                },
              ),
            ],
          },
        ),
    },
  ];
});

onMounted(() => {
  void load();
  void loadPlugins();
});
</script>

<template>
  <div class="page">
    <PageHeader title="支付渠道" subtitle="上游支付通道的插件配置、优先级、权重与启用状态管理">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="openCreate">新建渠道</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :row-key="(row: Channel) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
      >
        <template #empty>
          <n-empty description="暂无支付渠道，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑渠道' : '新建渠道'"
      style="width: 720px; max-width: 94vw"
    >
      <n-alert v-if="isEditing" type="warning" :bordered="false" style="margin-bottom: 16px">
        编辑时不回显已保存的配置（密钥类字段一律留空），请重新填写本插件需要的全部字段；留空提交会以空值覆盖原配置。
      </n-alert>
      <n-alert v-else-if="isSandbox" type="info" :bordered="false" style="margin-bottom: 16px">
        沙箱渠道不依赖上游配置，不填配置也能直接保存。
      </n-alert>
      <n-alert v-else-if="!pluginsReady" type="info" :bordered="false" style="margin-bottom: 16px">
        插件元数据接口未就绪，当前仅能选择插件编码；配置字段由插件注册表提供，接口可用后自动出现。
      </n-alert>

      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="130">
        <n-form-item label="渠道名称" path="name">
          <n-input v-model:value="form.name" placeholder="例如：支付宝-主通道" />
        </n-form-item>
        <n-form-item label="插件" path="plugin_code">
          <n-select :value="form.plugin_code" :options="pluginOptions" @update:value="onPluginChange" />
        </n-form-item>
        <n-form-item label="支付方式" path="payment_type">
          <n-select v-model:value="form.payment_type" :options="paymentTypeOptions" />
        </n-form-item>
        <n-form-item label="优先级" path="priority">
          <n-input-number v-model:value="form.priority" :min="0" :step="1" style="width: 100%" />
        </n-form-item>
        <n-form-item label="权重" path="weight">
          <n-input-number v-model:value="form.weight" :min="1" :step="1" style="width: 100%" />
        </n-form-item>
        <n-form-item label="状态" path="status">
          <n-select v-model:value="form.status" :options="statusOptions" />
        </n-form-item>

        <template v-if="configFields.length > 0">
          <n-divider title-placement="left">插件配置（{{ form.plugin_code }}）</n-divider>
          <n-form-item
            v-for="field in configFields"
            :key="field.key"
            :label="field.label || field.key"
            :path="`config.${field.key}`"
          >
            <div class="field-wrap">
              <n-select
                v-if="fieldOptions(field)"
                v-model:value="form.config[field.key]"
                :options="fieldOptions(field) ?? []"
                :placeholder="fieldPlaceholder(field)"
              />
              <n-input
                v-else
                v-model:value="form.config[field.key]"
                :type="inputTypeOf(field)"
                :show-password-on="field.type === 'password' ? 'click' : undefined"
                :autosize="field.type === 'textarea' ? { minRows: 3, maxRows: 8 } : undefined"
                :placeholder="fieldPlaceholder(field)"
              />
              <div class="field-hint">
                <span class="mono">{{ field.key }}</span>
                <span v-if="field.required" class="tag-required">必填</span>
                <span v-if="field.secret" class="tag-secret">密文</span>
                <span v-if="field.secret && isEditing" class="muted">编辑需重新填写密钥（后端不回显）</span>
              </div>
            </div>
          </n-form-item>
        </template>

        <template v-if="isEpay">
          <n-divider title-placement="left">上游参数（epay 兼容字段，同时写入 config）</n-divider>
          <n-form-item label="接口地址" path="api_url">
            <n-input v-model:value="form.api_url" placeholder="上游网关地址，例如 https://epay.example.com" />
          </n-form-item>
          <n-form-item label="商户号 PID" path="pid">
            <n-input v-model:value="form.pid" placeholder="上游分配的商户号" />
          </n-form-item>
          <n-form-item label="密钥 Key" path="key">
            <n-input v-model:value="form.key" type="password" show-password-on="click" placeholder="上游分配的通信密钥" />
          </n-form-item>
        </template>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showTest" preset="card" title="渠道测试结果" style="width: 660px; max-width: 94vw">
      <n-empty v-if="!testResult" description="暂无测试结果" />
      <template v-else>
        <n-descriptions :column="1" label-placement="left" size="small" bordered>
          <n-descriptions-item label="渠道">{{ testResult.channelName }}</n-descriptions-item>
          <n-descriptions-item label="结果">
            <n-tag :type="testResult.ok ? 'success' : 'error'" size="small" round>
              {{ testResult.ok ? '成功' : '失败' }}
            </n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="pay_type">{{ testResult.pay_type }}</n-descriptions-item>
          <n-descriptions-item v-if="testResult.error" label="错误">
            <span class="error-text">{{ testResult.error }}</span>
          </n-descriptions-item>
        </n-descriptions>
        <div class="payinfo">
          <div class="payinfo-label">pay_info</div>
          <pre class="json">{{ testResult.pay_info }}</pre>
        </div>
      </template>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showTest = false">关闭</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.field-wrap {
  width: 100%;
}
.field-hint {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 4px;
  font-size: 12px;
  color: #64748b;
}
.tag-required {
  color: #b45309;
}
.tag-secret {
  color: #b91c1c;
}
.payinfo {
  margin-top: 14px;
}
.payinfo-label {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 6px;
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
.error-text {
  color: #b91c1c;
}
</style>
