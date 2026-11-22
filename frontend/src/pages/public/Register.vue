<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import axios from 'axios';
import { useMessage, type FormInst, type FormItemRule, type FormRules } from 'naive-ui';
import { authApi } from '@/api';
import { errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import { loadSite, siteInfo } from '@/stores/site';
import BrandMark from '@/components/BrandMark.vue';

const route = useRoute();
const router = useRouter();
const message = useMessage();
const auth = useAuthStore();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const form = reactive({ identifier: '', password: '', confirm: '' });

// 注册门禁（register.mode）：关掉或仅邀请时不再展示注册表单，避免填完才被后端拒。
const registerMode = computed(() => siteInfo.value.register_mode || 'open');
const canRegister = computed(
  () => registerMode.value === 'open' || (registerMode.value === 'invite' && invite.value !== ''),
);

onMounted(() => {
  void loadSite();
});

// 邀请码由分享链接带入（/register?invite=XXXXXXXX），只做去空白 + 大写透传，合法性由后端判定
const invite = computed(() => {
  const raw = route.query.invite;
  const value = Array.isArray(raw) ? raw[0] : raw;
  return typeof value === 'string' ? value.trim().toUpperCase() : '';
});

const rules: FormRules = {
  identifier: [
    { required: true, message: '请输入邮箱或手机号', trigger: ['blur', 'input'] },
    {
      validator: (_rule: FormItemRule, value: string) => {
        if (!value) return true;
        const looksLikeEmail = value.includes('@');
        if (looksLikeEmail && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) {
          return new Error('邮箱格式不正确');
        }
        if (!looksLikeEmail && !/^\d{6,15}$/.test(value)) {
          return new Error('请输入 6-15 位手机号，或完整的邮箱地址');
        }
        return true;
      },
      trigger: ['blur', 'input'],
    },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: ['blur', 'input'] },
    {
      validator: (_rule: FormItemRule, value: string) => {
        if (!value || value.length >= 6) return true;
        return new Error('密码至少 6 位');
      },
      trigger: ['blur', 'input'],
    },
  ],
  confirm: [
    { required: true, message: '请再次输入密码', trigger: ['blur', 'input'] },
    {
      validator: (_rule: FormItemRule, value: string) => {
        if (!value || value === form.password) return true;
        return new Error('两次输入的密码不一致');
      },
      trigger: ['blur', 'input'],
    },
  ],
};

async function handleSubmit() {
  if (submitting.value) return;
  submitting.value = true;
  try {
    await formRef.value?.validate();
  } catch {
    submitting.value = false;
    return;
  }
  if (form.password !== form.confirm) {
    message.error('两次输入的密码不一致');
    submitting.value = false;
    return;
  }
  try {
    const res = await authApi.register(form.identifier.trim(), form.password, invite.value || undefined);
    if (!res.merchant) {
      message.error('注册成功但未返回商户信息，请使用登录页登录');
      return;
    }
    auth.setMerchantLogin(res.merchant);
    message.success('注册成功，正在进入商户中心');
    await router.replace('/merchant');
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 409) {
      message.error('该账号已注册');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-brand">
      <brand-mark :size="34" />
    </div>
    <n-card class="auth-card" :bordered="false">
      <div class="auth-head">
        <h1 class="auth-title">注册商户账号</h1>
        <p class="auth-subtitle">注册即开通商户号，可直接调用收银台与 V1 / V2 接口</p>
      </div>
      <div v-if="invite" class="invite-box">
        <n-tag type="info" :bordered="false" size="small">邀请码：{{ invite }}</n-tag>
        <span class="invite-hint">注册成功后自动绑定邀请关系</span>
      </div>
      <div v-if="!canRegister" class="closed-box">
        <n-alert :type="registerMode === 'closed' ? 'warning' : 'info'" :bordered="false">
          {{ registerMode === 'closed'
            ? '本站暂未开放注册。如需开通商户号，请联系平台运营。'
            : '本站仅限邀请注册，请使用带邀请码的注册链接，或联系邀请你的人。' }}
        </n-alert>
        <n-button block size="large" @click="router.push('/login')">去登录</n-button>
      </div>
      <n-form
        v-else
        ref="formRef"
        :model="form"
        :rules="rules"
        label-placement="top"
        size="large"
        @submit.prevent="handleSubmit"
      >
        <n-form-item label="邮箱或手机号" path="identifier">
          <n-input v-model:value="form.identifier" placeholder="you@example.com 或 13800000000" clearable />
        </n-form-item>
        <n-form-item label="登录密码" path="password">
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            placeholder="至少 6 位字符"
          />
        </n-form-item>
        <n-form-item label="确认密码" path="confirm">
          <n-input
            v-model:value="form.confirm"
            type="password"
            show-password-on="click"
            placeholder="再次输入登录密码"
          />
        </n-form-item>
        <n-button type="primary" block size="large" :loading="submitting" attr-type="submit" @click="handleSubmit">
          立即注册
        </n-button>
      </n-form>
      <div class="auth-footer">
        已有账号？<router-link to="/login" class="auth-link">商户登录</router-link>
      </div>
      <div class="auth-tip">注册即表示同意平台服务条款与隐私政策</div>
    </n-card>
    <div class="auth-bottom">
      <router-link to="/">返回首页</router-link>
      <span class="sep">·</span>
      <router-link to="/doc">开发文档</router-link>
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
  padding: 40px var(--gp-gutter);
  background: var(--gp-bg);
}
.auth-brand {
  margin-bottom: 20px;
}
.auth-card {
  width: 100%;
  max-width: 440px;
  box-shadow: var(--gp-shadow);
  border-radius: var(--gp-radius-lg);
}
.auth-head {
  margin-bottom: 20px;
  text-align: center;
}
.invite-box {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}
.closed-box {
  display: grid;
  gap: 14px;
  margin-top: 4px;
}
.invite-hint {
  font-size: 12px;
  color: var(--gp-text-3);
}
.auth-title {
  font-size: 22px;
  margin: 0 0 6px;
  color: var(--gp-text-1);
}
.auth-subtitle {
  margin: 0;
  font-size: 13px;
  color: var(--gp-text-3);
  line-height: 1.7;
}
.auth-footer {
  margin-top: 18px;
  text-align: center;
  font-size: 13px;
  color: var(--gp-text-3);
}
.auth-link {
  color: var(--gp-accent);
  font-weight: 600;
}
.auth-tip {
  margin-top: 10px;
  text-align: center;
  font-size: 12px;
  color: var(--gp-text-3);
}
.auth-bottom {
  margin-top: 22px;
  font-size: 12.5px;
  color: var(--gp-text-3);
  display: flex;
  gap: 8px;
}
.auth-bottom a:hover {
  color: var(--gp-accent);
}
.sep {
  color: var(--gp-text-3);
}
@media (max-width: 768px) {
  .auth-page {
    justify-content: flex-start;
    padding: 28px var(--gp-gutter);
  }
}
</style>
