<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NButton, NDrawer, NDrawerContent, NIcon, NMenu, NSpace, NText } from 'naive-ui';
import type { MenuOption } from 'naive-ui';
import { CloseOutline, MenuOutline } from '@vicons/ionicons5';
import BrandMark from './BrandMark.vue';

// 官网统一页头：首页、文档、支付测试、登录页共用同一份。
// 窄屏不再让菜单挤成「…」，改成汉堡 + 抽屉，动作按钮留在抽屉里。
const route = useRoute();
const router = useRouter();

const links: MenuOption[] = [
  { label: '首页', key: '/' },
  { label: '开发文档', key: '/doc' },
  { label: '支付测试', key: '/test' },
];

const active = computed(() => {
  if (route.path.startsWith('/doc')) return '/doc';
  if (route.path.startsWith('/test')) return '/test';
  return route.path === '/' ? '/' : '';
});

const narrow = ref(false);
const drawerOpen = ref(false);
let mql: MediaQueryList | null = null;

function syncNarrow(event: MediaQueryListEvent) {
  narrow.value = event.matches;
  if (!event.matches) drawerOpen.value = false;
}

function go(path: string) {
  drawerOpen.value = false;
  router.push(path);
}

onMounted(() => {
  mql = window.matchMedia('(max-width: 900px)');
  narrow.value = mql.matches;
  mql.addEventListener('change', syncNarrow);
});
onUnmounted(() => mql?.removeEventListener('change', syncNarrow));
</script>

<template>
  <header class="public-nav">
    <div class="gp-container nav-inner">
      <brand-mark subtitle="聚合支付" />

      <n-menu
        v-if="!narrow"
        class="nav-menu"
        mode="horizontal"
        :value="active"
        :options="links"
        @update:value="go"
      />

      <n-space v-if="!narrow" class="nav-actions" align="center" :size="8">
        <n-button quaternary @click="go('/login')">商户登录</n-button>
        <n-button type="primary" @click="go('/register')">免费注册</n-button>
      </n-space>

      <n-space v-else class="nav-actions" align="center" :size="6">
        <n-button type="primary" size="small" @click="go('/register')">注册</n-button>
        <n-button quaternary circle size="small" @click="drawerOpen = true">
          <template #icon>
            <n-icon :component="drawerOpen ? CloseOutline : MenuOutline" />
          </template>
        </n-button>
      </n-space>
    </div>

    <n-drawer v-model:show="drawerOpen" :width="narrow ? '86vw' : 320" placement="right">
      <n-drawer-content closable>
        <template #header>
          <brand-mark :size="26" :name="true" />
        </template>
        <n-menu :value="active" :options="links" :indent="16" :root-indent="16" @update:value="go" />
        <template #footer>
          <n-space vertical :size="10" style="width: 100%">
            <n-button block @click="go('/login')">商户登录</n-button>
            <n-button block type="primary" @click="go('/register')">免费注册</n-button>
            <n-text depth="3" style="font-size: 12px">运营入口：/admin/login</n-text>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </header>
</template>

<style scoped>
.public-nav {
  position: sticky;
  top: 0;
  z-index: 20;
  background: var(--gp-surface);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid var(--gp-line);
}
.nav-inner {
  height: 60px;
  display: flex;
  align-items: center;
  gap: 24px;
}
.nav-menu {
  flex: 1;
  min-width: 0;
}
.nav-actions {
  flex: none;
  /* 动作永远贴右边：窄屏时品牌和汉堡之间会留出一大段空白，不靠这个就挤在左边。 */
  margin-left: auto;
}
@media (max-width: 900px) {
  .nav-inner {
    gap: 10px;
    height: 56px;
  }
}
/* 手机上副标题只会把品牌挤窄，站点名本身已经够说明是谁了。 */
@media (max-width: 520px) {
  .public-nav :deep(.brand-mark-sub) {
    display: none;
  }
  .public-nav :deep(.brand-mark-name) {
    font-size: 15px;
  }
}
</style>