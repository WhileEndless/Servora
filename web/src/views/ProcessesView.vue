<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import ProcessDetailPanel from "@/components/process/ProcessDetailPanel.vue";
import ResourceAnalysisPanel from "@/components/process/ResourceAnalysisPanel.vue";
import { monitorStore } from "@/services/MonitorStore";
import { apiClient } from "@/services/ApiClient";
import type { ProcessInfo } from "@/types";

type SortKey = "PID" | "Name" | "User" | "State" | "CPU" | "Memory" | "ReadBytes" | "WriteBytes";
interface WatchRule { id: string; name: string; field: string; pattern: string; enabled: boolean }
interface PendingSignal { process: ProcessInfo; signal: "TERM" | "KILL" | "STOP" | "CONT" }

const showWatch = ref(false);
const view = ref<"live" | "analysis">("live");
const selectedPID = ref<number | null>(null);
const pendingSignal = ref<PendingSignal | null>(null);
const watch = ref({ name: "", field: "name", pattern: "" });
const message = ref("");
const watches = ref<WatchRule[]>([]);
const sortKey = ref<SortKey>("CPU");
const sortDirection = ref<"asc" | "desc">("desc");
const columns: { key: SortKey; label: string }[] = [
  { key: "PID", label: "PID" }, { key: "Name", label: "NAME" },
  { key: "User", label: "USER" }, { key: "State", label: "STATE" },
  { key: "CPU", label: "CPU" }, { key: "Memory", label: "MEMORY" },
  { key: "ReadBytes", label: "READ" }, { key: "WriteBytes", label: "WRITE" },
];

const filtered = computed(() => {
  const query = monitorStore.search.value.toLowerCase();
  const items = monitorStore.snapshot.value.processes.filter((item) =>
    !query || `${item.PID} ${item.Name} ${item.User} ${item.Command}`.toLowerCase().includes(query),
  );
  return [...items].sort((left, right) => {
    const a = left[sortKey.value];
    const b = right[sortKey.value];
    const comparison = typeof a === "number" && typeof b === "number"
      ? a - b
      : String(a).localeCompare(String(b));
    return sortDirection.value === "asc" ? comparison : -comparison;
  });
});

function bytes(value: number): string {
  if (!value) return "0 B";
  return value >= 1e9 ? `${(value / 1e9).toFixed(1)} GB`
    : value >= 1e6 ? `${(value / 1e6).toFixed(1)} MB`
      : `${(value / 1e3).toFixed(0)} KB`;
}
function setSort(key: SortKey): void {
  if (sortKey.value === key) sortDirection.value = sortDirection.value === "asc" ? "desc" : "asc";
  else { sortKey.value = key; sortDirection.value = "desc"; }
}
function sortMark(key: SortKey): string {
  return sortKey.value === key ? (sortDirection.value === "desc" ? "↓" : "↑") : "";
}
function requestSignal(process: ProcessInfo, signal: PendingSignal["signal"]): void {
  pendingSignal.value = { process, signal };
}
async function confirmSignal(): Promise<void> {
  const pending = pendingSignal.value;
  if (!pending) return;
  pendingSignal.value = null;
  try {
    await apiClient.action("process.signal", String(pending.process.PID), { signal: pending.signal });
    message.value = `${pending.signal} sent to PID ${pending.process.PID}`;
  } catch (cause) {
    message.value = cause instanceof Error ? cause.message : "Process action failed";
  }
}
async function saveWatch(): Promise<void> {
  await apiClient.create("/watches", { ...watch.value, enabled: true });
  showWatch.value = false; await loadWatches();
}
async function loadWatches(): Promise<void> { watches.value = await apiClient.list<WatchRule>("/watches"); }
async function removeWatch(id: string): Promise<void> {
  await apiClient.remove(`/watches/${id}`); await loadWatches();
}
onMounted(loadWatches);
</script>

