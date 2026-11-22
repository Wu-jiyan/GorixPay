<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { DownloadOutline, RefreshOutline } from '@vicons/ionicons5';
import dayjs from 'dayjs';
import { complaintApi, type Complaint, type ComplaintDetail, type ComplaintMessage } from '@/api/merchantComplaints';
import { errorMessage } from '@/api/client';
import { downloadMerchantCsv } from '@/api/merchantExports';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime, formatMoney, paymentTypeText } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(true);
const exporting = ref(false);
const items = ref<Complaint[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const detailVisible = ref(false);
const detailLoading = ref(false);
const detail = ref<ComplaintDetail | null>(null);

const STATUS_MAP: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  pending: { text: '待处理', type: 'warning' },
  processing: { text: '处理中', type: 'info' },
  resolved: { text: '已处理', type: 'success' },
  approved: { text: '已通过', type: 'success' },
  rejected: { text: '已驳回', type: 'error' },
  closed: { text: '已关闭', type: 'default' },
  overdue: { text: '已超时', type: 'error' },
};

function statusMeta(status?: string | null) {
  return STATUS_MAP[(status ?? '').toLowerCase()] ?? { text: status || '-', type: 'default' as const };
}

function channelText(channel?: string | null): string {
  if (!channel) return '-';
  const mapped = paymentTypeText(channel);
  return mapped || channel;
}

function deadlineMeta(row: Complaint | ComplaintDetail) {
  const raw = row.deadline_at;
  if (!raw) return { text: '-', overdue: false };
  const parsed = dayjs(raw);
  const overdue =
    parsed.isValid() &&
    parsed.isBefore(dayjs()) &&
    !['resolved', 'rejected', 'closed'].includes((row.status ?? '').toLowerCase());
  return { text: formatDateTime(raw), overdue };
}

function senderText(msg: ComplaintMessage): string {
  const role = (msg.role ?? msg.sender ?? '').toLowerCase();
  if (role === 'merchant') return '商户';
  if (role === 'admin' || role === 'administrator' || role === 'operator') return '平台运营';
  if (role === 'system') return '系统';
  return msg.sender || msg.role || '消息';
}

const rowKey = (row: Complaint) => row.id;

const columns: DataTableColumns<Complaint> = [
  {
    title: '渠道',
    key: 'channel',
    width: 110,
    render: (row) => channelText(row.channel),
  },
  {
    title: '投诉单号',
    key: 'complaint_no',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.complaint_no || row.id || '-'),
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => {
      const meta = statusMeta(row.status);
      return h(NTag, { size: 'small', type: meta.type, bordered: false }, () => meta.text);
    },
  },
  {
    title: '原因',
    key: 'reason',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => row.reason || '-',
  },
  {
    title: '金额',
    key: 'amount_cents',
    width: 130,
    align: 'right',
    render: (row) =>
      row.amount_cents === null || row.amount_cents === undefined
        ? '-'
        : h(NText, { class: 'amount' }, () => formatMoney(row.amount_cents)),
  },
  {
    title: '截止时间',
    key: 'deadline_at',
    width: 170,
    render: (row) => {
      const meta = deadlineMeta(row);
      if (meta.text === '-') return '-';
      return h(NText, { type: meta.overdue ? 'error' : 'default', strong: meta.overdue }, () => meta.text);
    },
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
    width: 90,
    fixed: 'right',
    render: (row) =>
      h(NButton, { text: true, type: 'primary', size: 'small', onClick: () => void openDetail(row) }, () => '详情'),
  },
];

