<script setup lang="ts">
import { computed } from "vue";
import type { MetricPoint } from "@/types";

const props = defineProps<{ points: readonly MetricPoint[] }>();
const width = 900;
const height = 210;
function pathFor(key: "cpu" | "memory"): string {
  if (props.points.length < 2) return "";
  return props.points.map((point, index) => {
    const x = index * width / (props.points.length - 1);
    const y = height - Math.min(100, Math.max(0, point[key])) * height / 100;
    return `${index ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
}
const cpuPath = computed(() => pathFor("cpu"));
const memoryPath = computed(() => pathFor("memory"));
</script>

<template>
  <div class="chart">
    <svg :viewBox="`0 0 ${width} ${height}`" preserveAspectRatio="none">
      <line v-for="line in 5" :key="line" x1="0" :x2="width" :y1="line * 42" :y2="line * 42" />
      <path :d="memoryPath" class="memory-line" />
      <path :d="cpuPath" class="cpu-line" />
    </svg>
  </div>
</template>
