<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import dayjs from 'dayjs';
import { inviteCodesApi } from '@/api/inviteCodes';
import type { InviteCode } from '@/api/inviteCodes';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { copyText, formatDateTime } from '@/utils/format';
import { asRow, itemsOf } from './shared';

interface CodeForm {
  max_uses: number | null;
  owner_merchant_id: string;
  expires_at: number | null;
}

const message = useMessage();

const rows = ref<InviteCode[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);

const showCode = ref(false);
const createdCode = ref('');
const removingId = ref('');

const form = reactive<CodeForm>({
  max_uses: 1,
  owner_merchant_id: '',
  expires_at: null,
});

const rules: FormRules = {
  max_uses: { required: true, type: 'number', message: '请输入可用次数', trigger: ['blur', 'change'] },
};

const isEditing = computed(() => editingId.value !== null);

async function load() {
  loading.value = true;
  try {
    rows.value = itemsOf<InviteCode>(await inviteCodesApi.list());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editingId.value = null;
  form.max_uses = 1;
  form.owner_merchant_id = '';
  form.expires_at = null;
  showForm.value = true;
}

function openEdit(row: InviteCode) {
  editingId.value = row.id;
  form.max_uses = typeof row.max_uses === 'number' ? row.max_uses : 0;
  form.owner_merchant_id = row.owner_merchant_id || '';
  const parsed = dayjs(row.expires_at);
  form.expires_at = parsed.isValid() ? parsed.valueOf() : null;
  showForm.value = true;
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const payload = {
      max_uses: form.max_uses ?? 0,
      owner_merchant_id: form.owner_merchant_id.trim() === '' ? null : form.owner_merchant_id.trim(),
      expires_at: form.expires_at === null ? null : dayjs(form.expires_at).toISOString(),
    };
    if (editingId.value) {
      await inviteCodesApi.update(editingId.value, payload);
      message.success('邀请码已更新');
    } else {
      const created = await inviteCodesApi.create(payload);
      message.success('邀请码已生成');
      const code = asRow(created).code;
      if (typeof code === 'string' && code !== '') {
        createdCode.value = code;
        showCode.value = true;
      }
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function remove(row: InviteCode) {
  if (removingId.value === row.id) return;
  removingId.value = row.id;
  try {
    await inviteCodesApi.remove(row.id);
    message.success('邀请码已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingId.value = '';
  }
}

async function copy(value: string) {
  const ok = await copyText(value);
  if (ok) message.success('已复制');
  else message.warning('复制失败，请手动选择');
}

function usesText(row: InviteCode): string {
  const max = typeof row.max_uses === 'number' ? row.max_uses : 0;
  if (max === 0) return '不限次';
  const used = typeof row.used_count === 'number' ? row.used_count : 0;
  return `${used} / ${max}`;
}

const columns = computed<DataTableColumns<InviteCode>>(() => [
  {
    title: '邀请码',
    key: 'code',
    width: 200,
    render: (row) =>
      h(NSpace, { size: 6, align: 'center' }, {
        default: () => [
          h('span', { class: 'mono' }, row.code || '-'),
          row.code
            ? h(
                NButton,
                { size: 'tiny', text: true, type: 'primary', onClick: () => copy(row.code) },
                { default: () => '复制' },
              )
            : null,
        ],
      }),
  },
  {
    title: '归属商户',
    key: 'owner_merchant_id',
    width: 260,
    ellipsis: true,
    render: (row) =>
      row.owner_merchant_id
        ? h('span', { class: 'mono' }, row.owner_merchant_id)
        : h('span', { class: 'muted' }, '平台通用'),
  },
  { title: '使用情况', key: 'used_count', width: 120, render: (row) => usesText(row) },
  {
    title: '过期时间',
    key: 'expires_at',
    width: 170,
    render: (row) => (row.expires_at ? formatDateTime(row.expires_at) : '不过期'),
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
    render: (row) => {
      const removing = removingId.value === row.id;
      return h(
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
                trigger: () =>
                  h(
                    NButton,
                    { size: 'tiny', quaternary: true, type: 'error', loading: removing, disabled: removing },
                    { default: () => '删除' },
                  ),
                default: () => '确认删除该邀请码？已注册的商户不受影响。',
              },
            ),
          ],
        },
      );
    },
  },
]);

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="邀请码" subtitle="生成注册邀请码，可限定归属商户、可用次数与有效期">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="openCreate">生成邀请码</n-button>
      </template>
    </PageHeader>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      邀请码由服务端生成，创建后可复制发给商户；可用次数填 0 表示不限次，过期时间留空表示不过期。
    </n-alert>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :row-key="(row: InviteCode) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
      >
        <template #empty>
          <n-empty description="还没有邀请码，点击右上角生成" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑邀请码' : '生成邀请码'"
      style="width: 560px; max-width: 94vw"
    >
      <n-alert v-if="isEditing" type="info" :bordered="false" style="margin-bottom: 14px">
        邀请码本身不可修改，这里只能调整归属商户、可用次数与过期时间。
      </n-alert>
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="120">
        <n-form-item label="可用次数" path="max_uses">
          <div class="row-inline">
            <n-input-number v-model:value="form.max_uses" :min="0" :step="1" style="width: 160px" />
            <span class="muted small">0 表示不限次</span>
          </div>
        </n-form-item>
        <n-form-item label="归属商户">
          <n-input v-model:value="form.owner_merchant_id" placeholder="留空表示平台通用，或填商户 ID" />
        </n-form-item>
        <n-form-item label="过期时间">
          <n-date-picker v-model:value="form.expires_at" type="datetime" clearable style="width: 220px" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showCode" preset="card" title="邀请码已生成" style="width: 520px; max-width: 94vw">
      <n-alert type="success" :bordered="false" style="margin-bottom: 14px">
        复制邀请码发给商户，对方在注册页填写即可绑定。
      </n-alert>
      <div class="code-box mono">{{ createdCode }}</div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="copy(createdCode)">复制</n-button>
          <n-button type="primary" @click="showCode = false">完成</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.small {
  font-size: 12px;
}
.row-inline {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.code-box {
  word-break: break-all;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 10px;
  padding: 14px 16px;
  font-size: 18px;
  letter-spacing: 2px;
  text-align: center;
}
</style>