const pagination = computed(() => ({
  page: page.value,
  pageSize: pageSize.value,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

async function load() {
  loading.value = true;
  try {
    const res = await complaintApi.list({ page: page.value, page_size: pageSize.value });
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function openDetail(row: Complaint) {
  detailVisible.value = true;
  detailLoading.value = true;
  detail.value = null;
  try {
    detail.value = await complaintApi.detail(row.id);
  } catch (err) {
    message.error(errorMessage(err));
    detailVisible.value = false;
  } finally {
    detailLoading.value = false;
  }
}

async function handleExport() {
  if (exporting.value) return;
  exporting.value = true;
  try {
    const result = await downloadMerchantCsv('complaints');
    if (result.truncated) {
      message.warning(`导出达到行数上限，仅包含前 ${result.rows} 行：${result.filename}`);
    } else {
      message.success(`已导出 ${result.filename}`);
    }
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    exporting.value = false;
  }
}

function handlePageChange(value: number) {
  page.value = value;
  void load();
}

function handlePageSizeChange(value: number) {
  pageSize.value = value;
  page.value = 1;
  void load();
}

onMounted(() => {
  void load();
});
</script>

<template>
  <admin-page title="我的投诉" subtitle="查看渠道投诉的处理进度与平台回复（只读）">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" :loading="exporting" @click="handleExport">
          <template #icon>
            <n-icon :component="DownloadOutline" />
          </template>
          导出 CSV
        </n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      投诉由平台运营与渠道处理中，商户端不提供回复入口；如需补充材料请联系平台运营。
    </n-alert>

    <n-card :bordered="false">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1200"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无投诉记录" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="detailVisible" :width="isNarrow ? '92vw' : 560" placement="right">
      <n-drawer-content title="投诉详情" closable>
        <n-spin :show="detailLoading">
          <template v-if="detail">
            <n-descriptions :column="isNarrow ? 1 : 2" bordered size="small" label-placement="left" :label-width="96">
              <n-descriptions-item label="投诉单号" :span="2">
                <n-text class="mono" style="word-break: break-all">{{ detail.complaint_no || detail.id }}</n-text>
              </n-descriptions-item>
              <n-descriptions-item label="渠道">{{ channelText(detail.channel) }}</n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag size="small" :type="statusMeta(detail.status).type" :bordered="false">
                  {{ statusMeta(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="涉及金额">
                <n-text v-if="detail.amount_cents !== null && detail.amount_cents !== undefined" class="amount">
                  {{ formatMoney(detail.amount_cents) }}
                </n-text>
                <span v-else>-</span>
              </n-descriptions-item>
              <n-descriptions-item label="截止时间">
                <n-text :type="deadlineMeta(detail).overdue ? 'error' : 'default'">
                  {{ deadlineMeta(detail).text }}
                </n-text>
              </n-descriptions-item>
              <n-descriptions-item label="投诉原因" :span="2">{{ detail.reason || '-' }}</n-descriptions-item>
              <n-descriptions-item label="创建时间" :span="2">
                {{ formatDateTime(detail.created_at) }}
              </n-descriptions-item>
            </n-descriptions>

            <n-card :bordered="false" title="沟通记录" class="gp-section-gap">
              <n-timeline v-if="detail.messages && detail.messages.length">
                <n-timeline-item
                  v-for="(msg, index) in detail.messages"
                  :key="msg.id ?? index"
                  :title="senderText(msg)"
                  :content="msg.content || ''"
                  :time="formatDateTime(msg.created_at)"
                />
              </n-timeline>
              <n-empty v-else description="暂无沟通记录" size="small" />
            </n-card>

            <n-card :bordered="false" title="回复投诉" class="gp-section-gap">
              <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
                <n-form-item label="回复内容">
                  <n-input
                    type="textarea"
                    :autosize="{ minRows: 3, maxRows: 6 }"
                    placeholder="投诉回复由平台运营在后台提交，商户端仅查看"
                    disabled
                  />
                </n-form-item>
                <n-form-item>
                  <n-tooltip trigger="hover">
                    <template #trigger>
                      <n-button type="primary" disabled>提交回复</n-button>
                    </template>
                    商户端未开放投诉回复接口，请通过平台运营补充材料
                  </n-tooltip>
                </n-form-item>
              </n-form>
            </n-card>
          </template>
          <n-empty v-else-if="!detailLoading" description="暂无可展示的投诉内容" size="small" />
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>
