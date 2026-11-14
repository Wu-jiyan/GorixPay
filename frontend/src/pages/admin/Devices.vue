<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { devicesApi } from '@/api/devices';
import type { Printer, VoiceDevice } from '@/api/devices';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { copyText, formatDateTime } from '@/utils/format';
import { asRow, itemsOf } from './shared';

interface DeviceForm {
  device_no: string;
  name: string;
  status: string;
}

interface PrinterForm {
  name: string;
  status: string;
}

const message = useMessage();

const tab = ref('voice');

const devices = ref<VoiceDevice[]>([]);
const printers = ref<Printer[]>([]);
const devicesLoading = ref(false);
const printersLoading = ref(false);

const showDeviceForm = ref(false);
const deviceEditingId = ref<string | null>(null);
const deviceFormRef = ref<FormInst | null>(null);
const deviceSubmitting = ref(false);
const deviceForm = reactive<DeviceForm>({ device_no: '', name: '', status: 'active' });

const showPrinterForm = ref(false);
const printerEditingId = ref<string | null>(null);
const printerFormRef = ref<FormInst | null>(null);
const printerSubmitting = ref(false);
const printerForm = reactive<PrinterForm>({ name: '', status: 'active' });

const showSecret = ref(false);
const secretTitle = ref('');
const secretValue = ref('');

const showVoiceTest = ref(false);
const voiceTestTarget = ref<VoiceDevice | null>(null);
const voiceTestText = ref('');
const voiceTesting = ref(false);
const printerTestingId = ref('');

const statusOptions: SelectOption[] = [
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'disabled' },
];

const deviceRules: FormRules = {
  device_no: { required: true, message: '请输入设备号', trigger: ['blur', 'input'] },
  name: { required: true, message: '请输入设备名称', trigger: ['blur', 'input'] },
};

const printerRules: FormRules = {
  name: { required: true, message: '请输入打印机名称', trigger: ['blur', 'input'] },
};

async function loadDevices() {
  devicesLoading.value = true;
  try {
    devices.value = itemsOf<VoiceDevice>(await devicesApi.voiceDevices());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    devicesLoading.value = false;
  }
}

async function loadPrinters() {
  printersLoading.value = true;
  try {
    printers.value = itemsOf<Printer>(await devicesApi.printers());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    printersLoading.value = false;
  }
}

function handleTab(value: string) {
  tab.value = value;
  if (value === 'voice') void loadDevices();
  else void loadPrinters();
}

function openSecret(title: string, secret?: string) {
  if (!secret) return;
  secretTitle.value = title;
  secretValue.value = secret;
  showSecret.value = true;
}

async function copySecret() {
  const ok = await copyText(secretValue.value);
  if (ok) message.success('已复制');
  else message.warning('复制失败，请手动选择');
}

function openCreateDevice() {
  deviceEditingId.value = null;
  deviceForm.device_no = '';
  deviceForm.name = '';
  deviceForm.status = 'active';
  showDeviceForm.value = true;
}

function openEditDevice(row: VoiceDevice) {
  deviceEditingId.value = row.id;
  deviceForm.device_no = row.device_no || '';
  deviceForm.name = row.name || '';
  deviceForm.status = row.status || 'active';
  showDeviceForm.value = true;
}

async function submitDevice() {
  try {
    await deviceFormRef.value?.validate();
  } catch {
    return;
  }
  deviceSubmitting.value = true;
  try {
    const payload = {
      device_no: deviceForm.device_no.trim(),
      name: deviceForm.name.trim(),
      status: deviceForm.status,
    };
    if (deviceEditingId.value) {
      await devicesApi.updateVoiceDevice(deviceEditingId.value, payload);
      message.success('语音设备已更新');
    } else {
      const created = await devicesApi.createVoiceDevice(payload);
      message.success('语音设备已创建');
      const secret = asRow(created).secret;
      openSecret('设备密钥', typeof secret === 'string' ? secret : '');
    }
    showDeviceForm.value = false;
    await loadDevices();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    deviceSubmitting.value = false;
  }
}

