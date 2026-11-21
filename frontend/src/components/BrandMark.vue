<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useThemeVars } from 'naive-ui';
import { siteLogo, siteName } from '@/stores/site';

// 站点标识：后台填了 Logo 用 Logo，没填就用站点名首字 + 主题色的方块。
// 只在 url 出错时退回文字块，避免出现"图片裂了"的空框。
const props = withDefaults(
  defineProps<{
    size?: number;
    name?: boolean;
    subtitle?: string;
    to?: string;
  }>(),
  { size: 32, name: true, subtitle: '', to: '/' },
);

const themeVars = useThemeVars();
const broken = ref(false);
watch(siteLogo, () => {
  broken.value = false;
});

const initial = computed(() => (siteName.value || 'G').slice(0, 1).toUpperCase());
const boxStyle = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
  borderRadius: `${Math.round(props.size * 0.28)}px`,
  background: themeVars.value.primaryColor,
  fontSize: `${Math.round(props.size * 0.48)}px`,
}));
const imgStyle = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
  borderRadius: `${Math.round(props.size * 0.28)}px`,
}));
</script>

<template>
  <router-link :to="to" class="brand-mark">
    <img
      v-if="siteLogo && !broken"
      :src="siteLogo"
      :alt="siteName"
      class="brand-mark-img"
      :style="imgStyle"
      @error="broken = true"
    />
    <span v-else class="brand-mark-tile" :style="boxStyle">{{ initial }}</span>
    <span v-if="name" class="brand-mark-text">
      <span class="brand-mark-name">{{ siteName }}</span>
      <span v-if="subtitle" class="brand-mark-sub">{{ subtitle }}</span>
    </span>
  </router-link>
</template>

<style scoped>
.brand-mark {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  /* 不让页头的导航把它挤成 "Go…"，站点名最多两三个词，永远放得下 */
  flex: none;
  color: inherit;
}
.brand-mark-img {
  object-fit: contain;
  background: var(--gp-surface);
  display: block;
  flex: none;
}
.brand-mark-tile {
  display: grid;
  place-items: center;
  color: var(--gp-on-accent);
  font-weight: 600;
  flex: none;
  line-height: 1;
}
.brand-mark-text {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
  min-width: 0;
}
.brand-mark-name {
  font-size: 16px;
  font-weight: 600;
  white-space: nowrap;
}
.brand-mark-sub {
  font-size: 12px;
  color: var(--gp-text-3);
  white-space: nowrap;
}
</style>
