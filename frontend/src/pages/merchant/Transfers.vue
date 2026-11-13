<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import axios from 'axios';
import { useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { PageQuery, Transfer } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import MoneyText from '@/components/MoneyText.vue';
import { formatDateTime, yuanToCents } from '@/utils/format';

const message = useMessage();

const loading = ref(false);
const items = ref<Transfer[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const notEnabled = ref(false);

const modalVisible = ref(false);
const submitting = ref(false);
const generatedNo = ref('');
const form = reactive({
  amountYuan: '',
  account_name: '',
  account_no: '',
  bank_name: '',
  branch_name: '',
});

const rowKey = (row: Transfer) => row.id;

const columns: DataTableColumns<Transfer> = [
  {
    title: '代付单号',
    key: 'transfer_no',
    width: 190,
    render: (row) => h('span', { class: 'mono-cell' }, row.transfer_no),
  },
  {
    title: '商户代付单号',
    key: 'out_transfer_no',
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell muted-cell' }, row.out_transfer_no),
  },
  {
    title: '代付金额',
    key: 'amount',
    width: 120,
    align: 'right',
    render: (row) => h(MoneyText, { amount: row.amount }),
  },
  { title: '收款人', key: 'account_name', width: 110, ellipsis: { tooltip: true } },
  {
    title: '收款账号',
    key: 'account_no',
    width: 170,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'mono-cell' }, row.account_no),
  },
  { title: '开户银行', key: 'bank_name', minWidth: 130, ellipsis: { tooltip: true } },
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

function generateOutTransferNo(): string {
  return `TF${dayjs().format('YYYYMMDDHHmmss')}${Math.floor(Math.random() * 900 + 100)}`;
}

async function load() {
  loading.value = true;
  try {
    const query: PageQuery = { page: page.value, page_size: pageSize.value };
    const res = await merchantApi.transfers(query);
    items.value = res.items;
    total.value = res.total;
    notEnabled.value = false;
  } catch (err) {
    if (axios.isAxiosError(err) && (err.response?.status === 404 || err.response?.status === 501)) {
      notEnabled.value = true;
      items.value = [];
      total.value = 0;
      return;
    }
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function openModal() {
  form.amountYuan = '';
  form.account_name = '';
  form.account_no = '';
  form.bank_name = '';
  form.branch_name = '';
  generatedNo.value = generateOutTransferNo();
  modalVisible.value = true;
}

async function submit() {
  const cents = yuanToCents(form.amountYuan);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的代付金额');
    return;
  }
  if (!form.account_name.trim()) {
    message.error('请填写收款人姓名');
    return;
  }
  if (!form.account_no.trim()) {
    message.error('请填写收款账号');
    return;
  }
  if (!form.bank_name.trim()) {
    message.error('请填写开户银行');
    return;
  }
  submitting.value = true;
  try {
    await merchantApi.createTransfer({
      out_transfer_no: generatedNo.value,
      amount: cents,
      account_name: form.account_name.trim(),
      account_no: form.account_no.trim(),
      bank_name: form.bank_name.trim(),
      branch_name: form.branch_name.trim() || undefined,
    });
    message.success('代付申请已提交，等待平台审核');
    modalVisible.value = false;
    page.value = 1;
    await load();
  } catch (err) {
    if (axios.isAxiosError(err) && (err.response?.status === 404 || err.response?.status === 501)) {
      notEnabled.value = true;
      modalVisible.value = false;
      return;
    }
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
});
</script>

<template>
  <div class="page">
    <PageHeader title="代付申请" subtitle="向指定银行卡发起代付，扣减可用余额后进入平台审核">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
        <n-button size="small" type="primary" :disabled="notEnabled" @click="openModal">申请代付</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="notEnabled" type="warning" :bordered="false" class="disabled-alert">
      代付接口未启用：当前平台未开放代付能力，或该接口尚未部署。请联系平台运营开通后再试。
    </n-alert>

    <n-card v-if="!notEnabled" :bordered="false" class="table-card">
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
      />
    </n-card>

    <n-modal v-model:show="modalVisible" preset="card" title="申请代付" style="max-width: 500px">
      <n-form label-placement="top">
        <n-form-item label="代付金额（元）">
          <n-input v-model:value="form.amountYuan" placeholder="0.00">
            <template #prefix>¥</template>
          </n-input>
        </n-form-item>
        <n-form-item label="收款人姓名">
          <n-input v-model:value="form.account_name" placeholder="请输入收款人姓名" />
        </n-form-item>
        <n-form-item label="收款账号">
          <n-input v-model:value="form.account_no" placeholder="请输入银行卡号或收款账号" />
        </n-form-item>
        <n-form-item label="开户银行">
          <n-input v-model:value="form.bank_name" placeholder="例如：中国工商银行" />
        </n-form-item>
        <n-form-item label="开户支行（可选）">
          <n-input v-model:value="form.branch_name" placeholder="例如：上海分行营业部" />
        </n-form-item>
        <n-form-item label="商户代付单号（自动生成）">
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
.disabled-alert {
  margin-bottom: 16px;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
