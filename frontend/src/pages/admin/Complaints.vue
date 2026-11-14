<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import dayjs from 'dayjs';
import { complaintAutoRulesApi } from '@/api/complaintAutoRules';
import type { ComplaintAutoRule } from '@/api/complaintAutoRules';
import { complaintsApi } from '@/api/adminComplaints';
import type { Complaint } from '@/api/adminComplaints';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

interface ComplaintForm {
  merchant_id: string;
  order_id: string;
  channel: string;
  complaint_no: string;
  reason: string;
  amount_yuan: number | null;
  deadline: number | null;
}

const message = useMessage();

const rows = ref<Complaint[]>([]);
const loading = ref(false);

const status = ref<string | null>(null);
const channel = ref<string | null>(null);
const merchantId = ref('');

const showCreate = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const form = reactive<ComplaintForm>({
  merchant_id: '',
  order_id: '',
  channel: 'alipay',
  complaint_no: '',
  reason: '',
  amount_yuan: null,
  deadline: null,
});

const showDetail = ref(false);
const detail = ref<Complaint | null>(null);
const detailLoading = ref(false);
const replyText = ref('');
const replying = ref(false);
const closeReason = ref('');
const closing = ref(false);

const showRules = ref(false);
const autoRules = ref<ComplaintAutoRule[]>([]);
const rulesLoading = ref(false);
const showRuleForm = ref(false);
const ruleEditingId = ref<string | null>(null);
const ruleFormRef = ref<FormInst | null>(null);
const ruleSubmitting = ref(false);
const ruleForm = reactive({
  channel: 'alipay',
  reply_text: '',
  close_after_hours: 24,
  status: 'active',
});

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待处理', value: 'pending' },
  { label: '已回复', value: 'replied' },
  { label: '已超期', value: 'overdue' },
  { label: '已关闭', value: 'closed' },
];

const channelOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信支付', value: 'wxpay' },
  { label: '银联', value: 'unionpay' },
  { label: '其他', value: 'other' },
];

const statusMap: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  pending: { text: '待处理', type: 'warning' },
  replied: { text: '已回复', type: 'info' },
  overdue: { text: '已超期', type: 'error' },
  closed: { text: '已关闭', type: 'default' },
};

const channelLabels: Record<string, string> = {
  alipay: '支付宝',
  wxpay: '微信支付',
  unionpay: '银联',
  other: '其他',
};

const rules: FormRules = {
  merchant_id: { required: true, message: '请输入商户 ID', trigger: ['blur', 'input'] },
  channel: { required: true, message: '请选择渠道', trigger: ['change', 'blur'] },
  complaint_no: { required: true, message: '请输入投诉单号', trigger: ['blur', 'input'] },
  reason: { required: true, message: '请输入投诉原因', trigger: ['blur', 'input'] },
};

