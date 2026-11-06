<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{ label: string; value: string | number; icon: Component; tone?: 'green' | 'blue' | 'gold' | 'red' }>()
</script>

<template>
  <div class="stat-card">
    <span class="tile" :class="tone ?? 'blue'"><component :is="icon" :size="20" /></span>
    <span class="label">{{ label }}</span>
    <span class="value">{{ value }}</span>
  </div>
</template>

<style scoped>
.stat-card {
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}
.tile {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  flex: none;
}
.tile.green {
  background: rgba(52, 211, 153, 0.12);
  color: #34d399;
}
.tile.blue {
  background: rgba(96, 165, 250, 0.14);
  color: #60a5fa;
}
.tile.gold {
  background: rgba(230, 199, 122, 0.14);
  color: var(--accent);
}
.tile.red {
  background: rgba(248, 113, 113, 0.12);
  color: #f87171;
}
.label {
  color: var(--muted);
  font-size: 13px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.value {
  font-size: 24px;
  font-weight: 700;
}
@media (max-width: 720px) {
  .stat-card {
    padding: 10px 12px;
    gap: 10px;
  }
  .tile {
    width: 32px;
    height: 32px;
  }
  .label {
    white-space: normal;
    font-size: 12px;
    line-height: 1.25;
  }
  .value {
    font-size: 20px;
  }
}
</style>
