<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { merchantQrApi } from '@/api/merchantQrLogin';
import { errorMessage } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import BrandMark from '@/components/BrandMark.vue';

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
      <brand-mark :size="34" />
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
  padding: 40px var(--gp-gutter);
  background: var(--gp-bg);
}
.qr-brand {
  margin-bottom: 20px;
}
.qr-card {
  width: 100%;
  max-width: 460px;
  box-shadow: var(--gp-shadow);
  border-radius: var(--gp-radius-lg);
}
.qr-head {
  margin-bottom: 18px;
  text-align: center;
}
.qr-title {
  font-size: 22px;
  margin: 0 0 6px;
  color: var(--gp-text-1);
}
.qr-subtitle {
  margin: 0;
  font-size: 13px;
  color: var(--gp-text-3);
  line-height: 1.7;
}
.account-row,
.ticket-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 0;
  font-size: 13px;
  border-bottom: 1px dashed var(--gp-line);
}
.account-label {
  color: var(--gp-text-3);
  flex: none;
}
.account-name {
  color: var(--gp-text-1);
  font-weight: 600;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--gp-text-2);
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
    padding: 28px var(--gp-gutter);
  }
}
</style>