async function removeDevice(row: VoiceDevice) {
  try {
    await devicesApi.deleteVoiceDevice(row.id);
    message.success('语音设备已删除');
    await loadDevices();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function openVoiceTest(row: VoiceDevice) {
  voiceTestTarget.value = row;
  voiceTestText.value = '';
  showVoiceTest.value = true;
}

async function runVoiceTest() {
  const target = voiceTestTarget.value;
  if (!target) return;
  const text = voiceTestText.value.trim();
  if (text === '') {
    message.warning('请输入播报内容');
    return;
  }
  voiceTesting.value = true;
  try {
    await devicesApi.testVoiceDevice(target.id, text);
    message.success('测试播报已入队');
    showVoiceTest.value = false;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    voiceTesting.value = false;
  }
}

function openCreatePrinter() {
  printerEditingId.value = null;
  printerForm.name = '';
  printerForm.status = 'active';
  showPrinterForm.value = true;
}

function openEditPrinter(row: Printer) {
  printerEditingId.value = row.id;
  printerForm.name = row.name || '';
  printerForm.status = row.status || 'active';
  showPrinterForm.value = true;
}

async function submitPrinter() {
  try {
    await printerFormRef.value?.validate();
  } catch {
    return;
  }
  printerSubmitting.value = true;
  try {
    const payload = { name: printerForm.name.trim(), status: printerForm.status };
    if (printerEditingId.value) {
      await devicesApi.updatePrinter(printerEditingId.value, payload);
      message.success('打印机已更新');
    } else {
      const created = await devicesApi.createPrinter(payload);
      message.success('打印机已创建');
      const secret = asRow(created).secret;
      openSecret('打印机密钥', typeof secret === 'string' ? secret : '');
    }
    showPrinterForm.value = false;
    await loadPrinters();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    printerSubmitting.value = false;
  }
}

async function removePrinter(row: Printer) {
  try {
    await devicesApi.deletePrinter(row.id);
    message.success('打印机已删除');
    await loadPrinters();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

async function testPrinter(row: Printer) {
  printerTestingId.value = row.id;
  try {
    await devicesApi.testPrinter(row.id);
    message.success('测试打印已入队');
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    printerTestingId.value = '';
  }
}

const deviceColumns = computed<DataTableColumns<VoiceDevice>>(() => [
  {
    title: '设备号',
    key: 'device_no',
    width: 220,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.device_no || '-'),
  },
  { title: '名称', key: 'name', minWidth: 160, ellipsis: true, render: (row) => row.name || '-' },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
  },
  {
    title: '最后心跳',
    key: 'last_seen_at',
    width: 170,
    render: (row) => formatDateTime(row.last_seen_at),
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
              { size: 'tiny', quaternary: true, type: 'info', onClick: () => openEditDevice(row) },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              { size: 'tiny', quaternary: true, type: 'primary', onClick: () => openVoiceTest(row) },
              { default: () => '测试播报' },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => removeDevice(row) },
              {
                trigger: () =>
                  h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                default: () => '确认删除该语音设备？删除后设备无法再轮询。',
              },
            ),
          ],
        },
      ),
  },
]);

const printerColumns = computed<DataTableColumns<Printer>>(() => {
  const testing = printerTestingId.value;
  return [
    { title: '名称', key: 'name', minWidth: 200, ellipsis: true, render: (row) => row.name || '-' },
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
                { size: 'tiny', quaternary: true, type: 'info', onClick: () => openEditPrinter(row) },
                { default: () => '编辑' },
              ),
              h(
                NButton,
                {
                  size: 'tiny',
                  quaternary: true,
                  type: 'primary',
                  loading: testing === row.id,
                  onClick: () => testPrinter(row),
                },
                { default: () => '测试打印' },
              ),
              h(
                NPopconfirm,
                { onPositiveClick: () => removePrinter(row) },
                {
                  trigger: () =>
                    h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                  default: () => '确认删除该打印机？',
                },
              ),
            ],
          },
        ),
    },
  ];
});

onMounted(() => {
  void loadDevices();
});
</script>

