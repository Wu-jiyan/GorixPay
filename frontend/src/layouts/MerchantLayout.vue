<script setup lang="ts">
import { computed, h, ref, watch } from 'vue';
import type { Component } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  NAvatar,
  NButton,
  NBreadcrumb,
  NBreadcrumbItem,
  NDropdown,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NSpace,
  NText,
  useMessage,
} from 'naive-ui';
import type { MenuOption } from 'naive-ui';
import {
  BookOutline,
  CardOutline,
  CashOutline,
  ChatboxEllipsesOutline,
  ChevronDownOutline,
  GiftOutline,
  GitCompareOutline,
  GlobeOutline,
  IdCardOutline,
  KeyOutline,
  LinkOutline,
  MenuOutline,
  PaperPlaneOutline,
  PersonCircleOutline,
  PricetagsOutline,
  QrCodeOutline,
  ReceiptOutline,
  ReturnDownBackOutline,
  SpeedometerOutline,
  WalletOutline,
} from '@vicons/ionicons5';
import { useAuthStore } from '@/stores/auth';
import { siteLogo, siteName } from '@/stores/site';

const router = useRouter();
const route = useRoute();
const message = useMessage();
const auth = useAuthStore();
const collapsed = ref(false);
// 站点 Logo 是后台填的 URL，加载失败时退回站点名首字
const logoBroken = ref(false);
watch(siteLogo, () => {
  logoBroken.value = false;
});

const renderIcon = (icon: Component) => () => h(NIcon, null, { default: () => h(icon) });

const groups: Array<{ key: string; label: string; items: Array<[string, string, Component]> }> = [
  {
    key: 'overview',
    label: '概览',
    items: [['/merchant', '商户概览', SpeedometerOutline]],
  },
  {
    key: 'trade',
    label: '交易',
    items: [
      ['/merchant/orders', '我的订单', ReceiptOutline],
      ['/merchant/refunds', '退款记录', ReturnDownBackOutline],
      ['/merchant/complaints', '我的投诉', ChatboxEllipsesOutline],
      ['/merchant/pay-code', '收款码', QrCodeOutline],
    ],
  },
  {
    key: 'fund',
    label: '资金',
    items: [
      ['/merchant/records', '资金明细', WalletOutline],
      ['/merchant/settlements', '结算记录', CashOutline],
      ['/merchant/transfers', '代付申请', PaperPlaneOutline],
      ['/merchant/profit-sharing', '分账管理', GitCompareOutline],
      ['/merchant/red-packet', '红包', GiftOutline],
      ['/merchant/recharge', '余额充值', CardOutline],
    ],
  },
  {
    key: 'account',
    label: '账号',
    items: [
      ['/merchant/group-plans', '用户组', PricetagsOutline],
      ['/merchant/certification', '实名认证', IdCardOutline],
      ['/merchant/domains', '支付域名', GlobeOutline],
      ['/merchant/invite', '邀请返佣', LinkOutline],
      ['/merchant/credentials', '密钥管理', KeyOutline],
      ['/merchant/profile', '账户资料', PersonCircleOutline],
    ],
  },
  {
    key: 'help',
    label: '帮助',
    items: [['/doc', '开发文档', BookOutline]],
  },
];

const menuOptions: MenuOption[] = groups.map((group) => ({
  type: 'group',
  key: group.key,
  label: group.label,
  children: group.items.map(([key, label, icon]) => ({ key, label, icon: renderIcon(icon) })),
}));

const routeIndex = computed(() => {
  const index = new Map<string, { group: string; label: string }>();
  for (const group of groups) {
    for (const [key, label] of group.items) {
      index.set(key, { group: group.label, label });
    }
  }
  return index;
});

const activeKey = computed(() => (route.path === '/merchant/' ? '/merchant' : route.path));
const current = computed(() => routeIndex.value.get(activeKey.value));
const headerTitle = computed(() => current.value?.label ?? (route.meta.title as string | undefined) ?? '商户中心');

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
</script>

<template>
  <n-layout has-sider class="merchant-shell">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="216"
      :collapsed="collapsed"
      :native-scrollbar="false"
      content-style="height: 100%; padding-bottom: 12px;"
      show-trigger="bar"
      @collapse="collapsed = true"
      @expand="collapsed = false"
    >
      <div class="brand" :class="{ 'brand-collapsed': collapsed }">
        <img
          v-if="siteLogo && !logoBroken"
          :src="siteLogo"
          :alt="siteName"
          class="brand-logo"
          @error="logoBroken = true"
        />
        <div v-else class="brand-logo brand-logo-text">{{ (siteName || 'G').slice(0, 1) }}</div>
        <div v-if="!collapsed" class="brand-text">
          <n-text strong>{{ siteName }}</n-text>
          <n-text depth="3" class="brand-sub">商户中心</n-text>
        </div>
      </div>
      <n-menu
        :value="activeKey"
        :options="menuOptions"
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="20"
        :indent="18"
        :root-indent="18"
        @update:value="(key: string) => router.push(key)"
      />
    </n-layout-sider>

    <n-layout style="height: 100vh">
      <n-layout-header bordered class="topbar">
        <n-space align="center" :size="12">
          <n-button quaternary circle size="small" @click="collapsed = !collapsed">
            <template #icon>
              <n-icon :component="MenuOutline" />
            </template>
          </n-button>
          <n-breadcrumb>
            <n-breadcrumb-item>商户中心</n-breadcrumb-item>
            <n-breadcrumb-item v-if="current?.group">{{ current.group }}</n-breadcrumb-item>
            <n-breadcrumb-item>{{ headerTitle }}</n-breadcrumb-item>
          </n-breadcrumb>
        </n-space>

        <n-dropdown :options="userOptions" trigger="click" @select="handleUserSelect">
          <n-space align="center" :size="8" style="cursor: pointer">
            <n-avatar round size="small">{{ auth.displayName.slice(0, 1) }}</n-avatar>
            <n-text depth="2">{{ auth.displayName }}</n-text>
            <n-icon :component="ChevronDownOutline" size="14" />
          </n-space>
        </n-dropdown>
      </n-layout-header>

      <n-layout-content
        class="content"
        :native-scrollbar="false"
        style="height: calc(100vh - 57px)"
        content-style="padding: 20px 24px 40px; height: 100%;"
      >
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
.merchant-shell {
  /* 整个外壳固定一屏高：侧栏自己滚，内容区自己滚 */
  height: 100vh;
  overflow: hidden;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 18px 12px;
}
.brand-collapsed {
  justify-content: center;
  padding: 16px 0 12px;
}
.brand-logo {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  object-fit: contain;
  background: #fff;
  flex: none;
}
.brand-logo-text {
  display: grid;
  place-items: center;
  background: #18a058;
  color: #fff;
  font-weight: 600;
  font-size: 16px;
}
.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
  min-width: 0;
}
.brand-sub {
  font-size: 12px;
}
.topbar {
  height: 57px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.content {
  background: var(--n-color-embedded, #f5f7fa);
}
</style>
