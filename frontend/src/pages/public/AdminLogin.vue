<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import axios from 'axios';
import { useMessage, type FormInst, type FormRules } from 'naive-ui';
import { authApi } from '@/api';
import { errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import { siteLogo, siteName } from '@/stores/site';

const route = useRoute();
const router = useRouter();
const message = useMessage();
const auth = useAuthStore();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const form = reactive({ username: '', password: '' });

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

async function handleSubmit() {
  if (submitting.value) return;
  submitting.value = true;
  try {
    await formRef.value?.validate();
  } catch {
    submitting.value = false;
    return;
  }
  try {
    const res = await authApi.loginAdministrator(form.username.trim(), form.password);
    if (!res.administrator) {
      message.error('登录成功但未返回管理员信息，请联系平台运维');
      return;
    }
    auth.setAdminLogin(res.administrator);
    message.success('登录成功');
    await router.replace(nextTarget('/admin'));
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 401) {
      message.error('账号或密码错误');
    } else {
      message.error(errorMessage(err));
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="admin-auth-page">
    <div class="admin-auth-panel">
      <div class="admin-brand">
        <img v-if="siteLogo" :src="siteLogo" class="brand-logo" alt="" />
        <span v-else class="brand-mark">G</span>
        <div>
          <div class="brand-name">{{ siteName }}</div>
          <div class="brand-sub">运营后台</div>
        </div>
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
  padding: 40px 16px;
  background: radial-gradient(800px 380px at 20% 0%, #1e293b 0%, transparent 65%),
    linear-gradient(160deg, #0b1220 0%, #111b2e 55%, #0b1220 100%);
}
.admin-auth-panel {
  width: 100%;
  max-width: 420px;
  padding: 30px 28px 26px;
  border-radius: 16px;
  background: #121c2f;
  border: 1px solid rgba(148, 163, 184, 0.18);
  box-shadow: 0 24px 60px rgba(2, 6, 23, 0.55);
}
.admin-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}
.brand-mark {
  width: 38px;
  height: 38px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  color: #fff;
  font-weight: 700;
  background: linear-gradient(135deg, #1e3a8a, #0ea5e9);
}
.brand-logo {
  width: 38px;
  height: 38px;
  border-radius: 11px;
  object-fit: cover;
  display: block;
  flex: none;
}
.brand-name {
  color: #f8fafc;
  font-weight: 700;
  font-size: 16px;
}
.brand-sub {
  color: #7c8da8;
  font-size: 11.5px;
}
.auth-head {
  margin-bottom: 20px;
}
.auth-title {
  font-size: 20px;
  margin: 0 0 6px;
  color: #f1f5f9;
}
.auth-subtitle {
  margin: 0;
  font-size: 12.5px;
  color: #94a3b8;
  line-height: 1.7;
}
:deep(.n-form-item-label) {
  color: #cbd5e1 !important;
}
:deep(.n-input) {
  background: #0d1626;
}
:deep(.n-input .n-input__input-el),
:deep(.n-input .n-input__placeholder) {
  color: #e2e8f0;
}
:deep(.n-input .n-input__placeholder) {
  color: #64748b;
}
.auth-bottom {
  margin-top: 20px;
  font-size: 12.5px;
  color: #7c8da8;
  display: flex;
  gap: 8px;
  justify-content: center;
}
.auth-bottom a:hover {
  color: #93c5fd;
}
.sep {
  color: #334155;
}
@media (max-width: 768px) {
  .admin-auth-page {
    align-items: flex-start;
    padding: 28px 14px;
  }
}
</style>
