<script setup lang="ts">
import { h, onMounted, ref } from 'vue';
import axios from 'axios';
import { NButton, NPopconfirm, useMessage, type DataTableColumns } from 'naive-ui';
import { merchantApi } from '@/api';
import type { MerchantDomain } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';

const message = useMessage();

const loading = ref(true);
const notEnabled = ref(false);
const items = ref<MerchantDomain[]>([]);

const modalVisible = ref(false);
const submitting = ref(false);
const domainInput = ref('');

const DOMAIN_PATTERN = /^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i;

const rowKey = (row: MerchantDomain) => row.id;

async function load() {
  loading.value = true;
  try {
    items.value = await merchantApi.domains();
    notEnabled.value = false;
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      notEnabled.value = true;
      items.value = [];
      return;
    }
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

function openModal() {
  domainInput.value = '';
  modalVisible.value = true;
}

async function handleAdd() {
  const value = domainInput.value.trim().toLowerCase();
  if (!value) {
    message.error('请输入域名');
    return;
  }
  if (!DOMAIN_PATTERN.test(value)) {
    message.error('域名格式不正确，例如 example.com 或 *.example.com');
    return;
  }
  submitting.value = true;
  try {
    await merchantApi.addDomain(value);
    message.success('域名已提交，等待平台审核');
    modalVisible.value = false;
    await load();
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 409) {
      message.error('该域名已存在，无需重复添加');
    } else if (axios.isAxiosError(err) && err.response?.status === 404) {
      message.error('域名接口未启用，请联系平台运营');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(row: MerchantDomain) {
  try {
    await merchantApi.deleteDomain(row.id);
    message.success('域名已删除');
    await load();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

const columns: DataTableColumns<MerchantDomain> = [
  {
    title: '域名',
    key: 'domain',
    minWidth: 200,
    render: (row) => h('span', { class: 'mono-cell' }, row.domain),
  },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) => h(StatusTag, { status: row.status, kind: 'finance' }),
  },
  {
    title: '提交时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '审核时间',
    key: 'reviewed_at',
    width: 170,
    render: (row) => formatDateTime(row.reviewed_at),
  },
  {
    title: '驳回原因',
    key: 'rejection_reason',
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) => row.rejection_reason || '-',
  },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    fixed: 'right',
    render: (row) => {
      if (row.status === 'rejected') {
        return h('span', { class: 'muted-text' }, '不可删除');
      }
      return h(
        NPopconfirm,
        { onPositiveClick: () => void handleDelete(row), positiveText: '删除', negativeText: '取消' },
        {
          trigger: () => h(NButton, { text: true, type: 'error', size: 'small' }, () => '删除'),
          default: () => '删除后该域名立即失效（审核中的域名也可删除），确认删除？',
        },
      );
    },
  },
];

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="page">
    <PageHeader title="支付域名" subtitle="添加下单使用的 notify_url / return_url 域名，平台审核通过后生效">
      <template #actions>
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
        <n-button size="small" type="primary" :disabled="notEnabled" @click="openModal">添加域名</n-button>
      </template>
    </PageHeader>

    <n-alert v-if="notEnabled" type="warning" :bordered="false" class="tips-alert">
      支付域名接口未启用：当前平台未开放域名白名单功能，可稍后再试或联系平台运营。
    </n-alert>

    <n-alert v-else type="info" :bordered="false" class="tips-alert">
      平台开启域名白名单后，只有审核通过的域名（或匹配的通配符域名）才能作为 notify_url / return_url 下单。
      支持填写 <span class="mono-cell">example.com</span> 或 <span class="mono-cell">*.example.com</span>。
    </n-alert>

    <n-card :bordered="false" class="table-card">
      <n-data-table
        :columns="columns"
        :data="items"
        :loading="loading"
        :bordered="false"
        :row-key="rowKey"
        :pagination="false"
        :scroll-x="900"
        size="small"
      >
        <template #empty>
          <n-empty description="还没有添加域名" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="modalVisible" preset="card" title="添加支付域名" style="max-width: 460px">
      <n-form label-placement="top">
        <n-form-item label="域名">
          <n-input
            v-model:value="domainInput"
            placeholder="example.com 或 *.example.com"
            :maxlength="191"
            @keyup.enter="handleAdd"
          />
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        通配符域名只匹配一级子域。提交后需要平台运营审核，审核通过前下单会被拦截。
      </n-alert>
      <template #footer>
        <div class="modal-footer">
          <n-button @click="modalVisible = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="handleAdd">提交审核</n-button>
        </div>
      </template>
    </n-modal>
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
:deep(.muted-text) {
  color: #94a3b8;
  font-size: 12.5px;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
