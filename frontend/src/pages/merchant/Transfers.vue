<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import axios from 'axios';
import { NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { PaperPlaneOutline, RefreshOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { PageQuery, Transfer } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { financeStatus, formatDateTime, formatMoney, yuanToCents } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(false);
const items = ref<Transfer[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const notEnabled = ref(false);

const submitting = ref(false);
const generatedNo = ref('');
const form = reactive({
  amountYuan: null as number | null,
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
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.transfer_no || '-'),
  },
  {
    title: '商户代付单号',
    key: 'out_transfer_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono', depth: 3 }, () => row.out_transfer_no || '-'),
  },
  {
    title: '代付金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  { title: '收款人', key: 'account_name', width: 110, ellipsis: { tooltip: true } },
  {
    title: '收款账号',
    key: 'account_no',
    width: 180,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.account_no || '-'),
  },
  { title: '开户银行', key: 'bank_name', minWidth: 140, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: financeStatus(row.status).type }, () =>
        financeStatus(row.status).text,
      ),
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

function resetForm() {
  form.amountYuan = null;
  form.account_name = '';
  form.account_no = '';
  form.bank_name = '';
  form.branch_name = '';
  generatedNo.value = generateOutTransferNo();
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

async function submit() {
  const cents = yuanToCents(form.amountYuan ?? 0);
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
    resetForm();
    page.value = 1;
    await load();
  } catch (err) {
    if (axios.isAxiosError(err) && (err.response?.status === 404 || err.response?.status === 501)) {
      notEnabled.value = true;
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
  resetForm();
  void load();
});
</script>

<template>
  <admin-page title="代付申请" subtitle="向指定银行卡发起代付，扣减可用余额后进入平台审核">
    <template #actions>
      <n-button :loading="loading" @click="load">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-alert v-if="notEnabled" type="warning" :bordered="false" style="margin-bottom: 16px">
      代付接口未启用：当前平台未开放代付能力，或该接口尚未部署。请联系平台运营开通后再试。
    </n-alert>

    <n-card v-if="!notEnabled" :bordered="false" title="申请代付">
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160" @submit.prevent="submit">
        <n-grid cols="1 m:2" responsive="screen" :x-gap="16">
          <n-gi>
            <n-form-item label="代付金额（元）">
              <n-input-number
                v-model:value="form.amountYuan"
                :precision="2"
                :min="0.01"
                placeholder="0.00"
                style="width: 100%"
              >
                <template #prefix>¥</template>
              </n-input-number>
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="收款人姓名">
              <n-input v-model:value="form.account_name" placeholder="请输入收款人姓名" :maxlength="64" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="收款账号">
              <n-input v-model:value="form.account_no" placeholder="请输入银行卡号或收款账号" :maxlength="64" class="mono" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="开户银行">
              <n-input v-model:value="form.bank_name" placeholder="例如：中国工商银行" :maxlength="64" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="开户支行（可选）">
              <n-input v-model:value="form.branch_name" placeholder="例如：上海分行营业部" :maxlength="64" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="商户代付单号">
              <n-input :value="generatedNo" readonly class="mono" />
            </n-form-item>
          </n-gi>
        </n-grid>
        <n-form-item>
          <n-space :wrap="true">
            <n-button type="primary" :loading="submitting" @click="submit">
              <template #icon>
                <n-icon :component="PaperPlaneOutline" />
              </template>
              提交申请
            </n-button>
            <n-button @click="resetForm">重置表单</n-button>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card v-if="!notEnabled" :bordered="false" title="代付记录" class="gp-section-gap">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1240"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无代付记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </admin-page>
</template>
