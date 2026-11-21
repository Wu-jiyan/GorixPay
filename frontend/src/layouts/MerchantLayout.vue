<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref } from 'vue';
import type { Component } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  NAvatar,
  NButton,
  NBreadcrumb,
  NBreadcrumbItem,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  useMessage,
} from 'naive-ui';
import type { DropdownOption, MenuOption } from 'naive-ui';
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
import ShellMenu from '@/components/ShellMenu.vue';
import ChangePasswordModal from '@/components/ChangePasswordModal.vue';

const router = useRouter();
const route = useRoute();
const message = useMessage();
const auth = useAuthStore();
const collapsed = ref(false);
const passwordVisible = ref(false);

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

// 右上角账号菜单：改密码、账户资料、回官网、退出。
const userOptions: DropdownOption[] = [
  { label: '修改登录密码', key: 'password', icon: renderIcon(KeyOutline) },
  { label: '账户资料', key: 'profile', icon: renderIcon(PersonCircleOutline) },
  { label: '返回官网', key: 'home' },
  { type: 'divider', key: 'divider' },
  { label: '退出登录', key: 'logout' },
];

// 窄屏（≤992px）把侧栏收进抽屉：手机上固定侧栏会把内容挤到没法看。
const narrow = ref(false);
const drawerOpen = ref(false);
let mql: MediaQueryList | null = null;
const contentStyle = computed(() =>
  narrow.value ? 'padding: 14px 12px 32px; height: 100%;' : 'padding: 20px 24px 40px; height: 100%;',
);
function syncNarrow(event: MediaQueryListEvent) {
  narrow.value = event.matches;
  if (!event.matches) drawerOpen.value = false;
}
function toggleSider() {
  if (narrow.value) drawerOpen.value = !drawerOpen.value;
  else collapsed.value = !collapsed.value;
}
function navigate(key: string) {
  drawerOpen.value = false;
  router.push(key);
}

onMounted(() => {
  mql = window.matchMedia('(max-width: 992px)');
  narrow.value = mql.matches;
  mql.addEventListener('change', syncNarrow);
});
onUnmounted(() => mql?.removeEventListener('change', syncNarrow));

async function handleUserSelect(key: string) {
  if (key === 'password') {
    passwordVisible.value = true;
    return;
  }
  if (key === 'profile') {
    router.push('/merchant/profile');
    return;
  }
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
      v-if="!narrow"
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
      <shell-menu
        :options="menuOptions"
        :value="activeKey"
        :collapsed="collapsed"
        subtitle="商户中心"
        @update:value="navigate"
      />
    </n-layout-sider>

    <n-drawer v-if="narrow" v-model:show="drawerOpen" placement="left" :width="narrow ? '84vw' : 260">
      <n-drawer-content :native-scrollbar="false" body-content-style="padding: 0">
        <shell-menu :options="menuOptions" :value="activeKey" subtitle="商户中心" @update:value="navigate" />
      </n-drawer-content>
    </n-drawer>

    <n-layout style="height: 100vh">
      <n-layout-header bordered class="topbar">
        <n-space align="center" :size="12">
          <n-button quaternary circle size="small" @click="toggleSider">
            <template #icon>
              <n-icon :component="MenuOutline" />
            </template>
          </n-button>
          <n-breadcrumb v-if="!narrow">
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
        :content-style="contentStyle"
      >
        <router-view />
      </n-layout-content>
    </n-layout>

    <change-password-modal v-model:show="passwordVisible" scope="merchant" />
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
.topbar {
  height: 57px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.content {
  background: var(--gp-bg);
}
</style>
