<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NTag, useMessage, type DataTableColumns, type InputInst } from 'naive-ui';
import { financeApi, type RedPacket } from '@/api/merchantFinance';
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
const items = ref<RedPacket[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const rowKey = (row: RedPacket) => row.id;

function claimMeta(row: RedPacket) {
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

const columns: DataTableColumns<RedPacket> = [
  {
    title: '红包单号',
    key: 'out_no',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.out_no || row.transfer_no || row.id),
  },
  {
    title: '金额',
    key: 'total_amount',
    width: 130,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.total_amount ?? row.amount ?? 0 }),
  },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
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
    title: '创建时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 130,
    render: (row) =>
      row.status === 'pending'
        ? h(NTag, { size: 'small', type: 'warning', bordered: false }, () => '等待审核')
        : h('span', { class: 'muted-text' }, '-'),
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
    const res = await financeApi.redPackets({ page: page.value, page_size: pageSize.value });
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
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
    message.success(`红包已创建：${created.out_no || created.id}，共 ${formatMoney(cents)}`);
    form.totalYuan = '';
    form.remark = '';
    page.value = 1;
    await load();
  } catch (err) {
    message.error(errorMessage(err));
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

    <n-card :bordered="false" class="table-card" title="我发出的红包">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="760"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="还没有发出过红包" size="small" />
        </template>
      </n-data-table>
    </n-card>
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
