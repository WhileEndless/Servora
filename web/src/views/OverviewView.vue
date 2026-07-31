<script setup lang="ts">
import { computed } from "vue";
import ResourceGauge from "@/components/common/ResourceGauge.vue";
import DiskUsageCard from "@/components/common/DiskUsageCard.vue";
import HistoryChart from "@/components/common/HistoryChart.vue";
import { monitorStore } from "@/services/MonitorStore";
import { useI18n } from "@/composables/useI18n";

defineOptions({ name: "OverviewView" });
const { t, locale, l } = useI18n();
const snapshot = monitorStore.snapshot;
const memoryPercent = computed(() => percent(snapshot.value.memory.Used, snapshot.value.memory.Total));
const swapPercent = computed(() => percent(snapshot.value.memory.SwapUsed, snapshot.value.memory.SwapTotal));
const primaryDisk = computed(() => snapshot.value.disks.find((disk) => text(disk, "Mount") === "/") ??
  snapshot.value.disks.reduce((current, disk) =>
    number(disk, "UsedPercent") > number(current, "UsedPercent") ? disk : current, {}));
const topProcesses = computed(() => [...snapshot.value.processes].sort((a, b) => b.CPU - a.CPU).slice(0, 6));
const networkRate = computed(() => snapshot.value.network.reduce((total, row) =>
  total + number(row, "RXRate") + number(row, "TXRate"), 0));
const healthItems = computed(() => {
  const age = snapshot.value.timestamp ? (Date.now() - new Date(snapshot.value.timestamp).getTime()) / 1000 : Infinity;
  return [
    { name: l("Live event stream", "Canlı olay akışı"), detail: l("1 second updates", "1 saniyelik güncellemeler"), ok: monitorStore.streamConnected.value },
    { name: l("Privileged collector", "Yetkili toplayıcı"), detail: age < 3 ? l("{age}s ago", "{age} sn önce", { age: age.toFixed(1) }) : l("stale", "güncel değil"), ok: age < 3 },
    { name: l("Historical metrics", "Geçmiş metrikleri"), detail: l("{count} points loaded", "{count} nokta yüklendi", { count: monitorStore.history.value.length }), ok: monitorStore.history.value.length > 0 },
    { name: "systemd inventory", detail: l("15 second cache", "15 saniyelik önbellek"), ok: snapshot.value.capabilities.systemd === true },
  ];
});

function number(value: Record<string, unknown>, key: string): number {
  const item = value[key];
  return typeof item === "number" ? item : 0;
}
function text(value: Record<string, unknown>, key: string): string {
  const item = value[key];
  return typeof item === "string" ? item : "—";
}
function percent(used: number, total: number): number { return total ? used * 100 / total : 0; }
function bytes(value: number): string {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const rank = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** rank).toFixed(rank > 2 ? 1 : 0)} ${units[rank]}`;
}
function duration(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor(seconds % 86400 / 3600);
  const minutes = Math.floor(seconds % 3600 / 60);
  return `${days}d ${hours}h ${minutes}m`;
}
</script>

<template>
  <section class="page active overview-page">
    <div class="hero-row">
      <div>
        <div class="host-line"><span class="live-pulse" /><strong>{{ snapshot.hostname || "—" }}</strong><span class="muted">{{ snapshot.kernel }}</span></div>
        <p>{{ t("uptime") }} <b>{{ duration(snapshot.uptime_seconds) }}</b> · {{ t("updated") }} {{ snapshot.timestamp ? new Date(snapshot.timestamp).toLocaleTimeString() : "—" }}</p>
      </div>
      <div class="range-picker">
        <button v-for="hours in [1, 6, 24, 168, 720]" :key="hours" :class="{ active: hours === 24 }" @click="monitorStore.loadHistory(hours)">{{ hours < 24 ? `${hours}H` : `${hours / 24}D` }}</button>
      </div>
    </div>
    <div class="metric-grid">
      <ResourceGauge label="CPU" :value="snapshot.cpu.usage" :detail="l('{cores} cores · {load} load', '{cores} çekirdek · {load} yük', { cores: snapshot.cpu.cores, load: snapshot.cpu.load[0]?.toFixed(2) ?? 0 })" color="#39d6c5" />
      <ResourceGauge label="RAM" :value="memoryPercent" :detail="`${bytes(snapshot.memory.Used)} / ${bytes(snapshot.memory.Total)}`" color="#5d8cff" />
      <ResourceGauge label="SWAP" :value="swapPercent" :detail="`${bytes(snapshot.memory.SwapUsed)} / ${bytes(snapshot.memory.SwapTotal)}`" color="#f5b942" />
      <DiskUsageCard
        :mount="text(primaryDisk, 'Mount')"
        :filesystem="text(primaryDisk, 'Filesystem') === '—' ? '' : text(primaryDisk, 'Filesystem')"
        :used="number(primaryDisk, 'Used')"
        :available="number(primaryDisk, 'Available')"
        :total="number(primaryDisk, 'Total')"
        :percent="number(primaryDisk, 'UsedPercent')"
        :locale="locale"
      />
    </div>
    <div class="panel-grid">
      <article class="panel span2 performance-panel">
        <div class="panel-title"><div><span>{{ t("performance") }}</span><small>CPU & MEMORY</small></div><div class="legend"><i class="cpu" />CPU <i class="memory" />RAM</div></div>
        <HistoryChart :points="monitorStore.history.value" />
      </article>
      <article class="panel"><div class="panel-title"><span>{{ l("Network flow", "Ağ akışı") }}</span><small>{{ l("LIVE", "CANLI") }}</small></div><div class="big-stat">{{ bytes(networkRate) }}/s</div><div class="health-list"><div v-for="item in snapshot.network" :key="text(item, 'Name')" class="health-item"><span>{{ text(item, "Name") }}</span><small>↓ {{ bytes(number(item, "RXRate")) }}/s · ↑ {{ bytes(number(item, "TXRate")) }}/s</small></div></div></article>
      <article class="panel span2"><div class="panel-title"><span>{{ t("topProcesses") }}</span><button class="link" @click="monitorStore.setPage('processes')">{{ l("View all", "Tümünü görüntüle") }} →</button></div><div class="table-wrap"><table><thead><tr><th>PID</th><th>{{ l("NAME", "AD") }}</th><th>{{ l("USER", "KULLANICI") }}</th><th>CPU</th><th>RAM</th></tr></thead><tbody><tr v-for="process in topProcesses" :key="process.PID"><td>{{ process.PID }}</td><td>{{ process.Name }}</td><td>{{ process.User }}</td><td>{{ process.CPU.toFixed(1) }}%</td><td>{{ bytes(process.Memory) }}</td></tr></tbody></table></div></article>
      <article class="panel"><div class="panel-title"><span>{{ t("systemHealth") }}</span><span class="live-label"><i />{{ l("LIVE", "CANLI") }}</span></div><div class="health-list"><div v-for="item in healthItems" :key="item.name" class="health-item"><div><span>{{ item.name }}</span><small>{{ item.detail }}</small></div><span class="badge" :class="item.ok ? 'good' : 'bad'">{{ item.ok ? l("HEALTHY", "SAĞLIKLI") : l("CHECK", "KONTROL ET") }}</span></div></div></article>
    </div>
  </section>
</template>
