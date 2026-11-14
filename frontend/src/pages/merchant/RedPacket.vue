<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import axios from 'axios';
import { NButton, NTag, useMessage, type DataTableColumns, type InputInst } from 'naive-ui';
import { financeApi, type RedPacketBatch } from '@/api/merchantFinance';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime, formatMoney, yuanToCents } from '@/utils/format';

const message = useMessage();

const creating = ref(false);
const form = reactive({
  totalYuan: '',
  count: 10,
  remark: '',
});
const totalInputRef = ref<InputInst | null>(null);

const loading = ref(true);
const items = ref<RedPacketBatch[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
/** 后端没有商户自助撤销入口时置为 true，按钮换成提示 */
const cancelUnavailable = ref(false);

const rowKey = (row: RedPacketBatch) => row.batch_no;

function claimMeta(row: RedPacketBatch) {
  const count = row.count ?? 0;
  const claimed = row.claimed_count ?? 0;
  if (count > 0 && claimed >= count) {
    return { text: `已领完 ${claimed}/${count}`, type: 'success' as const };
  }
  if (claimed > 0) {
    return { text: `领取中 ${claimed}/${count}`, type: 'warning' as const };
  }
  return { text: `待领取 0/${count}`, type: 'default' as const };
}

// ---------- 撤销批次 ----------
const cancelVisible = ref(false);
const cancelTarget = ref<RedPacketBatch | null>(null);
const cancelReason = ref('');
const cancelling = ref(false);

function openCancel(row: RedPacketBatch) {
  cancelTarget.value = row;
  cancelReason.value = '';
  cancelVisible.value = true;
}

function cancelErrorMessage(err: unknown): string {
  if (!axios.isAxiosError(err)) return errorMessage(err);
  const status = err.response?.status;
  const data = err.response?.data;
  const payload =
    data && typeof data === 'object' && !(data instanceof Blob) ? (data as Record<string, unknown>) : null;
  if (status === 409) return '该批次有已审核的红包，不能整批撤销';
  if (status === 403) return '没有权限撤销该批次';
  if (status === 404) {
    // 有 JSON 错误体说明路由存在、只是批次找不到；纯 404 说明后端还没挂商户撤销入口
    if (payload && (typeof payload.message === 'string' || typeof payload.code === 'string')) {
      return '该红包批次不存在或已被清理';
    }
    cancelUnavailable.value = true;
    return '平台暂未开放商户自助撤销，请联系平台运营';
  }
  return errorMessage(err);
}

async function submitCancel() {
  if (cancelling.value) return;
  const target = cancelTarget.value;
  if (!target) return;
  cancelling.value = true;
  try {
    const result = await financeApi.cancelRedBatch(target.batch_no, cancelReason.value.trim());
    cancelVisible.value = false;
    message.success(`已撤销 ${result.cancelled} 个红包`);
    await load();
  } catch (err) {
    message.error(cancelErrorMessage(err));
    if (cancelUnavailable.value) cancelVisible.value = false;
  } finally {
    cancelling.value = false;
  }
}

const columns = computed<DataTableColumns<RedPacketBatch>>(() => [
  {
    title: '批次号',
    key: 'batch_no',
    minWidth: 190,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.batch_no || row.transfer_no || '-'),
  },
  {
    title: '金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  {
    title: '个数',
    key: 'count',
    width: 80,
    align: 'right',
    render: (row) => `${row.count ?? 0}`,
  },
  {
    title: '领取状态',
    key: 'claimed_count',
    width: 150,
    render: (row) => {
      const meta = claimMeta(row);
      return h(NTag, { size: 'small', type: meta.type, bordered: false }, () => meta.text);
    },
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '备注',
    key: 'remark',
    minWidth: 140,
    ellipsis: { tooltip: true },
    render: (row) => row.remark || '-',
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
    render: (row) => {
      if (row.status !== 'pending') return h('span', { class: 'muted-text' }, '-');
      if (cancelUnavailable.value) {
        return h(NTag, { size: 'small', type: 'default', bordered: false }, () => '联系运营撤销');
      }
      return h(
        NButton,
        { text: true, type: 'error', size: 'small', onClick: () => openCancel(row) },
        () => '撤销',
      );
    },
  },
]);

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
    const res = await financeApi.redBatches({ page: page.value, page_size: pageSize.value });
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function createErrorMessage(err: unknown): string {
  if (axios.isAxiosError(err) && err.response?.status === 409) {
    const data = err.response?.data;
    if (data && typeof data === 'object' && !(data instanceof Blob)) {
      const payload = data as Record<string, unknown>;
      if (payload.code === 'insufficient_balance') return '可用余额不足，请先充值再创建红包';
    }
  }
  return errorMessage(err);
}

