<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
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
  NTooltip,
  useMessage,
} from 'naive-ui';
import { useAuthStore } from '@/stores/auth';
import { siteLogo, siteName } from '@/stores/site';

const router = useRouter();
const route = useRoute();
const message = useMessage();
const auth = useAuthStore();
const collapsed = ref(false);
// 站点 Logo 是后台填的 URL，加载失败时退回默认的「G」方块
const logoBroken = ref(false);
watch(siteLogo, () => {
  logoBroken.value = false;
});

const menuOptions = [
  { label: '数据概览', key: '/admin' },
  { label: '报表', key: '/admin/reports' },
  { label: '商户管理', key: '/admin/merchants' },
  { label: '用户组与费率', key: '/admin/groups' },
  { label: '用户组购买', key: '/admin/group-purchases' },
  { label: '实名审核', key: '/admin/certifications' },
  { label: '域名审核', key: '/admin/domains' },
  { label: '订单管理', key: '/admin/orders' },
  { label: '支付渠道', key: '/admin/channels' },
  { label: '测试支付', key: '/admin/test-payment' },
  { label: '支付方式', key: '/admin/pay-types' },
  { label: '按日轮询', key: '/admin/pay-rolls' },
  { label: '公众号配置', key: '/admin/wechat-mp' },
  { label: '插件管理', key: '/admin/plugins' },
  { label: '退款管理', key: '/admin/refunds' },
  { label: '投诉管理', key: '/admin/complaints' },
  { label: '结算管理', key: '/admin/settlements' },
  { label: '结算规则', key: '/admin/settle-rules' },
  { label: '分账', key: '/admin/profit-sharing' },
  { label: '代付管理', key: '/admin/transfers' },
  { label: '返佣记录', key: '/admin/invites' },
  { label: '邀请码', key: '/admin/invite-codes' },
  { label: '风控与黑名单', key: '/admin/risk' },
  { label: '资金账本', key: '/admin/ledger' },
  { label: '消息队列', key: '/admin/outbox' },
  { label: '通知通道', key: '/admin/notification-channels' },
  { label: '设备与打印', key: '/admin/devices' },
  { label: '公告管理', key: '/admin/announcements' },
  { label: '安全设置', key: '/admin/security' },
  { label: '系统设置', key: '/admin/settings' },
  { label: '审计日志', key: '/admin/audit' },
  { label: '数据清理', key: '/admin/maintenance' },
];

const activeKey = computed(() => (route.path === '/admin/' ? '/admin' : route.path));

const userOptions = [
  { label: '返回官网', key: 'home' },
  { label: '退出登录', key: 'logout' },
];

async function handleUserSelect(key: string) {
  if (key === 'logout') {
    await auth.logout();
    message.success('已退出登录');
    router.replace('/admin/login');
    return;
  }
  router.push('/');
}

onMounted(() => {
  if (!auth.administrator) {
    auth.loadSession();
  }
});
</script>

<template>
  <n-layout has-sider class="admin-shell">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="0"
      :width="220"
      :collapsed="collapsed"
      show-trigger="bar"
      @collapse="collapsed = true"
      @expand="collapsed = false"
    >
      <div class="brand">
        <img
          v-if="siteLogo && !logoBroken"
          :src="siteLogo"
          :alt="siteName"
          class="brand-logo"
          @error="logoBroken = true"
        />
        <div v-else class="brand-mark">G</div>
        <div v-if="!collapsed" class="brand-text">
          <strong>{{ siteName }}</strong>
          <span>运营后台</span>
        </div>
      </div>
      <n-menu
        :value="activeKey"
        :options="menuOptions"
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="18"
        @update:value="(key: string) => router.push(key)"
      />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="topbar">
        <n-space align="center">
          <n-button text @click="collapsed = !collapsed">☰</n-button>
          <div class="topbar-title">{{ route.meta.title ?? '运营后台' }}</div>
        </n-space>
        <n-dropdown :options="userOptions" trigger="click" @select="handleUserSelect">
          <n-space align="center" style="cursor: pointer">
            <n-avatar round size="small" color="#2563eb">
              {{ auth.displayName.slice(0, 1) }}
            </n-avatar>
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
.admin-shell {
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
  background: linear-gradient(135deg, #2563eb, #06b6d4);
  color: #fff;
  font-weight: 700;
  display: grid;
  place-items: center;
  flex: none;
}
.brand-logo {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  object-fit: contain;
  background: #fff;
  flex: none;
}
.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
  min-width: 0;
}
.brand-text strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 140px;
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
