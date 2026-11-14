<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { merchantQrApi } from '@/api/merchantQrLogin';
import { errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import { siteLogo, siteName } from '@/stores/site';

const route = useRoute();
const router = useRouter();
const message = useMessage();
const auth = useAuthStore();

const ticket = computed(() => {
  const raw = route.query.ticket;
  const value = Array.isArray(raw) ? raw[0] : raw;
  return typeof value === 'string' ? value.trim() : '';
});

const confirming = ref(false);
const confirmed = ref(false);

async function handleConfirm() {
  if (confirming.value || confirmed.value) return;
  if (!ticket.value) {
    message.error('缺少 ticket 参数，请重新扫码');
    return;
  }
  if (!auth.isLoggedIn) {
    message.warning('请先登录商户账号再确认');
    return;
  }
  confirming.value = true;
  try {
    await merchantQrApi.confirm(ticket.value);
    confirmed.value = true;
    message.success('已确认登录，请回到电脑上继续');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    confirming.value = false;
  }
}
</script>

<template>
  <div class="qr-page">
    <div class="qr-brand">
      <router-link to="/" class="brand-link">
        <img v-if="siteLogo" :src="siteLogo" class="brand-logo" alt="" />
        <span v-else class="brand-mark">G</span>
        <span class="brand-name">{{ siteName }}</span>
      </router-link>
    </div>

    <n-card class="qr-card" :bordered="false">
      <div class="qr-head">
        <h1 class="qr-title">扫码登录确认</h1>
        <p class="qr-subtitle">在已登录的商户账号下确认，电脑端会自动完成登录</p>
      </div>

      <n-alert v-if="!ticket" type="error" :bordered="false">
        缺少 ticket 参数，请回到电脑上重新获取二维码后再扫码。
      </n-alert>

      <template v-else-if="!auth.isLoggedIn">
        <n-alert type="warning" :bordered="false">
          请先登录商户账号，登录完成后回到本页确认这次扫码登录。
        </n-alert>
        <div class="qr-actions">
          <n-button type="primary" @click="router.push('/login')">去登录</n-button>
        </div>
      </template>

      <template v-else-if="confirmed">
        <n-result status="success" title="已确认登录" description="请回到电脑上查看，页面会自动进入商户中心。">
          <template #footer>
            <n-button @click="router.push('/merchant')">进入商户中心</n-button>
          </template>
        </n-result>
      </template>

      <template v-else>
        <div class="account-row">
          <span class="account-label">当前账号</span>
          <span class="account-name">{{ auth.displayName }}</span>
        </div>
        <div class="ticket-row">
          <span class="account-label">登录票据</span>
          <span class="mono">{{ ticket }}</span>
        </div>
        <n-alert type="info" :bordered="false" class="qr-hint">
          确认后电脑端会立即使用当前账号登录；如果这不是你发起的登录，请直接关闭本页。
        </n-alert>
        <div class="qr-actions">
          <n-button type="primary" size="large" :loading="confirming" @click="handleConfirm">
            确认登录
          </n-button>
        </div>
      </template>
    </n-card>
  </div>
</template>

<style scoped>
.qr-page {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  background: radial-gradient(900px 420px at 12% 0%, #dbeafe 0%, transparent 62%),
    linear-gradient(160deg, #f8fbff 0%, #eef4ff 60%, #f5f7fb 100%);
}
.qr-brand {
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
.qr-card {
  width: 100%;
  max-width: 460px;
  box-shadow: 0 18px 48px rgba(15, 23, 42, 0.1);
  border-radius: 16px;
}
.qr-head {
  margin-bottom: 18px;
  text-align: center;
}
.qr-title {
  font-size: 22px;
  margin: 0 0 6px;
  color: #0f172a;
}
.qr-subtitle {
  margin: 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.7;
}
.account-row,
.ticket-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 0;
  font-size: 13px;
  border-bottom: 1px dashed #e2e8f0;
}
.account-label {
  color: #94a3b8;
  flex: none;
}
.account-name {
  color: #0f172a;
  font-weight: 600;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: #334155;
  word-break: break-all;
  text-align: right;
}
.qr-hint {
  margin-top: 16px;
}
.qr-actions {
  margin-top: 18px;
  display: flex;
  justify-content: center;
}
@media (max-width: 768px) {
  .qr-page {
    justify-content: flex-start;
    padding: 28px 14px;
  }
}
</style>
