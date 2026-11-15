<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import type { Component } from 'vue';
import { NButton, NIcon, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import {
  CheckmarkOutline,
  CloseOutline,
  DocumentTextOutline,
  RefreshOutline,
  ReloadOutline,
  SearchOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import { errorMessage } from '@/api/client';
import { formatDateTime, formatMoney } from '@/utils/format';
import {
  asRow,
  hasKeys,
  httpStatus,
  itemsOf,
  numberOr,
  pick,
  textOf,
  usePendingAction,
  useServerPaging,
} from './shared';

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

const certStatusMap: Record<
  string,
  { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }
> = {
  pending: { text: '待审核', type: 'warning' },
  approved: { text: '已通过', type: 'success' },
  rejected: { text: '已驳回', type: 'error' },
  none: { text: '未提交', type: 'default' },
};

function renderIcon(icon: Component) {
  return () => h(NIcon, null, { default: () => h(icon) });
}

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
    submittedAt:
      typeof pick(source, 'submitted_at') === 'string' ? String(pick(source, 'submitted_at')) : null,
    reviewedAt:
      typeof pick(source, 'reviewed_at') === 'string' ? String(pick(source, 'reviewed_at')) : null,
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

/** 抽屉里的「通过认证 / 驳回」共用表格行的处理逻辑 */
function approveDetail() {
  if (detail.value) void approve(detail.value);
}

function rejectDetail() {
  if (detail.value) openReject(detail.value);
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

function statusTagOf(value: string) {
  const resolved = certStatusMap[value] ?? { text: value || '-', type: 'default' as const };
  return h(
    NTag,
    { size: 'small', round: true, bordered: false, type: resolved.type },
    { default: () => resolved.text },
  );
}

const detailApproving = computed(() =>
  detail.value ? actions.pending.value.has(`approve:${detail.value.merchantId}`) : false,
);

const columns = computed<DataTableColumns<CertRow>>(() => {
  const pendingSet = actions.pending.value;
  return [
    {
      title: '商户 ID',
      key: 'merchantId',
      width: 260,
      ellipsis: true,
      render: (row) => h(NText, { code: true }, { default: () => row.merchantId || '-' }),
    },
    {
      title: '商户名称',
      key: 'displayName',
      minWidth: 150,
      ellipsis: true,
      render: (row) => row.displayName || '-',
    },
    { title: '状态', key: 'status', width: 110, render: (row) => statusTagOf(row.status) },
    {
      title: '证件类型',
      key: 'certType',
      width: 110,
      render: (row) => certTypeLabels[row.certType] ?? row.certType,
    },
    { title: '姓名', key: 'realName', width: 130, ellipsis: true, render: (row) => row.realName },
    {
      title: '证件号',
      key: 'idNo',
      width: 180,
      ellipsis: true,
      render: (row) => h(NText, { code: true }, { default: () => row.idNo }),
    },
    {
      title: '企业名称',
      key: 'corpName',
      minWidth: 170,
      ellipsis: true,
      render: (row) => row.corpName,
    },
    {
      title: '提交时间',
      key: 'submittedAt',
      width: 180,
      render: (row) => formatDateTime(row.submittedAt),
    },
    {
      title: '操作',
      key: 'actions',
      width: 240,
      fixed: 'right',
      render: (row) => {
        const approving = pendingSet.has(`approve:${row.merchantId}`);
        const detailButton = h(
          NButton,
          { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) },
          { icon: renderIcon(DocumentTextOutline), default: () => '详情' },
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
                    {
                      size: 'tiny',
                      quaternary: true,
                      type: 'primary',
                      loading: approving,
                      disabled: approving,
                    },
                    { icon: renderIcon(CheckmarkOutline), default: () => '通过' },
                  ),
                default: () => '确认通过该商户的实名认证？',
              },
            ),
            h(
              NButton,
              {
                size: 'tiny',
                quaternary: true,
                type: 'error',
                disabled: approving,
                onClick: () => openReject(row),
              },
              { icon: renderIcon(CloseOutline), default: () => '驳回' },
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
  <admin-page title="实名审核" subtitle="商户实名认证（个人 / 企业）审核，通过后可用于强制认证校验">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        刷新
      </n-button>
    </template>

    <n-alert v-if="unavailable" type="warning" :bordered="false" :show-icon="true" style="margin-bottom: 16px">
      实名审核接口未启用：后端 <n-text code>/admin/certifications</n-text> 尚未就绪（404），完成后本页自动可用。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-space align="center" :size="12" style="margin-bottom: 12px">
        <n-input
          v-model:value="keyword"
          placeholder="商户名称 / 邮箱 / 手机号"
          clearable
          style="width: 260px"
          @keyup.enter="search"
        />
        <n-select
          v-model:value="status"
          :options="statusOptions"
          placeholder="全部状态"
          clearable
          style="width: 160px"
        />
        <n-button type="primary" @click="search">
          <template #icon><n-icon><SearchOutline /></n-icon></template>
          查询
        </n-button>
        <n-button @click="resetFilters">
          <template #icon><n-icon><ReloadOutline /></n-icon></template>
          重置
        </n-button>
        <n-text depth="3">共 {{ total }} 条</n-text>
      </n-space>

      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: CertRow) => row.merchantId"
        :bordered="false"
        :single-line="false"
        :scroll-x="1600"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty
            :description="unavailable ? '接口未启用' : '暂无实名认证申请'"
            size="small"
          >
            <template v-if="unavailable" #extra>
              <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
            </template>
          </n-empty>
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showDetail" :width="640" placement="right">
      <n-drawer-content :title="detail?.displayName || '实名认证详情'" closable>
        <n-empty v-if="!detail" description="暂无数据" size="small" />
        <n-descriptions v-else :column="2" label-placement="left" bordered size="small">
          <n-descriptions-item label="商户 ID">
            <n-text code>{{ detail.merchantId || '-' }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="商户名称">{{ detail.displayName || '-' }}</n-descriptions-item>
          <n-descriptions-item label="邮箱">{{ detail.email }}</n-descriptions-item>
          <n-descriptions-item label="手机号">{{ detail.phone }}</n-descriptions-item>
          <n-descriptions-item label="认证状态">
            <n-tag
              size="small"
              round
              :bordered="false"
              :type="(certStatusMap[detail.status] ?? { type: 'default' }).type"
            >
              {{ (certStatusMap[detail.status] ?? { text: detail.status || '-' }).text }}
            </n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="证件类型">
            {{ certTypeLabels[detail.certType] ?? detail.certType }}
          </n-descriptions-item>
          <n-descriptions-item label="姓名">{{ detail.realName }}</n-descriptions-item>
          <n-descriptions-item label="证件号">
            <n-text code>{{ detail.idNo }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="企业名称">{{ detail.corpName }}</n-descriptions-item>
          <n-descriptions-item label="企业注册号">
            <n-text code>{{ detail.corpNo }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="认证费用">
            <n-text strong>{{ formatMoney(detail.feeCents ?? 0) }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="强制认证">{{ detail.required ? '是' : '否' }}</n-descriptions-item>
          <n-descriptions-item label="提交时间">
            {{ formatDateTime(detail.submittedAt) }}
          </n-descriptions-item>
          <n-descriptions-item label="审核时间">
            {{ formatDateTime(detail.reviewedAt) }}
          </n-descriptions-item>
          <n-descriptions-item label="驳回原因">{{ detail.rejectionReason }}</n-descriptions-item>
          <n-descriptions-item label="商户创建时间">
            {{ formatDateTime(detail.merchantCreatedAt) }}
          </n-descriptions-item>
        </n-descriptions>

        <template #footer>
          <n-space justify="end">
            <n-button
              v-if="detail?.status === 'pending'"
              type="primary"
              :loading="detailApproving"
              :disabled="detailApproving"
              @click="approveDetail"
            >
              <template #icon><n-icon><CheckmarkOutline /></n-icon></template>
              通过认证
            </n-button>
            <n-button
              v-if="detail?.status === 'pending'"
              type="error"
              :disabled="detailApproving"
              @click="rejectDetail"
            >
              <template #icon><n-icon><CloseOutline /></n-icon></template>
              驳回
            </n-button>
            <n-button @click="showDetail = false">关闭</n-button>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>

    <n-modal
      v-model:show="showReject"
      preset="card"
      title="驳回实名认证"
      style="width: 560px; max-width: 94vw"
    >
      <n-descriptions v-if="rejectTarget" :column="2" label-placement="left" bordered size="small">
        <n-descriptions-item label="商户">
          {{ rejectTarget.displayName || rejectTarget.merchantId }}
        </n-descriptions-item>
        <n-descriptions-item label="姓名">{{ rejectTarget.realName }}</n-descriptions-item>
        <n-descriptions-item label="证件号">
          <n-text code>{{ rejectTarget.idNo }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item label="证件类型">
          {{ certTypeLabels[rejectTarget.certType] ?? rejectTarget.certType }}
        </n-descriptions-item>
      </n-descriptions>

      <n-form label-placement="left" label-width="180" style="margin-top: 16px">
        <n-form-item label="驳回原因">
          <n-input
            v-model:value="rejectionReason"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 6 }"
            placeholder="请填写驳回原因，商户可见"
          />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showReject = false">取消</n-button>
          <n-button type="error" :loading="processing" @click="submitReject">
            <template #icon><n-icon><CloseOutline /></n-icon></template>
            确认驳回
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
