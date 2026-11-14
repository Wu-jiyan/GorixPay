<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NTag, useMessage, type DataTableColumns } from 'naive-ui';
import { complaintApi, type Complaint, type ComplaintDetail, type ComplaintMessage } from '@/api/merchantComplaints';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime, paymentTypeText } from '@/utils/format';
import dayjs from 'dayjs';

const message = useMessage();

const loading = ref(true);
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
  const overdue = parsed.isValid() && parsed.isBefore(dayjs()) && !['resolved', 'rejected', 'closed'].includes(
    (row.status ?? '').toLowerCase(),
  );
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
    render: (row) => h('span', { class: 'mono-cell' }, row.complaint_no || row.id),
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
        : h(MoneyText, { amount: row.amount_cents }),
  },
  {
    title: '截止时间',
    key: 'deadline_at',
    width: 170,
    render: (row) => {
      const meta = deadlineMeta(row);
      if (meta.text === '-') return '-';
      return h('span', { class: meta.overdue ? 'deadline overdue' : 'deadline' }, meta.text);
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
  <div class="page">
    <PageHeader title="我的投诉" subtitle="查看渠道投诉的处理进度与平台回复（只读）">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-alert type="info" :bordered="false" class="tips-alert">
      投诉由平台运营与渠道处理中，商户端不提供回复入口；如需补充材料请联系平台运营。
    </n-alert>

    <n-card :bordered="false" class="table-card">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1180"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无投诉记录" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="detailVisible" :width="520" placement="right">
      <n-drawer-content title="投诉详情" closable>
        <n-spin :show="detailLoading">
          <template v-if="detail">
            <n-descriptions :column="1" label-placement="left" bordered size="small">
              <n-descriptions-item label="投诉单号">
                <span class="mono-cell">{{ detail.complaint_no || detail.id }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="渠道">{{ channelText(detail.channel) }}</n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag size="small" :type="statusMeta(detail.status).type" :bordered="false">
                  {{ statusMeta(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="投诉原因">{{ detail.reason || '-' }}</n-descriptions-item>
              <n-descriptions-item label="涉及金额">
                <MoneyText v-if="detail.amount_cents !== null && detail.amount_cents !== undefined" :amount="detail.amount_cents" />
                <span v-else>-</span>
              </n-descriptions-item>
              <n-descriptions-item label="截止时间">
                <span :class="deadlineMeta(detail).overdue ? 'overdue' : ''">{{ deadlineMeta(detail).text }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
            </n-descriptions>

            <div class="messages-head">沟通记录</div>
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
          </template>
          <n-empty v-else-if="!detailLoading" description="暂无可展示的投诉内容" size="small" />
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>

<style scoped>
.tips-alert {
  margin-bottom: 16px;
}
.table-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
:deep(.deadline.overdue) {
  color: #dc2626;
  font-weight: 600;
}
.messages-head {
  margin: 20px 0 12px;
  font-size: 13px;
  font-weight: 600;
  color: #1e293b;
}
</style>
