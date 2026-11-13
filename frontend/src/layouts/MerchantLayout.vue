<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  NAvatar,
  NButton,
  NDropdown,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NSpace,
  useMessage,
} from 'naive-ui';
import { useAuthStore } from '@/stores/auth';

const router = useRouter();
const route = useRoute();
const message = useMessage();
const auth = useAuthStore();
const collapsed = ref(false);

const menuOptions = [
  { label: '商户概览', key: '/merchant' },
  { label: '我的订单', key: '/merchant/orders' },
  { label: '退款记录', key: '/merchant/refunds' },
  { label: '结算记录', key: '/merchant/settlements' },
  { label: '代付申请', key: '/merchant/transfers' },
  { label: '余额充值', key: '/merchant/recharge' },
  { label: '实名认证', key: '/merchant/certification' },
  { label: '支付域名', key: '/merchant/domains' },
  { label: '邀请返佣', key: '/merchant/invite' },
  { label: '密钥管理', key: '/merchant/credentials' },
  { label: '账户资料', key: '/merchant/profile' },
  { label: '开发文档', key: '/doc' },
];

const activeKey = computed(() => (route.path === '/merchant/' ? '/merchant' : route.path));

const userOptions = [
  { label: '返回官网', key: 'home' },
  { label: '退出登录', key: 'logout' },
];

async function handleUserSelect(key: string) {
  if (key === 'logout') {
    await auth.logout();
    message.success('已退出登录');
    router.replace('/login');
    return;
  }
  router.push('/');
}

onMounted(() => {
  if (!auth.merchant) {
    auth.loadSession();
  }
});
</script>

<template>
  <n-layout has-sider class="merchant-shell">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="0"
      :width="210"
      :collapsed="collapsed"
      show-trigger="bar"
      @collapse="collapsed = true"
      @expand="collapsed = false"
    >
      <div class="brand">
        <div class="brand-mark">G</div>
        <div v-if="!collapsed" class="brand-text">
          <strong>GorixPay</strong>
          <span>商户中心</span>
        </div>
      </div>
      <n-menu
        :value="activeKey"
        :options="menuOptions"
        :collapsed="collapsed"
        :collapsed-width="64"
        @update:value="(key: string) => router.push(key)"
      />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="topbar">
        <n-space align="center">
          <n-button text @click="collapsed = !collapsed">☰</n-button>
          <div class="topbar-title">{{ route.meta.title ?? '商户中心' }}</div>
        </n-space>
        <n-dropdown :options="userOptions" trigger="click" @select="handleUserSelect">
          <n-space align="center" style="cursor: pointer">
            <n-avatar round size="small" color="#0ea5e9">{{ auth.displayName.slice(0, 1) }}</n-avatar>
            <span class="topbar-user">{{ auth.displayName }}</span>
          </n-space>
        </n-dropdown>
      </n-layout-header>
      <n-layout-content class="content" content-style="min-height: calc(100vh - 56px);">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
.merchant-shell {
  min-height: 100vh;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 18px 18px 12px;
}
.brand-mark {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: linear-gradient(135deg, #0ea5e9, #22c55e);
  color: #fff;
  font-weight: 700;
  display: grid;
  place-items: center;
  flex: none;
}
.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}
.brand-text span {
  font-size: 11px;
  color: #94a3b8;
}
.topbar {
  height: 56px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
}
.topbar-title {
  font-weight: 600;
  font-size: 15px;
}
.topbar-user {
  font-size: 13px;
  color: #475569;
}
.content {
  background: #f5f7fb;
}
</style>
