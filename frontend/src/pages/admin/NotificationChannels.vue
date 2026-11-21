<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import {
  NButton,
  NInput,
  NPopconfirm,
  NSpace,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from 'naive-ui';
import { AddOutline, PaperPlaneOutline, RefreshOutline } from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { notificationChannelsApi } from '@/api/notificationChannels';
import type { NotificationChannel } from '@/api/notificationChannels';
import type { PageQuery } from '@/api/types';
import { errorMessage } from '@/api/client';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useNarrow, useServerPaging } from './shared';

/** 后端只接受 sms / mail 两类通道；微信的凭据在「公众号配置」页维护 */
const kindTabs = [
  { name: 'sms', label: '短信' },
  { name: 'mail', label: '邮件' },
  { name: 'wechat', label: '微信' },
];

const kindLabels: Record<string, string> = { sms: '短信', mail: '邮件' };

const sampleKeys: Record<string, string[]> = {
  sms: ['access_key', 'secret', 'sign_name', 'template_code'],
  mail: ['host', 'port', 'username', 'password', 'from'],
};

interface ChannelCard {
  id: string;
  kind: string;
  provider: string;
  status: string;
  keys: string[];
  existing: string[];
  values: Record<string, string>;
  newKey: string;
  saving: boolean;
  updatedAt: string;
}

const message = useMessage();
const isNarrow = useNarrow();

const activeTab = ref('sms');
const cards = ref<ChannelCard[]>([]);
const loading = ref(false);
const removingId = ref('');

const showCreate = ref(false);
const creating = ref(false);
const createForm = reactive<{
  kind: string;
  provider: string;
  status: string;
  rows: { key: string; value: string }[];
}>({
  kind: 'sms',
  provider: '',
  status: 'active',
  rows: [{ key: '', value: '' }],
});

const showTest = ref(false);
const testTarget = ref<ChannelCard | null>(null);
const testValue = ref('');
const testing = ref(false);

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } = useServerPaging(load);

const isEditableTab = computed(() => activeTab.value === 'sms' || activeTab.value === 'mail');

function isSensitive(key: string): boolean {
  return /(secret|password|token|pass|key)/i.test(key);
}

function toCard(row: NotificationChannel): ChannelCard {
  const keys = (row.config_keys ?? []).filter(
    (key): key is string => typeof key === 'string' && key !== '',
  );
  const values: Record<string, string> = {};
  keys.forEach((key) => {
    values[key] = '';
  });
  return {
    id: row.id,
    kind: row.kind || 'sms',
    provider: row.provider || '',
    status: row.status || 'active',
    keys,
    existing: [...keys],
    values,
    newKey: '',
    saving: false,
    updatedAt: row.updated_at || '',
  };
}

