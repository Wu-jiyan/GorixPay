<script setup lang="ts">
import { computed, h, ref } from 'vue';
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
  useMessage,
} from 'naive-ui';
import type { DropdownOption, MenuOption } from 'naive-ui';
import {
  AlbumsOutline,
  BagHandleOutline,
  BuildOutline,
  CashOutline,
  ChatboxEllipsesOutline,
  ChatbubbleEllipsesOutline,
  ChevronDownOutline,
  DocumentTextOutline,
  ExtensionPuzzleOutline,
  FlaskOutline,
  GitCompareOutline,
  GitNetworkOutline,
  GlobeOutline,
  IdCardOutline,
  KeyOutline,
  LibraryOutline,
  LinkOutline,
  LockClosedOutline,
  MegaphoneOutline,
  MenuOutline,
  NotificationsOutline,
  OptionsOutline,
  PaperPlaneOutline,
  PeopleOutline,
  PricetagsOutline,
  PrintOutline,
  QrCodeOutline,
  ReceiptOutline,
  RepeatOutline,
  ReturnDownBackOutline,
  SettingsOutline,
  ShieldCheckmarkOutline,
  SpeedometerOutline,
  StatsChartOutline,
  WalletOutline,
} from '@vicons/ionicons5';
import { useAuthStore } from '@/stores/auth';
import BrandMark from '@/components/BrandMark.vue';
import ChangePasswordModal from '@/components/ChangePasswordModal.vue';

const router = useRouter();
const route = useRoute();
const message = useMessage();
const auth = useAuthStore();
const collapsed = ref(false);
const passwordVisible = ref(false);

const renderIcon = (icon: Component) => () => h(NIcon, null, { default: () => h(icon) });

// 菜单按业务分七组：平铺 32 项时用户找不到东西，分组后每组 2-8 项。
const groups: Array<{ key: string; label: string; icon: Component; items: Array<[string, string, Component]> }> = [
  {
    key: 'overview',
    label: '概览',
    icon: SpeedometerOutline,
    items: [
      ['/admin', '数据总览', SpeedometerOutline],
      ['/admin/reports', '报表', StatsChartOutline],
    ],
  },
  {
    key: 'merchant',
    label: '商户',
    icon: PeopleOutline,
    items: [
      ['/admin/merchants', '商户管理', PeopleOutline],
      ['/admin/groups', '用户组与费率', PricetagsOutline],
      ['/admin/group-purchases', '用户组购买', BagHandleOutline],
      ['/admin/certifications', '实名审核', IdCardOutline],
      ['/admin/domains', '域名审核', GlobeOutline],
    ],
  },
  {
    key: 'trade',
    label: '交易',
    icon: ReceiptOutline,
    items: [
      ['/admin/orders', '订单管理', ReceiptOutline],
      ['/admin/refunds', '退款管理', ReturnDownBackOutline],
      ['/admin/complaints', '投诉管理', ChatboxEllipsesOutline],
      ['/admin/channels', '支付渠道', GitNetworkOutline],
      ['/admin/pay-types', '支付方式', WalletOutline],
      ['/admin/pay-rolls', '按日轮询', RepeatOutline],
      ['/admin/test-payment', '测试支付', FlaskOutline],
      ['/admin/plugins', '插件管理', ExtensionPuzzleOutline],
    ],
  },
  {
    key: 'fund',
    label: '资金',
    icon: CashOutline,
    items: [
      ['/admin/settlements', '结算管理', CashOutline],
      ['/admin/settle-rules', '结算规则', OptionsOutline],
      ['/admin/profit-sharing', '分账', GitCompareOutline],
      ['/admin/transfers', '代付管理', PaperPlaneOutline],
      ['/admin/ledger', '资金账本', LibraryOutline],
      ['/admin/invites', '返佣记录', LinkOutline],
      ['/admin/invite-codes', '邀请码', QrCodeOutline],
    ],
  },
  {
    key: 'risk',
    label: '风控',
    icon: ShieldCheckmarkOutline,
    items: [
      ['/admin/risk', '风控与黑名单', ShieldCheckmarkOutline],
      ['/admin/security', '安全设置', LockClosedOutline],
    ],
  },
  {
    key: 'message',
    label: '消息',
    icon: NotificationsOutline,
    items: [
      ['/admin/wechat-mp', '公众号配置', ChatbubbleEllipsesOutline],
      ['/admin/notification-channels', '通知通道', NotificationsOutline],
      ['/admin/devices', '设备与打印', PrintOutline],
      ['/admin/outbox', '消息队列', AlbumsOutline],
      ['/admin/announcements', '公告管理', MegaphoneOutline],
    ],
  },
  {
    key: 'system',
    label: '系统',
    icon: SettingsOutline,
    items: [
      ['/admin/audit', '审计日志', DocumentTextOutline],
      ['/admin/maintenance', '数据清理', BuildOutline],
      ['/admin/settings', '系统设置', SettingsOutline],
    ],
  },
];

const menuOptions: MenuOption[] = groups.map((group) => ({
  type: 'group',
  key: group.key,
  label: group.label,
  children: group.items.map(([key, label, icon]) => ({ key, label, icon: renderIcon(icon) })),
}));

// 路径 → 面包屑（分组名 + 页面名），菜单是唯一事实来源
const routeIndex = computed(() => {
  const index = new Map<string, { group: string; label: string }>();
  for (const group of groups) {
    for (const [key, label] of group.items) {
      index.set(key, { group: group.label, label });
    }
  }
  return index;
});

const activeKey = computed(() => (route.path === '/admin/' ? '/admin' : route.path));
const current = computed(() => routeIndex.value.get(activeKey.value));
const headerTitle = computed(() => current.value?.label ?? (route.meta.title as string | undefined) ?? '运营后台');

// 右上角账号菜单：改密码、回官网、退出。改密码走弹窗，不打断当前页面。
const userOptions: DropdownOption[] = [
  { label: '修改登录密码', key: 'password', icon: renderIcon(KeyOutline) },
  { label: '返回官网', key: 'home' },
  { type: 'divider', key: 'divider' },
  { label: '退出登录', key: 'logout' },
];

async function handleUserSelect(key: string) {
  if (key === 'password') {
    passwordVisible.value = true;
    return;
  }
  if (key === 'logout') {
    await auth.logout();
    message.success('已退出登录');
    router.replace('/admin/login');
    return;
  }
  router.push('/');
}
</script>

<template>
  <n-layout has-sider class="admin-shell">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="228"
      :collapsed="collapsed"
      :native-scrollbar="false"
      content-style="height: 100%; padding-bottom: 12px;"
      show-trigger="bar"
      @collapse="collapsed = true"
      @expand="collapsed = false"
    >
      <div class="brand" :class="{ 'brand-collapsed': collapsed }">
        <brand-mark :size="32" :name="!collapsed" subtitle="运营后台" />
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
            <n-breadcrumb-item>运营后台</n-breadcrumb-item>
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

    <change-password-modal v-model:show="passwordVisible" />
  </n-layout>
</template>

<style scoped>
.admin-shell {
  /* 整个外壳固定一屏高：侧栏自己滚，内容区自己滚，互不撑高 */
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
