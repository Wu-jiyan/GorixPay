<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import axios from 'axios';
import { useMessage, type FormInst, type FormRules } from 'naive-ui';
import { authApi } from '@/api';
import { errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';

const router = useRouter();
const message = useMessage();
const auth = useAuthStore();

const formRef = ref<FormInst | null>(null);
const submitting = ref(false);
const form = reactive({ identifier: '', password: '' });

const rules: FormRules = {
  identifier: [{ required: true, message: '请输入邮箱或手机号', trigger: ['blur', 'input'] }],
  password: [{ required: true, message: '请输入登录密码', trigger: ['blur', 'input'] }],
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
  try {
    const res = await authApi.loginMerchant(form.identifier.trim(), form.password);
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
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-brand">
      <router-link to="/" class="brand-link">
        <span class="brand-mark">G</span>
        <span class="brand-name">GorixPay</span>
      </router-link>
    </div>
    <n-card class="auth-card" :bordered="false">
      <div class="auth-head">
        <h1 class="auth-title">商户登录</h1>
        <p class="auth-subtitle">登录后管理订单、密钥、退款与结算</p>
      </div>
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
        <n-button type="primary" block size="large" :loading="submitting" attr-type="submit" @click="handleSubmit">
          登录
        </n-button>
      </n-form>
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
.auth-card {
  width: 100%;
  max-width: 420px;
  box-shadow: 0 18px 48px rgba(15, 23, 42, 0.1);
  border-radius: 16px;
}
.auth-head {
  margin-bottom: 20px;
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