async function handleCreate() {
  if (creating.value) return;
  const cents = yuanToCents(form.totalYuan);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的红包总额');
    return;
  }
  const count = Number(form.count);
  if (!Number.isInteger(count) || count < 1) {
    message.error('红包个数需为大于 0 的整数');
    return;
  }
  if (cents < count) {
    message.error('红包总额不能小于红包个数（每个至少 1 分）');
    return;
  }
  creating.value = true;
  try {
    const created = await financeApi.createRedPacket({
      amount: cents,
      count,
      remark: form.remark.trim() || undefined,
    });
    message.success(`红包已创建：${created.batch_no || created.transfer_no}，共 ${formatMoney(cents)}`);
    form.totalYuan = '';
    form.remark = '';
    page.value = 1;
    await load();
  } catch (err) {
    message.error(createErrorMessage(err));
  } finally {
    creating.value = false;
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
    <PageHeader title="红包" subtitle="创建拼手气红包，金额随机分配到每个红包，合计等于总额">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="create-card" title="创建红包">
      <div class="create-grid">
        <div class="field">
          <label class="field-label">红包总额（元）</label>
          <n-input
            ref="totalInputRef"
            v-model:value="form.totalYuan"
            placeholder="例如 100.00"
            @keyup.enter="handleCreate"
          >
            <template #prefix>¥</template>
          </n-input>
        </div>
        <div class="field">
          <label class="field-label">红包个数</label>
          <n-input-number v-model:value="form.count" :min="1" :max="1000" :precision="0" style="width: 100%" />
        </div>
        <div class="field field-wide">
          <label class="field-label">备注（可选）</label>
          <n-input v-model:value="form.remark" placeholder="例如 十一月活动奖励" :maxlength="64" />
        </div>
        <div class="field field-submit">
          <n-button type="primary" :loading="creating" @click="handleCreate">创建红包</n-button>
        </div>
      </div>
      <p class="create-tip">
        每个红包最少 1 分，金额由平台随机拆分；创建后从可用余额扣减并生成对应的代付单。
      </p>
    </n-card>

    <n-alert v-if="cancelUnavailable" type="info" :bordered="false" class="cancel-note">
      当前平台未开放商户自助撤销红包批次，如需整批退回请联系平台运营。
    </n-alert>

    <n-card :bordered="false" class="table-card" title="我的红包批次">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="980"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="还没有发出过红包" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="cancelVisible" preset="card" title="撤销红包批次" style="max-width: 480px">
      <n-alert type="warning" :bordered="false" class="cancel-alert">
        将撤销该批次里所有待审核的红包，冻结金额会退回可用余额；已经审核或领取的红包不会被撤销。
      </n-alert>
      <div class="cancel-meta">
        <div class="cancel-row">
          <span class="cancel-label">批次号</span>
          <span class="mono-cell cancel-value">{{ cancelTarget?.batch_no || '-' }}</span>
        </div>
        <div class="cancel-row">
          <span class="cancel-label">金额 / 个数</span>
          <span class="cancel-value">
            {{ cancelTarget ? `${formatMoney(cancelTarget.amount)} / ${cancelTarget.count} 个` : '-' }}
          </span>
        </div>
      </div>
      <n-form label-placement="top">
        <n-form-item label="撤销原因（可选）">
          <n-input
            v-model:value="cancelReason"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="例如 活动取消，整批退回"
            :maxlength="120"
          />
        </n-form-item>
      </n-form>
      <template #footer>
        <div class="modal-footer">
          <n-button @click="cancelVisible = false">取消</n-button>
          <n-button type="error" :loading="cancelling" @click="submitCancel">确认撤销</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.create-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.create-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.4fr) auto;
  gap: 16px;
  align-items: end;
}
.field {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.field-label {
  font-size: 12.5px;
  color: #64748b;
}
.field-submit {
  justify-content: flex-end;
}
.field-submit .n-button {
  width: 120px;
}
.create-tip {
  margin: 14px 0 0;
  font-size: 12px;
  color: #94a3b8;
  line-height: 1.7;
}
.cancel-note {
  margin-top: 16px;
}
.table-card {
  margin-top: 16px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
:deep(.muted-text) {
  color: #94a3b8;
  font-size: 12.5px;
}
.cancel-alert {
  margin-bottom: 16px;
}
.cancel-meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border-radius: 10px;
  background: #f8fafc;
  margin-bottom: 16px;
}
.cancel-row {
  display: flex;
  gap: 12px;
  font-size: 13px;
}
.cancel-label {
  color: #94a3b8;
  flex: none;
  width: 80px;
}
.cancel-value {
  color: #334155;
  word-break: break-all;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
@media (max-width: 1100px) {
  .create-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .field-submit {
    grid-column: span 2;
  }
}
@media (max-width: 768px) {
  .create-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .field-submit {
    grid-column: auto;
  }
  .field-submit .n-button {
    width: 100%;
  }
}
</style>
