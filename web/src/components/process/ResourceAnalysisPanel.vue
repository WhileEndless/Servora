<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import { apiClient } from "@/services/ApiClient";
import { monitorStore } from "@/services/MonitorStore";
import type { NetworkUsageDetail, ProcessDetail, ResourceUsage, ResourceUsageDetail, ResourceUsageResponse } from "@/types";

const groupBy = ref<"process" | "group">("process");
const range = ref(24);
const query = ref("");
const response = ref<ResourceUsageResponse | null>(null);
const selected = ref<ResourceUsage | null>(null);
const detail = ref<ResourceUsageDetail | null>(null);
const networkDetail = ref<NetworkUsageDetail | null>(null);
const liveDetails = ref<ProcessDetail[]>([]);
const loading = ref(false);
const error = ref("");
let timer: number | undefined;
let searchTimer: number | undefined;

const enabledMetrics = computed(() => {
  const storage = response.value?.storage;
  return [
    storage?.cpu_enabled && "CPU",
    storage?.memory_enabled && "RAM",
    storage?.disk_io_enabled && "DISK I/O",
  ].filter(Boolean).join(" · ");
});
const totalCPU = computed(() => response.value?.items.reduce((sum, item) => sum + item.cpu_average, 0) ?? 0);
const totalMemory = computed(() => response.value?.items.reduce((sum, item) => sum + item.memory_average, 0) ?? 0);
const totalRead = computed(() => response.value?.items.reduce((sum, item) => sum + item.read_bytes, 0) ?? 0);
const totalWrite = computed(() => response.value?.items.reduce((sum, item) => sum + item.write_bytes, 0) ?? 0);

