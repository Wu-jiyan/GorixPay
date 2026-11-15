<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NButton, NMenu, NSpace } from 'naive-ui';
import type { MenuOption } from 'naive-ui';
import BrandMark from './BrandMark.vue';

// 官网统一页头：首页、开发文档、登录页共用同一份，避免各页各写一套导航。
const route = useRoute();
const router = useRouter();

const links: MenuOption[] = [
  { label: '首页', key: '/' },
  { label: '开发文档', key: '/doc' },
];

const active = computed(() => (route.path.startsWith('/doc') ? '/doc' : route.path === '/' ? '/' : ''));

function go(path: string) {
  router.push(path);
}
</script>

<template>
  <header class="public-nav">
    <div class="gp-container nav-inner">
      <brand-mark subtitle="聚合支付" />
      <n-menu class="nav-menu" mode="horizontal" responsive :value="active" :options="links" @update:value="go" />
      <n-space class="nav-actions" align="center" :size="8">
        <n-button quaternary @click="go('/login')">商户登录</n-button>
        <n-button type="primary" @click="go('/register')">免费注册</n-button>
      </n-space>
    </div>
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
}
@media (max-width: 720px) {
  .nav-inner {
    gap: 10px;
  }
  .nav-actions :deep(.n-button) {
    padding-left: 10px;
    padding-right: 10px;
  }
}
</style>
