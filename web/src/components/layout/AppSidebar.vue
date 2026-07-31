<script setup lang="ts">
import { computed } from "vue";
import type { PageName } from "@/types";
import { useI18n, type MessageKey } from "@/composables/useI18n";
import { monitorStore } from "@/services/MonitorStore";

defineProps<{ page: PageName; open?: boolean }>();
const emit = defineEmits<{ navigate: [page: PageName]; close: [] }>();
const { t } = useI18n();
const cpu = computed(() => monitorStore.snapshot.value.cpu.usage);
const memory = computed(() => {
  const value = monitorStore.snapshot.value.memory;
  return value.Total ? value.Used * 100 / value.Total : 0;
});
const memoryAmount = computed(() => {
  const value = monitorStore.snapshot.value.memory;
  return `${bytes(value.Used)} / ${bytes(value.Total)}`;
});
const network = computed(() => monitorStore.snapshot.value.network.reduce((sum, item) =>
  sum + Number(item.RXRate || 0) + Number(item.TXRate || 0), 0));
const uptime = computed(() => {
  const seconds = monitorStore.snapshot.value.uptime_seconds;
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor(seconds % 86_400 / 3_600);
  return days ? `${days}d ${hours}h` : `${hours}h`;
});
function rate(value: number): string {
  if (value >= 1e9) return `${(value / 1e9).toFixed(1)} GB/s`;
  if (value >= 1e6) return `${(value / 1e6).toFixed(1)} MB/s`;
  return `${(value / 1e3).toFixed(0)} KB/s`;
}
function bytes(value: number): string {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)}G`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(0)}M`;
  return `${(value / 1024).toFixed(0)}K`;
}

const items: { page: PageName; icon: string; label?: MessageKey; text?: string }[] = [
  { page: "overview", icon: "◫", label: "overview" },
  { page: "processes", icon: "⌁", label: "processes" },
  { page: "services", icon: "◆", label: "services" },
  { page: "docker", icon: "▣", text: "Docker" },
  { page: "packages", icon: "⬡", label: "packages" },
  { page: "network", icon: "⌘", label: "network" },
  { page: "ssh", icon: "›_", text: "SSH" },
  { page: "schedules", icon: "◷", label: "schedules" },
  { page: "alerts", icon: "△", label: "alerts" },
  { page: "activity", icon: "≡", label: "activity" },
  { page: "settings", icon: "⚙", label: "settings" },
];
</script>

<template>
  <button class="sidebar-backdrop" :class="{ open }" aria-label="Menüyü kapat" @click="emit('close')" />
  <aside class="sidebar" :class="{ open }">
    <div class="brand">
      <span class="brand-mark small"><img src="/assets/servora-logo.png" alt=""></span>
      <span class="brand-copy"><strong>Servora</strong><small>System Operations</small></span>
    </div>
    <nav>
      <button
        v-for="item in items"
        :key="item.page"
        :class="{ active: page === item.page }"
        @click="emit('navigate', item.page); emit('close')"
      >
        <span>{{ item.icon }}</span><b>{{ item.label ? t(item.label) : item.text }}</b>
      </button>
    </nav>
    <div class="sidebar-telemetry">
      <div><span>CPU</span><b>{{ cpu.toFixed(0) }}%</b></div><i><span :style="{ width: `${Math.min(cpu, 100)}%` }" /></i>
      <div><span>RAM</span><b><small>{{ memoryAmount }}</small>{{ memory.toFixed(0) }}%</b></div><i><span :style="{ width: `${Math.min(memory, 100)}%` }" /></i>
      <div><span>NET</span><b>{{ rate(network) }}</b></div>
      <div><span>UPTIME</span><b>{{ uptime }}</b></div>
    </div>
    <div class="sidebar-foot">
      <a
        href="https://github.com/WhileEndless/Servora"
        target="_blank"
        rel="noopener noreferrer"
        title="Servora on GitHub"
        aria-label="Open the Servora GitHub repository"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.11.79-.25.79-.56v-2.23c-3.22.7-3.9-1.37-3.9-1.37-.53-1.34-1.29-1.69-1.29-1.69-1.05-.72.08-.71.08-.71 1.17.08 1.78 1.2 1.78 1.2 1.04 1.77 2.72 1.26 3.38.97.1-.75.4-1.26.74-1.55-2.57-.3-5.27-1.29-5.27-5.69 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.47.11-3.06 0 0 .97-.31 3.16 1.18a10.96 10.96 0 0 1 5.76 0c2.2-1.49 3.16-1.18 3.16-1.18.63 1.59.23 2.77.11 3.06.74.81 1.19 1.84 1.19 3.1 0 4.42-2.71 5.39-5.29 5.68.42.36.79 1.06.79 2.14v3.25c0 .31.21.68.8.56A11.5 11.5 0 0 0 12 .7Z"/></svg>
        <span>Servora</span>
        <i>↗</i>
      </a>
      <b>v{{ monitorStore.session.value?.version ?? "—" }}</b>
    </div>
  </aside>
</template>
