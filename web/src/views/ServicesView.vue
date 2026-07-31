<script setup lang="ts">
import { computed, ref } from "vue";
import { monitorStore } from "@/services/MonitorStore";
import { apiClient } from "@/services/ApiClient";
import type { ServiceInfo } from "@/types";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";

const message = ref("");
const pending = ref<{ item: ServiceInfo; verb: string } | null>(null);
const services = computed(() => {
  const query = monitorStore.search.value.toLowerCase();
  return monitorStore.snapshot.value.services.filter((item) =>
    !query || `${item.Name} ${item.Description} ${item.Active} ${item.Sub}`.toLowerCase().includes(query),
  );
});
async function control(item: ServiceInfo, verb: string): Promise<void> {
  if (verb !== "start") { pending.value = { item, verb }; return; }
  await execute(item, verb);
}
async function execute(item: ServiceInfo, verb: string): Promise<void> {
  try {
    await apiClient.action("service", item.Name, { verb });
    message.value = `${item.Name}: ${verb} requested`;
  } catch (error) { message.value = error instanceof Error ? error.message : "Action failed"; }
}
async function confirmAction(): Promise<void> {
  if (!pending.value) return;
  const action = pending.value; pending.value = null;
  await execute(action.item, action.verb);
}
</script>

<template>
  <section class="page active"><div class="toolbar"><span class="summary">{{ services.length }} services</span><span class="success">{{ message }}</span></div><article class="panel"><div class="table-wrap tall"><table><thead><tr><th>UNIT</th><th>STATE</th><th>SUB</th><th>RUNNING</th><th>PID</th><th>RESTARTS</th><th>DESCRIPTION</th><th>ACTIONS</th></tr></thead><tbody><tr v-for="item in services" :key="item.Name"><td>{{ item.Name }}</td><td><span class="status" :class="item.Active === 'active' ? 'ok' : 'warn'">{{ item.Active }}</span></td><td>{{ item.Sub }}</td><td :title="item.ActiveSince">{{ item.Duration || "—" }}</td><td>{{ item.PID || "—" }}</td><td>{{ item.Restarts }}</td><td>{{ item.Description }}</td><td class="actions"><template v-if="item.Manageable && !item.Protected"><button v-for="verb in ['start','restart','stop']" :key="verb" class="tiny" @click="control(item, verb)">{{ verb }}</button></template><span v-else class="badge">{{ item.Protected ? "PROTECTED" : "READ ONLY" }}</span></td></tr></tbody></table></div></article><ConfirmDialog v-if="pending" :title="`${pending.verb} service?`" :message="`${pending.item.Name} will be ${pending.verb === 'stop' ? 'stopped' : 'restarted'}. This action is audited.`" :confirm-label="pending.verb" :dangerous="pending.verb === 'stop'" @cancel="pending = null" @confirm="confirmAction" /></section>
</template>
