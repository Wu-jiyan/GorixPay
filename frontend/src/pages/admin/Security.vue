<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NQrCode, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { adminApi } from '@/api';
import { securityApi } from '@/api/security';
import type { LoginAttempt, TotpSetup, TotpStatus } from '@/api/security';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { formatDateTime } from '@/utils/format';
import { asRow, itemsOf, numberOr, useServerPaging } from './shared';

const message = useMessage();

const totp = ref<TotpStatus | null>(null);
const totpLoading = ref(false);
const setup = ref<TotpSetup | null>(null);
const enableCode = ref('');
const disableCode = ref('');
const totpBusy = ref(false);

const captchaRequired = ref(false);
const captchaFound = ref(true);
const captchaLoading = ref(false);
const captchaSaving = ref(false);

const attempts = ref<LoginAttempt[]>([]);
const attemptsLoading = ref(false);
const identifier = ref('');
const success = ref<string | null>(null);

const {
  page,
  pageSize,
  total,
  pagination,
  onPage,
  onPageSize,
  resetPage,
} = useServerPaging(loadAttempts);

const successOptions: SelectOption[] = [
  { label: '成功', value: 'true' },
  { label: '失败', value: 'false' },
];

async function loadTotp() {
  totpLoading.value = true;
  try {
    totp.value = await securityApi.totp();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    totpLoading.value = false;
  }
}

async function startSetup() {
  totpBusy.value = true;
  try {
    setup.value = await securityApi.setupTotp();
    enableCode.value = '';
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    totpBusy.value = false;
  }
}

async function enableTotp() {
  const code = enableCode.value.trim();
  if (code === '') {
    message.warning('请输入 6 位验证码');
    return;
  }
  totpBusy.value = true;
  try {
    await securityApi.enableTotp(code);
    message.success('双重验证已开启');
    setup.value = null;
    enableCode.value = '';
    await loadTotp();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    totpBusy.value = false;
  }
}

async function disableTotp() {
  const code = disableCode.value.trim();
  if (code === '') {
    message.warning('请输入 6 位验证码');
    return;
  }
  totpBusy.value = true;
  try {
    await securityApi.disableTotp(code);
    message.success('双重验证已关闭');
    disableCode.value = '';
    await loadTotp();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    totpBusy.value = false;
  }
}

async function loadCaptcha() {
  captchaLoading.value = true;
  try {
    const items = await adminApi.settings();
    const row = items.find((item) => item.key === 'security.captcha_required');
    captchaFound.value = Boolean(row);
    const value = row?.value;
    captchaRequired.value = value === true || value === 'true' || value === 1;
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    captchaLoading.value = false;
  }
}

async function saveCaptcha(value: boolean) {
  captchaSaving.value = true;
  try {
    await adminApi.updateSetting('security.captcha_required', value);
    captchaRequired.value = value;
    message.success(value ? '已开启登录图形验证码' : '已关闭登录图形验证码');
  } catch (error) {
    captchaRequired.value = !value;
    message.error(errorMessage(error));
  } finally {
    captchaSaving.value = false;
  }
}

async function loadAttempts() {
  attemptsLoading.value = true;
  try {
    const data = await securityApi.loginAttempts({
      page: page.value,
      page_size: pageSize.value,
      identifier: identifier.value.trim() || undefined,
      success: success.value === null || success.value === '' ? undefined : success.value === 'true',
    });
    const items = itemsOf<LoginAttempt>(data);
    attempts.value = items;
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    attemptsLoading.value = false;
  }
}

function searchAttempts() {
  resetPage();
  void loadAttempts();
}

function resetAttempts() {
  identifier.value = '';
  success.value = null;
  resetPage();
  void loadAttempts();
}

