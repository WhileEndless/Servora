<script setup lang="ts">
import { computed } from "vue";
import type { Locale } from "@/composables/useI18n";

const props = defineProps<{
  mount: string;
  filesystem: string;
  used: number;
  available: number;
  total: number;
  percent: number;
  locale: Locale;
}>();

const normalized = computed(() => Math.max(0, Math.min(100, props.percent || 0)));
const severity = computed(() => normalized.value >= 90 ? "critical" : normalized.value >= 75 ? "warning" : "healthy");
const labels = computed(() => props.locale === "tr" ? {
  title: "DİSK DEPOLAMA", full: "dolu", system: "Sistem diski",
  used: "Kullanılan", available: "Kullanılabilir", total: "Toplam kapasite",
  healthy: "Sağlıklı", warning: "Dikkat", critical: "Kritik",
} : {
  title: "DISK STORAGE", full: "full", system: "System disk",
  used: "Used", available: "Available", total: "Total capacity",
  healthy: "Healthy", warning: "Warning", critical: "Critical",
});
function bytes(value: number): string {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const rank = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** rank).toFixed(rank >= 3 ? 1 : 0)} ${units[rank]}`;
}
</script>

<template>
  <article class="metric disk-capacity-card">
    <div class="disk-card-heading">
      <span>{{ labels.title }}</span>
      <b>{{ normalized.toFixed(1) }}% <small>{{ labels.full }}</small></b>
    </div>
    <div class="disk-identity">
      <strong>{{ labels.system }}</strong>
      <span><code>{{ mount || "—" }}</code><template v-if="filesystem"> · {{ filesystem }}</template></span>
    </div>
    <div
      class="disk-capacity-meter"
      :class="severity"
      role="meter"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-valuenow="normalized"
      :aria-valuetext="`${normalized.toFixed(1)}% ${labels.full}; ${bytes(used)} ${labels.used.toLowerCase()}, ${bytes(available)} ${labels.available.toLowerCase()}`"
    ><i :style="{ width: `${normalized}%` }" /></div>
    <div class="disk-capacity-stats">
      <div><small>{{ labels.used }}</small><b>{{ bytes(used) }}</b></div>
      <div><small>{{ labels.available }}</small><b>{{ bytes(available) }}</b></div>
      <div><small>{{ labels.total }}</small><b>{{ bytes(total) }}</b></div>
    </div>
    <span class="disk-health" :class="severity"><i />{{ labels[severity] }}</span>
  </article>
</template>
