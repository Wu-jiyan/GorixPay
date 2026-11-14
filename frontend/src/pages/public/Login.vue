<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import axios from 'axios';
import { useMessage, type FormInst, type FormRules } from 'naive-ui';
import { authApi, merchantApi } from '@/api';
import { accountSecurityApi } from '@/api/accountSecurity';
import { qrLoginApi, type QrTicket, type QrPollResult } from '@/api/publicQrLogin';
import { errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import { siteLogo, siteName } from '@/stores/site';

const router = useRouter();
const message = useMessage();
const auth = useAuthStore();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const form = reactive({ identifier: '', password: '' });

// 图形验证码：后端开启时才返回内容，404/400 或空内容就整块隐藏
const captchaEnabled = ref(false);
const captchaId = ref('');
const captchaSvg = ref('');
const captchaCode = ref('');
const captchaLoading = ref(false);

// 登录方式：账号密码 / 扫码
const loginMode = ref<'password' | 'qr'>('password');
const qrLoading = ref(false);
const qrError = ref('');
const qrTicket = ref<QrTicket | null>(null);
const qrStatus = ref('pending');
let qrTimer: number | undefined;

const oauthProviders = [
  { key: 'wechat', label: '微信' },
  { key: 'qq', label: 'QQ' },
  { key: 'douyin', label: '抖音' },
];

const rules: FormRules = {
  identifier: [{ required: true, message: '请输入邮箱或手机号', trigger: ['blur', 'input'] }],
  password: [{ required: true, message: '请输入登录密码', trigger: ['blur', 'input'] }],
};

async function loadCaptcha() {
  if (captchaLoading.value) return;
  captchaLoading.value = true;
  try {
    const challenge = await accountSecurityApi.captcha();
    if (!challenge.svg) {
      captchaEnabled.value = false;
      captchaId.value = '';
      captchaSvg.value = '';
      return;
    }
    captchaEnabled.value = true;
    captchaId.value = challenge.id;
    captchaSvg.value = challenge.svg;
    captchaCode.value = '';
  } catch {
    captchaEnabled.value = false;
    captchaId.value = '';
    captchaSvg.value = '';
  } finally {
    captchaLoading.value = false;
  }
}

async function handleSubmit() {
  if (submitting.value) return;
  submitting.value = true;
  try {
    await formRef.value?.validate();
  } catch {
    submitting.value = false;
    return;
  }
  if (captchaEnabled.value && !captchaCode.value.trim()) {
    message.error('请输入图形验证码');
    submitting.value = false;
    return;
  }
  try {
    const res = captchaEnabled.value
      ? await accountSecurityApi.loginMerchantWithCaptcha(
          form.identifier.trim(),
          form.password,
          captchaId.value,
          captchaCode.value.trim(),
        )
      : await authApi.loginMerchant(form.identifier.trim(), form.password);
    if (!res.merchant) {
      message.error('登录成功但未返回商户信息，请联系平台客服');
      return;
    }
    auth.setMerchantLogin(res.token, res.expires_at, res.merchant);
    message.success('登录成功');
    await router.replace('/merchant');
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 401) {
      message.error('账号或密码错误');
    } else {
      message.error(errorMessage(err));
    }
    if (captchaEnabled.value) void loadCaptcha();
  } finally {
    submitting.value = false;
  }
}

function stopQrPolling() {
  if (qrTimer !== undefined) {
    window.clearInterval(qrTimer);
    qrTimer = undefined;
  }
}

async function loadQrTicket() {
  stopQrPolling();
  qrLoading.value = true;
  qrError.value = '';
  qrStatus.value = 'pending';
  try {
    const ticket = await qrLoginApi.createTicket();
    if (!ticket?.qr_svg || !ticket.ticket) {
      qrError.value = '二维码生成失败，请稍后重试';
      return;
    }
    qrTicket.value = ticket;
    qrTimer = window.setInterval(() => {
      void pollQr(ticket.ticket);
    }, 2000);
  } catch (err) {
    qrError.value = errorMessage(err, '二维码生成失败，请稍后重试');
  } finally {
    qrLoading.value = false;
  }
}

