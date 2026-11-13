<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import { useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { PageQuery, Settlement } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime, formatMoney, yuanToCents } from '@/utils/format';

const message = useMessage();

const loading = ref(false);
const items = ref<Settlement[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const availableBalance = ref(0);
const balanceLoaded = ref(false);
const modalVisible = ref(false);
const submitting = ref(false);
const form = reactive({ amountYuan: '', destination: '' });
const generatedNo = ref('');

const rowKey = (row: Settlement) => row.id;

const columns: DataTableColumns<Settlement> = [
  {
    title: '结算单号',
    key: 'settlement_no',
    width: 190,
    render: (row) => h('span', { class: 'mono-cell' }, row.settlement_no),
  },
  {
    title: '商户结算单号',
    key: 'out_settlement_no',
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell muted-cell' }, row.out_settlement_no),
  },
  {
    title: '结算金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  { title: '币种', key: 'currency', width: 80 },
  { title: '收款账户', key: 'destination', minWidth: 180, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '申请时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
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

function generateOutSettlementNo(): string {
  return `ST${dayjs().format('YYYYMMDDHHmmss')}${Math.floor(Math.random() * 900 + 100)}`;
}

async function load() {
  loading.value = true;
  try {
    const query: PageQuery = { page: page.value, page_size: pageSize.value };
    const res = await merchantApi.settlements(query);
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function loadBalance() {
  try {
    const stats = await merchantApi.dashboard();
    availableBalance.value = stats.available_balance;
    balanceLoaded.value = true;
  } catch {
    // 余额仅用于提示与前置校验，接口不可用时交给服务端校验
    balanceLoaded.value = false;
  }
}

async function openModal() {
  form.amountYuan = '';
  form.destination = '';
  generatedNo.value = generateOutSettlementNo();
  modalVisible.value = true;
  await loadBalance();
}

async function submit() {
  const cents = yuanToCents(form.amountYuan);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的结算金额');
    return;
  }
  if (balanceLoaded.value && cents > availableBalance.value) {
    message.error(`结算金额不能超过可用余额 ${formatMoney(availableBalance.value)}`);
    return;
  }
  if (!form.destination.trim()) {
    message.error('请填写收款账户信息');
    return;
  }
  submitting.value = true;
  try {
    await merchantApi.createSettlement({
      out_settlement_no: generatedNo.value,
      amount: cents,
      currency: 'CNY',
      destination: form.destination.trim(),
    });
    message.success('结算申请已提交，等待平台审核');
    modalVisible.value = false;
    page.value = 1;
    void load();
    void loadBalance();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    submitting.value = false;
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
  void loadBalance();
});
</script>

<template>
  <div class="page">
    <PageHeader title="结算记录" subtitle="提交结算申请并跟踪审核、出款进度">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
        <n-button size="small" type="primary" @click="openModal">申请结算</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" class="table-card">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1100"
        size="small"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      />
    </n-card>

    <n-modal v-model:show="modalVisible" preset="card" title="申请结算" style="max-width: 480px">
      <n-alert type="info" :bordered="false" class="settle-alert">
        <template v-if="balanceLoaded">
          可用余额 <strong>{{ formatMoney(availableBalance) }}</strong>。结算申请提交后进入平台审核，审核通过后按收款账户出款。
        </template>
        <template v-else>
          暂时无法获取可用余额，最终可结算金额以平台审核结果为准。
        </template>
      </n-alert>
      <n-form label-placement="top">
        <n-form-item label="结算金额（元）">
          <n-input v-model:value="form.amountYuan" placeholder="0.00">
            <template #prefix>¥</template>
          </n-input>
        </n-form-item>
        <n-form-item label="收款账户">
          <n-input
            v-model:value="form.destination"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="请输入收款账户信息，例如：招商银行 6225 **** 1234 张三"
          />
        </n-form-item>
        <n-form-item label="币种">
          <n-tag :bordered="false" type="default">CNY（固定）</n-tag>
        </n-form-item>
        <n-form-item label="商户结算单号（自动生成）">
          <n-input :value="generatedNo" readonly class="mono-input" />
        </n-form-item>
      </n-form>
      <template #footer>
        <div class="modal-footer">
          <n-button @click="modalVisible = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">提交申请</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.table-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
:deep(.mono-cell) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #334155;
}
:deep(.muted-cell) {
  color: #64748b;
}
:deep(.mono-input .n-input__input-el) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #64748b;
}
.settle-alert {
  margin-bottom: 16px;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
