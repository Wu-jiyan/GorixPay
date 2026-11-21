<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import {
  NButton,
  NIcon,
  NInput,
  NPopconfirm,
  NSpace,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { AddOutline, CopyOutline, PaperPlaneOutline, RefreshOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { devicesApi } from '@/api/devices';
import type { Printer, VoiceDevice } from '@/api/devices';
import { errorMessage } from '@/api/client';
import { activeStatus, copyText, formatDateTime } from '@/utils/format';
import { itemsOf, useNarrow } from './shared';

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
const isNarrow = useNarrow();

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
const removingDeviceId = ref('');
const removingPrinterId = ref('');
const resettingDeviceId = ref('');
const resettingPrinterId = ref('');

const deviceRules: FormRules = {
  device_no: { required: true, message: '请输入设备号', trigger: ['blur', 'input'] },
  name: { required: true, message: '请输入设备名称', trigger: ['blur', 'input'] },
};

const printerRules: FormRules = {
  name: { required: true, message: '请输入打印机名称', trigger: ['blur', 'input'] },
};

const deviceStatusSwitch = computed({
  get: () => deviceForm.status === 'active',
  set: (value: boolean) => {
    deviceForm.status = value ? 'active' : 'disabled';
  },
});

const printerStatusSwitch = computed({
  get: () => printerForm.status === 'active',
  set: (value: boolean) => {
    printerForm.status = value ? 'active' : 'disabled';
  },
});

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

function reloadCurrent() {
  if (tab.value === 'voice') void loadDevices();
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
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
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
      openSecret('设备密钥', created.secret);
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
  removingDeviceId.value = row.id;
  try {
    await devicesApi.deleteVoiceDevice(row.id);
    message.success('语音设备已删除');
    await loadDevices();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingDeviceId.value = '';
  }
}

/** 重置语音设备密钥：POST /admin/voice-devices/:id/reset-secret，回 {secret} 只显示这一次 */
async function resetDeviceSecret(row: VoiceDevice) {
  resettingDeviceId.value = row.id;
  try {
    const result = await devicesApi.resetVoiceDeviceSecret(row.id);
    message.success('设备密钥已重置');
    openSecret(`设备密钥 · ${row.device_no}`, result.secret);
    await loadDevices();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    resettingDeviceId.value = '';
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
      openSecret('打印机密钥', created.secret);
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
  removingPrinterId.value = row.id;
  try {
    await devicesApi.deletePrinter(row.id);
    message.success('打印机已删除');
    await loadPrinters();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingPrinterId.value = '';
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

/** 重置打印机密钥：POST /admin/printers/:id/reset-secret，回 {secret} 只显示这一次 */
async function resetPrinterSecret(row: Printer) {
  resettingPrinterId.value = row.id;
  try {
    const result = await devicesApi.resetPrinterSecret(row.id);
    message.success('打印机密钥已重置');
    openSecret(`打印机密钥 · ${row.name || row.id}`, result.secret);
    await loadPrinters();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    resettingPrinterId.value = '';
  }
}

function statusTag(status?: string) {
  const meta = activeStatus(status);
  return h(
    NTag,
    { size: 'small', round: true, type: meta.type, bordered: false },
    { default: () => meta.text },
  );
}

const deviceColumns = computed<DataTableColumns<VoiceDevice>>(() => [
  {
    title: '设备号',
    key: 'device_no',
    width: 220,
    ellipsis: true,
    render: (row) => h(NText, { code: true }, { default: () => row.device_no || '-' }),
  },
  { title: '名称', key: 'name', minWidth: 160, ellipsis: true, render: (row) => row.name || '-' },
  { title: '状态', key: 'status', width: 110, render: (row) => statusTag(row.status) },
  {
    title: '最后心跳',
    key: 'last_seen_at',
    width: 170,
    render: (row) => formatDateTime(row.last_seen_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 280,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 12, wrap: false },
        {
          default: () => [
            h(
              NButton,
              { text: true, size: 'small', type: 'primary', onClick: () => openEditDevice(row) },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              { text: true, size: 'small', type: 'info', onClick: () => openVoiceTest(row) },
              { default: () => '测试播报' },
            ),
            h(
              NPopconfirm,
              { positiveText: '确定', negativeText: '取消', onPositiveClick: () => resetDeviceSecret(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: 'warning',
                      loading: resettingDeviceId.value === row.id,
                      disabled: resettingDeviceId.value === row.id,
                    },
                    { default: () => '重置密钥' },
                  ),
                default: () => '重置后旧密钥立即失效，设备需要用新 secret 才能继续轮询。',
              },
            ),
            h(
              NPopconfirm,
              { positiveText: '确定', negativeText: '取消', onPositiveClick: () => removeDevice(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: 'error',
                      loading: removingDeviceId.value === row.id,
                    },
                    { default: () => '删除' },
                  ),
                default: () => '确认删除该语音设备？删除后设备无法再轮询。',
              },
            ),
          ],
        },
      ),
  },
]);

const printerColumns = computed<DataTableColumns<Printer>>(() => [
  { title: '名称', key: 'name', minWidth: 200, ellipsis: true, render: (row) => row.name || '-' },
  { title: '状态', key: 'status', width: 110, render: (row) => statusTag(row.status) },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 280,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 12, wrap: false },
        {
          default: () => [
            h(
              NButton,
              { text: true, size: 'small', type: 'primary', onClick: () => openEditPrinter(row) },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              {
                text: true,
                size: 'small',
                type: 'info',
                loading: printerTestingId.value === row.id,
                onClick: () => testPrinter(row),
              },
              { default: () => '测试打印' },
            ),
            h(
              NPopconfirm,
              { positiveText: '确定', negativeText: '取消', onPositiveClick: () => resetPrinterSecret(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: 'warning',
                      loading: resettingPrinterId.value === row.id,
                      disabled: resettingPrinterId.value === row.id,
                    },
                    { default: () => '重置密钥' },
                  ),
                default: () => '重置后旧密钥立即失效，打印机需要用新 secret 才能继续拉单。',
              },
            ),
            h(
              NPopconfirm,
              { positiveText: '确定', negativeText: '取消', onPositiveClick: () => removePrinter(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: 'error',
                      loading: removingPrinterId.value === row.id,
                    },
                    { default: () => '删除' },
                  ),
                default: () => '确认删除该打印机？',
              },
            ),
          ],
        },
      ),
  },
]);