async function pollQr(ticket: string) {
  try {
    const result = await qrLoginApi.poll(ticket);
    qrStatus.value = result.status;
    if (result.status === 'confirmed') {
      stopQrPolling();
      await completeQrLogin(result);
      return;
    }
    if (result.status === 'expired' || result.status === 'cancelled') {
      stopQrPolling();
    }
  } catch {
    // 单次轮询失败不打断，下一轮继续
  }
}

async function completeQrLogin(result: QrPollResult) {
  if (!result.token) {
    qrError.value = '扫码登录确认失败，请重试';
    return;
  }
  const expiresAt = result.expires_at || new Date(Date.now() + 7 * 86400000).toISOString();
  const merchant = result.merchant ?? { id: '', display_name: '商户' };
  auth.setMerchantLogin(result.token, expiresAt, merchant);
  if (!result.merchant) {
    try {
      const profile = await merchantApi.profile();
      auth.merchant = { ...profile };
      auth.persist();
    } catch {
      // 拿不到资料也不影响登录，顶部名称稍后由 loadSession 补齐
    }
  }
  message.success('登录成功');
  await router.replace('/merchant');
}

function handleOauth(provider: string) {
  const redirect = encodeURIComponent('/merchant');
  window.location.href = `/api/v1/auth/oauth/${provider}/authorize?redirect=${redirect}`;
}

watch(loginMode, (mode) => {
  if (mode === 'qr') {
    void loadQrTicket();
  } else {
    stopQrPolling();
  }
});

onMounted(() => {
  void loadCaptcha();
});

onBeforeUnmount(() => {
  stopQrPolling();
});
</script>

