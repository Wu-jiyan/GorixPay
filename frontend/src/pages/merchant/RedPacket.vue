<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import axios from 'axios';
import { NButton, NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { GiftOutline, RefreshOutline } from '@vicons/ionicons5';
import { financeApi, type RedPacketBatch } from '@/api/merchantFinance';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { financeStatus, formatDateTime, formatMoney, yuanToCents } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const creating = ref(false);
const form = reactive({
  totalYuan: null as number | null,
  count: 10 as number | null,
  remark: '',
});

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
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.batch_no || row.transfer_no || '-'),
  },
  {
    title: '金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  {
    title: '个数',
    key: 'count',
    width: 90,
    align: 'right',
    render: (row) => String(row.count ?? 0),
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
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: financeStatus(row.status).type }, () =>
        financeStatus(row.status).text,
      ),
  },
  {
    title: '备注',
    key: 'remark',
    minWidth: 150,
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
    width: 140,
    fixed: 'right',
    render: (row) => {
      if (row.status !== 'pending') return h(NText, { depth: 3 }, () => '-');
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
  const cents = yuanToCents(form.totalYuan ?? 0);
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
    form.totalYuan = null;
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
  <admin-page title="红包" subtitle="创建拼手气红包，金额随机分配到每个红包，合计等于总额">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-card size="small" :bordered="false" title="创建红包">
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-grid cols="1 m:2" responsive="screen" item-responsive :x-gap="16">
          <n-gi>
            <n-form-item label="红包总额（元）">
              <n-input-number
                v-model:value="form.totalYuan"
                :precision="2"
                :min="0.01"
                placeholder="例如 100.00"
                style="width: 100%"
              >
                <template #prefix>¥</template>
              </n-input-number>
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="红包个数">
              <n-input-number v-model:value="form.count" :min="1" :max="1000" :precision="0" style="width: 100%" />
            </n-form-item>
          </n-gi>
          <n-gi span="1 m:2">
            <n-form-item label="备注（可选）">
              <n-input v-model:value="form.remark" placeholder="例如 十一月活动奖励" :maxlength="64" />
            </n-form-item>
          </n-gi>
        </n-grid>
        <n-form-item>
          <n-space align="center" :wrap="true">
            <n-button type="primary" :loading="creating" @click="handleCreate">
              <template #icon>
                <n-icon :component="GiftOutline" />
              </template>
              创建红包
            </n-button>
            <n-text depth="3" class="gp-hint">
              每个红包最少 1 分，金额由平台随机拆分；创建后从可用余额扣减并生成对应的代付单。
            </n-text>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>

    <n-alert v-if="cancelUnavailable" type="info" :bordered="false" style="margin-top: 16px">
      当前平台未开放商户自助撤销红包批次，如需整批退回请联系平台运营。
    </n-alert>

    <n-card size="small" :bordered="false" title="我的红包批次" style="margin-top: 16px">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1040"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="还没有发出过红包" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="cancelVisible" preset="card" title="撤销红包批次" style="width: 620px; max-width: 94vw">
      <n-space vertical :size="16">
        <n-alert type="warning" :bordered="false">
          将撤销该批次里所有待审核的红包，冻结金额会退回可用余额；已经审核或领取的红包不会被撤销。
        </n-alert>
        <n-descriptions :column="1" :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120" size="small" bordered>
          <n-descriptions-item label="批次号">
            <n-text class="mono" style="word-break: break-all">{{ cancelTarget?.batch_no || '-' }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="金额 / 个数">
            {{ cancelTarget ? `${formatMoney(cancelTarget.amount)} / ${cancelTarget.count} 个` : '-' }}
          </n-descriptions-item>
        </n-descriptions>
        <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
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
      </n-space>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="cancelVisible = false">取消</n-button>
          <n-button type="error" :loading="cancelling" @click="submitCancel">确认撤销</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
