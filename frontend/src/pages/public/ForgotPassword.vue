<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { accountSecurityApi } from '@/api/accountSecurity';
import { errorMessage } from '@/api/client';
import { siteLogo, siteName } from '@/stores/site';

const router = useRouter();
const message = useMessage();

const step = ref<1 | 2>(1);
const sending = ref(false);
const resetting = ref(false);
const sentIdentifier = ref('');
const form = reactive({ identifier: '', token: '', password: '', confirm: '' });

async function handleForgot() {
  if (sending.value) return;
  const identifier = form.identifier.trim();
  if (!identifier) {
    message.error('请输入注册时使用的邮箱或手机号');
    return;
  }
  sending.value = true;
  try {
    await accountSecurityApi.forgotPassword(identifier);
    sentIdentifier.value = identifier;
    step.value = 2;
    message.info('如果账号存在，重置邮件已发出');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    sending.value = false;
  }
}

async function handleReset() {
  if (resetting.value) return;
  const identifier = sentIdentifier.value || form.identifier.trim();
  const token = form.token.trim();
  if (!identifier) {
    message.error('请先填写账号并获取重置邮件');
    step.value = 1;
    return;
  }
  if (!token) {
    message.error('请填写邮件里的重置令牌');
    return;
  }
  if (form.password.length < 6) {
    message.error('新密码至少 6 位');
    return;
  }
  if (form.password !== form.confirm) {
    message.error('两次输入的新密码不一致');
    return;
  }
  resetting.value = true;
  try {
    await accountSecurityApi.resetPassword({ identifier, token, password: form.password });
    message.success('密码已重置，请使用新密码登录');
    await router.replace('/login');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    resetting.value = false;
  }
}

function backToStepOne() {
  step.value = 1;
  form.token = '';
  form.password = '';
  form.confirm = '';
}
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
        <h1 class="auth-title">找回密码</h1>
        <p class="auth-subtitle">通过注册邮箱或手机号接收重置令牌，然后设置新密码</p>
      </div>

      <n-steps :current="step" size="small" class="steps">
        <n-step title="发送重置邮件" />
        <n-step title="设置新密码" />
      </n-steps>

      <n-form v-if="step === 1" label-placement="top" size="large" @submit.prevent="handleForgot">
        <n-form-item label="邮箱或手机号">
          <n-input v-model:value="form.identifier" placeholder="注册时使用的邮箱或手机号" clearable />
        </n-form-item>
        <n-button type="primary" block size="large" :loading="sending" attr-type="submit" @click="handleForgot">
          发送重置邮件
        </n-button>
      </n-form>

      <div v-else>
        <n-alert type="info" :bordered="false" class="sent-alert">
          如果账号存在，重置邮件已发出。请把邮件里的重置令牌粘贴到下方。
          <div class="sent-account">账号：{{ sentIdentifier }}</div>
        </n-alert>
        <n-form label-placement="top" size="large" @submit.prevent="handleReset">
          <n-form-item label="重置令牌">
            <n-input v-model:value="form.token" placeholder="邮件中的重置令牌" clearable />
          </n-form-item>
          <n-form-item label="新密码">
            <n-input
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              placeholder="至少 6 位字符"
            />
          </n-form-item>
          <n-form-item label="确认新密码">
            <n-input
              v-model:value="form.confirm"
              type="password"
              show-password-on="click"
              placeholder="再次输入新密码"
              @keyup.enter="handleReset"
            />
          </n-form-item>
          <n-button type="primary" block size="large" :loading="resetting" attr-type="submit" @click="handleReset">
            重置密码
          </n-button>
        </n-form>
        <div class="resend">
          没收到邮件？<n-button text type="primary" @click="backToStepOne">重新发送</n-button>
        </div>
      </div>

      <div class="auth-footer">
        想起来了？<router-link to="/login" class="auth-link">返回登录</router-link>
      </div>
    </n-card>

    <div class="auth-bottom">
      <router-link to="/">返回首页</router-link>
      <span class="sep">·</span>
      <router-link to="/register">免费注册</router-link>
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
  line-height: 1.7;
}
.steps {
  margin-bottom: 20px;
}
.sent-alert {
  margin-bottom: 16px;
}
.sent-account {
  margin-top: 6px;
  font-size: 12.5px;
  color: #334155;
}
.resend {
  margin-top: 14px;
  text-align: center;
  font-size: 12.5px;
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