async function load() {
  loading.value = true;
  try {
    const data = await complaintsApi.list({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
      channel: channel.value ?? undefined,
      merchant_id: merchantId.value.trim() || undefined,
    });
    const items = itemsOf<Complaint>(data);
    rows.value = items;
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function search() {
  resetPage();
  void load();
}

function resetFilters() {
  status.value = null;
  channel.value = null;
  merchantId.value = '';
  resetPage();
  void load();
}

function openCreate() {
  form.merchant_id = '';
  form.order_id = '';
  form.channel = 'alipay';
  form.complaint_no = '';
  form.reason = '';
  form.amount_yuan = null;
  form.deadline = null;
  showCreate.value = true;
}

async function submitCreate() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    await complaintsApi.create({
      merchant_id: form.merchant_id.trim(),
      order_id: form.order_id.trim() || undefined,
      channel: form.channel,
      complaint_no: form.complaint_no.trim(),
      reason: form.reason.trim(),
      amount_cents: form.amount_yuan === null ? undefined : Math.round(form.amount_yuan * 100),
      deadline_at: form.deadline === null ? undefined : dayjs(form.deadline).toISOString(),
    });
    message.success('投诉已登记');
    showCreate.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function openDetail(row: Complaint) {
  showDetail.value = true;
  detail.value = null;
  detailLoading.value = true;
  replyText.value = '';
  closeReason.value = '';
  try {
    detail.value = await complaintsApi.detail(row.id);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    detailLoading.value = false;
  }
}

async function sendReply() {
  const target = detail.value;
  if (!target) return;
  const content = replyText.value.trim();
  if (content === '') {
    message.warning('请输入回复内容');
    return;
  }
  replying.value = true;
  try {
    const updated = await complaintsApi.reply(target.id, content);
    detail.value = updated && asRow(updated).id ? updated : await complaintsApi.detail(target.id);
    replyText.value = '';
    message.success('回复已发送');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    replying.value = false;
  }
}

async function closeComplaint() {
  const target = detail.value;
  if (!target) return;
  const reason = closeReason.value.trim();
  if (reason === '') {
    message.warning('请填写关闭原因');
    return;
  }
  closing.value = true;
  try {
    await complaintsApi.close(target.id, reason);
    message.success('投诉已关闭');
    closeReason.value = '';
    detail.value = await complaintsApi.detail(target.id);
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    closing.value = false;
  }
}

async function loadRules() {
  rulesLoading.value = true;
  try {
    autoRules.value = itemsOf<ComplaintAutoRule>(await complaintAutoRulesApi.list());
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    rulesLoading.value = false;
  }
}

function openRules() {
  showRules.value = true;
  void loadRules();
}

function openRuleCreate() {
  ruleEditingId.value = null;
  ruleForm.channel = channel.value || 'alipay';
  ruleForm.reply_text = '';
  ruleForm.close_after_hours = 24;
  ruleForm.status = 'active';
  showRuleForm.value = true;
}

function openRuleEdit(row: ComplaintAutoRule) {
  ruleEditingId.value = row.id;
  ruleForm.channel = row.channel || 'alipay';
  ruleForm.reply_text = row.reply_text || '';
  ruleForm.close_after_hours = typeof row.close_after_hours === 'number' ? row.close_after_hours : 0;
  ruleForm.status = row.status || 'active';
  showRuleForm.value = true;
}

async function submitRule() {
  if (ruleForm.reply_text.trim() === '') {
    message.warning('请填写自动回复内容');
    return;
  }
  ruleSubmitting.value = true;
  try {
    const payload = {
      channel: ruleForm.channel,
      reply_text: ruleForm.reply_text.trim(),
      close_after_hours: ruleForm.close_after_hours ?? 0,
      status: ruleForm.status,
    };
    if (ruleEditingId.value) {
      await complaintAutoRulesApi.update(ruleEditingId.value, payload);
      message.success('自动规则已更新');
    } else {
      await complaintAutoRulesApi.create(payload);
      message.success('自动规则已创建');
    }
    showRuleForm.value = false;
    await loadRules();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    ruleSubmitting.value = false;
  }
}

async function removeRule(row: ComplaintAutoRule) {
  try {
    await complaintAutoRulesApi.remove(row.id);
    message.success('自动规则已删除');
    await loadRules();
  } catch (error) {
    message.error(errorMessage(error));
  }
}

function ruleColumns(): DataTableColumns<ComplaintAutoRule> {
  return [
    {
      title: '渠道',
      key: 'channel',
      width: 100,
      render: (row) => channelLabels[row.channel ?? ''] ?? row.channel ?? '-',
    },
    { title: '自动回复', key: 'reply_text', minWidth: 220, ellipsis: true, render: (row) => row.reply_text || '-' },
    {
      title: '几小时后关闭',
      key: 'close_after_hours',
      width: 130,
      align: 'right',
      render: (row) =>
        typeof row.close_after_hours === 'number' && row.close_after_hours > 0 ? `${row.close_after_hours} 小时` : '不自动关闭',
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
      render: (row) =>
        h(
          NSpace,
          { size: 4, wrap: false },
          {
            default: () => [
              h(
                NButton,
                { size: 'tiny', quaternary: true, type: 'info', onClick: () => openRuleEdit(row) },
                { default: () => '编辑' },
              ),
              h(
                NPopconfirm,
                { onPositiveClick: () => removeRule(row) },
                {
                  trigger: () =>
                    h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => '删除' }),
                  default: () => '确认删除该自动规则？',
                },
              ),
            ],
          },
        ),
    },
  ];
}

function statusMeta(value?: string) {
  return statusMap[value ?? ''] ?? { text: value || '-', type: 'default' as const };
}

function statusTag(value?: string) {
  const resolved = statusMeta(value);
  return h(NTag, { size: 'small', round: true, type: resolved.type }, { default: () => resolved.text });
}

function directionText(direction?: string): string {
  const map: Record<string, string> = {
    inbound: '商户/渠道',
    outbound: '平台',
    merchant: '商户',
    platform: '平台',
    buyer: '买家',
    system: '系统',
  };
  return map[direction ?? ''] ?? direction ?? '-';
}

