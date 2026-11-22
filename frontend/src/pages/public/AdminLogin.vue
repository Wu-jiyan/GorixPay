<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import axios from 'axios';
import { NAlert, NButton, NForm, NFormItem, NInput, useMessage, type FormInst, type FormRules } from 'naive-ui';
import { accountSecurityApi } from '@/api/accountSecurity';
import { errorCode, errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import { loadSite, siteInfo } from '@/stores/site';
import BrandMark from '@/components/BrandMark.vue';

const route = useRoute();
const router = useRouter();
const message = useMessage();
const auth = useAuthStore();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const form = reactive({ username: '', password: '' });

// 后台可能在「安全设置」里打开图形验证码与 TOTP。以前登录页只有账号密码，
// 一开开关就再也登不进去；这两个输入口必须跟着出现。
const captchaEnabled = ref(false);
const captchaId = ref('');
const captchaSvg = ref('');
const captchaCode = ref('');
const captchaLoading = ref(false);
const totpRequired = ref(false);
const totpCode = ref('');

const rules: FormRules = {
  username: [{ required: true, message: '请输入运营账号', trigger: ['blur', 'input'] }],
  password: [{ required: true, message: '请输入登录密码', trigger: ['blur', 'input'] }],
};

/** 登录成功后回跳 ?next=，只接受站内路径 */
function nextTarget(fallback: string): string {
  const raw = route.query.next;
  const value = Array.isArray(raw) ? raw[0] : raw;
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//')) return fallback;
  if (value === '/login' || value === '/admin/login') return fallback;
  return value;
}

/** 平台是否要求图形验证码（公开站点信息里的 captcha_required） */
async function captchaRequired(): Promise<boolean> {
  await loadSite();
  return siteInfo.value.captcha_required === true;
}

async function loadCaptcha() {
  if (captchaLoading.value) return;
  // 平台没开验证码时不去取图，避免每次都让人多填一格。
  if (!(await captchaRequired())) {
    captchaEnabled.value = false;
    captchaId.value = '';
    captchaSvg.value = '';
    return;
  }
  captchaLoading.value = true;
  try {
    const challenge = await accountSecurityApi.captcha();
    if (!challenge?.id || !challenge?.svg) {
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
  if (captchaEnabled.value && captchaCode.value.trim() === '') {
    message.warning('请输入图形验证码');
    submitting.value = false;
    return;
  }
  if (totpRequired.value && totpCode.value.trim() === '') {
    message.warning('请输入动态口令');
    submitting.value = false;
    return;
  }
  try {
    const res = await accountSecurityApi.loginAdministratorWithSecurity(
      form.username.trim(),
      form.password,
      captchaEnabled.value ? captchaId.value : undefined,
      captchaEnabled.value ? captchaCode.value.trim() : undefined,
      totpRequired.value ? totpCode.value.trim() : undefined,
    );
    if (!res.administrator) {
      message.error('登录成功但未返回管理员信息，请联系平台运维');
      return;
    }
    auth.setAdminLogin(res.administrator);
    message.success('登录成功');
    await router.replace(nextTarget('/admin'));
  } catch (err) {
    const code = errorCode(err);
    if (code === 'totp_required') {
      // 密码与验证码已通过，只差动态口令；把这一格亮出来，别让人以为密码错了。
      totpRequired.value = true;
      message.warning('该账号开启了动态口令，请输入验证器上的 6 位数字');
    } else if (code === 'captcha_required') {
      message.warning('图形验证码不正确，请重新输入');
    } else if (axios.isAxiosError(err) && err.response?.status === 401) {
      message.error('账号或密码错误');
    } else {
      message.error(errorMessage(err));
    }
    if (captchaEnabled.value) void loadCaptcha();
  } finally {
    submitting.value = false;
  }
}

onMounted(() => {
  void loadCaptcha();
});
</script>

<template>
  <div class="admin-auth-page">
    <div class="admin-auth-panel">
      <div class="admin-brand">
        <brand-mark :size="34" subtitle="运营后台" />
      </div>
      <div class="auth-head">
        <h1 class="auth-title">运营账号登录</h1>
        <p class="auth-subtitle">仅限平台运营人员使用，登录行为将写入审计日志</p>
      </div>
      <n-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-placement="top"
        size="large"
        @submit.prevent="handleSubmit"
      >
        <n-form-item label="运营账号" path="username">
          <n-input v-model:value="form.username" placeholder="请输入运营账号" clearable />
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
              placeholder="请输入右侧字符"
              :maxlength="8"
              @keyup.enter="handleSubmit"
            />
            <button
              type="button"
              class="captcha-image"
              :disabled="captchaLoading"
              title="看不清？点击换一张"
              @click="loadCaptcha"
              v-html="captchaSvg"
            />
          </div>
        </n-form-item>
        <n-form-item v-if="totpRequired" label="动态口令">
          <n-input
            v-model:value="totpCode"
            placeholder="验证器上的 6 位数字"
            :maxlength="6"
            @keyup.enter="handleSubmit"
          />
        </n-form-item>
        <n-alert v-if="totpRequired" type="info" :bordered="false" style="margin-bottom: 14px">
          该账号已开启双因素认证，请打开验证器 App 填写当前 6 位口令。
        </n-alert>
        <n-button type="primary" block size="large" :loading="submitting" attr-type="submit" @click="handleSubmit">
          登录运营后台
        </n-button>
      </n-form>
      <div class="auth-bottom">
        <router-link to="/">返回官网</router-link>
        <span class="sep">·</span>
        <router-link to="/login">商户登录</router-link>
      </div>
    </div>
  </div>
</template>

<style scoped>
.admin-auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px var(--gp-gutter);
  background: var(--gp-bg);
}
.admin-auth-panel {
  width: 100%;
  max-width: 420px;
  padding: 30px 28px 26px;
  border-radius: var(--gp-radius-lg);
  background: var(--gp-bg-soft);
  border: 1px solid var(--gp-line);
  box-shadow: var(--gp-shadow);
}
.admin-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}
.auth-head {
  margin-bottom: 20px;
}
.auth-title {
  font-size: 20px;
  margin: 0 0 6px;
  color: var(--gp-text-1);
}
.auth-subtitle {
  margin: 0;
  font-size: 12.5px;
  color: var(--gp-text-3);
  line-height: 1.7;
}
.captcha-row {
  display: flex;
  gap: 10px;
  width: 100%;
  align-items: center;
}
.captcha-image {
  flex: none;
  width: 116px;
  height: 34px;
  padding: 0;
  border: 1px solid var(--gp-line);
  border-radius: var(--gp-radius-sm);
  background: var(--gp-surface);
  cursor: pointer;
  overflow: hidden;
  display: grid;
  place-items: center;
}
.captcha-image :deep(svg) {
  width: 100%;
  height: 100%;
}
.auth-bottom {
  margin-top: 20px;
  font-size: 12.5px;
  color: var(--gp-text-3);
  display: flex;
  gap: 8px;
  justify-content: center;
}
.auth-bottom a:hover {
  color: var(--gp-accent);
}
.sep {
  color: var(--gp-text-3);
}
@media (max-width: 768px) {
  .admin-auth-page {
    align-items: flex-start;
    padding: 28px var(--gp-gutter);
  }
}
</style>