<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { groupsApi } from '@/api/groups';
import type { MerchantGroup } from '@/api/groups';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime, paymentTypeText } from '@/utils/format';
import { asRow, httpStatus, itemsOf, numberOr, stringList, useServerPaging } from './shared';

interface GroupForm {
  name: string;
  rate_percent: number | null;
  pay_types: string[];
  settle_mode: string;
}

const message = useMessage();

const rows = ref<MerchantGroup[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const editingStatus = ref('active');
const formRef = ref<FormInst | null>(null);

const { page, pageSize, total, pagination, onPage, onPageSize } = useServerPaging(load);

const form = reactive<GroupForm>({
  name: '',
  rate_percent: 0,
  pay_types: [],
  settle_mode: 'normal',
});

const payTypeOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信支付', value: 'wxpay' },
  { label: 'QQ 钱包', value: 'qqpay' },
  { label: '银联', value: 'unionpay' },
  { label: '网银', value: 'bank' },
  { label: 'USDT', value: 'usdt' },
  { label: '收银台', value: 'cashier' },
];

const settleModeOptions: SelectOption[] = [
  { label: '普通（normal）', value: 'normal' },
  { label: '直连（direct）', value: 'direct' },
];

const settleModeLabels: Record<string, string> = {
  normal: '普通',
  direct: '直连',
};

const rules: FormRules = {
  name: { required: true, message: '请输入用户组名称', trigger: ['blur', 'input'] },
  rate_percent: { required: true, type: 'number', message: '请输入费率', trigger: ['blur', 'change'] },
  settle_mode: { required: true, message: '请选择结算模式', trigger: ['change', 'blur'] },
};

const isEditing = computed(() => editingId.value !== null);

async function load() {
  loading.value = true;
  try {
    const data = await groupsApi.list({ page: page.value, page_size: pageSize.value });
    const items = itemsOf<MerchantGroup>(data);
    rows.value = items;
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  form.name = '';
  form.rate_percent = 0;
  form.pay_types = [];
  form.settle_mode = 'normal';
}

function openCreate() {
  editingId.value = null;
  editingStatus.value = 'active';
  resetForm();
  showForm.value = true;
}

function openEdit(row: MerchantGroup) {
  editingId.value = row.id;
  editingStatus.value = row.status || 'active';
  form.name = row.name || '';
  form.rate_percent = numberOr(row.rate_bp, 0) / 100;
  form.pay_types = stringList(row.pay_types);
  form.settle_mode = row.settle_mode || 'normal';
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
      name: form.name.trim(),
      // 页面按百分比录入，后端存万分比
      rate_bp: Math.round(numberOr(form.rate_percent, 0) * 100),
      pay_types: form.pay_types,
      settle_mode: form.settle_mode,
      status: editingStatus.value,
    };
    if (editingId.value) {
      await groupsApi.update(editingId.value, payload);
      message.success('用户组已更新');
    } else {
      await groupsApi.create(payload);
      message.success('用户组已创建');
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: MerchantGroup) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  try {
    await groupsApi.setStatus(row.id, next);
    message.success(next === 'active' ? '用户组已启用' : '用户组已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

async function remove(row: MerchantGroup) {
  try {
    await groupsApi.remove(row.id);
    message.success('用户组已删除');
    await load();
  } catch (error) {
    if (httpStatus(error) === 409) {
      message.error('该用户组下还有商户在使用，先把商户移出组再删除');
      return;
    }
    message.error(errorMessage(error));
  }
}

function rateText(rateBp?: number): string {
  return `${(numberOr(rateBp, 0) / 100).toFixed(2)}%`;
}

function payTypeCell(row: MerchantGroup) {
  const list = stringList(row.pay_types);
  if (list.length === 0) return h('span', { class: 'muted' }, '全部');
  return h(
    'div',
    { class: 'tag-cell' },
    list.map((item) =>
      h(NTag, { size: 'small', round: true, bordered: false }, { default: () => paymentTypeText(item) }),
    ),
  );
}

const columns = computed<DataTableColumns<MerchantGroup>>(() => [
  { title: '名称', key: 'name', minWidth: 160, ellipsis: true, render: (row) => row.name || '-' },
  {
    title: '费率',
    key: 'rate_bp',
    width: 100,
    align: 'right',
    render: (row) => rateText(row.rate_bp),
  },
  { title: '可用支付方式', key: 'pay_types', minWidth: 240, render: (row) => payTypeCell(row) },
  {
    title: '结算模式',
    key: 'settle_mode',
    width: 110,
    render: (row) => settleModeLabels[row.settle_mode || ''] ?? row.settle_mode ?? '-',
  },
  {
    title: '商户数',
    key: 'merchant_count',
    width: 90,
    align: 'right',
    render: (row) => (typeof row.merchant_count === 'number' ? row.merchant_count : '-'),
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
    width: 210,
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
                default: () => (row.status === 'active' ? '确认禁用该用户组？' : '确认启用该用户组？'),
              },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => remove(row) },
              {
                trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                default: () => '确认删除该用户组？删除后不可恢复。',
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
    <PageHeader title="用户组与费率" subtitle="按用户组设置商户费率、可用支付方式与结算模式">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="openCreate">新建用户组</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: MerchantGroup) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无用户组，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑用户组' : '新建用户组'"
      style="width: 640px; max-width: 94vw"
    >
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="130">
        <n-form-item label="用户组名称" path="name">
          <n-input v-model:value="form.name" placeholder="例如：普通商户 / 大客户" />
        </n-form-item>
        <n-form-item label="费率" path="rate_percent">
          <div class="rate-row">
            <n-input-number
              v-model:value="form.rate_percent"
              :min="0"
              :max="100"
              :step="0.01"
              :precision="2"
              style="width: 180px"
            />
            <span class="muted">%</span>
            <span class="muted small">后端按万分比存储，填 0.60 表示 60bp</span>
          </div>
        </n-form-item>
        <n-form-item label="可用支付方式">
          <n-select
            v-model:value="form.pay_types"
            multiple
            :options="payTypeOptions"
            placeholder="留空表示全部支付方式"
          />
        </n-form-item>
        <n-form-item label="结算模式" path="settle_mode">
          <n-select v-model:value="form.settle_mode" :options="settleModeOptions" />
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
.rate-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.small {
  font-size: 12px;
}
.tag-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
