<script setup lang="ts">
defineProps<{
  label: string;
  value: string | number;
  hint?: string;
  accent?: string;
}>();
</script>

<template>
  <n-card :bordered="false" class="stat-card">
    <div class="stat-label">{{ label }}</div>
    <div class="stat-value" :style="{ color: accent }">{{ value }}</div>
    <div v-if="hint" class="stat-hint">{{ hint }}</div>
  </n-card>
</template>

<style scoped>
.stat-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.stat-label {
  color: #6b7280;
  font-size: 13px;
  margin-bottom: 8px;
}
.stat-value {
  font-size: 26px;
  font-weight: 700;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}
.stat-hint {
  margin-top: 6px;
  color: #9ca3af;
  font-size: 12px;
}
</style>
