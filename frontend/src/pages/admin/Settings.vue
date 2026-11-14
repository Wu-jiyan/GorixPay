<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import axios from 'axios';
import { adminSettingsApi } from '@/api/adminSettings';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { formatDateTime, formatMoney } from '@/utils/format';

type SettingKind = 'string' | 'number' | 'boolean';
type SettingValue = string | number | boolean | null;
type SettingUnit = 'cents' | 'bp';

interface SettingMeta {
  label: string;
  hint: string;
  kind: SettingKind;
  /** 金额（分）或万分比，用于展示换算后的可读值 */
  unit?: SettingUnit;
  /** 用多行文本框渲染（页脚、说明类文本） */
  textarea?: boolean;
  /** 有选项时渲染成下拉框 */
  options?: { label: string; value: string }[];
}

interface SettingRow {
  key: string;
  label: string;
  hint: string;
  kind: SettingKind;
  unit?: SettingUnit;
  textarea?: boolean;
  options?: { label: string; value: string }[];
  /** 凭据类：后端不回显值，只能写入 */
  secret?: boolean;
  /** 凭据类当前是否已配置 */
  configured?: boolean;
  value: SettingValue;
  original: SettingValue;
  updated_at: string;
}

interface SettingGroup {
  title: string;
  rows: SettingRow[];
}

const meta: Record<string, SettingMeta> = {
  'site.name': {
    label: '站点名称',
    hint: '显示在官网、收银台、浏览器标题，以及运营后台与商户中心左上角',
    kind: 'string',
  },
  'site.logo_url': {
    label: '站点 Logo 地址',
    hint: '可填 https:// 开头的图片地址，留空用默认图标；后台左上角按 34×34 圆角渲染',
    kind: 'string',
  },
  'site.footer_text': {
    label: '页脚文本',
    hint: '显示在官网页脚，通常填备案号或公司名，留空不显示',
    kind: 'string',
    textarea: true,
  },
  'site.skin': {
    label: '界面皮肤',
    hint: '保存后刷新生效；dark 为深色，compact 为紧凑排版',
    kind: 'string',
    options: [
      { label: '默认（default）', value: 'default' },
      { label: '深色（dark）', value: 'dark' },
      { label: '紧凑（compact）', value: 'compact' },
    ],
  },
  'payment.order_expiry_minutes': {
    label: '订单过期时间（分钟）',
    hint: '订单创建后未支付则自动关闭',
    kind: 'number',
  },
  'payment.domain_whitelist_required': {
    label: '下单校验支付域名',
    hint: '开启后下单会校验 notify_url / return_url 的域名在商户白名单内，否则拒绝下单',
    kind: 'boolean',
  },
  'certification.required': {
    label: '强制实名认证',
    hint: '开启后未通过实名认证的商户无法下单',
    kind: 'boolean',
  },
  'certification.fee_cents': {
    label: '实名认证费用（分）',
    hint: '认证通过时从商户可用余额扣除，余额不足会被拒绝；0 表示免费',
    kind: 'number',
    unit: 'cents',
  },
  'invite.enabled': {
    label: '邀请返佣开关',
    hint: '开启后订单支付成功会按比例向邀请人返佣入账',
    kind: 'boolean',
  },
  'invite.rate_bp': {
    label: '返佣比例（万分比）',
    hint: '100 = 1%，1000 = 10%；按订单金额计算并向下取整，结果为 0 则跳过',
    kind: 'number',
    unit: 'bp',
  },
  'risk.daily_amount_limit_cents': {
    label: '商户单日成功金额上限（分）',
    hint: '按当日已支付订单金额累计，超过后拒绝下单；0 表示不限制',
    kind: 'number',
    unit: 'cents',
  },
  'risk.daily_order_limit': {
    label: '商户单日订单数上限',
    hint: '按当日已支付订单数累计，超过后拒绝下单；0 表示不限制',
    kind: 'number',
  },
  'risk.ip_daily_limit': {
    label: '单 IP 单日下单上限',
    hint: '同一 client_ip 当日下单次数上限；0 表示不限制',
    kind: 'number',
  },
  'merchant.deposit_required_cents': {
    label: '保证金下限（分）',
    hint: '保证金账户余额下限，用于展示与下单提示；0 表示不要求',
    kind: 'number',
    unit: 'cents',
  },
  'notification.max_attempts': {
    label: '通知最大重试次数',
    hint: '商户通知投递失败的重试上限，超过后进入死信',
    kind: 'number',
  },
  'notification.poll_interval_seconds': {
    label: '通知轮询间隔（秒）',
    hint: 'Outbox worker 扫描待发送消息的间隔',
    kind: 'number',
  },
};

