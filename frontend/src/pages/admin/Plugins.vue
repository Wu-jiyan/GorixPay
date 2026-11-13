<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { NTag, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { adminApi } from '@/api';
import type { PluginConfigField, PluginMeta } from '@/api';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { httpStatus, itemsOf } from './shared';

const message = useMessage();
const router = useRouter();

const plugins = ref<PluginMeta[]>([]);
const loading = ref(false);
const unavailable = ref(false);

const typeLabels: Record<string, string> = {
  string: '字符串',
  number: '数字',
  boolean: '布尔',
  text: '长文本',
};

const capabilityLabels: Record<string, string> = {
  qrcode: '扫码',
  jump: '跳转',
  jsapi: 'JSAPI',
  h5: 'H5',
  native: 'Native',
  app: 'APP',
  callback: '回调验签',
  refund: '退款',
  query: '查询',
  close: '关单',
  scan: '被扫',
};

async function load() {
  loading.value = true;
  try {
    plugins.value = itemsOf<PluginMeta>(await adminApi.plugins());
    unavailable.value = false;
  } catch (error) {
    if (httpStatus(error) === 404) {
      plugins.value = [];
      unavailable.value = true;
      return;
    }
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

const totalFields = computed(() =>
  plugins.value.reduce((acc, plugin) => acc + (plugin.config_fields?.length ?? 0), 0),
);

const configFieldColumns: DataTableColumns<PluginConfigField> = [
  {
    title: '配置键',
    key: 'key',
    width: 150,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.key),
  },
  { title: '名称', key: 'label', minWidth: 120, ellipsis: true, render: (row) => row.label || row.key },
  {
    title: '类型',
    key: 'type',
    width: 90,
    render: (row) => typeLabels[row.type] ?? row.type ?? '-',
  },
  {
    title: '必填',
    key: 'required',
    width: 70,
    render: (row) =>
      h(NTag, { size: 'tiny', round: true, type: row.required ? 'warning' : 'default' }, {
        default: () => (row.required ? '必填' : '可选'),
      }),
  },
  {
    title: '密文',
    key: 'secret',
    width: 70,
    render: (row) =>
      h(NTag, { size: 'tiny', round: true, type: row.secret ? 'error' : 'default' }, {
        default: () => (row.secret ? '是' : '否'),
      }),
  },
];

function capabilityLabel(value: string): string {
  return capabilityLabels[value] ?? value;
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="插件与渠道" subtitle="支付插件注册表元数据；具体渠道请在「支付渠道」页创建与配置">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
        <n-button type="primary" @click="router.push('/admin/channels')">去支付渠道页</n-button>
      </template>
    </PageHeader>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      插件只提供元数据与配置字段定义；渠道（上游商户号、密钥、优先级、权重）在
      <n-button text type="primary" @click="router.push('/admin/channels')">支付渠道</n-button>
      页面创建与维护。
    </n-alert>

    <n-spin :show="loading">
      <n-empty v-if="unavailable" description="插件接口未启用" size="large">
        <template #extra>
          <n-button size="small" :loading="loading" @click="load">重新检测</n-button>
          <n-button size="small" type="primary" @click="router.push('/admin/channels')">去支付渠道页</n-button>
        </template>
      </n-empty>
      <n-empty v-else-if="plugins.length === 0 && !loading" description="暂无已注册插件" size="large" />
      <template v-else>
        <div class="summary muted">
          已注册插件 {{ plugins.length }} 个，配置字段总计 {{ totalFields }} 项
        </div>
        <n-grid cols="1 s:1 m:2 l:2" responsive="screen" :x-gap="16" :y-gap="16">
          <n-gi v-for="plugin in plugins" :key="plugin.code">
            <n-card :bordered="false" class="plugin-card">
              <template #header>
                <div class="plugin-head">
                  <span class="plugin-name">{{ plugin.name || plugin.code }}</span>
                  <n-tag size="small" round type="success" :bordered="false">{{ plugin.code }}</n-tag>
                </div>
              </template>
              <div class="block">
                <div class="block-label">支持能力</div>
                <div class="cap-list">
                  <n-tag
                    v-for="cap in plugin.capabilities ?? []"
                    :key="cap"
                    size="small"
                    round
                    type="info"
                    :bordered="false"
                  >
                    {{ capabilityLabel(cap) }}
                  </n-tag>
                  <span v-if="(plugin.capabilities ?? []).length === 0" class="muted">未声明能力</span>
                </div>
              </div>
              <div class="block">
                <div class="block-label">配置字段</div>
                <n-data-table
                  :columns="configFieldColumns"
                  :data="plugin.config_fields ?? []"
                  :bordered="false"
                  :single-line="false"
                  size="small"
                  :row-key="(row: PluginConfigField) => row.key"
                >
                  <template #empty>
                    <n-empty description="该插件无需配置" size="small" />
                  </template>
                </n-data-table>
              </div>
            </n-card>
          </n-gi>
        </n-grid>
      </template>
    </n-spin>
  </div>
</template>

<style scoped>
.summary {
  font-size: 12px;
  margin-bottom: 12px;
}
.plugin-card {
  height: 100%;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.plugin-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.plugin-name {
  font-weight: 600;
  font-size: 15px;
}
.block {
  margin-bottom: 14px;
}
.block:last-child {
  margin-bottom: 0;
}
.block-label {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 8px;
}
.cap-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
</style>