<template>
  <section class="page active">
    <div class="toolbar">
      <div class="summary"><b>{{ view === "live" ? filtered.length : "Historical" }}</b> {{ view === "live" ? `visible · ${monitorStore.snapshot.value.processes.length} total` : "CPU, RAM and disk I/O" }} <span class="success">{{ message }}</span></div>
      <div class="button-row"><div class="network-view-switch"><button :class="{ active: view === 'live' }" @click="view = 'live'">Live processes</button><button :class="{ active: view === 'analysis' }" @click="view = 'analysis'">Resource analysis</button></div><button v-if="view === 'live'" class="primary" @click="showWatch = true">+ Watch rule</button></div>
    </div>
    <ResourceAnalysisPanel v-if="view === 'analysis'" />
    <template v-else>
    <article class="panel flush">
      <div class="table-wrap tall">
        <table class="interactive-table">
          <thead><tr>
            <th v-for="column in columns" :key="column.key">
              <button class="sort-button" @click="setSort(column.key)">{{ column.label }} <span>{{ sortMark(column.key) }}</span></button>
            </th>
            <th>ACTIONS</th>
          </tr></thead>
          <tbody>
            <tr v-for="process in filtered" :key="`${process.PID}-${process.StartTime}`" class="clickable-row" @click="selectedPID = process.PID">
              <td>{{ process.PID }}</td>
              <td><b>{{ process.Name }}</b><small class="cell-subtitle">{{ process.Command }}</small></td>
              <td>{{ process.User }}</td><td>{{ process.State }}</td>
              <td><span class="metric-cell">{{ process.CPU.toFixed(1) }}%</span></td>
              <td>{{ bytes(process.Memory) }}</td><td>{{ bytes(process.ReadBytes) }}</td><td>{{ bytes(process.WriteBytes) }}</td>
              <td class="actions" @click.stop>
                <button class="tiny" @click="requestSignal(process, 'TERM')">TERM</button>
                <button class="tiny danger-text" @click="requestSignal(process, 'KILL')">KILL</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </article>
    <article v-if="watches.length" class="panel section-gap"><div class="panel-title"><span>Persistent process watches</span><small>{{ watches.length }} RULES</small></div><div class="table-wrap"><table><thead><tr><th>NAME</th><th>FIELD</th><th>PATTERN</th><th>STATE</th><th></th></tr></thead><tbody><tr v-for="item in watches" :key="item.id"><td>{{ item.name }}</td><td>{{ item.field }}</td><td>{{ item.pattern }}</td><td><span class="badge good">{{ item.enabled ? "TRACKING" : "DISABLED" }}</span></td><td><button class="tiny danger-text" @click="removeWatch(item.id)">delete</button></td></tr></tbody></table></div></article>
    </template>
    <ProcessDetailPanel v-if="selectedPID !== null" :pid="selectedPID" @close="selectedPID = null" />
    <ConfirmDialog v-if="pendingSignal" :title="`${pendingSignal.signal} process?`" :message="`${pendingSignal.process.Name} (PID ${pendingSignal.process.PID}) will receive ${pendingSignal.signal}. This action is audited.`" :confirm-label="`Send ${pendingSignal.signal}`" :dangerous="pendingSignal.signal === 'KILL'" @cancel="pendingSignal = null" @confirm="confirmSignal" />
    <ModalDialog v-if="showWatch" title="Create process watch" @close="showWatch = false"><div class="form-grid"><label><span>Name</span><input v-model="watch.name"></label><label><span>Match field</span><select v-model="watch.field"><option value="name">Process name</option><option value="executable">Executable</option><option value="command">Command line</option></select></label><label class="full"><span>RE2 regular expression</span><input v-model="watch.pattern" placeholder="^nginx$"></label></div><div class="modal-actions"><button class="secondary" @click="showWatch = false">Cancel</button><button class="primary" @click="saveWatch">Save rule</button></div></ModalDialog>
  </section>
</template>