<template>
  <div class="auth-page">
    <div class="auth-brand">
      <router-link to="/" class="brand-link">
        <img v-if="siteLogo" :src="siteLogo" class="brand-logo" alt="" />
        <span v-else class="brand-mark">G</span>
        <span class="brand-name">{{ siteName }}</span>
      </router-link>
    </div>
    <n-card class="auth-card" :bordered="false">
      <div class="auth-head">
        <h1 class="auth-title">商户登录</h1>
        <p class="auth-subtitle">登录后管理订单、密钥、退款与结算</p>
      </div>

      <n-tabs v-model:value="loginMode" type="line" animated size="small" class="login-tabs">
        <n-tab-pane name="password" tab="账号登录">
          <n-form ref="formRef" :model="form" :rules="rules" label-placement="top" size="large" @submit.prevent="handleSubmit">
            <n-form-item label="邮箱或手机号" path="identifier">
              <n-input v-model:value="form.identifier" placeholder="注册时使用的邮箱或手机号" clearable />
            </n-form-item>
            <n-form-item label="登录密码" path="password">
              <n-input
                v-model:value="form.password"
                type="password"
                show-password-on="click"
                placeholder="请输入登录密码"
                @keyup.enter="handleSubmit"
              />
            </n-form-item>
            <n-form-item v-if="captchaEnabled" label="图形验证码">
              <div class="captcha-row">
                <n-input
                  v-model:value="captchaCode"
                  placeholder="请输入图中字符"
                  :maxlength="8"
                  @keyup.enter="handleSubmit"
                />
                <button type="button" class="captcha-image" title="点击刷新验证码" @click="loadCaptcha">
                  <span v-if="captchaSvg" class="captcha-svg" v-html="captchaSvg"></span>
                  <span v-else class="captcha-refresh">刷新</span>
                </button>
              </div>
            </n-form-item>
            <n-button type="primary" block size="large" :loading="submitting" attr-type="submit" @click="handleSubmit">
              登录
            </n-button>
          </n-form>
        </n-tab-pane>

        <n-tab-pane name="qr" tab="扫码登录">
          <div class="qr-panel">
            <div v-if="qrLoading" class="qr-loading">
              <n-spin size="large" />
              <p class="qr-tip">正在生成二维码…</p>
            </div>
            <template v-else-if="qrError">
              <n-alert type="error" :bordered="false">{{ qrError }}</n-alert>
              <n-button size="small" class="qr-refresh" @click="loadQrTicket">重新生成</n-button>
            </template>
            <template v-else>
              <button type="button" class="qr-image" title="点击刷新二维码" @click="loadQrTicket">
                <span v-if="qrTicket?.qr_svg" class="qr-svg" v-html="qrTicket.qr_svg"></span>
              </button>
              <p class="qr-tip">
                <template v-if="qrStatus === 'expired'">二维码已过期，点击上方二维码重新生成</template>
                <template v-else>使用已登录的商户账号扫码，在手机上确认后自动登录</template>
              </p>
              <n-button size="small" quaternary @click="loadQrTicket">刷新二维码</n-button>
            </template>
          </div>
        </n-tab-pane>
      </n-tabs>

      <div class="oauth-row">
        <span class="oauth-label">第三方登录</span>
        <n-button
          v-for="provider in oauthProviders"
          :key="provider.key"
          size="small"
          secondary
          @click="handleOauth(provider.key)"
        >
          {{ provider.label }}
        </n-button>
      </div>

      <div class="auth-links">
        <router-link to="/forgot-password" class="auth-link">忘记密码？</router-link>
      </div>
      <div class="auth-footer">
        还没有账号？<router-link to="/register" class="auth-link">免费注册</router-link>
      </div>
    </n-card>
    <div class="auth-bottom">
      <router-link to="/">返回首页</router-link>
      <span class="sep">·</span>
      <router-link to="/doc">开发文档</router-link>
      <span class="sep">·</span>
      <router-link to="/admin/login">运营登录</router-link>
    </div>
  </div>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  background: radial-gradient(900px 420px at 12% 0%, #dbeafe 0%, transparent 62%),
    linear-gradient(160deg, #f8fbff 0%, #eef4ff 60%, #f5f7fb 100%);
}
.auth-brand {
  margin-bottom: 20px;
}
.brand-link {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 700;
  font-size: 18px;
  color: #0f172a;
}
.brand-mark {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  color: #fff;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
}
.brand-logo {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  object-fit: cover;
  display: block;
  flex: none;
}
.auth-card {
  width: 100%;
  max-width: 440px;
  box-shadow: 0 18px 48px rgba(15, 23, 42, 0.1);
  border-radius: 16px;
}
.auth-head {
  margin-bottom: 14px;
  text-align: center;
}
.auth-title {
  font-size: 22px;
  margin: 0 0 6px;
  color: #0f172a;
}
.auth-subtitle {
  margin: 0;
  font-size: 13px;
  color: #64748b;
}
.login-tabs {
  margin-bottom: 4px;
}
.qr-panel {
  min-height: 236px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 8px 0 4px;
}
.qr-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}
.qr-image {
  width: 180px;
  height: 180px;
  padding: 8px;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  background: #fff;
  display: grid;
  place-items: center;
  cursor: pointer;
}
.qr-image:hover {
  border-color: #93c5fd;
}
.qr-svg {
  display: block;
  width: 100%;
  height: 100%;
}
.qr-svg :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
}
.qr-tip {
  margin: 0;
  font-size: 12.5px;
  color: #64748b;
  text-align: center;
  line-height: 1.7;
}
.qr-refresh {
  margin-top: 4px;
}
.oauth-row {
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px dashed #e2e8f0;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.oauth-label {
  font-size: 12.5px;
  color: #64748b;
}
.auth-links {
  margin-top: 12px;
  text-align: center;
  font-size: 13px;
}
.captcha-row {
  display: flex;
  gap: 10px;
  width: 100%;
  align-items: center;
}
.captcha-row :deep(.n-input) {
  flex: 1;
  min-width: 0;
}
.captcha-image {
  width: 116px;
  height: 40px;
  padding: 0;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #f8fafc;
  display: grid;
  place-items: center;
  overflow: hidden;
  cursor: pointer;
  flex: none;
}
.captcha-image:hover {
  border-color: #93c5fd;
}
.captcha-svg {
  display: block;
  width: 100%;
  height: 100%;
}
.captcha-svg :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
}
.captcha-refresh {
  font-size: 12px;
  color: #64748b;
}
.auth-footer {
  margin-top: 18px;
  text-align: center;
  font-size: 13px;
  color: #64748b;
}
.auth-link {
  color: #2563eb;
  font-weight: 600;
}
.auth-bottom {
  margin-top: 22px;
  font-size: 12.5px;
  color: #64748b;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: center;
}
.auth-bottom a:hover {
  color: #2563eb;
}
.sep {
  color: #cbd5e1;
}
@media (max-width: 768px) {
  .auth-page {
    justify-content: flex-start;
    padding: 28px 14px;
  }
}
</style>
