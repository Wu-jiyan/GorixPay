<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import axios from 'axios';
import AdminPage from '@/components/AdminPage.vue';
import { adminSettingsApi } from '@/api/adminSettings';
import { errorMessage } from '@/api/client';
import { refreshSite } from '@/stores/site';
import { formatDateTime } from '@/utils/format';

type FieldType = 'text' | 'textarea' | 'number' | 'bool' | 'select' | 'secret' | 'cents';

interface Field {
  key: string;
  label: string;
  type: FieldType;
  hint?: string;
  min?: number;
  max?: number;
  options?: Array<{ label: string; value: string }>;
}

interface Tab {
  name: string;
  label: string;
  intro: string;
  fields: Field[];
}

const tabs: Tab[] = [
  {
    name: 'site',
    label: '站点',
    intro: '站点名称、Logo、页脚与皮肤。改完立刻作用到官网与后台侧栏。',
    fields: [
      { key: 'site.name', label: '站点名称', type: 'text', hint: '显示在浏览器标题与后台侧栏' },
      { key: 'site.logo_url', label: 'Logo 地址', type: 'text', hint: '填图片 URL；留空则用站点名首字方块' },
      { key: 'site.footer_text', label: '页脚文案', type: 'textarea', hint: '官网底部一行说明，可留空' },
      {
        key: 'site.skin',
        label: '皮肤',
        type: 'select',
        hint: 'dark 为深色主题，compact 收紧纵向留白',
        options: [
          { label: '默认', value: 'default' },
          { label: '深色', value: 'dark' },
          { label: '紧凑', value: 'compact' },
        ],
      },
    ],
  },
  {
    name: 'business',
    label: '业务',
    intro: '下单有效期、域名白名单、实名、返佣与通知重试。',
    fields: [
      { key: 'payment.order_expiry_minutes', label: '订单有效期', type: 'number', hint: '单位分钟，超时后收银台不可支付', min: 1, max: 1440 },
      { key: 'payment.domain_whitelist_required', label: '强制 notify_url 域名白名单', type: 'bool', hint: '开启后商户必须先报备域名' },
      { key: 'certification.required', label: '强制实名认证', type: 'bool', hint: '未通过实名的商户不能下单' },
      { key: 'certification.fee_cents', label: '实名认证费', type: 'cents', hint: '从商户余额扣，0 表示免费', min: 0 },
      { key: 'invite.enabled', label: '开启邀请返佣', type: 'bool' },
      { key: 'invite.rate_bp', label: '返佣比例', type: 'number', hint: '万分比：10000 = 100%，250 = 2.5%', min: 0, max: 10000 },
      { key: 'notification.max_attempts', label: '通知最大重试次数', type: 'number', min: 1, max: 50 },
      { key: 'notification.poll_interval_seconds', label: '通知轮询间隔', type: 'number', hint: '单位秒', min: 1, max: 600 },
    ],
  },
  {
    name: 'risk',
    label: '风控与保证金',
    intro: '按金额、单量、IP 三个维度限额，以及店铺保证金与登录验证码。',
    fields: [
      { key: 'risk.daily_amount_limit_cents', label: '单商户单日交易额上限', type: 'cents', hint: '0 表示不限制', min: 0 },
      { key: 'risk.daily_order_limit', label: '单商户单日订单数上限', type: 'number', hint: '0 表示不限制', min: 0 },
      { key: 'risk.ip_daily_limit', label: '单 IP 单日下单上限', type: 'number', hint: '0 表示不限制', min: 0 },
      { key: 'merchant.deposit_required_cents', label: '店铺保证金要求', type: 'cents', hint: '未达标的商户不能发起销售单', min: 0 },
      { key: 'security.captcha_required', label: '登录需要图形验证码', type: 'bool', hint: '后台与商户登录都会要求验证码' },
    ],
  },
  {
    name: 'notify',
    label: '通知通道',
    intro: '找回密码与验证码的发送通道。没配通道时生产环境只记录「消息被抑制」，不会把令牌打进日志。',
    fields: [
      { key: 'notify.smtp_host', label: 'SMTP 主机', type: 'text' },
      { key: 'notify.smtp_port', label: 'SMTP 端口', type: 'number', hint: '常用 465 / 587 / 25', min: 1, max: 65535 },
      { key: 'notify.smtp_username', label: 'SMTP 用户名', type: 'text' },
      { key: 'notify.smtp_password', label: 'SMTP 密码', type: 'secret', hint: '加密存储，页面不再回显' },
      { key: 'notify.smtp_from', label: '发件人地址', type: 'text' },
      { key: 'notify.sms_endpoint', label: '短信网关地址', type: 'text', hint: 'POST JSON：{phone, message}；留空表示不启用短信' },
      { key: 'notify.sms_token', label: '短信网关 Token', type: 'secret', hint: '以 Bearer 头发送，加密存储' },
      { key: 'wechat.mp.template_id', label: '公众号模板消息 ID', type: 'text', hint: '需认证服务号；留空则不发送模板消息' },
    ],
  },
  {
    name: 'oauth',
    label: '第三方登录',
    intro: '微信 / QQ / 抖音开放平台的应用凭据。AppSecret 加密存储，列表不回显。',
    fields: [
      { key: 'oauth.wechat.app_id', label: '微信 AppID', type: 'text' },
      { key: 'oauth.wechat.app_secret', label: '微信 AppSecret', type: 'secret' },
      { key: 'oauth.qq.app_id', label: 'QQ AppID', type: 'text' },
      { key: 'oauth.qq.app_secret', label: 'QQ AppKey', type: 'secret' },
      { key: 'oauth.douyin.app_id', label: '抖音 ClientKey', type: 'text' },
      { key: 'oauth.douyin.app_secret', label: '抖音 ClientSecret', type: 'secret' },
    ],
  },
];