function fromDate(): string {
  return new Date(Date.now() - range.value * 3_600_000).toISOString();
}
async function load(): Promise<void> {
  if (loading.value) return;
  loading.value = true;
  try {
    response.value = await apiClient.resourceUsage(fromDate(), groupBy.value, query.value);
    error.value = "";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Resource history could not be loaded";
  } finally {
    loading.value = false;
  }
}
async function open(item: ResourceUsage): Promise<void> {
  selected.value = item;
  detail.value = null;
  networkDetail.value = null;
  liveDetails.value = [];
  const liveProcesses = monitorStore.snapshot.value.processes;
  const pids = groupBy.value === "process"
    ? (liveProcesses.some((process) => process.PID === item.pid && process.Name === item.key) ? [item.pid] : [])
    : liveProcesses
      .filter((process) => processGroup(process.Name) === item.key)
      .sort((left, right) => right.CPU - left.CPU)
      .slice(0, 5)
      .map((process) => process.PID);
  const [resourceResult, networkResult, ...processResults] = await Promise.allSettled([
    apiClient.resourceUsageDetail(fromDate(), groupBy.value, item.key),
    apiClient.networkUsageDetail(fromDate(), groupBy.value, item.key),
    ...pids.filter((pid) => pid > 0).map((pid) => apiClient.processDetail(pid)),
  ]);
  if (resourceResult.status === "fulfilled") detail.value = resourceResult.value;
  if (networkResult.status === "fulfilled") networkDetail.value = networkResult.value;
  liveDetails.value = processResults
    .filter((result): result is PromiseFulfilledResult<ProcessDetail> => result.status === "fulfilled")
    .map((result) => result.value);
}
function processGroup(name: string): string {
  const value = name.toLowerCase();
  if (value.startsWith("ssh")) return "ssh";
  if (value.includes("servora") || value.includes("system-maintenance")) return "servora";
  if (value.includes("docker") || value.includes("containerd")) return "docker";
  if (value.startsWith("postgres")) return "postgresql";
  if (value.startsWith("mysql") || value.startsWith("mariadb")) return "mysql";
  return value;
}
function bytes(value: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  if (!value) return "0 B";
  const rank = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** rank).toFixed(rank > 1 ? 1 : 0)} ${units[rank]}`;
}
function when(value: string): string { return new Date(value).toLocaleString(); }

watch([groupBy, range], () => void load());
watch(query, () => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(load, 250);
});
onMounted(() => {
  void load();
  timer = window.setInterval(load, 10_000);
});
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer);
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
});
</script>

<template>
  <div class="resource-analysis">
    <div class="network-toolbar">
      <div class="tabs"><button :class="{ active: groupBy === 'process' }" @click="groupBy = 'process'">Processes</button><button :class="{ active: groupBy === 'group' }" @click="groupBy = 'group'">Groups</button></div>
      <label class="usage-search"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" /></svg><input v-model="query" placeholder="Search process, group or user…"></label>
      <div class="range-picker"><button v-for="item in [{ h: 1, t: '1H' }, { h: 24, t: '24H' }, { h: 168, t: '7D' }, { h: 240, t: '10D' }]" :key="item.h" :class="{ active: range === item.h }" @click="range = item.h">{{ item.t }}</button></div>
    </div>
    <p v-if="error" class="notice error-state">{{ error }}</p>
    <div class="resource-summary">
      <div><small>COMBINED AVG CPU</small><b>{{ totalCPU.toFixed(1) }}%</b></div>
      <div><small>COMBINED AVG RAM</small><b>{{ bytes(totalMemory) }}</b></div>
      <div><small>TOTAL DISK READ</small><b>{{ bytes(totalRead) }}</b></div>
      <div><small>TOTAL DISK WRITE</small><b>{{ bytes(totalWrite) }}</b></div>
      <div><small>TRACKED {{ groupBy === "group" ? "GROUPS" : "PROCESSES" }}</small><b>{{ response?.items.length ?? 0 }}</b></div>
    </div>
    <article class="panel flush">
      <div class="panel-title padded-title"><div><span>Historical resource attribution</span><small>{{ response?.items.length ?? 0 }} RESULTS · {{ enabledMetrics || "COLLECTORS DISABLED" }}</small></div><span class="badge good">1 MINUTE STORAGE</span></div>
      <div v-if="loading && !response" class="detail-loading"><span class="spinner" /> Loading history…</div>
      <div v-else-if="!response?.items.length" class="empty"><b>No resource history yet</b><span>Enable CPU, memory or disk I/O history in Settings and allow the first sample to be stored.</span></div>
      <div v-else class="table-wrap tall"><table class="interactive-table usage-table"><thead><tr><th>{{ groupBy === "process" ? "PROCESS" : "GROUP" }}</th><th>USER</th><th>CPU AVG / PEAK</th><th>RAM AVG / PEAK</th><th>DISK READ</th><th>DISK WRITE</th><th>LAST</th></tr></thead><tbody><tr v-for="item in response.items" :key="item.key" class="clickable-row" @click="open(item)"><td><b>{{ item.key }}</b><small class="cell-subtitle">{{ item.group }}<template v-if="item.pid"> · PID {{ item.pid }}</template></small></td><td>{{ item.user || "—" }}</td><td><b>{{ item.cpu_average.toFixed(1) }}%</b> / {{ item.cpu_max.toFixed(1) }}%</td><td><b>{{ bytes(item.memory_average) }}</b> / {{ bytes(item.memory_max) }}</td><td>{{ bytes(item.read_bytes) }}</td><td>{{ bytes(item.write_bytes) }}</td><td>{{ when(item.last_seen) }}</td></tr></tbody></table></div>
    </article>
    <ModalDialog v-if="selected" :title="`${selected.key} resource history`" wide @close="selected = null; detail = null">
      <div v-if="!detail" class="detail-loading"><span class="spinner" /> Loading timeline…</div>
      <div v-else class="network-detail resource-detail">
        <div class="detail-metrics"><div><small>CPU AVERAGE</small><b>{{ selected.cpu_average.toFixed(1) }}%</b></div><div><small>CPU PEAK</small><b>{{ selected.cpu_max.toFixed(1) }}%</b></div><div><small>RAM PEAK</small><b>{{ bytes(selected.memory_max) }}</b></div><div><small>DISK I/O</small><b>{{ bytes(selected.read_bytes + selected.write_bytes) }}</b></div></div>
        <section><h4>Communication history <span class="badge">{{ networkDetail?.destinations?.length ?? 0 }} DESTINATIONS</span></h4><div v-if="!networkDetail?.destinations?.length" class="subtle-empty">No attributed network destination was stored for this {{ groupBy }} and time range.</div><div v-else class="table-wrap compact"><table><thead><tr><th>REMOTE</th><th>PROTO</th><th>RECEIVED</th><th>SENT</th><th>FIRST</th><th>LAST</th></tr></thead><tbody><tr v-for="destination in networkDetail.destinations" :key="`${destination.remote_ip}:${destination.remote_port}/${destination.protocol}`"><td><b>{{ destination.remote_ip }}</b>:{{ destination.remote_port }}</td><td>{{ destination.protocol.toUpperCase() }}</td><td>{{ bytes(destination.rx_bytes) }}</td><td>{{ bytes(destination.tx_bytes) }}</td><td>{{ when(destination.first_seen) }}</td><td>{{ when(destination.last_seen) }}</td></tr></tbody></table></div></section>
        <div class="table-wrap compact"><table><thead><tr><th>TIME</th><th>CPU AVG</th><th>CPU PEAK</th><th>RAM AVG</th><th>RAM PEAK</th><th>READ</th><th>WRITE</th></tr></thead><tbody><tr v-for="point in detail.timeline" :key="point.time"><td>{{ when(point.time) }}</td><td>{{ point.cpu_average.toFixed(1) }}%</td><td>{{ point.cpu_max.toFixed(1) }}%</td><td>{{ bytes(point.memory_average) }}</td><td>{{ bytes(point.memory_max) }}</td><td>{{ bytes(point.read_bytes) }}</td><td>{{ bytes(point.write_bytes) }}</td></tr></tbody></table></div>
        <section><h4>Current runtime & open files <span class="badge">LIVE EVIDENCE</span></h4><p class="evidence-note">Disk byte totals are historical counters. Linux does not expose their per-file breakdown through /proc; the paths below are files currently open by matching live processes and are not claimed as historical byte destinations.</p><div v-if="!liveDetails.length" class="subtle-empty">No matching process is currently running, so runtime paths are unavailable.</div><div v-for="runtime in liveDetails" :key="runtime.process.PID" class="runtime-evidence"><div><b>{{ runtime.process.Name }} · PID {{ runtime.process.PID }}</b><small>{{ runtime.executable || runtime.process.Command }}</small><small>cwd: {{ runtime.working_dir || "—" }} · cgroup: {{ runtime.cgroup || "—" }}</small></div><div class="code-list"><code v-for="file in runtime.open_files" :key="file">{{ file }}</code><span v-if="!runtime.open_files.length" class="muted">No readable open file.</span></div></div></section>
      </div>
    </ModalDialog>
  </div>
</template>
