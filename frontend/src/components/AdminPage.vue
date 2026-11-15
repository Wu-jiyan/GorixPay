<script setup lang="ts">
import { computed } from 'vue';
import { NPageHeader, NDivider } from 'naive-ui';

withDefaults(
  defineProps<{
    title: string;
    subtitle?: string;
  }>(),
  { subtitle: '' },
);
</script>

<template>
  <div class="admin-page">
    <n-page-header :title="title" :subtitle="subtitle">
      <template #extra>
        <slot name="actions" />
      </template>
    </n-page-header>
    <n-divider style="margin: 14px 0 18px" />
    <slot />
  </div>
</template>

<style scoped>
/* 撑满内容区：以前这里限了 max-width:1440px，宽屏上右侧会空出一条没法用的白边。 */
.admin-page {
  width: 100%;
  min-width: 0;
}
</style>
