<script setup lang="ts">
import { NMenu } from 'naive-ui';
import type { MenuOption } from 'naive-ui';
import BrandMark from './BrandMark.vue';

// 侧栏内容（品牌块 + 菜单）。后台与商户中心共用：桌面端塞进 n-layout-sider，
// 窄屏塞进 n-drawer，菜单定义只写一份。
withDefaults(
  defineProps<{
    options: MenuOption[];
    value: string;
    subtitle: string;
    collapsed?: boolean;
  }>(),
  { collapsed: false },
);

const emit = defineEmits<{
  (event: 'update:value', key: string): void;
}>();
</script>

<template>
  <div class="shell-menu">
    <div class="brand" :class="{ 'brand-collapsed': collapsed }">
      <brand-mark :size="32" :name="!collapsed" :subtitle="subtitle" />
    </div>
    <n-menu
      :value="value"
      :options="options"
      :collapsed="collapsed"
      :collapsed-width="64"
      :collapsed-icon-size="20"
      :indent="18"
      :root-indent="18"
      @update:value="(key: string) => emit('update:value', key)"
    />
  </div>
</template>

<style scoped>
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
</style>