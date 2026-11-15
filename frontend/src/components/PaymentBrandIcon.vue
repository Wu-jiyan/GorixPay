<script setup lang="ts">
import { computed } from 'vue';
import { NIcon } from 'naive-ui';
import { CardOutline, FlaskOutline, StorefrontOutline } from '@vicons/ionicons5';
import { brandIcons, type BrandIconKey } from './brandIcons';

// 支付方式的品牌图标。支付宝 / 微信 / QQ / USDT 用 simple-icons 的真实品牌路径，
// 银行卡、收银台、沙箱这类没有品牌的用 ionicons 顶上，颜色仍取品牌色，视觉上是一套。
const props = withDefaults(
  defineProps<{
    type?: string;
    size?: number;
    /** false = 单色（跟随父级颜色），用于按钮里的图标 */
    colored?: boolean;
  }>(),
  { type: '', size: 24, colored: true },
);

const BRAND_OF: Record<string, BrandIconKey> = {
  alipay: 'alipay',
  wxpay: 'wechat',
  wechat: 'wechat',
  qqpay: 'qq',
  qq: 'qq',
  usdt: 'tether',
  tether: 'tether',
  btc: 'bitcoin',
  visa: 'visa',
  mastercard: 'mastercard',
};

const FALLBACK: Record<string, { icon: typeof CardOutline; hex: string }> = {
  bank: { icon: CardOutline, hex: '334155' },
  cashier: { icon: StorefrontOutline, hex: '0ea5e9' },
  sandbox: { icon: FlaskOutline, hex: 'f59e0b' },
};

const key = computed<BrandIconKey | null>(() => BRAND_OF[props.type] ?? null);
const brand = computed(() => (key.value ? brandIcons[key.value] : null));
const fallback = computed(() => FALLBACK[props.type] ?? { icon: CardOutline, hex: '94a3b8' });
const fill = computed(() => (props.colored ? `#${brand.value?.hex ?? fallback.value.hex}` : 'currentColor'));
</script>

<template>
  <svg
    v-if="brand"
    class="brand-icon"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    role="img"
    :aria-label="brand.title"
  >
    <title>{{ brand.title }}</title>
    <path :d="brand.path" :fill="fill" />
  </svg>
  <n-icon v-else :size="size" :component="fallback.icon" :color="colored ? `#${fallback.hex}` : undefined" />
</template>

<style scoped>
.brand-icon {
  display: block;
  flex: none;
}
</style>
