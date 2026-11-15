<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import {
  CheckmarkOutline,
  CloseOutline,
  CopyOutline,
  KeyOutline,
  RefreshOutline,
  ReloadOutline,
  SearchOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import { securityApi } from '@/api/security';
import type { LoginAttempt, TotpSetup, TotpStatus } from '@/api/security';
import { errorMessage } from '@/api/client';
import { copyText, formatDateTime } from '@/utils/format';
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

// 管理员自助改密码。旧密码由后端校验，这里只做长度与一致性预检，少一次往返。
const passwordForm = ref({ old_password: '', new_password: '', confirm: '' });
const passwordBusy = ref(false);

function runeLength(value: string) {
  return Array.from(value).length;
}

async function submitPassword() {
  const form = passwordForm.value;
  if (form.old_password === '') {
    message.warning('请输入当前密码');
    return;
  }
  if (runeLength(form.new_password) < 8 || runeLength(form.new_password) > 72) {
    message.warning('新密码长度需为 8 到 72 个字符');
    return;
  }
  if (form.new_password !== form.confirm) {
    message.warning('两次输入的新密码不一致');
    return;
  }
  if (form.new_password === form.old_password) {
    message.warning('新密码不能与当前密码相同');
    return;
  }
  passwordBusy.value = true;
  try {
    await securityApi.changePassword({
      old_password: form.old_password,
      new_password: form.new_password,
    });
    message.success('密码已修改，其它设备上的登录已失效');
    passwordForm.value = { old_password: '', new_password: '', confirm: '' };
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    passwordBusy.value = false;
  }
}

const { page, pageSize, total, pagination, onPage, onPageSize, resetPage } =
  useServerPaging(loadAttempts);

const successOptions: SelectOption[] = [
  { label: '成功', value: 'true' },
  { label: '失败', value: 'false' },
];

const failedCount = computed(() => attempts.value.filter((item) => item.success === false).length);

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

async function copy(text: string) {
  if (!text) return;
  const ok = await copyText(text);
  if (ok) message.success('已复制');
  else message.warning('复制失败，请手动选择');
}

function reloadAll() {
  void loadTotp();
  void loadCaptcha();
  void loadAttempts();
}

const attemptColumns = computed<DataTableColumns<LoginAttempt>>(() => [
  {
    title: '账号标识',
    key: 'identifier',
    minWidth: 240,
    ellipsis: true,
    render: (row) => h(NText, { code: true }, { default: () => row.identifier || '-' }),
  },
  {
    title: '客户端 IP',
    key: 'client_ip',
    width: 180,
    render: (row) => h(NText, { code: true }, { default: () => row.client_ip || '-' }),
  },
  {
    title: '结果',
    key: 'success',
    width: 110,
    render: (row) =>
      h(
        NTag,
        { size: 'small', round: true, bordered: false, type: row.success ? 'success' : 'error' },
        { default: () => (row.success ? '成功' : '失败') },
      ),
  },
  {
    title: '时间',
    key: 'created_at',
    width: 190,
    render: (row) => formatDateTime(row.created_at),
  },
]);

onMounted(reloadAll);
</script>

<template>
  <admin-page title="安全设置" subtitle="管理员双重验证、登录图形验证码与登录记录">
    <template #actions>
      <n-button :loading="totpLoading || captchaLoading || attemptsLoading" @click="reloadAll">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        刷新
      </n-button>
    </template>

    <n-grid cols="1 m:2" responsive="screen" :x-gap="16" :y-gap="16" style="margin-bottom: 16px">
      <n-gi>
        <n-card size="small" :bordered="false" title="修改登录密码" style="height: 100%">
          <template #header-extra>
            <n-tag size="small" round :bordered="false">建议定期更换</n-tag>
          </template>

          <n-form label-placement="top">
            <n-form-item label="当前密码">
              <n-input
                v-model:value="passwordForm.old_password"
                type="password"
                show-password-on="click"
                placeholder="输入现在使用的密码"
                autocomplete="current-password"
              />
            </n-form-item>
            <n-form-item label="新密码">
              <n-input
                v-model:value="passwordForm.new_password"
                type="password"
                show-password-on="click"
                placeholder="8 到 72 个字符"
                autocomplete="new-password"
              />
            </n-form-item>
            <n-form-item label="确认新密码">
              <n-input
                v-model:value="passwordForm.confirm"
                type="password"
                show-password-on="click"
                placeholder="再输入一次新密码"
                autocomplete="new-password"
                @keyup.enter="submitPassword"
              />
            </n-form-item>
            <n-form-item :show-label="false">
              <n-space align="center" :size="12">
                <n-button type="primary" :loading="passwordBusy" @click="submitPassword">保存新密码</n-button>
                <n-text depth="3" style="font-size: 12.5px">保存后其它设备的登录会立即失效。</n-text>
              </n-space>
            </n-form-item>
          </n-form>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card size="small" :bordered="false" title="登录图形验证码" style="height: 100%">
          <n-spin :show="captchaLoading">
            <n-space vertical :size="12">
              <n-space align="center" :size="12">
                <n-switch
                  :value="captchaRequired"
                  :loading="captchaSaving"
                  @update:value="(value: boolean) => saveCaptcha(value)"
                >
                  <template #checked>开启</template>
                  <template #unchecked>关闭</template>
                </n-switch>
                <n-text>管理员与商户登录都要填验证码</n-text>
              </n-space>
              <n-text depth="3" style="font-size: 12.5px">
                验证码能拦住批量撞库，代价是每次登录多填四位字符。后台被反复试密码时先开这个。
              </n-text>
              <n-alert v-if="!captchaFound" type="warning" :bordered="false">
                后端设置里还没有「security.captcha_required」这一项，保存会失败。
              </n-alert>
            </n-space>
          </n-spin>
        </n-card>
      </n-gi>
    </n-grid>

    <n-card size="small" :bordered="false" title="双重验证（TOTP）" style="margin-bottom: 16px">
      <template #header-extra>
        <n-tag :type="totp?.enabled ? 'success' : 'default'" size="small" round :bordered="false">
          {{ totp?.enabled ? '已开启' : '未开启' }}
        </n-tag>
      </template>

      <n-spin :show="totpLoading">
        <template v-if="totp?.enabled">
          <n-descriptions :column="2" label-placement="left" bordered size="small">
            <n-descriptions-item label="状态">
              <n-tag size="small" round type="success" :bordered="false">已开启</n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="开启时间">
              {{ formatDateTime(totp.enabled_at) }}
            </n-descriptions-item>
          </n-descriptions>

          <n-text depth="3" style="display: block; margin: 12px 0">
            关闭后登录只校验密码，建议只在换绑设备时临时关闭。
          </n-text>

          <n-form label-placement="left" label-width="180">
            <n-form-item label="验证码">
              <n-space align="center" :size="10">
                <n-input
                  v-model:value="disableCode"
                  placeholder="输入 6 位验证码"
                  maxlength="6"
                  style="width: 200px"
                />
                <n-button type="error" secondary :loading="totpBusy" @click="disableTotp">
                  <template #icon><n-icon><CloseOutline /></n-icon></template>
                  关闭双重验证
                </n-button>
              </n-space>
            </n-form-item>
          </n-form>
        </template>

        <template v-else>
          <n-text depth="3" style="display: block">
            用 Google Authenticator、1Password 或 Authy 扫码或手动输入密钥完成绑定。
          </n-text>

          <n-button
            v-if="!setup"
            type="primary"
            :loading="totpBusy"
            style="margin-top: 12px"
            @click="startSetup"
          >
            <template #icon><n-icon><KeyOutline /></n-icon></template>
            开始绑定
          </n-button>

          <n-grid v-else cols="1 m:2" responsive="screen" :x-gap="20" :y-gap="16" style="margin-top: 12px">
            <n-gi>
              <n-space vertical align="center" :size="10" style="padding: 8px 0">
                <n-qr-code :value="setup.otpauth_url" :size="180" />
                <n-text depth="3">用验证器 App 扫码</n-text>
              </n-space>
            </n-gi>
            <n-gi>
              <n-descriptions :column="1" label-placement="left" bordered size="small">
                <n-descriptions-item label="密钥">
                  <n-space align="center" :size="8">
                    <n-text code>{{ setup.secret }}</n-text>
                    <n-button text type="primary" @click="copy(setup.secret)">
                      <template #icon><n-icon><CopyOutline /></n-icon></template>
                    </n-button>
                  </n-space>
                </n-descriptions-item>
                <n-descriptions-item label="otpauth 地址">
                  <n-space align="center" :size="8">
                    <n-text code style="word-break: break-all">{{ setup.otpauth_url }}</n-text>
                    <n-button text type="primary" @click="copy(setup.otpauth_url)">
                      <template #icon><n-icon><CopyOutline /></n-icon></template>
                    </n-button>
                  </n-space>
                </n-descriptions-item>
                <n-descriptions-item label="提示">
                  密钥只显示这一次，关掉后只能重新绑定。
                </n-descriptions-item>
              </n-descriptions>
            </n-gi>
          </n-grid>

          <n-form v-if="setup" label-placement="left" label-width="180" style="margin-top: 16px">
            <n-form-item label="验证码">
              <n-space align="center" :size="10">
                <n-input
                  v-model:value="enableCode"
                  placeholder="输入 App 上的 6 位验证码"
                  maxlength="6"
                  style="width: 220px"
                />
                <n-button type="primary" :loading="totpBusy" @click="enableTotp">
                  <template #icon><n-icon><CheckmarkOutline /></n-icon></template>
                  启用双重验证
                </n-button>
                <n-button :disabled="totpBusy" @click="setup = null">取消</n-button>
              </n-space>
            </n-form-item>
          </n-form>
        </template>
      </n-spin>
    </n-card>

    <n-card size="small" :bordered="false" title="登录审计">
      <template #header-extra>
        <n-text depth="3">当前页失败 {{ failedCount }} 条</n-text>
      </template>

      <n-space align="center" :size="12" style="margin-bottom: 12px">
        <n-input
          v-model:value="identifier"
          placeholder="账号标识（管理员用户名 / 商户邮箱）"
          clearable
          style="width: 280px"
          @keyup.enter="searchAttempts"
        />
        <n-select
          v-model:value="success"
          :options="successOptions"
          placeholder="全部结果"
          clearable
          style="width: 160px"
        />
        <n-button type="primary" @click="searchAttempts">
          <template #icon><n-icon><SearchOutline /></n-icon></template>
          查询
        </n-button>
        <n-button @click="resetAttempts">
          <template #icon><n-icon><ReloadOutline /></n-icon></template>
          重置
        </n-button>
        <n-text depth="3">共 {{ total }} 条</n-text>
      </n-space>

      <n-data-table
        :columns="attemptColumns"
        :data="attempts"
        :loading="attemptsLoading"
        :pagination="pagination"
        :row-key="(row: LoginAttempt) => row.id ?? `${row.identifier}-${row.created_at}`"
        :bordered="false"
        :single-line="false"
        :scroll-x="900"
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
  </admin-page>
</template>