const message = useMessage();
const loading = ref(false);
const savingTab = ref('');
const activeTab = ref(tabs[0].name);
const loadedAt = ref('');
// 非凭据项的实际值；凭据项只在这里放「新输入」，服务端永远不会把明文给我们
const values = ref<Record<string, string | number | boolean>>({});
const original = ref<Record<string, string | number | boolean>>({});
const secretInput = ref<Record<string, string>>({});
const secretConfigured = ref<Record<string, boolean>>({});
const secretMeta = ref<Record<string, string>>({});

function isSecret(field: Field) {
  return field.type === 'secret';
}

function normalized(value: unknown): string | number | boolean {
  if (typeof value === 'number' || typeof value === 'boolean') return value;
  if (typeof value === 'string') return value;
  if (value === null || value === undefined) return '';
  return String(value);
}

async function reload() {
  loading.value = true;
  try {
    const items = await adminSettingsApi.list();
    const nextValues: Record<string, string | number | boolean> = {};
    const nextConfigured: Record<string, boolean> = {};
    const nextSecretMeta: Record<string, string> = {};
    for (const item of items) {
      if (item.secret) {
        nextConfigured[item.key] = Boolean(item.configured);
        if (item.updated_at) nextSecretMeta[item.key] = item.updated_at;
        continue;
      }
      nextValues[item.key] = normalized(item.value);
    }
    values.value = nextValues;
    original.value = { ...nextValues };
    secretConfigured.value = nextConfigured;
    secretMeta.value = nextSecretMeta;
    secretInput.value = {};
    loadedAt.value = formatDateTime(new Date().toISOString());
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

const dirtyKeys = computed(() => {
  const keys: string[] = [];
  for (const tab of tabs) {
    for (const field of tab.fields) {
      if (isSecret(field)) {
        if ((secretInput.value[field.key] ?? '').trim() !== '') keys.push(field.key);
        continue;
      }
      if (values.value[field.key] !== original.value[field.key]) keys.push(field.key);
    }
  }
  return keys;
});

function tabDirtyKeys(tab: Tab): string[] {
  const keys: string[] = [];
  for (const field of tab.fields) {
    if (isSecret(field)) {
      if ((secretInput.value[field.key] ?? '').trim() !== '') keys.push(field.key);
      continue;
    }
    if (values.value[field.key] !== original.value[field.key]) keys.push(field.key);
  }
  return keys;
}

/** 分 → 元，展示用；保存时再乘回 100，避免浮点误差落库。 */
function centsToYuan(value: unknown): number | null {
  const cents = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(cents)) return null;
  return Math.round(cents) / 100;
}

function yuanToCents(value: number | null): number {
  if (value === null || !Number.isFinite(value)) return 0;
  return Math.round(value * 100);
}

async function saveTab(tab: Tab) {
  const keys = tabDirtyKeys(tab);
  if (keys.length === 0) {
    message.info('本组没有改动');
    return;
  }
  savingTab.value = tab.name;
  let saved = 0;
  try {
    for (const field of tab.fields) {
      if (!keys.includes(field.key)) continue;
      let payload: unknown;
      if (isSecret(field)) {
        payload = (secretInput.value[field.key] ?? '').trim();
      } else if (field.type === 'cents') {
        payload = yuanToCents(values.value[field.key] as number | null);
      } else {
        payload = values.value[field.key];
      }
      await adminSettingsApi.update(field.key, payload);
      saved += 1;
    }
    await reload();
    if (keys.some((key) => key.startsWith('site.'))) {
      await refreshSite();
    }
    message.success(`已保存 ${saved} 项`);
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 409) {
      message.error('未配置主密钥（GORIXPAY_MASTER_KEY_FILE），无法保存密钥类设置');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    savingTab.value = '';
  }
}

function resetTab(tab: Tab) {
  for (const field of tab.fields) {
    if (isSecret(field)) {
      secretInput.value[field.key] = '';
      continue;
    }
    values.value[field.key] = original.value[field.key];
  }
}

onMounted(reload);
</script>

<template>
  <admin-page title="系统设置" subtitle="站点身份、业务规则、风控、通知与第三方登录">
    <template #actions>
      <n-space>
        <n-text depth="3" v-if="loadedAt" style="font-size: 12px">读取于 {{ loadedAt }}</n-text>
        <n-button :loading="loading" @click="reload">重新加载</n-button>
      </n-space>
    </template>

    <n-alert type="info" :bordered="false" style="margin-bottom: 16px">
      凭据类字段（AppSecret、SMTP 密码、短信 Token）加密存储，页面不再回显：留空表示不修改，填新值才会覆盖。
    </n-alert>

    <n-tabs v-model:value="activeTab" type="line" animated>
      <n-tab-pane v-for="tab in tabs" :key="tab.name" :name="tab.name" :tab="tab.label">
        <n-card size="small" :bordered="false">
          <template #header>
            <n-text depth="2">{{ tab.intro }}</n-text>
          </template>

          <n-spin :show="loading">
            <n-form label-placement="top" :show-feedback="true" require-mark-placement="right-hanging">
              <n-grid cols="1 m:2" responsive="screen" :x-gap="32" :y-gap="6">
                <n-gi v-for="field in tab.fields" :key="field.key">
                  <n-form-item :label="field.label" :feedback="field.hint">
                    <!-- 布尔 -->
                    <n-switch v-if="field.type === 'bool'" v-model:value="values[field.key] as boolean" />

                    <!-- 枚举 -->
                    <n-select
                      v-else-if="field.type === 'select'"
                      v-model:value="values[field.key] as string"
                      :options="field.options ?? []"
                    />

                    <!-- 金额（元） -->
                    <n-input-number
                      v-else-if="field.type === 'cents'"
                      :value="centsToYuan(values[field.key])"
                      :min="0"
                      :step="1"
                      :precision="2"
                      @update:value="(value: number | null) => (values[field.key] = yuanToCents(value))"
                    >
                      <template #suffix>元</template>
                    </n-input-number>

                    <!-- 整数 -->
                    <n-input-number
                      v-else-if="field.type === 'number'"
                      v-model:value="values[field.key] as number"
                      :min="field.min"
                      :max="field.max"
                      :show-button="false"
                      style="max-width: 220px"
                    />

                    <!-- 凭据 -->
                    <n-space v-else-if="field.type === 'secret'" align="center" :size="10" style="width: 100%">
                      <n-input
                        v-model:value="secretInput[field.key]"
                        type="password"
                        show-password-on="click"
                        clearable
                        style="flex: 1; min-width: 0"
                        :placeholder="secretConfigured[field.key] ? '已配置，留空表示不修改' : '尚未配置，粘贴新值'"
                      />
                      <n-tag :type="secretConfigured[field.key] ? 'success' : 'warning'" size="small" :bordered="false">
                        {{ secretConfigured[field.key] ? '已配置' : '未配置' }}
                      </n-tag>
                      <n-text v-if="secretMeta[field.key]" depth="3" style="font-size: 12px">
                        更新于 {{ formatDateTime(secretMeta[field.key]) }}
                      </n-text>
                    </n-space>

                    <!-- 多行文本 -->
                    <n-input
                      v-else-if="field.type === 'textarea'"
                      v-model:value="values[field.key] as string"
                      type="textarea"
                      :autosize="{ minRows: 2, maxRows: 4 }"
                    />

                    <!-- 单行文本 -->
                    <n-input v-else v-model:value="values[field.key] as string" clearable />
                  </n-form-item>
                </n-gi>
              </n-grid>
            </n-form>
          </n-spin>

          <template #footer>
            <n-space justify="space-between" align="center">
              <n-text depth="3" style="font-size: 12px">
                {{ tabDirtyKeys(tab).length ? `${tabDirtyKeys(tab).length} 项待保存` : '没有改动' }}
              </n-text>
              <n-space>
                <n-button :disabled="!tabDirtyKeys(tab).length" @click="resetTab(tab)">还原</n-button>
                <n-button
                  type="primary"
                  :disabled="!tabDirtyKeys(tab).length"
                  :loading="savingTab === tab.name"
                  @click="saveTab(tab)"
                >
                  保存本组
                </n-button>
              </n-space>
            </n-space>
          </template>
        </n-card>
      </n-tab-pane>
    </n-tabs>

    <n-text v-if="dirtyKeys.length" depth="3" style="font-size: 12px; display: block; margin-top: 12px">
      注意：切换标签不会丢弃改动，{{ dirtyKeys.length }} 项改动仍在等待保存。
    </n-text>
  </admin-page>
</template>