onMounted(() => {
  void loadDevices();
});
</script>

<template>
  <admin-page title="语音设备与打印机" subtitle="设备侧拿 device_no + secret 轮询，播报与打印由支付成功钩子入队">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="devicesLoading || printersLoading" @click="reloadCurrent">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button v-if="tab === 'voice'" type="primary" @click="openCreateDevice">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          新增语音设备
        </n-button>
        <n-button v-else type="primary" @click="openCreatePrinter">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          新增打印机
        </n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      设备端用「device_no + secret」轮询取任务；支付成功钩子会把播报文案与打印小票写入队列。创建时返回的 secret 只显示一次，丢了用操作列的「重置密钥」换一个新的。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-tabs :value="tab" type="line" animated @update:value="handleTab">
        <n-tab-pane name="voice" tab="语音设备">
          <n-data-table
            :columns="deviceColumns"
            :data="devices"
            :loading="devicesLoading"
            :pagination="{ pageSize: 10 }"
            :row-key="(row: VoiceDevice) => row.id"
            :scroll-x="900"
            :size="isNarrow ? 'small' : 'medium'"
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
            :scroll-x="900"
            :size="isNarrow ? 'small' : 'medium'"
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
      style="width: 640px; max-width: 94vw"
    >
      <n-form
        ref="deviceFormRef"
        :model="deviceForm"
        :rules="deviceRules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
      >
        <n-form-item label="设备号" path="device_no">
          <n-input
            v-model:value="deviceForm.device_no"
            placeholder="设备唯一编号，设备端用它轮询"
            :disabled="!!deviceEditingId"
          />
        </n-form-item>
        <n-form-item label="名称" path="name">
          <n-input v-model:value="deviceForm.name" placeholder="例如 一号收银台音箱" />
        </n-form-item>
        <n-form-item label="状态">
          <n-switch v-model:value="deviceStatusSwitch">
            <template #checked>启用</template>
            <template #unchecked>禁用</template>
          </n-switch>
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
      style="width: 640px; max-width: 94vw"
    >
      <n-form
        ref="printerFormRef"
        :model="printerForm"
        :rules="printerRules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
      >
        <n-form-item label="名称" path="name">
          <n-input v-model:value="printerForm.name" placeholder="例如 前台热敏打印机" />
        </n-form-item>
        <n-form-item label="状态">
          <n-switch v-model:value="printerStatusSwitch">
            <template #checked>启用</template>
            <template #unchecked>禁用</template>
          </n-switch>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showPrinterForm = false">取消</n-button>
          <n-button type="primary" :loading="printerSubmitting" @click="submitPrinter">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showSecret" preset="card" :title="secretTitle" style="width: 640px; max-width: 94vw">
      <n-alert type="warning" :bordered="false" style="margin-bottom: 16px">
        secret 只显示这一次，关掉就查不到了；忘了就再重置一次密钥（旧密钥会立即失效）。
      </n-alert>
      <n-space align="center" :wrap="true" :size="12">
        <n-input
          :value="secretValue"
          type="password"
          show-password-on="click"
          readonly
          style="width: 100%; max-width: 440px"
        />
        <n-button @click="copySecret">
          <template #icon>
            <n-icon :component="CopyOutline" />
          </template>
          复制
        </n-button>
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button type="primary" @click="showSecret = false">我已保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal
      v-model:show="showVoiceTest"
      preset="card"
      title="发送测试播报"
      style="width: 640px; max-width: 94vw"
    >
      <n-descriptions v-if="voiceTestTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="设备号">{{ voiceTestTarget.device_no }}</n-descriptions-item>
        <n-descriptions-item label="名称">{{ voiceTestTarget.name || '-' }}</n-descriptions-item>
      </n-descriptions>
      <n-form
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
        style="margin-top: 16px"
      >
        <n-form-item label="播报内容">
          <n-input
            v-model:value="voiceTestText"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="例如：支付宝到账 1.00 元"
          />
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        设备在线时会通过轮询取到这条播报；离线时任务会留在队列里。
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showVoiceTest = false">取消</n-button>
          <n-button type="primary" :loading="voiceTesting" @click="runVoiceTest">
            <template #icon>
              <n-icon :component="PaperPlaneOutline" />
            </template>
            发送
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
