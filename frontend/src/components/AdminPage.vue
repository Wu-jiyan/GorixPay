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
    <n-divider class="admin-page-divider" />
    <slot />
  </div>
</template>

<style scoped>
/* 撑满内容区：以前这里限了 max-width:1440px，宽屏上右侧会空出一条没法用的白边。 */
.admin-page {
  width: 100%;
  min-width: 0;
}
/* 页头分割线的上下留白，从模板里的行内 style 挪到这里 */
.admin-page-divider {
  margin: 14px 0 18px;
}
/* 手机上标题会被副标题挤成两行（「订单 / 管理」），副标题这时只是噪音，直接收掉。 */
@media (max-width: 768px) {
  .admin-page :deep(.n-page-header__subtitle) {
    display: none;
  }
  .admin-page :deep(.n-page-header) {
    flex-wrap: wrap;
    gap: 10px;
  }
  .admin-page :deep(.n-page-header__extra) {
    width: 100%;
  }
}
</style>
