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
import { AddOutline, PaperPlaneOutline, RefreshOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { wechatMpApi } from '@/api/payTypes';
import type { WechatMpConfig } from '@/api/payTypes';
import { errorMessage } from '@/api/client';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

interface MpForm {
  merchant_id: string;
  app_id: string;
  app_secret: string;
  token: string;
  aes_key: string;
  status: string;
}

const message = useMessage();

const rows = ref<WechatMpConfig[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const editingRow = ref<WechatMpConfig | null>(null);
const formRef = ref<FormInst | null>(null);
const pendingStatusId = ref('');
const removingId = ref('');

const showTest = ref(false);
const testTarget = ref<WechatMpConfig | null>(null);
const testOpenid = ref('');
const testSubmitting = ref(false);

const form = reactive<MpForm>({
  merchant_id: '',
  app_id: '',
  app_secret: '',
  token: '',
  aes_key: '',
  status: 'active',
});

const rules: FormRules = {
  app_id: { required: true, message: '请输入公众号 AppID', trigger: ['blur', 'input'] },
};

const isEditing = computed(() => editingId.value !== null);

const statusSwitch = computed({
  get: () => form.status === 'active',
  set: (value: boolean) => {
    form.status = value ? 'active' : 'disabled';
  },
});

function flagOf(row: WechatMpConfig | null, key: string): boolean {
  return asRow(row)[key] === true;
}

const secretConfigured = computed(() => flagOf(editingRow.value, 'secret_configured'));
const aesKeyConfigured = computed(() => flagOf(editingRow.value, 'aes_key_configured'));

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

async function load() {
  loading.value = true;
  try {
    const data = await wechatMpApi.list({ page: page.value, page_size: pageSize.value });
    const items = itemsOf<WechatMpConfig>(data);
    rows.value = items;
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editingId.value = null;
  editingRow.value = null;
  form.merchant_id = '';
  form.app_id = '';
  form.app_secret = '';
  form.token = '';
  form.aes_key = '';
  form.status = 'active';
  showForm.value = true;
}

function openEdit(row: WechatMpConfig) {
  editingId.value = row.id;
  editingRow.value = row;
  form.merchant_id = row.merchant_id || '';
  form.app_id = row.app_id || '';
  form.app_secret = '';
  form.token = row.token || '';
  form.aes_key = '';
  form.status = row.status || 'active';
  showForm.value = true;
}

function openTest(row: WechatMpConfig) {
  testTarget.value = row;
  testOpenid.value = '';
  showTest.value = true;
}

async function submitTest() {
  const target = testTarget.value;
  if (!target) return;
  const openid = testOpenid.value.trim();
  if (openid === '') {
    message.warning('请输入接收消息的 openid');
    return;
  }
  if (testSubmitting.value) return;
  testSubmitting.value = true;
  try {
    await wechatMpApi.testMessage(target.id, openid);
    message.success('测试消息已发送');
    showTest.value = false;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    testSubmitting.value = false;
  }
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const payload: {
      merchant_id: string | null;
      app_id: string;
      app_secret?: string;
      token: string;
      aes_key?: string;
      status: string;
    } = {
      merchant_id: form.merchant_id.trim() === '' ? null : form.merchant_id.trim(),
      app_id: form.app_id.trim(),
      token: form.token.trim(),
      status: form.status,
    };
    // AppSecret / EncodingAESKey 只写：编辑时留空表示保持原值
    if (form.app_secret.trim() !== '') payload.app_secret = form.app_secret.trim();
    if (form.aes_key.trim() !== '') payload.aes_key = form.aes_key.trim();
    if (editingId.value) {
      await wechatMpApi.update(editingId.value, payload);
      message.success('公众号配置已更新');
      showForm.value = false;
      await load();
    } else {
      await wechatMpApi.create(payload);
      message.success('公众号配置已创建');
      showForm.value = false;
      resetPage();
      await load();
    }
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: WechatMpConfig) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  pendingStatusId.value = row.id;
  try {
    await wechatMpApi.update(row.id, {
      merchant_id: row.merchant_id ?? null,
      app_id: row.app_id,
      token: row.token || '',
      status: next,
    });
    message.success(next === 'active' ? '已启用' : '已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    pendingStatusId.value = '';
  }
}

async function remove(row: WechatMpConfig) {
  removingId.value = row.id;
  try {
    await wechatMpApi.remove(row.id);
    message.success('公众号配置已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingId.value = '';
  }
}

function scopeCell(row: WechatMpConfig) {
  if (row.merchant_id) {
    return h(NText, { code: true }, { default: () => row.merchant_id });
  }
  return h(
    NTag,
    { size: 'small', round: true, bordered: false },
    { default: () => '全局' },
  );
}

function secretTags(row: WechatMpConfig) {
  const items = [
    { label: 'AppSecret', ok: flagOf(row, 'secret_configured') },
    { label: 'AESKey', ok: flagOf(row, 'aes_key_configured') },
  ];
  return h(
    NSpace,
    { size: 4, wrap: false },
    {
      default: () =>
        items.map((item) =>
          h(
            NTag,
            {
              size: 'small',
              round: true,
              bordered: false,
              type: item.ok ? 'success' : 'warning',
            },
            { default: () => `${item.label} ${item.ok ? '已配置' : '未配置'}` },
          ),
        ),
    },
  );
}

const columns = computed<DataTableColumns<WechatMpConfig>>(() => [
  {
    title: '适用范围',
    key: 'merchant_id',
    width: 240,
    ellipsis: true,
    render: (row) => scopeCell(row),
  },
  {
    title: 'AppID',
    key: 'app_id',
    minWidth: 180,
    ellipsis: true,
    render: (row) => h(NText, { code: true }, { default: () => row.app_id || '-' }),
  },
  {
    title: 'Token',
    key: 'token',
    width: 180,
    ellipsis: true,
    render: (row) =>
      row.token
        ? h(NText, { code: true }, { default: () => row.token })
        : h(NText, { depth: 3 }, { default: () => '-' }),
  },
  { title: '密钥', key: 'secret', width: 220, render: (row) => secretTags(row) },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) =>
      h(
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
                loading: pendingStatusId.value === row.id,
              },
              { checked: () => '启用', unchecked: () => '禁用' },
            ),
          default: () => (row.status === 'active' ? '确认禁用该公众号配置？' : '确认启用该公众号配置？'),
        },
      ),
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
    width: 210,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 12, wrap: false },
        {
          default: () => [
            h(
              NButton,
              { text: true, size: 'small', type: 'primary', onClick: () => openEdit(row) },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              { text: true, size: 'small', type: 'info', onClick: () => openTest(row) },
              {
                icon: () => h(NIcon, { component: PaperPlaneOutline }),
                default: () => '测试消息',
              },
            ),
            h(
              NPopconfirm,
              { positiveText: '确定', negativeText: '取消', onPositiveClick: () => remove(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: 'error',
                      loading: removingId.value === row.id,
                    },
                    { default: () => '删除' },
                  ),
                default: () => '确认删除该公众号配置？',
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
  <admin-page title="公众号配置" subtitle="微信 JSAPI 支付用的公众号 AppID、Token 与 EncodingAESKey">
    <template #actions>
      <n-space>
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
          新建配置
        </n-button>
      </n-space>
    </template>

    <n-card size="small" :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: WechatMpConfig) => row.id"
        :scroll-x="1280"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无公众号配置" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showForm" :width="560">
      <n-drawer-content :title="isEditing ? '编辑公众号配置' : '新建公众号配置'" closable>
        <n-alert v-if="isEditing" type="warning" :bordered="false" style="margin-bottom: 16px">
          后端不回显 AppSecret 与 EncodingAESKey，留空表示保持原值，只有填写才会覆盖。
        </n-alert>
        <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="150">
          <n-form-item label="适用商户">
            <n-input v-model:value="form.merchant_id" placeholder="留空表示全局配置，或填商户 ID" />
          </n-form-item>
          <n-form-item label="AppID" path="app_id">
            <n-input v-model:value="form.app_id" placeholder="公众号 AppID，例如 wx1234567890" />
          </n-form-item>
          <n-form-item label="AppSecret">
            <n-space align="center" :size="8">
              <n-input
                v-model:value="form.app_secret"
                type="password"
                show-password-on="click"
                style="width: 240px"
                :placeholder="isEditing ? '留空表示不修改' : '公众号 AppSecret'"
              />
              <n-tag :type="secretConfigured ? 'success' : 'warning'" size="small" :bordered="false">
                {{ secretConfigured ? '已配置' : '未配置' }}
              </n-tag>
            </n-space>
          </n-form-item>
          <n-form-item label="Token">
            <n-input v-model:value="form.token" placeholder="服务器配置里的 Token" />
          </n-form-item>
          <n-form-item label="EncodingAESKey">
            <n-space align="center" :size="8">
              <n-input
                v-model:value="form.aes_key"
                type="password"
                show-password-on="click"
                style="width: 240px"
                :placeholder="isEditing ? '留空表示不修改' : '43 位 EncodingAESKey'"
              />
              <n-tag :type="aesKeyConfigured ? 'success' : 'warning'" size="small" :bordered="false">
                {{ aesKeyConfigured ? '已配置' : '未配置' }}
              </n-tag>
            </n-space>
          </n-form-item>
          <n-form-item label="状态">
            <n-switch v-model:value="statusSwitch">
              <template #checked>启用</template>
              <template #unchecked>禁用</template>
            </n-switch>
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showForm = false">取消</n-button>
            <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>

    <n-modal v-model:show="showTest" preset="card" title="发送公众号测试消息" style="width: 640px">
      <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
        后端会用这条配置的 AppID/AppSecret 调用微信接口，给指定 openid 发一条模板测试消息；配置不完整时会直接报错。
      </n-alert>
      <n-descriptions v-if="testTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="适用范围">
          <span v-if="testTarget.merchant_id">{{ testTarget.merchant_id }}</span>
          <span v-else>全局</span>
        </n-descriptions-item>
        <n-descriptions-item label="AppID">{{ testTarget.app_id || '-' }}</n-descriptions-item>
      </n-descriptions>
      <n-form label-placement="left" label-width="120" style="margin-top: 16px">
        <n-form-item label="接收 openid">
          <n-input v-model:value="testOpenid" placeholder="微信 openid，测试用" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showTest = false">取消</n-button>
          <n-button type="primary" :loading="testSubmitting" @click="submitTest">
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