const attemptColumns = computed<DataTableColumns<LoginAttempt>>(() => [
  {
    title: '账号标识',
    key: 'identifier',
    minWidth: 220,
    ellipsis: true,
    render: (row) => h('span', { class: 'mono' }, row.identifier || '-'),
  },
  {
    title: '客户端 IP',
    key: 'client_ip',
    width: 160,
    render: (row) => h('span', { class: 'mono' }, row.client_ip || '-'),
  },
  {
    title: '结果',
    key: 'success',
    width: 100,
    render: (row) =>
      h(
        NTag,
        { size: 'small', round: true, type: row.success ? 'success' : 'error' },
        { default: () => (row.success ? '成功' : '失败') },
      ),
  },
  {
    title: '时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
]);

onMounted(() => {
  void loadTotp();
  void loadCaptcha();
  void loadAttempts();
});
</script>

<template>
  <div class="page">
    <PageHeader title="安全设置" subtitle="管理员双重验证、登录图形验证码与登录记录">
      <template #actions>
        <n-button secondary :loading="totpLoading || captchaLoading || attemptsLoading" @click="loadTotp(); loadCaptcha(); loadAttempts()">
          刷新
        </n-button>
      </template>
    </PageHeader>

    <n-card :bordered="false" title="双重验证（TOTP）" class="section">
      <template #header-extra>
        <n-tag :type="totp?.enabled ? 'success' : 'default'" size="small" round>
          {{ totp?.enabled ? '已开启' : '未开启' }}
        </n-tag>
      </template>
      <n-spin :show="totpLoading">
        <template v-if="totp?.enabled">
          <p class="line">已开启，开启时间：{{ formatDateTime(totp.enabled_at) }}。</p>
          <p class="line muted">关闭后登录只校验密码，建议只在换绑设备时临时关闭。</p>
          <n-space align="center" :size="10">
            <n-input v-model:value="disableCode" placeholder="输入 6 位验证码" style="width: 200px" maxlength="6" />
            <n-button type="error" secondary :loading="totpBusy" @click="disableTotp">关闭双重验证</n-button>
          </n-space>
        </template>
        <template v-else>
          <p class="line">用 Google Authenticator、1Password 或 Authy 扫码或手动输入密钥完成绑定。</p>
          <n-space v-if="!setup" :size="10">
            <n-button type="primary" @click="startSetup" :loading="totpBusy">开始绑定</n-button>
          </n-space>
          <template v-else>
            <div class="totp-box">
              <NQrCode :value="setup.otpauth_url" :size="180" />
              <div class="totp-meta">
                <div class="muted small">密钥（手动输入时用）</div>
                <div class="mono secret">{{ setup.secret }}</div>
                <div class="muted small">otpauth 地址</div>
                <div class="mono url">{{ setup.otpauth_url }}</div>
                <div class="muted small">密钥只显示这一次，关掉后只能重新绑定。</div>
              </div>
            </div>
            <n-space align="center" :size="10" style="margin-top: 14px">
              <n-input v-model:value="enableCode" placeholder="输入 App 上的 6 位验证码" style="width: 220px" maxlength="6" />
              <n-button type="primary" :loading="totpBusy" @click="enableTotp">启用双重验证</n-button>
              <n-button @click="setup = null">取消</n-button>
            </n-space>
          </template>
        </template>
      </n-spin>
    </n-card>

    <n-card :bordered="false" title="登录图形验证码" class="section">
      <n-spin :show="captchaLoading">
        <div class="row">
          <n-switch
            :value="captchaRequired"
            :loading="captchaSaving"
            @update:value="(value: boolean) => saveCaptcha(value)"
          >
            <template #checked>开启</template>
            <template #unchecked>关闭</template>
          </n-switch>
          <span class="line">
            开启后管理员与商户登录都需要填写图形验证码，降低撞库成功率。
          </span>
        </div>
        <p v-if="!captchaFound" class="muted small warn">
          后端设置里还没有「security.captcha_required」这一项，保存会失败；把开关拨过去可以验证接口是否就绪。
        </p>
      </n-spin>
    </n-card>

    <n-card :bordered="false" title="登录记录" class="section">
      <div class="toolbar">
        <n-input
          v-model:value="identifier"
          placeholder="账号标识（管理员用户名 / 商户邮箱）"
          clearable
          style="width: 280px"
          @keyup.enter="searchAttempts"
        />
        <n-select v-model:value="success" :options="successOptions" placeholder="全部结果" clearable style="width: 140px" />
        <n-button type="primary" @click="searchAttempts">查询</n-button>
        <n-button @click="resetAttempts">重置</n-button>
        <span class="muted small">共 {{ total }} 条</span>
      </div>
      <n-data-table
        :columns="attemptColumns"
        :data="attempts"
        :loading="attemptsLoading"
        :pagination="pagination"
        :row-key="(row: LoginAttempt) => row.id ?? `${row.identifier}-${row.created_at}`"
        :bordered="false"
        :single-line="false"
        size="small"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无登录记录" size="small" />
        </template>
      </n-data-table>
    </n-card>
  </div>
</template>

<style scoped>
.section {
  margin-bottom: 16px;
}
.section:last-child {
  margin-bottom: 0;
}
.line {
  margin: 0 0 10px;
  font-size: 13px;
  color: #374151;
}
.small {
  font-size: 12px;
}
.warn {
  margin: 10px 0 0;
}
.totp-box {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  flex-wrap: wrap;
  margin-top: 14px;
}
.totp-meta {
  flex: 1;
  min-width: 240px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.secret {
  font-size: 15px;
  word-break: break-all;
  color: #0f766e;
}
.url {
  font-size: 12px;
  word-break: break-all;
  color: #475569;
}
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
</style>