async function load() {
  if (!isEditableTab.value) {
    cards.value = [];
    total.value = 0;
    return;
  }
  loading.value = true;
  try {
    const query: PageQuery & { kind: string } = {
      page: page.value,
      page_size: pageSize.value,
      kind: activeTab.value,
    };
    const data = await notificationChannelsApi.list(query);
    const items = itemsOf<NotificationChannel>(data);
    cards.value = items.map(toCard);
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function handleTab(value: string) {
  activeTab.value = value;
  resetPage();
  void load();
}

function buildConfig(card: ChannelCard): Record<string, string> {
  const config: Record<string, string> = {};
  card.keys.forEach((key) => {
    const name = key.trim();
    if (name === '') return;
    const value = (card.values[key] ?? '').trim();
    if (value === '') return;
    config[name] = value;
  });
  return config;
}

async function saveCard(card: ChannelCard) {
  if (card.provider.trim() === '') {
    message.warning('请输入服务商名称');
    return;
  }
  const config = buildConfig(card);
  card.saving = true;
  try {
    const updated = await notificationChannelsApi.update(card.id, {
      kind: card.kind,
      provider: card.provider.trim(),
      status: card.status,
      config,
    });
    // 保存成功的键变成「已配置」，输入框清空（值不回显，留空即保持原值）
    card.existing = Array.from(new Set([...card.existing, ...Object.keys(config)]));
    card.keys = Array.from(new Set([...card.keys, ...card.existing]));
    const blank: Record<string, string> = {};
    card.keys.forEach((key) => {
      blank[key] = '';
    });
    card.values = blank;
    card.newKey = '';
    card.updatedAt = updated?.updated_at || card.updatedAt;
    message.success('通知通道已更新');
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    card.saving = false;
  }
}

function addCardKey(card: ChannelCard) {
  const key = card.newKey.trim();
  if (key === '') {
    message.warning('请输入配置键');
    return;
  }
  if (card.keys.includes(key)) {
    message.warning('该配置键已存在');
    return;
  }
  card.keys = [...card.keys, key];
  card.values = { ...card.values, [key]: '' };
  card.newKey = '';
}

function fillCardSample(card: ChannelCard) {
  const missing = (sampleKeys[card.kind] ?? []).filter((key) => !card.keys.includes(key));
  if (missing.length === 0) {
    message.info('示例键已经都在列表里了');
    return;
  }
  card.keys = [...card.keys, ...missing];
  const next = { ...card.values };
  missing.forEach((key) => {
    next[key] = '';
  });
  card.values = next;
  message.success(`已插入 ${missing.length} 个示例键`);
}

async function removeCard(card: ChannelCard) {
  removingId.value = card.id;
  try {
    await notificationChannelsApi.remove(card.id);
    message.success('通知通道已删除');
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    removingId.value = '';
  }
}

function openCreate() {
  createForm.kind = activeTab.value === 'mail' ? 'mail' : 'sms';
  createForm.provider = '';
  createForm.status = 'active';
  createForm.rows = (sampleKeys[createForm.kind] ?? []).map((key) => ({ key, value: '' }));
  if (createForm.rows.length === 0) createForm.rows = [{ key: '', value: '' }];
  showCreate.value = true;
}

function addCreateRow() {
  createForm.rows = [...createForm.rows, { key: '', value: '' }];
}

function removeCreateRow(index: number) {
  createForm.rows = createForm.rows.filter((_, i) => i !== index);
}

function fillCreateSample() {
  const existing = new Set(createForm.rows.map((row) => row.key.trim()));
  const missing = (sampleKeys[createForm.kind] ?? []).filter((key) => !existing.has(key));
  if (missing.length === 0) {
    message.info('示例键已经都在列表里了');
    return;
  }
  createForm.rows = [...createForm.rows, ...missing.map((key) => ({ key, value: '' }))];
}

async function submitCreate() {
  if (createForm.provider.trim() === '') {
    message.warning('请输入服务商名称');
    return;
  }
  const keys = createForm.rows.map((row) => row.key.trim()).filter((key) => key !== '');
  if (keys.length === 0) {
    message.warning('至少配置一个配置项');
    return;
  }
  const config: Record<string, string> = {};
  createForm.rows.forEach((row) => {
    const key = row.key.trim();
    if (key === '') return;
    // 新建时把空值也提交，让后端拿到完整键集合
    config[key] = row.value.trim();
  });
  creating.value = true;
  try {
    await notificationChannelsApi.create({
      kind: createForm.kind,
      provider: createForm.provider.trim(),
      status: createForm.status,
      config,
    });
    message.success('通知通道已创建');
    showCreate.value = false;
    resetPage();
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    creating.value = false;
  }
}

function openTest(card: ChannelCard) {
  testTarget.value = card;
  testValue.value = '';
  showTest.value = true;
}

async function runTest() {
  const target = testTarget.value;
  if (!target) return;
  const value = testValue.value.trim();
  if (value === '') {
    message.warning('请输入测试接收地址');
    return;
  }
  testing.value = true;
  try {
    const result = await notificationChannelsApi.test(target.id, value);
    if (result?.sent === false) {
      message.error(result.message || '测试消息发送失败');
    } else {
      message.success('测试消息已发送');
    }
    showTest.value = false;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    testing.value = false;
  }
}

onMounted(load);
</script>

<template>
  <admin-page title="通知通道" subtitle="短信与邮件的服务商配置，配置键值由后端加密保存">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" :disabled="!isEditableTab" @click="openCreate">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          新建通道
        </n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      配置值加密存储、后端只回键名：留空表示不修改，填新值才会覆盖。没有接真实短信/邮件网关时，测试消息只会打到服务端日志，不会真的发出去。
    </n-alert>

    <n-card size="small" :bordered="false">
      <n-tabs :value="activeTab" type="line" animated @update:value="handleTab">
        <n-tab-pane v-for="tab in kindTabs" :key="tab.name" :name="tab.name" :tab="tab.label">
          <n-alert v-if="tab.name === 'wechat'" type="info" :bordered="false">
            微信通知不走通知通道：公众号凭据请在「公众号配置」页维护，后端通知通道目前只支持短信（sms）与邮件（mail）两类。
          </n-alert>
          <n-empty
            v-if="tab.name === 'wechat'"
            description="微信通知请到「公众号配置」页维护"
            style="margin: 32px 0"
          />

          <n-spin v-else :show="loading">
            <n-space vertical size="large">
              <n-empty v-if="cards.length === 0" description="暂无通知通道，点击右上角新建" />

              <n-card v-for="card in cards" :key="card.id" size="small" :bordered="true">
                <template #header>
                  <n-space align="center" :wrap="true">
                    <n-text strong>{{ card.provider || '(未填写服务商)' }}</n-text>
                    <n-tag
                      size="small"
                      :bordered="false"
                      :type="card.kind === 'sms' ? 'info' : 'success'"
                    >
                      {{ kindLabels[card.kind] ?? card.kind }}
                    </n-tag>
                    <n-text depth="3" style="font-size: 12px">
                      更新于 {{ formatDateTime(card.updatedAt) }}
                    </n-text>
                  </n-space>
                </template>
                <template #header-extra>
                  <n-space align="center" :size="8">
                    <n-switch v-model:value="card.status" checked-value="active" unchecked-value="disabled">
                      <template #checked>启用</template>
                      <template #unchecked>禁用</template>
                    </n-switch>
                  </n-space>
                </template>

                <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 200" :show-feedback="false">
                  <n-form-item label="服务商">
                    <n-input v-model:value="card.provider" placeholder="例如 aliyun / tencent / smtp" />
                  </n-form-item>

                  <n-form-item v-for="key in card.keys" :key="key" :label="key">
                    <n-space align="center" :size="8" :wrap="true" style="width: 100%">
                      <n-input
                        v-model:value="card.values[key]"
                        :type="isSensitive(key) ? 'password' : 'text'"
                        :show-password-on="isSensitive(key) ? 'click' : undefined"
                        :placeholder="
                          card.existing.includes(key)
                            ? '留空表示保持原值'
                            : '新增配置项，填值后保存'
                        "
                        style="flex: 1 1 200px; min-width: 0"
                      />
                      <n-tag
                        size="small"
                        :bordered="false"
                        :type="card.existing.includes(key) ? 'success' : 'warning'"
                      >
                        {{ card.existing.includes(key) ? '已配置' : '未配置' }}
                      </n-tag>
                    </n-space>
                  </n-form-item>

                  <n-form-item label="新增配置项">
                    <n-space align="center" :wrap="true" style="width: 100%">
                      <n-input
                        v-model:value="card.newKey"
                        placeholder="额外配置键，例如 template_code"
                        style="flex: 1 1 180px; min-width: 0"
                        @keyup.enter="addCardKey(card)"
                      />
                      <n-button size="small" @click="addCardKey(card)">添加键</n-button>
                    </n-space>
                  </n-form-item>
                </n-form>

                <template #footer>
                  <n-space justify="space-between" align="center" :wrap="true">
                    <n-button size="small" dashed @click="fillCardSample(card)">插入示例键</n-button>
                    <n-space :wrap="true">
                      <n-button size="small" @click="openTest(card)">
                        <template #icon>
                          <n-icon :component="PaperPlaneOutline" />
                        </template>
                        测试
                      </n-button>
                      <n-popconfirm
                        positive-text="确定"
                        negative-text="取消"
                        @positive-click="removeCard(card)"
                      >
                        <template #trigger>
                          <n-button size="small" type="error" :loading="removingId === card.id">
                            删除
                          </n-button>
                        </template>
                        确认删除该通知通道？
                      </n-popconfirm>
                      <n-button size="small" type="primary" :loading="card.saving" @click="saveCard(card)">
                        保存
                      </n-button>
                    </n-space>
                  </n-space>
                </template>
              </n-card>
            </n-space>
          </n-spin>
        </n-tab-pane>
      </n-tabs>

      <n-pagination
        v-if="isEditableTab"
        :page="page"
        :page-size="pageSize"
        :item-count="total"
        show-size-picker
        :page-sizes="[10, 20, 50]"
        style="margin-top: 16px; justify-content: flex-end"
        @update:page="onPage"
        @update:page-size="onPageSize"
      />
    </n-card>

    <n-modal v-model:show="showCreate" preset="card" title="新建通知通道" style="width: 640px; max-width: 94vw">
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120">
        <n-form-item label="类型">
          <n-radio-group v-model:value="createForm.kind">
            <n-space>
              <n-radio value="sms">短信（sms）</n-radio>
              <n-radio value="mail">邮件（mail）</n-radio>
            </n-space>
          </n-radio-group>
        </n-form-item>
        <n-form-item label="服务商">
          <n-input v-model:value="createForm.provider" placeholder="例如 aliyun / tencent / smtp" />
        </n-form-item>
        <n-form-item label="状态">
          <n-switch v-model:value="createForm.status" checked-value="active" unchecked-value="disabled">
            <template #checked>启用</template>
            <template #unchecked>禁用</template>
          </n-switch>
        </n-form-item>
        <n-form-item label="配置项">
          <n-space vertical style="width: 100%">
            <n-text depth="3" style="font-size: 12px">
              {{
                createForm.kind === 'sms'
                  ? '短信常见键：access_key、secret、sign_name、template_code'
                  : '邮件常见键：host、port、username、password、from'
              }}
            </n-text>
            <n-space
              v-for="(row, index) in createForm.rows"
              :key="index"
              align="center"
              :wrap="true"
              style="width: 100%"
            >
              <n-input v-model:value="row.key" placeholder="配置键" style="flex: 1 1 140px; min-width: 0" />
              <n-input
                v-model:value="row.value"
                :type="isSensitive(row.key) ? 'password' : 'text'"
                :show-password-on="isSensitive(row.key) ? 'click' : undefined"
                placeholder="配置值"
                style="flex: 1 1 160px; min-width: 0"
              />
              <n-button text type="error" @click="removeCreateRow(index)">删除</n-button>
            </n-space>
            <n-space>
              <n-button size="small" dashed @click="addCreateRow">添加配置项</n-button>
              <n-button size="small" text type="primary" @click="fillCreateSample">插入示例键</n-button>
            </n-space>
          </n-space>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="showCreate = false">取消</n-button>
          <n-button type="primary" :loading="creating" @click="submitCreate">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showTest" preset="card" title="发送测试消息" style="width: 640px; max-width: 94vw">
      <n-descriptions v-if="testTarget" :column="1" label-placement="left" size="small" bordered>
        <n-descriptions-item label="类型">
          {{ kindLabels[testTarget.kind] ?? testTarget.kind }}
        </n-descriptions-item>
        <n-descriptions-item label="服务商">{{ testTarget.provider || '-' }}</n-descriptions-item>
      </n-descriptions>
      <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120" style="margin-top: 16px">
        <n-form-item label="接收目标">
          <n-input
            v-model:value="testValue"
            :placeholder="
              testTarget?.kind === 'mail' ? '收件邮箱，例如 ops@example.com' : '手机号，例如 13800000000'
            "
          />
        </n-form-item>
      </n-form>
      <n-alert type="info" :bordered="false">
        未配置真实网关时，这条测试消息只会写入服务端日志。
      </n-alert>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="showTest = false">取消</n-button>
          <n-button type="primary" :loading="testing" @click="runTest">
            <template #icon>
              <n-icon :component="PaperPlaneOutline" />
            </template>
            发送测试
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
