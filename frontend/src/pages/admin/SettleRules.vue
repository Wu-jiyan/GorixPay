<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { settleRulesApi } from '@/api/settleRules';
import type { SettleRule } from '@/api/settleRules';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

interface RuleForm {
  name: string;
  merchant_id: string;
  min_amount_yuan: number | null;
  fee_percent: number | null;
  period: string;
  auto_transfer: boolean;
}

const message = useMessage();

const rows = ref<SettleRule[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const editingStatus = ref('active');
const formRef = ref<FormInst | null>(null);
const runningId = ref('');

const { page, pageSize, total, pagination, onPage, onPageSize } = useServerPaging(load);

const form = reactive<RuleForm>({
  name: '',
  merchant_id: '',
  min_amount_yuan: 0,
  fee_percent: 0,
  period: 'daily',
  auto_transfer: false,
});

const periodOptions: SelectOption[] = [
  { label: '每日（daily）', value: 'daily' },
  { label: '每周（weekly）', value: 'weekly' },
  { label: '每月（monthly）', value: 'monthly' },
];

const periodLabels: Record<string, string> = {
  daily: '每日',
  weekly: '每周',
  monthly: '每月',
};

const rules: FormRules = {
  name: { required: true, message: '请输入规则名称', trigger: ['blur', 'input'] },
  period: { required: true, message: '请选择或填写周期', trigger: ['change', 'blur'] },
};

const isEditing = computed(() => editingId.value !== null);

async function load() {
  loading.value = true;
  try {
    const data = await settleRulesApi.list({ page: page.value, page_size: pageSize.value });
    const items = itemsOf<SettleRule>(data);
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
  form.merchant_id = '';
  form.min_amount_yuan = 0;
  form.fee_percent = 0;
  form.period = 'daily';
  form.auto_transfer = false;
}

function openCreate() {
  editingId.value = null;
  editingStatus.value = 'active';
  resetForm();
  showForm.value = true;
}

function openEdit(row: SettleRule) {
  editingId.value = row.id;
  editingStatus.value = row.status || 'active';
  form.name = row.name || '';
  form.merchant_id = row.merchant_id || '';
  form.min_amount_yuan = numberOr(row.min_amount_cents, 0) / 100;
  form.fee_percent = numberOr(row.fee_bp, 0) / 100;
  form.period = row.period || 'daily';
  form.auto_transfer = row.auto_transfer === true;
  showForm.value = true;
}

function payloadOf(status: string) {
  return {
    name: form.name.trim(),
    merchant_id: form.merchant_id.trim() === '' ? null : form.merchant_id.trim(),
    min_amount_cents: Math.round(numberOr(form.min_amount_yuan, 0) * 100),
    fee_bp: Math.round(numberOr(form.fee_percent, 0) * 100),
    period: form.period,
    auto_transfer: form.auto_transfer,
    status,
  };
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  const name = form.name.trim();
  if (name === '') {
    message.warning('请输入规则名称');
    return;
  }
  submitting.value = true;
  try {
    if (editingId.value) {
      await settleRulesApi.update(editingId.value, payloadOf(editingStatus.value));
      message.success('结算规则已更新');
    } else {
      await settleRulesApi.create(payloadOf('active'));
      message.success('结算规则已创建');
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

function rulePayload(row: SettleRule, status: string) {
  return {
    name: row.name || '',
    merchant_id: row.merchant_id || null,
    min_amount_cents: numberOr(row.min_amount_cents, 0),
    fee_bp: numberOr(row.fee_bp, 0),
    period: row.period || 'daily',
    auto_transfer: row.auto_transfer === true,
    status,
  };
}

async function toggleStatus(row: SettleRule) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  try {
    await settleRulesApi.update(row.id, rulePayload(row, next));
    message.success(next === 'active' ? '规则已启用' : '规则已禁用');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

async function runNow(row: SettleRule) {
  runningId.value = row.id;
  try {
    const result = await settleRulesApi.run(row.id);
    const count = countOf(result);
    message.success(`${row.name || '规则'} 执行完成，本次生成 ${count} 张结算单`);
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    runningId.value = '';
  }
}

/** 后端返回生成数量的字段名待定，做兼容取值 */
function countOf(result: unknown): number {
  if (typeof result === 'number') return result;
  const row = asRow(result);
  for (const key of ['generated', 'count', 'created', 'settled', 'generated_count']) {
    const value = row[key];
    if (typeof value === 'number' && Number.isFinite(value)) return value;
  }
  return 0;
}

async function remove(row: SettleRule) {
  try {
    await settleRulesApi.remove(row.id);
    message.success('结算规则已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

const columns = computed<DataTableColumns<SettleRule>>(() => {
  const running = runningId.value;
  return [
    { title: '规则名称', key: 'name', minWidth: 150, ellipsis: true, render: (row) => row.name || '-' },
    {
      title: '适用商户',
      key: 'merchant_id',
      width: 260,
      ellipsis: true,
      render: (row) =>
        row.merchant_id
          ? h('span', { class: 'mono' }, row.merchant_id)
          : h(NTag, { size: 'small', round: true, bordered: false }, { default: () => '全部商户' }),
    },
    {
      title: '最低金额',
      key: 'min_amount_cents',
      width: 130,
      align: 'right',
      render: (row) => h(MoneyText, { amount: numberOr(row.min_amount_cents, 0) }),
    },
    {
      title: '费率',
      key: 'fee_bp',
      width: 100,
      align: 'right',
      render: (row) => `${(numberOr(row.fee_bp, 0) / 100).toFixed(2)}%`,
    },
    {
      title: '周期',
      key: 'period',
      width: 100,
      render: (row) => periodLabels[row.period || ''] ?? row.period ?? '-',
    },
    {
      title: '自动打款',
      key: 'auto_transfer',
      width: 100,
      render: (row) =>
        h(
          NTag,
          { size: 'small', round: true, type: row.auto_transfer ? 'success' : 'default', bordered: false },
          { default: () => (row.auto_transfer ? '开启' : '关闭') },
        ),
    },
    {
      title: '状态',
      key: 'status',
      width: 90,
      render: (row) => h(StatusTag, { status: row.status, kind: 'active' }),
    },
    {
      title: '上次执行',
      key: 'last_run_at',
      width: 170,
      render: (row) => formatDateTime(row.last_run_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 250,
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
                { size: 'tiny', quaternary: true, type: 'primary', loading: running === row.id, onClick: () => runNow(row) },
                { default: () => '立即执行' },
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
                  default: () => (row.status === 'active' ? '确认禁用该规则？' : '确认启用该规则？'),
                },
              ),
              h(
                NPopconfirm,
                { onPositiveClick: () => remove(row) },
                {
                  trigger: () =>
                    h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                  default: () => '确认删除该结算规则？',
                },
              ),
            ],
          },
        ),
    },
  ];
});

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="结算规则" subtitle="按周期自动生成结算单，可配置最低金额、费率与自动打款">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="openCreate">新建规则</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: SettleRule) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无结算规则，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑结算规则' : '新建结算规则'"
      style="width: 680px; max-width: 94vw"
    >
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="140">
        <n-form-item label="规则名称" path="name">
          <n-input v-model:value="form.name" placeholder="例如：T+1 自动结算" />
        </n-form-item>
        <n-form-item label="适用商户">
          <n-input v-model:value="form.merchant_id" placeholder="留空表示全部商户，或填商户 ID" />
        </n-form-item>
        <n-form-item label="最低金额（元）">
          <div class="row-inline">
            <n-input-number
              v-model:value="form.min_amount_yuan"
              :min="0"
              :step="10"
              :precision="2"
              style="width: 180px"
            />
            <span class="muted small">可用余额达到该金额才生成结算单</span>
          </div>
        </n-form-item>
        <n-form-item label="费率（%）">
          <div class="row-inline">
            <n-input-number
              v-model:value="form.fee_percent"
              :min="0"
              :max="100"
              :step="0.01"
              :precision="2"
              style="width: 180px"
            />
            <span class="muted small">后端按万分比存储，填 0.60 表示 60bp</span>
          </div>
        </n-form-item>
        <n-form-item label="结算周期" path="period">
          <n-select
            v-model:value="form.period"
            :options="periodOptions"
            tag
            filterable
            placeholder="daily / weekly / monthly，也可直接输入"
          />
        </n-form-item>
        <n-form-item label="自动打款">
          <n-switch v-model:value="form.auto_transfer">
            <template #checked>开启</template>
            <template #unchecked>关闭</template>
          </n-switch>
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
.row-inline {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.small {
  font-size: 12px;
}
</style>