function deadlineCell(row: Complaint) {
  if (!row.deadline_at) return h('span', { class: 'muted' }, '-');
  const overdue = row.status !== 'closed' && dayjs(row.deadline_at).isBefore(dayjs());
  return h('span', { style: overdue ? 'color: #b91c1c; font-weight: 600' : undefined }, formatDateTime(row.deadline_at));
}

const columns = computed<DataTableColumns<Complaint>>(() => [
  {
    title: '投诉单号',
    key: 'complaint_no',
    width: 190,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.complaint_no || '-'),
  },
  {
    title: '渠道',
    key: 'channel',
    width: 100,
    render: (row) => channelLabels[row.channel ?? ''] ?? row.channel ?? '-',
  },
  {
    title: '商户 ID',
    key: 'merchant_id',
    width: 240,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchant_id || '-'),
  },
  {
    title: '订单号',
    key: 'order_id',
    width: 200,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.order_id || '-'),
  },
  { title: '投诉原因', key: 'reason', minWidth: 180, ellipsis: true, render: (row) => row.reason || '-' },
  {
    title: '金额',
    key: 'amount_cents',
    width: 120,
    align: 'right',
    render: (row) =>
      typeof row.amount_cents === 'number' ? h(MoneyText, { amount: row.amount_cents }) : '-',
  },
  { title: '状态', key: 'status', width: 100, render: (row) => statusTag(row.status) },
  { title: '截止时间', key: 'deadline_at', width: 170, render: (row) => deadlineCell(row) },
  {
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    fixed: 'right',
    render: (row) =>
      h(
        NButton,
        { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) },
        { default: () => '详情' },
      ),
  },
]);

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="投诉管理" subtitle="渠道投诉登记、回复与关闭，超期未处理会标红">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button secondary type="primary" @click="openRules">自动规则</n-button>
        <n-button type="primary" @click="openCreate">手工登记</n-button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <n-select v-model:value="status" :options="statusOptions" placeholder="全部状态" clearable style="width: 140px" />
      <n-select v-model:value="channel" :options="channelOptions" placeholder="全部渠道" clearable style="width: 140px" />
      <n-input
        v-model:value="merchantId"
        placeholder="商户 ID"
        clearable
        style="width: 260px"
        @keyup.enter="search"
      />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
      <span class="muted small">共 {{ total }} 条</span>
    </div>

    <n-card :bordered="false">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Complaint) => row.id"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无投诉记录" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showDetail" :width="620" placement="right">
      <n-drawer-content :title="detail?.complaint_no || '投诉详情'" closable>
        <n-spin :show="detailLoading">
          <n-empty v-if="!detail && !detailLoading" description="暂无数据" />
          <template v-else-if="detail">
            <n-descriptions :column="1" label-placement="left" bordered size="small">
              <n-descriptions-item label="投诉单号">
                <span class="mono">{{ detail.complaint_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag :type="statusMeta(detail.status).type" size="small" round>
                  {{ statusMeta(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="渠道">
                {{ channelLabels[detail.channel ?? ''] ?? detail.channel ?? '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="商户 ID">
                <span class="mono">{{ detail.merchant_id || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="订单号">
                <span class="mono">{{ detail.order_id || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="投诉金额">
                <MoneyText v-if="typeof detail.amount_cents === 'number'" :amount="detail.amount_cents" />
                <span v-else>-</span>
              </n-descriptions-item>
              <n-descriptions-item label="投诉原因">{{ detail.reason || '-' }}</n-descriptions-item>
              <n-descriptions-item label="截止时间">{{ formatDateTime(detail.deadline_at) }}</n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
            </n-descriptions>

            <n-divider title-placement="left">沟通记录</n-divider>
            <n-empty v-if="(detail.messages ?? []).length === 0" description="暂无沟通记录" size="small" />
            <div v-else class="msg-list">
              <div v-for="(msg, index) in detail.messages ?? []" :key="msg.id ?? index" class="msg-item">
                <div class="msg-head">
                  <span class="msg-who">{{ directionText(msg.direction) }}</span>
                  <span class="muted small">{{ msg.operator || '-' }} · {{ formatDateTime(msg.created_at) }}</span>
                </div>
                <div class="msg-body">{{ msg.content }}</div>
              </div>
            </div>

            <n-divider title-placement="left">回复</n-divider>
            <n-input
              v-model:value="replyText"
              type="textarea"
              :autosize="{ minRows: 3, maxRows: 6 }"
              placeholder="填写回复内容，会记录到投诉沟通记录"
            />
            <n-space justify="end" style="margin-top: 10px">
              <n-button type="primary" :loading="replying" :disabled="detail.status === 'closed'" @click="sendReply">
                发送回复
              </n-button>
            </n-space>

            <n-divider title-placement="left">关闭投诉</n-divider>
            <n-input v-model:value="closeReason" placeholder="关闭原因，例如：已退款完成 / 双方协商一致" />
            <n-space justify="end" style="margin-top: 10px">
              <n-button
                type="error"
                secondary
                :loading="closing"
                :disabled="detail.status === 'closed'"
                @click="closeComplaint"
              >
                关闭投诉
              </n-button>
            </n-space>
          </template>
        </n-spin>
      </n-drawer-content>
    </n-drawer>

    <n-modal v-model:show="showCreate" preset="card" title="手工登记投诉" style="width: 660px; max-width: 94vw">
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="120">
        <n-form-item label="商户 ID" path="merchant_id">
          <n-input v-model:value="form.merchant_id" placeholder="投诉所属商户" />
        </n-form-item>
        <n-form-item label="订单号">
          <n-input v-model:value="form.order_id" placeholder="可选，关联的支付订单" />
        </n-form-item>
        <n-form-item label="渠道" path="channel">
          <n-select v-model:value="form.channel" :options="channelOptions" tag filterable />
        </n-form-item>
        <n-form-item label="投诉单号" path="complaint_no">
          <n-input v-model:value="form.complaint_no" placeholder="渠道侧投诉单号" />
        </n-form-item>
        <n-form-item label="投诉原因" path="reason">
          <n-input
            v-model:value="form.reason"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 6 }"
            placeholder="渠道给出的投诉描述"
          />
        </n-form-item>
        <n-form-item label="投诉金额（元）">
          <n-input-number v-model:value="form.amount_yuan" :min="0" :precision="2" :step="1" style="width: 180px" />
        </n-form-item>
        <n-form-item label="截止时间">
          <n-date-picker v-model:value="form.deadline" type="datetime" clearable style="width: 220px" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submitCreate">登记</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-drawer v-model:show="showRules" :width="720" placement="right">
      <n-drawer-content title="客诉自动规则" closable>
        <n-alert type="info" :bordered="false" style="margin-bottom: 14px">
          规则按渠道匹配：命中渠道的新投诉会自动回复，并按设定小时数自动关闭（填 0 表示只回复不关闭）。
        </n-alert>
        <div class="rules-toolbar">
          <n-button type="primary" size="small" @click="openRuleCreate">新建规则</n-button>
          <n-button size="small" :loading="rulesLoading" @click="loadRules">刷新</n-button>
        </div>
        <n-data-table
          :columns="ruleColumns()"
          :data="autoRules"
          :loading="rulesLoading"
          :pagination="{ pageSize: 10 }"
          :row-key="(row: ComplaintAutoRule) => row.id"
          :bordered="false"
          :single-line="false"
          size="small"
        >
          <template #empty>
            <n-empty description="还没有自动规则" size="small" />
          </template>
        </n-data-table>
      </n-drawer-content>
    </n-drawer>

    <n-modal
      v-model:show="showRuleForm"
      preset="card"
      :title="ruleEditingId ? '编辑自动规则' : '新建自动规则'"
      style="width: 560px; max-width: 94vw"
    >
      <n-form ref="ruleFormRef" :model="ruleForm" label-placement="left" label-width="120">
        <n-form-item label="适用渠道">
          <n-select v-model:value="ruleForm.channel" :options="channelOptions" tag filterable />
        </n-form-item>
        <n-form-item label="自动回复">
          <n-input
            v-model:value="ruleForm.reply_text"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 6 }"
            placeholder="命中渠道后自动发给我方/商户的回复内容"
          />
        </n-form-item>
        <n-form-item label="几小时后关闭">
          <n-input-number v-model:value="ruleForm.close_after_hours" :min="0" :step="1" style="width: 160px" />
          <span class="muted small" style="margin-left: 10px">0 表示不自动关闭</span>
        </n-form-item>
        <n-form-item label="状态">
          <n-select
            v-model:value="ruleForm.status"
            :options="[
              { label: '启用', value: 'active' },
              { label: '禁用', value: 'disabled' },
            ]"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRuleForm = false">取消</n-button>
          <n-button type="primary" :loading="ruleSubmitting" @click="submitRule">保存</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.small {
  font-size: 12px;
}
.rules-toolbar {
  display: flex;
  gap: 10px;
  margin-bottom: 12px;
}
.msg-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.msg-item {
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 10px 12px;
  background: #fafbfc;
}
.msg-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 6px;
  font-size: 12px;
}
.msg-who {
  font-weight: 600;
  color: #2563eb;
}
.msg-body {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  color: #1f2937;
}
</style>