<template>
  <div class="page">
    <PageHeader title="语音设备与打印机" subtitle="设备侧拿 device_no + secret 轮询，播报与打印由支付成功钩子入队">
      <template #actions>
        <n-button secondary :loading="devicesLoading || printersLoading" @click="handleTab(tab)">刷新</n-button>
        <n-button v-if="tab === 'voice'" type="primary" @click="openCreateDevice">新增语音设备</n-button>
        <n-button v-else type="primary" @click="openCreatePrinter">新增打印机</n-button>
      </template>
    </PageHeader>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      设备端用「device_no + secret」轮询取任务；支付成功钩子会把播报文案与打印小票写入队列。
      创建时返回的 secret 只显示一次，丢了只能删设备重建。
    </n-alert>

    <n-card :bordered="false">
      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="voice" tab="语音设备">
          <n-data-table
            :columns="deviceColumns"
            :data="devices"
            :loading="devicesLoading"
            :pagination="{ pageSize: 10 }"
            :row-key="(row: VoiceDevice) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
          >
            <template #empty>
              <n-empty description="暂无语音设备" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>

        <n-tab-pane name="printer" tab="打印机">
          <n-data-table
            :columns="printerColumns"
            :data="printers"
            :loading="printersLoading"
            :pagination="{ pageSize: 10 }"
            :row-key="(row: Printer) => row.id"
            :bordered="false"
            :single-line="false"
            size="small"
          >
            <template #empty>
              <n-empty description="暂无打印机" size="small" />
            </template>
          </n-data-table>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <n-modal
      v-model:show="showDeviceForm"
      preset="card"
      :title="deviceEditingId ? '编辑语音设备' : '新增语音设备'"
      style="width: 560px; max-width: 94vw"
    >
      <n-form ref="deviceFormRef" :model="deviceForm" :rules="deviceRules" label-placement="left" label-width="100">
        <n-form-item label="设备号" path="device_no">
          <n-input v-model:value="deviceForm.device_no" placeholder="设备唯一编号，设备端用它轮询" :disabled="!!deviceEditingId" />
        </n-form-item>
        <n-form-item label="名称" path="name">
          <n-input v-model:value="deviceForm.name" placeholder="例如 一号收银台音箱" />
        </n-form-item>
        <n-form-item label="状态">
          <n-select v-model:value="deviceForm.status" :options="statusOptions" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDeviceForm = false">取消</n-button>
          <n-button type="primary" :loading="deviceSubmitting" @click="submitDevice">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal
      v-model:show="showPrinterForm"
      preset="card"
      :title="printerEditingId ? '编辑打印机' : '新增打印机'"
      style="width: 560px; max-width: 94vw"
    >
      <n-form ref="printerFormRef" :model="printerForm" :rules="printerRules" label-placement="left" label-width="100">
        <n-form-item label="名称" path="name">
          <n-input v-model:value="printerForm.name" placeholder="例如 前台热敏打印机" />
        </n-form-item>
        <n-form-item label="状态">
          <n-select v-model:value="printerForm.status" :options="statusOptions" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showPrinterForm = false">取消</n-button>
          <n-button type="primary" :loading="printerSubmitting" @click="submitPrinter">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showSecret" preset="card" :title="secretTitle" style="width: 560px; max-width: 94vw">
      <n-alert type="warning" :bordered="false" style="margin-bottom: 14px">
        secret 只显示这一次，关掉就查不到了；丢了只能删除设备重建。
      </n-alert>
      <div class="secret-box mono">{{ secretValue }}</div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="copySecret">复制</n-button>
          <n-button type="primary" @click="showSecret = false">我已保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showVoiceTest" preset="card" title="发送测试播报" style="width: 560px; max-width: 94vw">
      <n-descriptions v-if="voiceTestTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="设备号">
          <span class="mono">{{ voiceTestTarget.device_no }}</span>
        </n-descriptions-item>
        <n-descriptions-item label="名称">{{ voiceTestTarget.name || '-' }}</n-descriptions-item>
      </n-descriptions>
      <div class="field">
        <div class="field-label">播报内容</div>
        <n-input
          v-model:value="voiceTestText"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 4 }"
          placeholder="例如：支付宝到账 1.00 元"
        />
      </div>
      <n-alert type="info" :bordered="false" style="margin-top: 14px">
        设备在线时会通过轮询取到这条播报；离线时任务会留在队列里。
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showVoiceTest = false">取消</n-button>
          <n-button type="primary" :loading="voiceTesting" @click="runVoiceTest">发送</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.secret-box {
  word-break: break-all;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 10px;
  padding: 12px 14px;
  font-size: 13px;
  line-height: 1.6;
}
.field {
  margin-top: 16px;
}
.field-label {
  font-size: 13px;
  color: #4b5563;
  margin-bottom: 6px;
}
</style>