const groupDefs: { title: string; prefix: string }[] = [
  { title: '站点信息', prefix: 'site.' },
  { title: '支付与域名', prefix: 'payment.' },
  { title: '实名认证', prefix: 'certification.' },
  { title: '邀请返佣', prefix: 'invite.' },
  { title: '风控限额', prefix: 'risk.' },
  { title: '保证金', prefix: 'merchant.' },
  { title: '通知队列', prefix: 'notification.' },
];

const message = useMessage();
const rows = ref<SettingRow[]>([]);
const loading = ref(false);
const saving = ref<Record<string, boolean>>({});

function inferKind(value: unknown): SettingKind {
  if (typeof value === 'boolean') return 'boolean';
  if (typeof value === 'number') return 'number';
  return 'string';
}

function normalizeValue(value: unknown, kind: SettingKind): SettingValue {
  if (kind === 'boolean') return value === true || value === 'true' || value === 1;
  if (kind === 'number') {
    const parsed = typeof value === 'number' ? value : Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  if (value === undefined || value === null) return '';
  return typeof value === 'string' ? value : String(value);
}

function groupOf(key: string): string {
  const found = groupDefs.find((group) => key.startsWith(group.prefix));
  return found ? found.title : '其他设置';
}

const groupedRows = computed<SettingGroup[]>(() => {
  const buckets = new Map<string, SettingRow[]>();
  rows.value.forEach((row) => {
    const title = groupOf(row.key);
    const list = buckets.get(title);
    if (list) list.push(row);
    else buckets.set(title, [row]);
  });
  const order = [...groupDefs.map((group) => group.title), '其他设置'];
  return order
    .filter((title, index) => order.indexOf(title) === index && buckets.has(title))
    .map((title) => ({ title, rows: buckets.get(title) ?? [] }));
});

/** 金额（分）/ 万分比的换算预览 */
function unitPreview(row: SettingRow): string {
  if (!row.unit) return '';
  const numeric = typeof row.value === 'number' ? row.value : Number(row.value);
  if (!Number.isFinite(numeric)) return '';
  if (row.unit === 'cents') return formatMoney(numeric);
  return `${(numeric / 100).toFixed(2)}%`;
}

async function load() {
  loading.value = true;
  try {
    const items = await adminSettingsApi.list();
    rows.value = items.map((item) => {
      const known = meta[item.key];
      const secret = item.secret === true || isCredentialKey(item.key);
      // 凭据类后端不回显值：表单初始一律留空，避免把占位当成明文
      const kind: SettingKind = secret ? 'string' : (known?.kind ?? inferKind(item.value));
      const value = secret ? '' : normalizeValue(item.value, kind);
      return {
        key: item.key,
        label: known?.label ?? item.key,
        hint: known?.hint ?? (secret ? '密钥类设置后端不回显，输入新值才会覆盖' : ''),
        kind,
        unit: known?.unit,
        textarea: known?.textarea,
        options: known?.options,
        secret,
        configured: item.configured === true,
        value,
        original: value,
        updated_at: item.updated_at ?? '',
      };
    });
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

/** 老版本里 secret 标记可能缺失时，按键名兜底识别凭据类设置 */
function isCredentialKey(key: string): boolean {
  return /(^|\.)app_secret$/.test(key) || /password$/.test(key) || /_token$/.test(key);
}

function isCipherMissing(error: unknown): boolean {
  if (!axios.isAxiosError(error)) return false;
  if (error.response?.status !== 409) return false;
  const data = error.response?.data as Record<string, unknown> | undefined;
  return data?.code === 'cipher_missing';
}

function isSaving(key: string): boolean {
  return saving.value[key] === true;
}

async function save(row: SettingRow) {
  let payload: SettingValue = row.value;
  if (row.kind === 'number') {
    const parsed = typeof row.value === 'number' ? row.value : Number(row.value ?? 0);
    if (!Number.isFinite(parsed)) {
      message.warning(`${row.label} 需要填写数字`);
      return;
    }
    payload = parsed;
  } else if (row.kind === 'boolean') {
    // 关闭状态也要提交 false（后端已允许布尔 false）
    payload = row.value === true;
  } else {
    payload = row.value === null ? '' : String(row.value);
  }
  if (row.secret && payload === '') {
    // 后端对凭据键传空串是「保留原值」，这里直接不发请求，语义更直白
    message.info('留空表示不修改，未提交');
    return;
  }
  saving.value = { ...saving.value, [row.key]: true };
  try {
    await adminSettingsApi.update(row.key, payload);
    message.success(`${row.label} 已保存`);
    await load();
  } catch (error) {
    if (isCipherMissing(error)) {
      message.error('未配置主密钥，无法保存密钥类设置（服务端需先配置 GORIXPAY_MASTER_KEY_FILE）');
      return;
    }
    message.error(errorMessage(error));
  } finally {
    saving.value = { ...saving.value, [row.key]: false };
  }
}

function reset(row: SettingRow) {
  row.value = row.original;
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader title="系统设置" subtitle="站点信息、实名认证、域名校验、邀请返佣、风控限额与通知参数，保存后立即生效">
      <template #actions>
        <n-button secondary :loading="loading" @click="load">刷新</n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false">
      <n-spin :show="loading">
        <n-empty v-if="rows.length === 0 && !loading" description="暂无可配置项" />
        <n-form label-placement="left" label-width="200" label-align="left">
          <div v-for="group in groupedRows" :key="group.title" class="setting-group">
            <div class="group-title">{{ group.title }}</div>
            <div v-for="row in group.rows" :key="row.key" class="setting-row">
              <n-form-item :label="row.label">
                <div class="setting-control">
                  <div v-if="row.secret" class="secret-row">
                    <n-input
                      v-model:value="row.value"
                      type="password"
                      show-password-on="click"
                      :placeholder="row.configured ? '已配置，留空表示不修改' : '尚未配置，请输入密钥'"
                      style="width: 320px"
                    />
                    <n-tag size="small" round :type="row.configured ? 'success' : 'warning'">
                      {{ row.configured ? '已配置' : '未配置' }}
                    </n-tag>
                  </div>
                  <n-switch v-else-if="row.kind === 'boolean'" v-model:value="row.value">
                    <template #checked>开启</template>
                    <template #unchecked>关闭</template>
                  </n-switch>
                  <n-input-number
                    v-else-if="row.kind === 'number'"
                    v-model:value="row.value"
                    :min="0"
                    :step="1"
                    style="width: 220px"
                  />
                  <n-select
                    v-else-if="row.options"
                    v-model:value="row.value"
                    :options="row.options"
                    style="width: 220px"
                  />
                  <n-input
                    v-else-if="row.textarea"
                    v-model:value="row.value"
                    type="textarea"
                    :autosize="{ minRows: 2, maxRows: 6 }"
                    placeholder="留空则不显示"
                  />
                  <n-input v-else v-model:value="row.value" placeholder="请输入配置值" style="width: 100%" />
                  <div class="setting-meta">
                    <span class="mono key">{{ row.key }}</span>
                    <span v-if="row.unit" class="unit">当前值：{{ unitPreview(row) }}</span>
                    <span v-if="row.hint" class="muted hint">{{ row.hint }}</span>
                    <span v-if="row.secret" class="muted hint">写入后立即生效，页面只显示「已配置/未配置」</span>
                    <span class="muted hint">更新于 {{ formatDateTime(row.updated_at) }}</span>
                  </div>
                </div>
              </n-form-item>
              <div class="setting-actions">
                <n-button v-if="!row.secret" size="small" :disabled="isSaving(row.key)" @click="reset(row)">还原</n-button>
                <n-button size="small" type="primary" :loading="isSaving(row.key)" @click="save(row)">保存</n-button>
              </div>
            </div>
          </div>
        </n-form>
      </n-spin>
    </n-card>
  </div>
</template>

<style scoped>
.setting-group {
  margin-bottom: 8px;
}
.setting-group:last-child {
  margin-bottom: 0;
}
.group-title {
  font-size: 13px;
  font-weight: 600;
  color: #2563eb;
  padding: 6px 0 6px 2px;
  margin-bottom: 8px;
  border-bottom: 1px solid #e5e7eb;
}
.setting-row {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  border-bottom: 1px dashed #eef2f7;
  padding-bottom: 8px;
  margin-bottom: 12px;
}
.setting-row:last-child {
  border-bottom: none;
  margin-bottom: 0;
}
.setting-control {
  flex: 1;
  min-width: 0;
}
.setting-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 6px;
  font-size: 12px;
}
.setting-meta .key {
  color: #475569;
}
.secret-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.setting-meta .unit {
  color: #0f766e;
  font-variant-numeric: tabular-nums;
}
.setting-actions {
  display: flex;
  gap: 8px;
  padding-top: 2px;
  flex: none;
}
@media (max-width: 768px) {
  .setting-row {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
