<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import { monitorStore } from "@/services/MonitorStore";
import { apiClient } from "@/services/ApiClient";
import type { NetworkStorage } from "@/types";

const storage = ref<NetworkStorage | null>(null);
const retention = ref(10);
const collectors = ref({ network_enabled: true, cpu_enabled: true, memory_enabled: true, disk_io_enabled: true });
const message = ref("");
const error = ref("");
const clearModal = ref(false);
const clearPhrase = ref("");
const clearResourceModal = ref(false);
const clearResourcePhrase = ref("");
const activeCollectors = computed(() => Object.values(collectors.value).filter(Boolean).length);
const totalHistoryBytes = computed(() => (storage.value?.bytes ?? 0) + (storage.value?.resource_bytes ?? 0));
const exactNetwork = computed(() => monitorStore.snapshot.value.network_attribution_mode === "ebpf-exact");

async function load(): Promise<void> {
  try {
    storage.value = await apiClient.networkSettings();
    retention.value = storage.value.retention_days;
    collectors.value = {
      network_enabled: storage.value.network_enabled,
      cpu_enabled: storage.value.cpu_enabled,
      memory_enabled: storage.value.memory_enabled,
      disk_io_enabled: storage.value.disk_io_enabled,
    };
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Settings could not be loaded";
  }
}
async function saveRetention(): Promise<void> {
  message.value = ""; error.value = "";
  try {
    storage.value = await apiClient.updateNetworkSettings({ retention_days: retention.value, ...collectors.value });
    message.value = "History retention and collectors were updated.";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Retention could not be saved";
  }
}
async function clearHistory(): Promise<void> {
  if (clearPhrase.value !== "DELETE NETWORK HISTORY") return;
  try {
    storage.value = await apiClient.clearNetworkHistory();
    clearModal.value = false;
    clearPhrase.value = "";
    message.value = "Network history was permanently cleared.";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Network history could not be cleared";
  }
}
async function clearResourceHistory(): Promise<void> {
  if (clearResourcePhrase.value !== "DELETE RESOURCE HISTORY") return;
  try {
    storage.value = await apiClient.clearResourceHistory();
    clearResourceModal.value = false;
    clearResourcePhrase.value = "";
    message.value = "CPU, memory and disk I/O history was permanently cleared.";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Resource history could not be cleared";
  }
}
async function power(action: "reboot" | "shutdown"): Promise<void> {
  const confirmation = prompt(`Type ${action.toUpperCase()} to confirm the host ${action}.`);
  if (confirmation !== action.toUpperCase()) return;
  await apiClient.action(`power.${action}`, "", { confirm: action.toUpperCase() });
}
const capabilityLabels: Record<string, [string, string]> = {
  systemd: ["systemd", "Service and timer inventory"],
  docker: ["Docker Engine", "Container inventory and controls"],
  sensors: ["Thermal sensors", "Hardware temperature telemetry"],
  process_network: ["Per-process traffic", "Kernel socket accounting and historical attribution"],
};
function capabilityName(key: string): string { return capabilityLabels[key]?.[0] ?? key; }
function capabilityDescription(key: string): string { return capabilityLabels[key]?.[1] ?? "Optional collector"; }
function bytes(value: number): string {
  const units = ["B", "KiB", "MiB", "GiB"];
  if (!value) return "0 B";
  const rank = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** rank).toFixed(rank > 1 ? 1 : 0)} ${units[rank]}`;
}
function date(timestamp: number): string { return timestamp ? new Date(timestamp * 1000).toLocaleString() : "No records"; }
onMounted(load);
</script>

<template>
  <section class="page active settings-page">
    <div class="settings-heading"><div><p class="eyebrow">CONTROL CENTER</p><h2>Settings</h2><p>Retention, collectors, security boundaries and host operations.</p></div><span v-if="message" class="notice">{{ message }}</span></div>
    <p v-if="error" class="notice error-state">{{ error }}</p>
    <div class="settings-overview">
      <div><i class="settings-overview-icon">◫</i><span><small>HISTORY STORAGE</small><b>{{ bytes(totalHistoryBytes) }}</b></span></div>
      <div><i class="settings-overview-icon">↻</i><span><small>RETENTION</small><b>{{ retention }} days</b></span></div>
      <div><i class="settings-overview-icon">●</i><span><small>ACTIVE COLLECTORS</small><b>{{ activeCollectors }} / 4</b></span></div>
      <div><i class="settings-overview-icon">⌁</i><span><small>NETWORK INTEGRITY</small><b :class="exactNetwork ? 'success' : 'warning-text'">{{ exactNetwork ? "Exact eBPF" : "Degraded" }}</b></span></div>
    </div>
    <div class="settings-layout">
      <article class="panel settings-wide">
        <div class="panel-title"><div><span>Network intelligence history</span><small>METADATA ONLY</small></div><span class="badge" :class="collectors.network_enabled ? 'good' : 'neutral'">{{ collectors.network_enabled ? "COLLECTING" : "PAUSED" }}</span></div>
        <p class="settings-description">Stores per-minute byte totals by process, group and remote endpoint. Packet payloads, credentials and message contents are never captured.</p>
        <div class="storage-stats">
          <div><small>STORED ROWS</small><b>{{ storage?.rows.toLocaleString() ?? "—" }}</b></div>
          <div><small>ESTIMATED SIZE</small><b>{{ bytes(storage?.bytes ?? 0) }}</b></div>
          <div><small>OLDEST RECORD</small><b>{{ date(storage?.oldest ?? 0) }}</b></div>
          <div><small>NEWEST RECORD</small><b>{{ date(storage?.newest ?? 0) }}</b></div>
        </div>
        <div class="setting-row">
          <div><b>Retention period</b><p>Records older than this are removed automatically. Allowed range: 1–365 days.</p></div>
          <div class="retention-control"><input v-model.number="retention" type="number" min="1" max="365"><span>days</span><button class="primary" @click="saveRetention">Save</button></div>
        </div>
        <div class="setting-row danger-setting">
          <div><b>Clear network history</b><p>Permanently removes process, group, destination and timeline records. This action is audited.</p></div>
          <button class="danger-outline" @click="clearModal = true">Clear stored history</button>
        </div>
      </article>

      <article class="panel settings-wide">
        <div class="panel-title"><div><span>Historical resource collectors</span><small>LOW OVERHEAD · PER PROCESS</small></div><button class="primary" @click="saveRetention">Save collectors</button></div>
        <p class="settings-description">Uses process data already read from /proc and stores one-minute aggregates. Disabling a collector stops new history without affecting live monitoring.</p>
        <div class="collector-toggle-grid">
          <label :class="{ enabled: collectors.network_enabled }"><input v-model="collectors.network_enabled" type="checkbox"><span><b>Network attribution</b><small>Process, group and remote endpoint byte totals</small></span><em>{{ collectors.network_enabled ? "ON" : "OFF" }}</em></label>
          <label :class="{ enabled: collectors.cpu_enabled }"><input v-model="collectors.cpu_enabled" type="checkbox"><span><b>CPU history</b><small>Average and peak CPU by process and group</small></span><em>{{ collectors.cpu_enabled ? "ON" : "OFF" }}</em></label>
          <label :class="{ enabled: collectors.memory_enabled }"><input v-model="collectors.memory_enabled" type="checkbox"><span><b>Memory history</b><small>Average and peak resident memory</small></span><em>{{ collectors.memory_enabled ? "ON" : "OFF" }}</em></label>
          <label :class="{ enabled: collectors.disk_io_enabled }"><input v-model="collectors.disk_io_enabled" type="checkbox"><span><b>Disk I/O history</b><small>Read and write byte deltas, not file contents</small></span><em>{{ collectors.disk_io_enabled ? "ON" : "OFF" }}</em></label>
        </div>
        <div class="storage-stats resource-storage-stats"><div><small>RESOURCE ROWS</small><b>{{ storage?.resource_rows.toLocaleString() ?? "—" }}</b></div><div><small>RESOURCE STORAGE</small><b>{{ bytes(storage?.resource_bytes ?? 0) }}</b></div></div>
        <div class="setting-row danger-setting"><div><b>Clear resource history</b><p>Removes stored CPU, memory and disk I/O aggregates. Live monitoring is unaffected.</p></div><button class="danger-outline" @click="clearResourceModal = true">Clear resource history</button></div>
      </article>

      <article class="panel">
        <div class="panel-title"><span>Collection policy</span><small>PRIVACY</small></div>
        <div class="policy-list"><div><i class="ok">✓</i><span><b>Byte accounting</b><small>RX and TX application bytes</small></span></div><div><i class="ok">✓</i><span><b>Connection metadata</b><small>Process, user, remote IP and port</small></span></div><div><i>—</i><span><b>Packet payloads</b><small>Never collected or retained</small></span></div><div><i>—</i><span><b>Credentials and content</b><small>Never inspected</small></span></div></div>
      </article>

      <article class="panel settings-wide">
        <div class="panel-title"><span>Modules & capabilities</span><small>COLLECTORS</small></div>
        <div class="capability-grid"><div v-for="(available, name) in monitorStore.snapshot.value.capabilities" :key="name" class="health-item"><div><span>{{ capabilityName(name) }}</span><small>{{ capabilityDescription(name) }}</small></div><span class="badge" :class="available ? 'good' : 'neutral'">{{ available ? "READY" : "NOT DETECTED" }}</span></div></div>
        <div v-if="monitorStore.snapshot.value.errors?.length" class="collector-errors"><b>Collector notices</b><p v-for="item in monitorStore.snapshot.value.errors" :key="item">{{ item }}</p></div>
      </article>

      <article class="panel">
        <div class="panel-title"><span>Session</span><small>IDENTITY</small></div>
        <p><b>{{ monitorStore.session.value?.username }}</b> · administrator</p><p class="muted">Servora {{ monitorStore.session.value?.version }}</p><button class="secondary" @click="monitorStore.logout()">Sign out</button>
      </article>
      <article class="panel danger-zone">
        <div class="panel-title"><span>Host power</span><small>AUDITED</small></div><p class="muted">These actions affect the entire host and require typed confirmation.</p><div class="button-row"><button class="danger-outline" @click="power('reboot')">Reboot host</button><button class="danger" @click="power('shutdown')">Shut down host</button></div>
      </article>
    </div>

    <ModalDialog v-if="clearModal" title="Permanently clear network history" @close="clearModal = false">
      <p class="muted">This cannot be undone. Type <b>DELETE NETWORK HISTORY</b> to confirm.</p>
      <label><span>Confirmation phrase</span><input v-model="clearPhrase" autocomplete="off"></label>
      <div class="modal-actions"><button class="secondary" @click="clearModal = false">Cancel</button><button class="danger" :disabled="clearPhrase !== 'DELETE NETWORK HISTORY'" @click="clearHistory">Clear history</button></div>
    </ModalDialog>
    <ModalDialog v-if="clearResourceModal" title="Permanently clear resource history" @close="clearResourceModal = false">
      <p class="muted">This cannot be undone. Type <b>DELETE RESOURCE HISTORY</b> to confirm.</p>
      <label><span>Confirmation phrase</span><input v-model="clearResourcePhrase" autocomplete="off"></label>
      <div class="modal-actions"><button class="secondary" @click="clearResourceModal = false">Cancel</button><button class="danger" :disabled="clearResourcePhrase !== 'DELETE RESOURCE HISTORY'" @click="clearResourceHistory">Clear history</button></div>
    </ModalDialog>
  </section>
</template>
