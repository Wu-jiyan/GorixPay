<script setup lang="ts">
import { computed } from 'vue';
import { formatMoney } from '@/utils/format';

const props = withDefaults(
  defineProps<{
    amount?: number | null;
    symbol?: boolean;
    strong?: boolean;
    color?: string;
  }>(),
  { symbol: true, strong: true },
);

const text = computed(() => formatMoney(props.amount, props.symbol));
</script>

<template>
  <span class="amount" :style="{ color: color, fontWeight: strong ? 600 : 400 }">{{ text }}</span>
</template>
