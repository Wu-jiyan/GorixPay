<script setup lang="ts">
import { computed } from 'vue';
import { NTag } from 'naive-ui';
import { activeStatus, financeStatus, orderStatus } from '@/utils/format';

const props = withDefaults(
  defineProps<{
    status?: string;
    kind?: 'order' | 'finance' | 'active';
  }>(),
  { kind: 'order' },
);

const resolved = computed(() => {
  if (props.kind === 'finance') return financeStatus(props.status);
  if (props.kind === 'active') return activeStatus(props.status);
  return orderStatus(props.status);
});
</script>

<template>
  <n-tag :type="resolved.type" size="small" round>{{ resolved.text }}</n-tag>
</template>
