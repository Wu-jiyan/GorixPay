<script setup lang="ts">
import { onMounted } from 'vue';
import { darkTheme, zhCN, dateZhCN, type GlobalThemeOverrides } from 'naive-ui';
import { loadSite, siteSkin } from '@/stores/site';

onMounted(() => {
  void loadSite();
});

const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#2563eb',
    primaryColorHover: '#3b82f6',
    primaryColorPressed: '#1d4ed8',
    primaryColorSuppl: '#3b82f6',
    borderRadius: '8px',
    fontFamily:
      '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
  },
  Button: {
    borderRadiusMedium: '8px',
  },
  Card: {
    borderRadius: '14px',
  },
};
</script>

<template>
  <n-config-provider
    :locale="zhCN"
    :date-locale="dateZhCN"
    :theme="siteSkin === 'dark' ? darkTheme : null"
    :theme-overrides="themeOverrides"
  >
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <div class="app-shell" :class="`skin-${siteSkin}`">
            <router-view />
          </div>
        </n-notification-provider>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
