<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import { errorMessage } from '@/api/client';
import MoneyText from '@/components/MoneyText.vue';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, hasKeys, httpStatus, itemsOf, numberOr, pick, textOf, usePendingAction, useServerPaging } from './shared';

interface CertRow {
  merchantId: string;
  displayName: string;
  email: string;
  phone: string;
  status: string;
  certType: string;
  realName: string;
  idNo: string;
  corpName: string;
  corpNo: string;
  feeCents: number | null;
  required: boolean;
  submittedAt: string | null;
  reviewedAt: string | null;
  rejectionReason: string;
  merchantCreatedAt: string;
}

const message = useMessage();

const rows = ref<CertRow[]>([]);
const loading = ref(false);
const unavailable = ref(false);
const status = ref<string | null>(null);
const keyword = ref('');

const showDetail = ref(false);
const detail = ref<CertRow | null>(null);

const processing = ref(false);
const showReject = ref(false);
const rejectTarget = ref<CertRow | null>(null);
const rejectionReason = ref('');

const actions = usePendingAction();

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const statusOptions: SelectOption[] = [
  { label: '待审核', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已驳回', value: 'rejected' },
];

const certTypeLabels: Record<string, string> = { person: '个人', corp: '企业' };

function certSource(value: unknown): Record<string, unknown> {
  const raw = asRow(value);
  const nested = asRow(raw.certification);
  return hasKeys(nested) ? nested : raw;
}

function certStatusOf(value: unknown): string {
  const raw = asRow(value);
  const nested = asRow(raw.certification);
  if (hasKeys(nested)) return textOf(pick(nested, 'status'), 'none');
  const flat = textOf(pick(raw, 'certification_status', 'cert_status'), '');
  if (flat) return flat;
  const merged = textOf(pick(raw, 'status'), '');
  return ['none', 'pending', 'approved', 'rejected'].includes(merged) ? merged : 'none';
}

function normalizeCert(value: unknown): CertRow {
  const raw = asRow(value);
  const source = certSource(value);
  const fee = pick(source, 'fee_cents');
  return {
    merchantId: textOf(pick(raw, 'id', 'merchant_id'), ''),
    displayName: textOf(pick(raw, 'display_name'), ''),
    email: textOf(pick(raw, 'email'), '-'),
    phone: textOf(pick(raw, 'phone'), '-'),
    status: certStatusOf(value),
    certType: textOf(pick(source, 'cert_type'), '-'),
    realName: textOf(pick(source, 'real_name_masked'), '-'),
    idNo: textOf(pick(source, 'id_no_masked'), '-'),
    corpName: textOf(pick(source, 'corp_name'), '-'),
    corpNo: textOf(pick(source, 'corp_no'), '-'),
    feeCents: typeof fee === 'number' ? fee : null,
    required: pick(source, 'required') === true,
    submittedAt: typeof pick(source, 'submitted_at') === 'string' ? String(pick(source, 'submitted_at')) : null,
    reviewedAt: typeof pick(source, 'reviewed_at') === 'string' ? String(pick(source, 'reviewed_at')) : null,
    rejectionReason: textOf(pick(source, 'rejection_reason'), '-'),
    merchantCreatedAt: textOf(pick(raw, 'created_at'), ''),
  };
}

async function load() {
  loading.value = true;
  try {
    const data = await adminApi.certifications({
      page: page.value,
      page_size: pageSize.value,
      status: status.value ?? undefined,
      keyword: keyword.value.trim() || undefined,
    });
    const items = itemsOf(data.items);
    rows.value = items.map((item) => normalizeCert(item));
    total.value = numberOr(data.total, items.length);
    unavailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      rows.value = [];
      total.value = 0;
      unavailable.value = true;
      return;
    }
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
  keyword.value = '';
  resetPage();
  void load();
}

function openDetail(row: CertRow) {
  detail.value = row;
  showDetail.value = true;
}

async function approve(row: CertRow) {
  await actions.run(`approve:${row.merchantId}`, async () => {
    try {
      await adminApi.reviewCertification(row.merchantId, 'approve');
      message.success('实名认证已通过');
      showDetail.value = false;
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

function openReject(row: CertRow) {
  rejectTarget.value = row;
  rejectionReason.value = '';
  showReject.value = true;
}

async function submitReject() {
  const target = rejectTarget.value;
  if (!target) return;
  if (!rejectionReason.value.trim()) {
    message.warning('请填写驳回原因');
    return;
  }
  processing.value = true;
  try {
    await adminApi.reviewCertification(target.merchantId, 'reject', rejectionReason.value.trim());
    message.success('实名认证已驳回');
    showReject.value = false;
    showDetail.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    processing.value = false;
  }
}

function statusCell(value: string) {
  if (value === 'pending' || value === 'approved' || value === 'rejected') {
    return h(StatusTag, { status: value, kind: 'finance' });
  }
  return h(NTag, { size: 'small', round: true }, { default: () => (value === 'none' ? '未提交' : value || '-') });
}

const columns = computed<DataTableColumns<CertRow>>(() => {
  const pendingSet = actions.pending.value;
  return [
  {
    title: '商户 ID',
    key: 'merchantId',
    width: 260,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.merchantId || '-'),
  },
  { title: '商户名称', key: 'displayName', minWidth: 140, ellipsis: true, render: (row) => row.displayName || '-' },
  { title: '状态', key: 'status', width: 100, render: (row) => statusCell(row.status) },
  {
    title: '证件类型',
    key: 'certType',
    width: 100,
    render: (row) => certTypeLabels[row.certType] ?? row.certType,
  },
  { title: '姓名', key: 'realName', width: 120, ellipsis: true, render: (row) => row.realName },
  {
    title: '证件号',
    key: 'idNo',
    width: 170,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.idNo),
  },
  { title: '企业名称', key: 'corpName', minWidth: 160, ellipsis: true, render: (row) => row.corpName },
  {
    title: '提交时间',
    key: 'submittedAt',
    width: 170,
    render: (row) => formatDateTime(row.submittedAt),
  },
  {
    title: '操作',
    key: 'actions',
    width: 190,
    fixed: 'right',
    render: (row) => {
      const approving = pendingSet.has(`approve:${row.merchantId}`);
      const detailButton = h(
        NButton,
        { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) },
        { default: () => '详情' },
      );
      if (row.status !== 'pending') {
        return h(NSpace, { size: 4, wrap: false }, { default: () => [detailButton] });
      }
      return h(NSpace, { size: 4, wrap: false }, {
        default: () => [
          detailButton,
          h(
            NPopconfirm,
            { onPositiveClick: () => approve(row) },
            {
              trigger: () =>
                h(
                  NButton,
                  { size: 'tiny', quaternary: true, type: 'primary', loading: approving, disabled: approving },
                  { default: () => '通过' },
                ),
              default: () => '确认通过该商户的实名认证？',
            },
          ),
          h(
            NButton,
            { size: 'tiny', quaternary: true, type: 'error', disabled: approving, onClick: () => openReject(row) },
            { default: () => '驳回' },
          ),
        ],
      });
    },
  },
  ];
});

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="实名审核" subtitle="商户实名认证（个人 / 企业）审核，通过后可用于强制认证校验">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="unavailable" type="warning" :bordered="false" style="margin-bottom: 16px">
      实名审核接口未启用：后端 <span class="mono">/admin/certifications</span> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <div class="toolbar">
      <n-input
        v-model:value="keyword"
        placeholder="商户名称 / 邮箱 / 手机号"
        clearable
        style="width: 260px"
        @keyup.enter="search"
      />
      <n-select v-model:value="status" :options="statusOptions" placeholder="全部状态" clearable style="width: 150px" />
      <n-button type="primary" @click="search">查询</n-button>
      <n-button @click="resetFilters">重置</n-button>
      <span class="muted" style="font-size: 12px">共 {{ total }} 条</span>
    </div>

    <n-card :bordered="false">
      <n-empty v-if="unavailable" description="接口未启用" size="large">
        <template #extra>
          <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
        </template>
      </n-empty>
      <n-data-table
        v-else
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: CertRow) => row.merchantId"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无实名认证申请" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showDetail" :width="560" placement="right">
      <n-drawer-content :title="detail?.displayName || '实名认证详情'" closable>
        <n-empty v-if="!detail" description="暂无数据" />
        <template v-else>
          <n-descriptions :column="1" label-placement="left" bordered size="small">
            <n-descriptions-item label="商户 ID">
              <span class="mono">{{ detail.merchantId || '-' }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="商户名称">{{ detail.displayName || '-' }}</n-descriptions-item>
            <n-descriptions-item label="邮箱">{{ detail.email }}</n-descriptions-item>
            <n-descriptions-item label="手机号">{{ detail.phone }}</n-descriptions-item>
            <n-descriptions-item label="认证状态">
              <StatusTag :status="detail.status" kind="finance" />
            </n-descriptions-item>
            <n-descriptions-item label="证件类型">
              {{ certTypeLabels[detail.certType] ?? detail.certType }}
            </n-descriptions-item>
            <n-descriptions-item label="姓名">{{ detail.realName }}</n-descriptions-item>
            <n-descriptions-item label="证件号">
              <span class="mono">{{ detail.idNo }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="企业名称">{{ detail.corpName }}</n-descriptions-item>
            <n-descriptions-item label="企业注册号">
              <span class="mono">{{ detail.corpNo }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="认证费用">
              <MoneyText :amount="detail.feeCents ?? 0" />
            </n-descriptions-item>
            <n-descriptions-item label="提交时间">{{ formatDateTime(detail.submittedAt) }}</n-descriptions-item>
            <n-descriptions-item label="审核时间">{{ formatDateTime(detail.reviewedAt) }}</n-descriptions-item>
            <n-descriptions-item label="驳回原因">{{ detail.rejectionReason }}</n-descriptions-item>
          </n-descriptions>
          <n-space justify="end" style="margin-top: 16px">
            <n-button
              v-if="detail.status === 'pending'"
              type="primary"
              @click="approve(detail)"
            >
              通过认证
            </n-button>
            <n-button v-if="detail.status === 'pending'" type="error" @click="openReject(detail)">驳回</n-button>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>

    <n-modal v-model:show="showReject" preset="card" title="驳回实名认证" style="width: 520px; max-width: 94vw">
      <n-descriptions v-if="rejectTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="商户">
          {{ rejectTarget.displayName || rejectTarget.merchantId }}
        </n-descriptions-item>
        <n-descriptions-item label="姓名">{{ rejectTarget.realName }}</n-descriptions-item>
      </n-descriptions>
      <div class="field">
        <div class="field-label">驳回原因</div>
        <n-input
          v-model:value="rejectionReason"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 6 }"
          placeholder="请填写驳回原因，商户可见"
        />
      </div>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showReject = false">取消</n-button>
          <n-button type="error" :loading="processing" @click="submitReject">确认驳回</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.field {
  margin-top: 16px;
}
.field-label {
  font-size: 13px;
  color: #4b5563;
  margin-bottom: 6px;
}
</style>
