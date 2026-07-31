<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import ProcessDetailPanel from "@/components/process/ProcessDetailPanel.vue";
import { apiClient } from "@/services/ApiClient";
import { monitorStore } from "@/services/MonitorStore";
import type { NetworkUsage, NetworkUsageDetail, NetworkUsageResponse } from "@/types";

const groupBy = ref<"process" | "group">("process");
const view = ref<"live" | "analysis">("live");
const range = ref(10 * 24);
const query = ref("");
const liveQuery = ref("");
const loading = ref(false);
const error = ref("");
const usage = ref<NetworkUsageResponse | null>(null);
const selected = ref<NetworkUsage | null>(null);
const detail = ref<NetworkUsageDetail | null>(null);
const detailLoading = ref(false);
const selectedPID = ref<number | null>(null);
let timer: number | undefined;
let searchTimer: number | undefined;

const totalRX = computed(() => usage.value?.items.reduce((sum, item) => sum + item.rx_bytes, 0) ?? 0);
const totalTX = computed(() => usage.value?.items.reduce((sum, item) => sum + item.tx_bytes, 0) ?? 0);
const exactAccounting = computed(() => monitorStore.snapshot.value.network_attribution_mode === "ebpf-exact");
const droppedBytes = computed(() => monitorStore.snapshot.value.network_accounting_dropped_bytes || 0);
const timelineMax = computed(() => Math.max(1, ...(detail.value?.timeline.map((point) => point.rx_bytes + point.tx_bytes) ?? [1])));
const connections = computed(() => monitorStore.snapshot.value.connections.filter((item) => {
  const needle = (liveQuery.value || monitorStore.search.value).toLowerCase();
  return !needle || JSON.stringify(item).toLowerCase().includes(needle);
}));

function fromDate(): string {
  return new Date(Date.now() - range.value * 3_600_000).toISOString();
}
async function load(): Promise<void> {
  if (view.value !== "analysis") return;
  loading.value = true;
  try {
    usage.value = await apiClient.networkUsage(fromDate(), groupBy.value, query.value);
    if (selected.value) {
      const updated = usage.value.items.find((item) => item.key === selected.value?.key);
      if (updated) selected.value = updated;
      await loadSelectedDetail();
    }
    error.value = "";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Network history could not be loaded";
  } finally {
    loading.value = false;
  }
}
async function loadSelectedDetail(): Promise<void> {
  if (!selected.value) return;
  try {
    detail.value = await apiClient.networkUsageDetail(fromDate(), groupBy.value, selected.value.key);
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Network details could not be refreshed";
  }
}
function showAnalysis(): void {
  view.value = "analysis";
  void load();
}
async function open(item: NetworkUsage): Promise<void> {
  selected.value = item;
  detail.value = null;
  detailLoading.value = true;
  try {
    await loadSelectedDetail();
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Network details could not be loaded";
  } finally {
    detailLoading.value = false;
  }
}
function value(item: Record<string, unknown>, key: string): string {
  return String(item[key] ?? "—");
}
function bytes(raw: number | unknown): string {
  const amount = Number(raw) || 0;
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  if (!amount) return "0 B";
  const rank = Math.min(Math.floor(Math.log(amount) / Math.log(1024)), units.length - 1);
  return `${(amount / 1024 ** rank).toFixed(rank > 1 ? 1 : 0)} ${units[rank]}`;
}
function when(value: string): string {
  return new Date(value).toLocaleString();
}
function barHeight(value: number): number {
  return Math.max(2, value / timelineMax.value * 72);
}
function openProcess(item: Record<string, unknown>): void {
  const pid = Number(item.PID);
  if (pid > 0) selectedPID.value = pid;
}

watch([groupBy, range], () => {
  if (view.value === "analysis") void load();
});
watch(query, () => {
  if (view.value !== "analysis") return;
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(load, 250);
});
onMounted(() => {
  timer = window.setInterval(() => {
    if (view.value === "analysis" && !loading.value) void load();
  }, 1_000);
});
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer);
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
});
</script>

