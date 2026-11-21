<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { AddOutline, CopyOutline, PrintOutline, RefreshOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { merchantApi } from '@/api';
import type { MerchantPrinter } from '@/api/types';
import { errorMessage } from '@/api/client';
import { activeStatus, copyText, formatDateTime } from '@/utils/format';
import { useNarrow } from './shared';

/**
 * 商户小票打印机：GET / POST / DELETE /merchant/printers，POST /merchant/printers/:id/test。
 * 列表项字段对齐 internal/merchant/httpapi/printers.go：id / name / status / created_at / updated_at，
 * secret 只在创建响应里出现一次。
 */
const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(false);
const printers = ref<MerchantPrinter[]>([]);

const showCreate = ref(false);
const creating = ref(false);
const createFormRef = ref<FormInst | null>(null);
const createForm = ref<{ name: string }>({ name: '' });

const showSecret = ref(false);
const secretValue = ref('');
const secretPrinterName = ref('');

const testingId = ref('');
const deletingId = ref('');
const resettingId = ref('');

const createRules: FormRules = {
  name: { required: true, message: '请输入打印机名称', trigger: ['blur', 'input'] },
};

async function load() {
  loading.value = true;
  try {
    printers.value = await merchantApi.printers();
  } catch (err) {
    printers.value = [];
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  createForm.value = { name: '' };
  showCreate.value = true;
}

async function copySecret() {
  const ok = await copyText(secretValue.value);
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
}

async function submitCreate() {
  try {
    await createFormRef.value?.validate();
  } catch {
    return;
  }
  const name = createForm.value.name.trim();
  if (name === '') {
    message.warning('请输入打印机名称');
    return;
  }
  creating.value = true;
  try {
    const created = await merchantApi.createPrinter({ name });
    message.success('打印机已添加');
    showCreate.value = false;
    // secret 只回这一次，立刻弹出来让人抄走
    secretPrinterName.value = created.printer?.name || name;
    secretValue.value = created.secret || '';
    showSecret.value = true;
    await load();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    creating.value = false;
  }
}

async function resetSecret(row: MerchantPrinter) {
  resettingId.value = row.id;
  try {
    const secret = await merchantApi.resetPrinterSecret(row.id);
    message.success('密钥已重置，旧密钥立即失效');
    secretPrinterName.value = row.name;
    secretValue.value = secret;
    showSecret.value = true;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    resettingId.value = '';
  }
}

async function testPrinter(row: MerchantPrinter) {
  testingId.value = row.id;
  try {
    await merchantApi.testPrinter(row.id);
    message.success('测试小票已加入打印队列');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    testingId.value = '';
  }
}

async function removePrinter(row: MerchantPrinter) {
  deletingId.value = row.id;
  try {
    await merchantApi.deletePrinter(row.id);
    message.success('打印机已删除');
    await load();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    deletingId.value = '';
  }
}

function statusTag(status?: string) {
  const meta = activeStatus(status);
  return h(NTag, { size: 'small', round: true, bordered: false, type: meta.type }, () => meta.text);
}

const columns = computed<DataTableColumns<MerchantPrinter>>(() => [
  { title: '名称', key: 'name', minWidth: 180, ellipsis: { tooltip: true }, render: (row) => row.name || '-' },
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
    width: 170,
    fixed: 'right',
    render: (row) =>
      h(NSpace, { size: 8, wrap: false }, () => [
        h(
          NButton,
          {
            size: 'small',
            text: true,
            type: 'primary',
            loading: testingId.value === row.id,
            disabled: testingId.value === row.id,
            onClick: () => testPrinter(row),
          },
          () => '测试打印',
        ),
        h(
          NPopconfirm,
          {
            onPositiveClick: () => resetSecret(row),
            positiveText: '重置',
            negativeText: '取消',
          },
          {
            trigger: () =>
              h(
                NButton,
                {
                  size: 'small',
                  text: true,
                  type: 'warning',
                  loading: resettingId.value === row.id,
                  disabled: resettingId.value === row.id,
                },
                () => '重置密钥',
              ),
            default: () => '重置后旧密钥立即失效，打印代理要换成新密钥才能继续拉任务。确认重置？',
          },
        ),
        h(
          NPopconfirm,
          { onPositiveClick: () => removePrinter(row) },
          {
            trigger: () =>
              h(
                NButton,
                {
                  size: 'small',
                  text: true,
                  type: 'error',
                  loading: deletingId.value === row.id,
                  disabled: deletingId.value === row.id,
                },
                () => '删除',
              ),
            default: () => '删除后打印代理无法再拉取任务，确认删除该打印机？',
          },
        ),
      ]),
  },
]);

onMounted(() => {
  void load();
});
</script>

<template>
  <admin-page title="小票打印机" subtitle="添加打印机并把它接到打印代理上，已支付订单可以一键出小票">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" @click="openCreate">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          添加打印机
        </n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      打印代理按 <n-text class="mono">POST /api/v1/device/printer/poll</n-text> 携带
      <n-text class="mono">printer_no</n-text> + <n-text class="mono">secret</n-text> 拉任务，打印成功后再
      <n-text class="mono">ack</n-text> 确认，任务才算完成并出纸。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="printers"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :row-key="(row: MerchantPrinter) => row.id"
        :scroll-x="760"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="还没有添加打印机" size="small">
            <template #extra>
              <n-space align="center" :size="6">
                <n-icon :component="PrintOutline" />
                <n-text depth="3" class="gp-hint">添加后把编号和密钥填进打印代理即可出票</n-text>
              </n-space>
            </template>
          </n-empty>
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="showCreate" preset="card" title="添加打印机" style="width: 520px; max-width: 94vw">
      <n-form
        ref="createFormRef"
        :model="createForm"
        :rules="createRules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
      >
        <n-form-item label="打印机名称" path="name">
          <n-input v-model:value="createForm.name" placeholder="例如 前台热敏打印机" />
        </n-form-item>
      </n-form>
      <n-text depth="3" class="gp-hint">名称只给自己看；保存后会立刻给出一串编号 + 密钥。</n-text>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" :loading="creating" @click="submitCreate">添加</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showSecret" preset="card" title="打印密钥" style="width: 620px; max-width: 94vw">
      <n-alert type="warning" :bordered="false" style="margin-bottom: 16px">
        这串密钥只显示这一次，关掉就查不到了。请把它和上面的打印机编号一起填到打印代理里，丢了只能删掉打印机重新添加。
      </n-alert>
      <n-descriptions :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="打印机">
          <n-text>{{ secretPrinterName || '-' }}</n-text>
        </n-descriptions-item>
      </n-descriptions>
      <n-space align="center" :wrap="true" :size="12" style="margin-top: 12px">
        <n-input
          :value="secretValue"
          type="password"
          show-password-on="click"
          readonly
          style="width: 100%; max-width: 420px"
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
  </admin-page>
</template>