<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { NButton, NIcon, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  LayersOutline,
  OpenOutline,
  RefreshOutline,
  ReloadOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import type { PluginConfigField, PluginMeta } from '@/api';
import { errorMessage } from '@/api/client';
import { httpStatus, itemsOf, useNarrow } from './shared';

const message = useMessage();
const router = useRouter();
const isNarrow = useNarrow();

const plugins = ref<PluginMeta[]>([]);
const loading = ref(false);
const unavailable = ref(false);

const showDetail = ref(false);
const detail = ref<PluginMeta | null>(null);

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

function capabilityLabel(value: string): string {
  return capabilityLabels[value] ?? value;
}

function supportsCallback(plugin: PluginMeta): boolean {
  return (plugin.capabilities ?? []).includes('callback');
}

function fieldCount(plugin: PluginMeta): number {
  return plugin.config_fields?.length ?? 0;
}

const totalFields = computed(() => plugins.value.reduce((acc, plugin) => acc + fieldCount(plugin), 0));

const callbackCount = computed(() => plugins.value.filter((plugin) => supportsCallback(plugin)).length);

const detailTitle = computed(() =>
  detail.value ? `${detail.value.name || detail.value.code} 的配置字段` : '插件配置字段',
);

function openDetail(plugin: PluginMeta) {
  detail.value = plugin;
  showDetail.value = true;
}

/** 插件清单：插件名 / 能力 / 回调 / 字段数，操作列固定右侧 */
const columns = computed<DataTableColumns<PluginMeta>>(() => [
  {
    title: '插件',
    key: 'name',
    minWidth: 220,
    render: (row) =>
      h(NSpace, { vertical: true, size: 0 }, {
        default: () => [
          h(NText, { strong: true }, { default: () => row.name || row.code }),
          h(NText, { depth: 3, code: true }, { default: () => row.code }),
        ],
      }),
  },
  {
    title: '支持能力',
    key: 'capabilities',
    minWidth: 320,
    render: (row) => {
      const capabilities = row.capabilities ?? [];
      if (capabilities.length === 0) {
        return h(NText, { depth: 3 }, { default: () => '未声明能力' });
      }
      return h(NSpace, { size: 4 }, {
        default: () =>
          capabilities.map((capability) =>
            h(
              NTag,
              { size: 'small', type: 'info', round: true, bordered: false },
              { default: () => capabilityLabel(capability) },
            ),
          ),
      });
    },
  },
  {
    title: '是否支持回调',
    key: 'callback',
    width: 140,
    render: (row) =>
      h(
        NTag,
        {
          size: 'small',
          round: true,
          bordered: false,
          type: supportsCallback(row) ? 'success' : 'default',
        },
        { default: () => (supportsCallback(row) ? '支持' : '不支持') },
      ),
  },
  {
    title: '配置字段数',
    key: 'config_fields',
    width: 130,
    align: 'right',
    render: (row) =>
      h(NText, { depth: fieldCount(row) > 0 ? 1 : 3 }, { default: () => `${fieldCount(row)} 项` }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 130,
    fixed: 'right',
    render: (row) =>
      h(
        NButton,
        { size: 'tiny', quaternary: true, type: 'info', onClick: () => openDetail(row) },
        {
          icon: () => h(NIcon, null, { default: () => h(LayersOutline) }),
          default: () => '查看配置',
        },
      ),
  },
]);

/** 抽屉里的配置字段明细表 */
const configFieldColumns: DataTableColumns<PluginConfigField> = [
  {
    title: '字段名',
    key: 'key',
    minWidth: 160,
    render: (row) => h(NText, { code: true }, { default: () => row.key }),
  },
  {
    title: '名称',
    key: 'label',
    minWidth: 140,
    render: (row) => row.label || row.key,
  },
  {
    title: '类型',
    key: 'type',
    width: 100,
    render: (row) => typeLabels[row.type] ?? row.type ?? '-',
  },
  {
    title: '必填',
    key: 'required',
    width: 90,
    render: (row) =>
      h(
        NTag,
        { size: 'small', round: true, bordered: false, type: row.required ? 'warning' : 'default' },
        { default: () => (row.required ? '必填' : '可选') },
      ),
  },
  {
    title: '是否密钥',
    key: 'secret',
    width: 110,
    render: (row) =>
      h(
        NTag,
        { size: 'small', round: true, bordered: false, type: row.secret ? 'error' : 'default' },
        { default: () => (row.secret ? '是' : '否') },
      ),
  },
];

onMounted(load);
</script>

<template>
  <admin-page title="插件与渠道" subtitle="支付插件注册表元数据；具体渠道请在「支付渠道」页创建与配置">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          刷新
        </n-button>
        <n-button type="primary" @click="router.push('/admin/channels')">
          <template #icon><n-icon><OpenOutline /></n-icon></template>
          去支付渠道页
        </n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" :show-icon="true" style="margin-bottom: 16px">
      插件只提供元数据与配置字段定义；渠道（上游商户号、密钥、优先级、权重）在
      <n-button text type="primary" @click="router.push('/admin/channels')">支付渠道</n-button>
      页面创建与维护。密钥类配置字段（列表里「是否密钥」为「是」）以加密形式存储，接口不回显明文。
    </n-alert>

    <n-card size="small" :bordered="false">
      <template #header>
        <n-space align="center" :size="10" :wrap="true">
          <n-text depth="3">
            已注册插件 {{ plugins.length }} 个，配置字段总计 {{ totalFields }} 项
          </n-text>
          <n-tag size="small" round :bordered="false" type="success">
            支持回调 {{ callbackCount }} 个
          </n-tag>
        </n-space>
      </template>

      <n-data-table
        :columns="columns"
        :data="plugins"
        :loading="loading"
        :row-key="(row: PluginMeta) => row.code"
        :bordered="false"
        :single-line="false"
        :scroll-x="1080"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty
            :description="unavailable ? '插件接口未启用' : '暂无已注册插件'"
            size="small"
          >
            <template v-if="unavailable" #extra>
              <n-button size="small" :loading="loading" @click="load">
                <template #icon><n-icon><ReloadOutline /></n-icon></template>
                重新检测
              </n-button>
            </template>
          </n-empty>
        </template>
      </n-data-table>
    </n-card>

    <n-drawer v-model:show="showDetail" :width="isNarrow ? '92vw' : 720" placement="right">
      <n-drawer-content :title="detailTitle" closable>
        <n-empty v-if="!detail" description="暂无数据" size="small" />
        <n-space v-else vertical :size="16">
          <n-descriptions :column="isNarrow ? 1 : 2" label-placement="left" bordered size="small">
            <n-descriptions-item label="插件代码">
              <n-text code>{{ detail.code }}</n-text>
            </n-descriptions-item>
            <n-descriptions-item label="插件名称">{{ detail.name || '-' }}</n-descriptions-item>
            <n-descriptions-item label="支持能力">
              <n-space :size="4">
                <n-tag
                  v-for="capability in detail.capabilities ?? []"
                  :key="capability"
                  size="small"
                  type="info"
                  round
                  :bordered="false"
                >
                  {{ capabilityLabel(capability) }}
                </n-tag>
                <n-text v-if="(detail.capabilities ?? []).length === 0" depth="3">未声明能力</n-text>
              </n-space>
            </n-descriptions-item>
            <n-descriptions-item label="支持回调">
              <n-tag
                size="small"
                round
                :bordered="false"
                :type="supportsCallback(detail) ? 'success' : 'default'"
              >
                {{ supportsCallback(detail) ? '支持' : '不支持' }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="配置字段数">{{ fieldCount(detail) }} 项</n-descriptions-item>
          </n-descriptions>

          <n-data-table
            :columns="configFieldColumns"
            :data="detail.config_fields ?? []"
            :row-key="(row: PluginConfigField) => row.key"
            :bordered="false"
            :single-line="false"
            :scroll-x="620"
            size="small"
          >
            <template #empty>
              <n-empty description="该插件无需配置" size="small" />
            </template>
          </n-data-table>
        </n-space>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>
