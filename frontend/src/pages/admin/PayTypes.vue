<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { payTypesApi } from '@/api/payTypes';
import type { PayType } from '@/api/payTypes';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { itemsOf } from './shared';

interface PayTypeForm {
  code: string;
  name: string;
  icon: string;
  sort: number | null;
  status: string;
}

const message = useMessage();

const rows = ref<PayType[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);

const form = reactive<PayTypeForm>({
  code: '',
  name: '',
  icon: '',
  sort: 0,
  status: 'active',
});

const statusOptions: SelectOption[] = [
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'disabled' },
];

const rules: FormRules = {
  code: { required: true, message: '请输入支付方式编码', trigger: ['blur', 'input'] },
  name: { required: true, message: '请输入显示名称', trigger: ['blur', 'input'] },
};

const isEditing = computed(() => editingId.value !== null);

async function load() {
  loading.value = true;
  try {
    rows.value = itemsOf<PayType>(await payTypesApi.list());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editingId.value = null;
  form.code = '';
  form.name = '';
  form.icon = '';
  form.sort = 0;
  form.status = 'active';
  showForm.value = true;
}

function openEdit(row: PayType) {
  editingId.value = row.id;
  form.code = row.code || '';
  form.name = row.name || '';
  form.icon = row.icon || '';
  form.sort = typeof row.sort === 'number' ? row.sort : 0;
  form.status = row.status || 'active';
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
      code: form.code.trim(),
      name: form.name.trim(),
      icon: form.icon.trim(),
      sort: form.sort ?? 0,
      status: form.status,
    };
    if (editingId.value) {
      await payTypesApi.update(editingId.value, payload);
      message.success('支付方式已更新');
    } else {
      await payTypesApi.create(payload);
      message.success('支付方式已创建');
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: PayType) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  try {
    await payTypesApi.setStatus(row.id, next);
    message.success(next === 'active' ? '已启用' : '已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

async function remove(row: PayType) {
  try {
    await payTypesApi.remove(row.id);
    message.success('支付方式已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function iconCell(row: PayType) {
  const icon = row.icon || '';
  if (icon === '') return h('span', { class: 'muted' }, '-');
  if (icon.startsWith('http') || icon.startsWith('/')) {
    return h('img', { src: icon, alt: row.name || row.code, class: 'icon' });
  }
  return h('span', { class: 'mono' }, icon);
}

const columns = computed<DataTableColumns<PayType>>(() => [
  {
    title: '编码',
    key: 'code',
    width: 160,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.code || '-'),
  },
  { title: '名称', key: 'name', minWidth: 140, ellipsis: true, render: (row) => row.name || '-' },
  { title: '图标', key: 'icon', width: 120, render: (row) => iconCell(row) },
  {
    title: '排序',
    key: 'sort',
    width: 90,
    align: 'right',
    render: (row) => (typeof row.sort === 'number' ? row.sort : '-'),
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
    title: '操作',
    key: 'actions',
    width: 190,
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
              { onPositiveClick: () => toggleStatus(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'tiny', quaternary: true, type: row.status === 'active' ? 'warning' : 'primary' },
                    { default: () => (row.status === 'active' ? '禁用' : '启用') },
                  ),
                default: () => (row.status === 'active' ? '确认禁用该支付方式？' : '确认启用该支付方式？'),
              },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => remove(row) },
              {
                trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                default: () => '确认删除该支付方式？',
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
    <PageHeader title="支付方式" subtitle="收银台展示的支付方式与排序，可上传图标地址">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="openCreate">新建支付方式</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :row-key="(row: PayType) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
      >
        <template #empty>
          <n-empty description="暂无支付方式，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑支付方式' : '新建支付方式'"
      style="width: 560px; max-width: 94vw"
    >
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="110">
        <n-form-item label="编码" path="code">
          <n-input v-model:value="form.code" placeholder="例如 alipay / wxpay，创建后尽量别改" />
        </n-form-item>
        <n-form-item label="显示名称" path="name">
          <n-input v-model:value="form.name" placeholder="例如 支付宝" />
        </n-form-item>
        <n-form-item label="图标地址">
          <n-input v-model:value="form.icon" placeholder="https:// 开头的图片地址，或图标代码" />
        </n-form-item>
        <n-form-item label="排序">
          <n-input-number v-model:value="form.sort" :min="0" :step="1" style="width: 100%" />
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
  </div>
</template>

<style scoped>
.icon {
  width: 28px;
  height: 28px;
  object-fit: contain;
  border-radius: 6px;
  background: #fff;
}
</style>
