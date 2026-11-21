<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import {
  NAvatar,
  NButton,
  NInput,
  NInputNumber,
  NPopconfirm,
  NSpace,
  NSwitch,
  NText,
  useMessage,
} from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { AddOutline, RefreshOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import PaymentBrandIcon from '@/components/PaymentBrandIcon.vue';
import { payTypesApi } from '@/api/payTypes';
import type { PayType } from '@/api/payTypes';
import { errorMessage } from '@/api/client';
import { formatDateTime } from '@/utils/format';
import { itemsOf, useNarrow } from './shared';

interface PayTypeForm {
  code: string;
  name: string;
  icon: string;
  sort: number | null;
  status: string;
}

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<PayType[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const formRef = ref<FormInst | null>(null);
const pendingStatusId = ref('');
const removingId = ref('');

const form = reactive<PayTypeForm>({
  code: '',
  name: '',
  icon: '',
  sort: 0,
  status: 'active',
});

const rules: FormRules = {
  code: { required: true, message: '请输入支付方式编码', trigger: ['blur', 'input'] },
  name: { required: true, message: '请输入显示名称', trigger: ['blur', 'input'] },
};

const isEditing = computed(() => editingId.value !== null);

const statusSwitch = computed({
  get: () => form.status === 'active',
  set: (value: boolean) => {
    form.status = value ? 'active' : 'disabled';
  },
});

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
  pendingStatusId.value = row.id;
  try {
    await payTypesApi.setStatus(row.id, next);
    message.success(next === 'active' ? '已启用' : '已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    pendingStatusId.value = '';
  }
}

async function remove(row: PayType) {
  removingId.value = row.id;
  try {
    await payTypesApi.remove(row.id);
    message.success('支付方式已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingId.value = '';
  }
}

function iconCell(row: PayType) {
  const icon = row.icon || '';
  if (icon === '') return h(NText, { depth: 3 }, { default: () => '-' });
  if (icon.startsWith('http') || icon.startsWith('/')) {
    return h(NAvatar, { src: icon, size: 24, objectFit: 'contain', round: false, bordered: true });
  }
  return h(NText, { code: true }, { default: () => icon });
}

/** 名称列：按支付方式编码取品牌图标（alipay / wxpay / qqpay …），图标垂直居中，不撑高行高 */
function nameCell(row: PayType) {
  return h('span', { style: 'display:inline-flex;align-items:center;gap:6px' }, [
    h(PaymentBrandIcon, { type: row.code || '', size: 18 }),
    h('span', null, row.name || '-'),
  ]);
}

const columns = computed<DataTableColumns<PayType>>(() => [
  {
    title: '编码',
    key: 'code',
    width: 160,
    ellipsis: true,
    render: (row) => h(NText, { code: true }, { default: () => row.code || '-' }),
  },
  { title: '名称', key: 'name', minWidth: 140, ellipsis: true, render: (row) => nameCell(row) },
  { title: '图标', key: 'icon', width: 110, render: (row) => iconCell(row) },
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
          default: () => (row.status === 'active' ? '确认禁用该支付方式？' : '确认启用该支付方式？'),
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
    width: 130,
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
  <admin-page title="支付方式" subtitle="收银台展示的支付方式与排序，可上传图标地址">
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
          新建支付方式
        </n-button>
      </n-space>
    </template>

    <n-card size="small" :bordered="false">
      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :row-key="(row: PayType) => row.id"
        :scroll-x="1000"
        :size="isNarrow ? 'small' : 'medium'"
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
      style="width: 640px; max-width: 94vw"
    >
      <n-form
        ref="formRef"
        :model="form"
        :rules="rules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
      >
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
          <n-input-number v-model:value="form.sort" :min="0" :step="1" style="width: 160px" />
        </n-form-item>
        <n-form-item label="状态">
          <n-switch v-model:value="statusSwitch">
            <template #checked>启用</template>
            <template #unchecked>禁用</template>
          </n-switch>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
