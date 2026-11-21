<script setup lang="ts">
import { h, onMounted, ref } from 'vue';
import axios from 'axios';
import { NButton, NPopconfirm, NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { AddOutline, GlobeOutline, RefreshOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { MerchantDomain } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { financeStatus, formatDateTime } from '@/utils/format';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

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
    minWidth: 220,
    render: (row) => h(NText, { class: 'mono' }, () => row.domain || '-'),
  },
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
    minWidth: 170,
    ellipsis: { tooltip: true },
    render: (row) => row.rejection_reason || '-',
  },
  {
    title: '操作',
    key: 'actions',
    width: 110,
    fixed: 'right',
    render: (row) => {
      if (row.status === 'rejected') {
        return h(NText, { depth: 3 }, () => '不可删除');
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
  <admin-page title="支付域名" subtitle="添加下单使用的 notify_url / return_url 域名，平台审核通过后生效">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" :disabled="notEnabled" @click="openModal">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          添加域名
        </n-button>
      </n-space>
    </template>

    <n-alert v-if="notEnabled" type="warning" :bordered="false" style="margin-bottom: 16px">
      支付域名接口未启用：当前平台未开放域名白名单功能，可稍后再试或联系平台运营。
    </n-alert>

    <n-alert v-else type="info" :bordered="false" style="margin-bottom: 16px">
      平台开启域名白名单后，只有审核通过的域名（或匹配的通配符域名）才能作为 notify_url / return_url 下单。
      支持填写 <n-text class="mono" code>example.com</n-text> 或 <n-text class="mono" code>*.example.com</n-text>。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-data-table
        :columns="columns"
        :data="items"
        :loading="loading"
        :bordered="false"
        :row-key="rowKey"
        :pagination="false"
        :scroll-x="940"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="还没有添加域名" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="modalVisible"
      preset="card"
      title="添加支付域名"
      style="width: 620px; max-width: 94vw"
    >
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
        <n-form-item label="域名">
          <n-input
            v-model:value="domainInput"
            placeholder="example.com 或 *.example.com"
            :maxlength="191"
            class="mono"
            @keyup.enter="handleAdd"
          >
            <template #prefix>
              <n-icon :component="GlobeOutline" />
            </template>
          </n-input>
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        通配符域名只匹配一级子域。提交后需要平台运营审核，审核通过前下单会被拦截。
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="modalVisible = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="handleAdd">提交审核</n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
