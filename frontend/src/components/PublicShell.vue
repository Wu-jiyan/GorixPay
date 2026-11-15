<script setup lang="ts">
import { NLayout } from 'naive-ui';
import PublicNav from './PublicNav.vue';
import PublicFooter from './PublicFooter.vue';

// 官网外壳：页头 / 页脚 / 容器宽度只在这里定义，页面只管内容。
withDefaults(
  defineProps<{
    /** 是否套默认宽度容器（1200px）；整页自绘布局时关掉 */
    container?: boolean;
    /** 文档这类宽内容用 1320px */
    wide?: boolean;
  }>(),
  { container: true, wide: false },
);
</script>

<template>
  <n-layout class="public-shell">
    <public-nav />
    <div class="shell-body">
      <div v-if="container" class="gp-container" :class="{ 'is-wide': wide }">
        <slot />
      </div>
      <slot v-else />
    </div>
    <public-footer />
  </n-layout>
</template>

<style scoped>
.public-shell {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
.shell-body {
  flex: 1;
  background: var(--gp-bg);
}
.is-wide {
  max-width: 1320px;
}
</style>
