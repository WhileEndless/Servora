<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{ label: string; value: number; detail: string; color?: string }>();
const normalized = computed(() => Math.max(0, Math.min(100, props.value || 0)));
const dash = computed(() => `${normalized.value * 1.57} 157`);
</script>

<template>
  <article class="metric">
    <div><span>{{ label }}</span><b>{{ normalized.toFixed(1) }}%</b></div>
    <svg viewBox="0 0 120 66" aria-hidden="true">
      <path d="M14 57a48 48 0 0 1 92 0" pathLength="157" class="gauge-track" />
      <path d="M14 57a48 48 0 0 1 92 0" pathLength="157" class="gauge-value" :style="{ strokeDasharray: dash, stroke: color }" />
    </svg>
    <small>{{ detail }}</small>
  </article>
</template>
