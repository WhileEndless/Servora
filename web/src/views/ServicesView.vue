<script setup lang="ts">
import { computed, ref } from "vue";
import { monitorStore } from "@/services/MonitorStore";
import { useI18n } from "@/composables/useI18n";
import { apiClient } from "@/services/ApiClient";
import type { ServiceInfo } from "@/types";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";

const message = ref("");
const { l } = useI18n();
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
  <section class="page active"><div class="toolbar"><span class="summary">{{ services.length }} {{ l("services", "servis") }}</span><span class="success">{{ message }}</span></div><article class="panel"><div class="table-wrap tall"><table><thead><tr><th>{{ l("UNIT", "BİRİM") }}</th><th>{{ l("STATE", "DURUM") }}</th><th>SUB</th><th>{{ l("RUNNING", "ÇALIŞMA") }}</th><th>PID</th><th>{{ l("RESTARTS", "YENİDEN BAŞLATMA") }}</th><th>{{ l("DESCRIPTION", "AÇIKLAMA") }}</th><th>{{ l("ACTIONS", "İŞLEMLER") }}</th></tr></thead><tbody><tr v-for="item in services" :key="item.Name"><td>{{ item.Name }}</td><td><span class="status" :class="item.Active === 'active' ? 'ok' : 'warn'">{{ item.Active }}</span></td><td>{{ item.Sub }}</td><td :title="item.ActiveSince">{{ item.Duration || "—" }}</td><td>{{ item.PID || "—" }}</td><td>{{ item.Restarts }}</td><td>{{ item.Description }}</td><td class="actions"><template v-if="item.Manageable && !item.Protected"><button v-for="verb in ['start','restart','stop']" :key="verb" class="tiny" @click="control(item, verb)">{{ verb === 'start' ? l('start', 'başlat') : verb === 'restart' ? l('restart', 'yeniden başlat') : l('stop', 'durdur') }}</button></template><span v-else class="badge">{{ item.Protected ? l("PROTECTED", "KORUMALI") : l("READ ONLY", "SALT OKUNUR") }}</span></td></tr></tbody></table></div></article><ConfirmDialog v-if="pending" :title="l('{verb} service?', '{verb} servisi?', { verb: pending.verb })" :message="l('{name} will be changed. This action is audited.', '{name} servisi değiştirilecek. Bu işlem denetlenir.', { name: pending.item.Name })" :confirm-label="pending.verb" :dangerous="pending.verb === 'stop'" @cancel="pending = null" @confirm="confirmAction" /></section>
</template>