<template>
  <section class="page active network-intelligence">
    <div class="network-page-header">
      <div><p class="eyebrow">NETWORK</p><h2>{{ view === "live" ? "Live network activity" : "Traffic analysis" }}</h2><p>{{ view === "live" ? "Interfaces and active sockets update continuously." : "Search retained process, group and destination metadata." }}</p></div>
      <div class="network-view-switch">
        <button :class="{ active: view === 'live' }" @click="view = 'live'">Live activity</button>
        <button :class="{ active: view === 'analysis' }" @click="showAnalysis">Traffic analysis</button>
      </div>
    </div>

    <template v-if="view === 'live'">
      <div class="cards interface-cards">
        <article v-for="item in monitorStore.snapshot.value.network" :key="value(item, 'Name')" class="card">
          <div class="panel-title"><h3>{{ value(item, "Name") }}</h3><span class="badge good">LIVE</span></div>
          <div class="stat-pair"><div><small>DOWNLOAD</small><b>{{ bytes(item.RXRate) }}/s</b></div><div><small>UPLOAD</small><b>{{ bytes(item.TXRate) }}/s</b></div><div><small>RX TOTAL</small><b>{{ bytes(item.RXBytes) }}</b></div><div><small>TX TOTAL</small><b>{{ bytes(item.TXBytes) }}</b></div></div>
        </article>
      </div>
      <article class="panel section-gap flush">
        <div class="panel-title padded-title">
          <div><span>Active connections</span><small>{{ connections.length }} · LIVE SOCKETS</small></div>
          <label class="usage-search live-connection-search">
            <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" /></svg>
            <input v-model="liveQuery" placeholder="Filter protocol, address, PID or process…">
          </label>
        </div>
        <div class="table-wrap tall"><table class="interactive-table"><thead><tr><th>PROTO</th><th>STATE</th><th>LOCAL</th><th>REMOTE</th><th>PID</th><th>PROCESS</th></tr></thead><tbody><tr v-for="(item, index) in connections" :key="index" :class="{ 'clickable-row': Number(item.PID) > 0 }" @click="openProcess(item)"><td>{{ value(item, "Protocol") }}</td><td>{{ value(item, "State") }}</td><td>{{ value(item, "Local") }}</td><td>{{ value(item, "Remote") }}</td><td>{{ value(item, "PID") }}</td><td>{{ value(item, "Process") }}</td></tr></tbody></table></div>
      </article>
    </template>

    <template v-else>
      <div class="network-hero analysis-hero">
        <div><p class="eyebrow">RETAINED METADATA</p><h2>Who communicated with where?</h2><p>Packet payloads are never stored.</p></div>
        <div class="network-totals">
          <div><small>ACCOUNTING</small><b :class="exactAccounting ? 'rx-value' : 'warning-text'">{{ exactAccounting ? "EXACT eBPF" : "DEGRADED" }}</b></div>
          <div><small>RECEIVED</small><b>↓ {{ bytes(totalRX) }}</b></div><div><small>SENT</small><b>↑ {{ bytes(totalTX) }}</b></div><div><small>RECORDS</small><b>{{ usage?.storage.rows.toLocaleString() ?? 0 }}</b></div>
        </div>
      </div>
      <div class="network-toolbar">
        <div class="tabs"><button :class="{ active: groupBy === 'process' }" @click="groupBy = 'process'">Processes</button><button :class="{ active: groupBy === 'group' }" @click="groupBy = 'group'">Groups</button></div>
        <label class="usage-search"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" /></svg><input v-model="query" placeholder="Search process, group or user…"></label>
        <div class="range-picker"><button v-for="item in [{ h: 1, t: '1H' }, { h: 24, t: '24H' }, { h: 168, t: '7D' }, { h: 240, t: '10D' }]" :key="item.h" :class="{ active: range === item.h }" @click="range = item.h">{{ item.t }}</button></div>
      </div>
      <p v-if="error" class="notice error-state">{{ error }}</p>
      <p v-if="!exactAccounting" class="notice warning-state">Exact accounting is not active. Install the eBPF build dependencies, run make upgrade, then verify the agent log.</p>
      <p v-else-if="droppedBytes > 0" class="notice error-state">Accounting capacity was exceeded: {{ bytes(droppedBytes) }} could not be attributed.</p>
      <article class="panel flush usage-panel">
        <div class="panel-title padded-title"><div><span>{{ groupBy === "process" ? "Process consumption" : "Grouped consumption" }}</span><small>{{ usage?.items.length ?? 0 }} RESULTS · AUTO-REFRESH 1S</small></div><span class="badge good">1 MINUTE STORAGE</span></div>
        <div v-if="loading && !usage" class="detail-loading"><span class="spinner" /> Loading network history…</div>
        <div v-else-if="!usage?.items.length" class="empty"><b>No attributed traffic recorded yet</b><span>Traffic metadata will appear after the collector records its first transfer.</span></div>
        <div v-else class="table-wrap tall"><table class="interactive-table usage-table"><thead><tr><th>{{ groupBy === "process" ? "PROCESS" : "GROUP" }}</th><th>USER</th><th>DESTINATIONS</th><th>RECEIVED</th><th>SENT</th><th>TOTAL</th><th>LAST ACTIVITY</th></tr></thead><tbody><tr v-for="item in usage.items" :key="item.key" class="clickable-row" @click="open(item)"><td><b>{{ item.key }}</b><small v-if="groupBy === 'process'" class="cell-subtitle">{{ item.group }} · latest PID {{ item.pid || "—" }}</small><small v-else class="cell-subtitle">Combined matching processes</small></td><td>{{ item.user || "—" }}</td><td>{{ item.destinations }}</td><td class="rx-value">↓ {{ bytes(item.rx_bytes) }}</td><td class="tx-value">↑ {{ bytes(item.tx_bytes) }}</td><td><b>{{ bytes(item.rx_bytes + item.tx_bytes) }}</b></td><td>{{ when(item.last_seen) }}</td></tr></tbody></table></div>
      </article>
    </template>

    <ModalDialog v-if="selected" :title="`${selected.key} network history`" wide @close="selected = null">
      <div v-if="detailLoading" class="detail-loading"><span class="spinner" /> Loading destinations…</div>
      <div v-else-if="detail" class="network-detail">
        <div class="detail-hero network-detail-hero">
          <div><span class="badge good">{{ detail.selector.toUpperCase() }}</span><h3>{{ detail.value }}</h3><p>{{ when(detail.from) }} — {{ when(detail.to) }}</p></div>
          <div class="detail-metrics"><div><small>RECEIVED</small><b>{{ bytes(selected.rx_bytes) }}</b></div><div><small>SENT</small><b>{{ bytes(selected.tx_bytes) }}</b></div><div><small>DESTINATIONS</small><b>{{ detail.destinations.length }}</b></div><div><small>TOTAL</small><b>{{ bytes(selected.rx_bytes + selected.tx_bytes) }}</b></div></div>
        </div>
        <section><h4>Activity timeline</h4><div class="traffic-chart"><div v-for="point in detail.timeline" :key="point.time" class="traffic-bar" :title="`${when(point.time)} · ↓ ${bytes(point.rx_bytes)} · ↑ ${bytes(point.tx_bytes)}`"><i class="rx" :style="{ height: `${barHeight(point.rx_bytes)}px` }" /><i class="tx" :style="{ height: `${barHeight(point.tx_bytes)}px` }" /></div></div><div class="traffic-legend"><span><i class="rx" />Received</span><span><i class="tx" />Sent</span><small>Hover bars for exact time and bytes</small></div></section>
        <section><h4>Destinations</h4><div class="table-wrap compact"><table><thead><tr><th>REMOTE</th><th>PROTO</th><th>RECEIVED</th><th>SENT</th><th>FIRST</th><th>LAST</th></tr></thead><tbody><tr v-for="destination in detail.destinations" :key="`${destination.remote_ip}:${destination.remote_port}/${destination.protocol}`"><td><b>{{ destination.remote_ip }}</b>:{{ destination.remote_port }}</td><td>{{ destination.protocol.toUpperCase() }}</td><td class="rx-value">{{ bytes(destination.rx_bytes) }}</td><td class="tx-value">{{ bytes(destination.tx_bytes) }}</td><td>{{ when(destination.first_seen) }}</td><td>{{ when(destination.last_seen) }}</td></tr></tbody></table></div></section>
      </div>
    </ModalDialog>
    <ProcessDetailPanel v-if="selectedPID !== null" :pid="selectedPID" @close="selectedPID = null" />
  </section>
</template>
