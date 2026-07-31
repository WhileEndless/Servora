<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import { apiClient } from "@/services/ApiClient";
import type { NetworkUsageDetail, ProcessDetail } from "@/types";

const props = defineProps<{ pid: number }>();
const emit = defineEmits<{ close: [] }>();
const detail = ref<ProcessDetail | null>(null);
const network = ref<NetworkUsageDetail | null>(null);
const error = ref("");
const networkError = ref("");
const loading = ref(true);
const refreshing = ref(false);
const lastUpdated = ref<Date | null>(null);
const rxRate = ref(0);
const txRate = ref(0);
let previousNetwork: { at: number; rx: number; tx: number } | null = null;
let refreshTimer: number | undefined;

const recentRX = computed(() => (network.value?.destinations ?? []).reduce((total, item) => total + item.rx_bytes, 0));
const recentTX = computed(() => (network.value?.destinations ?? []).reduce((total, item) => total + item.tx_bytes, 0));

function bytes(value: number): string {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const rank = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** rank).toFixed(rank > 1 ? 1 : 0)} ${units[rank]}`;
}

function rate(value: number): string {
  return `${bytes(value)}/s`;
}

function when(value: string): string {
  return new Date(value).toLocaleTimeString();
}

function field(item: Record<string, unknown>, key: string): string {
  return String(item[key] ?? "—");
}

function updateRates(next: NetworkUsageDetail): void {
  const now = Date.now();
  const rx = (next.destinations ?? []).reduce((total, item) => total + item.rx_bytes, 0);
  const tx = (next.destinations ?? []).reduce((total, item) => total + item.tx_bytes, 0);
  if (previousNetwork) {
    const seconds = Math.max((now - previousNetwork.at) / 1_000, 0.1);
    rxRate.value = rx >= previousNetwork.rx ? (rx - previousNetwork.rx) / seconds : 0;
    txRate.value = tx >= previousNetwork.tx ? (tx - previousNetwork.tx) / seconds : 0;
  }
  previousNetwork = { at: now, rx, tx };
}

async function load(): Promise<void> {
  if (refreshing.value) return;
  refreshing.value = true;
  loading.value = detail.value === null;

  const from = new Date(Date.now() - 2 * 60_000).toISOString();
  const [processResult, networkResult] = await Promise.allSettled([
    apiClient.processDetail(props.pid),
    apiClient.networkUsageDetail(from, "pid", String(props.pid)),
  ]);

  if (processResult.status === "fulfilled") {
    detail.value = processResult.value;
    error.value = "";
    lastUpdated.value = new Date();
  } else {
    error.value = processResult.reason instanceof Error ? processResult.reason.message : "Process details are unavailable";
  }

  if (networkResult.status === "fulfilled") {
    network.value = networkResult.value;
    networkError.value = "";
    updateRates(networkResult.value);
  } else {
    networkError.value = networkResult.reason instanceof Error ? networkResult.reason.message : "Attributed network data is unavailable";
  }

  loading.value = false;
  refreshing.value = false;
}

function resetAndLoad(): void {
  detail.value = null;
  network.value = null;
  previousNetwork = null;
  rxRate.value = 0;
  txRate.value = 0;
  error.value = "";
  networkError.value = "";
  void load();
}

watch(() => props.pid, resetAndLoad, { immediate: true });
onMounted(() => { refreshTimer = window.setInterval(() => void load(), 1_000); });
onBeforeUnmount(() => { if (refreshTimer !== undefined) window.clearInterval(refreshTimer); });
</script>

<template>
  <ModalDialog :title="detail ? `${detail.process.Name} · PID ${pid}` : `Process ${pid}`" wide @close="emit('close')">
    <div v-if="loading" class="detail-loading"><span class="spinner" /> Loading process details…</div>
    <div v-else-if="error && !detail" class="empty">{{ error }}</div>
    <div v-else-if="detail" class="process-detail">
      <div class="process-live-status">
        <span class="live-label"><i /> LIVE</span>
        <span>Updated {{ lastUpdated?.toLocaleTimeString() }}</span>
        <span v-if="refreshing">Refreshing…</span>
        <span v-if="error" class="warning-text">{{ error }}</span>
      </div>

      <div class="detail-hero">
        <div><span class="status ok">{{ detail.process.State }}</span><h3>{{ detail.executable || detail.process.Command }}</h3><p>{{ detail.process.Command }}</p></div>
        <div class="detail-metrics process-live-metrics">
          <div><small>CPU</small><b>{{ detail.process.CPU.toFixed(1) }}%</b></div>
          <div><small>MEMORY</small><b>{{ bytes(detail.process.Memory) }}</b></div>
          <div><small>DISK READ</small><b>{{ bytes(detail.process.ReadBytes) }}</b></div>
          <div><small>DISK WRITE</small><b>{{ bytes(detail.process.WriteBytes) }}</b></div>
          <div><small>THREADS</small><b>{{ detail.process.Threads }}</b></div>
          <div><small>OPEN FDs</small><b>{{ detail.open_fds }}</b></div>
        </div>
      </div>

      <section class="live-network-section">
        <div class="process-section-heading">
          <div><h4>Live attributed network</h4><p>Exact PID {{ pid }} activity; totals cover the most recent two minutes.</p></div>
          <span class="badge" :class="{ good: !networkError }">{{ networkError ? "UNAVAILABLE" : "LIVE eBPF" }}</span>
        </div>
        <p v-if="networkError" class="evidence-note">{{ networkError }}</p>
        <div v-else class="process-network-metrics">
          <div><small>RECEIVE RATE</small><b class="rx-value">{{ rate(rxRate) }}</b></div>
          <div><small>SEND RATE</small><b class="tx-value">{{ rate(txRate) }}</b></div>
          <div><small>RECENT RX</small><b>{{ bytes(recentRX) }}</b></div>
          <div><small>RECENT TX</small><b>{{ bytes(recentTX) }}</b></div>
        </div>
        <div v-if="!networkError && !(network?.destinations?.length ?? 0)" class="subtle-empty">No attributed transfer detected for this PID in the recent window.</div>
        <div v-else-if="!networkError" class="table-wrap compact">
          <table>
            <thead><tr><th>REMOTE</th><th>PROTO</th><th>RECEIVED</th><th>SENT</th><th>LAST SEEN</th></tr></thead>
            <tbody><tr v-for="destination in network?.destinations" :key="`${destination.remote_ip}:${destination.remote_port}/${destination.protocol}`"><td><b>{{ destination.remote_ip }}</b>:{{ destination.remote_port }}</td><td>{{ destination.protocol.toUpperCase() }}</td><td class="rx-value">{{ bytes(destination.rx_bytes) }}</td><td class="tx-value">{{ bytes(destination.tx_bytes) }}</td><td>{{ when(destination.last_seen) }}</td></tr></tbody>
          </table>
        </div>
      </section>

      <div class="detail-grid">
        <section><h4>Identity & runtime</h4><dl><dt>User</dt><dd>{{ detail.process.User }}</dd><dt>Parent PID</dt><dd>{{ detail.process.PPID }}</dd><dt>Started</dt><dd>{{ new Date(detail.process.StartTime).toLocaleString() }}</dd><dt>Working directory</dt><dd>{{ detail.working_dir || "—" }}</dd><dt>Children</dt><dd>{{ detail.children.join(", ") || "None" }}</dd><dt>Cgroup</dt><dd class="wrap">{{ detail.cgroup || "—" }}</dd></dl></section>
        <section><h4>Kernel status</h4><dl><template v-for="key in ['VmPeak','VmSize','VmRSS','RssAnon','RssFile','Threads','voluntary_ctxt_switches','nonvoluntary_ctxt_switches']" :key="key"><dt>{{ key }}</dt><dd>{{ detail.status[key] ?? "—" }}</dd></template></dl></section>
      </div>
      <section><h4>Active sockets <span class="badge">{{ detail.connections.length }}</span></h4><div v-if="!detail.connections.length" class="subtle-empty">No owned connection detected right now.</div><div v-else class="table-wrap compact"><table><thead><tr><th>PROTO</th><th>STATE</th><th>LOCAL</th><th>REMOTE</th></tr></thead><tbody><tr v-for="(connection, index) in detail.connections" :key="index"><td>{{ field(connection, "Protocol") }}</td><td>{{ field(connection, "State") }}</td><td>{{ field(connection, "Local") }}</td><td>{{ field(connection, "Remote") }}</td></tr></tbody></table></div></section>
      <section><h4>Open files <span class="badge">first {{ detail.open_files.length }}</span></h4><div class="code-list"><code v-for="file in detail.open_files" :key="file">{{ file }}</code><span v-if="!detail.open_files.length" class="muted">No readable open file.</span></div></section>
    </div>
  </ModalDialog>
</template>
