<script setup lang="ts">
import { ref } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import { monitorStore } from "@/services/MonitorStore";
import { apiClient } from "@/services/ApiClient";

const showCreate = ref(false);
const error = ref("");
const pendingDelete = ref<{ item: Record<string, unknown>; name: string } | null>(null);
const form = ref({ name: "", calendar: "daily", executable: "/opt/system-maintenance/bin/system-maintenance", args: "[]", user: "root" });
function value(item: Record<string, unknown>, key: string): string { return String(item[key] ?? "—"); }
function managed(item: Record<string, unknown>): boolean { return item.Managed === true; }
async function save(): Promise<void> {
  error.value = "";
  try {
    JSON.parse(form.value.args);
    await apiClient.schedule("schedule.create", form.value.name, {
      calendar: form.value.calendar, executable: form.value.executable,
      args: form.value.args, user: form.value.user,
    });
    showCreate.value = false; await monitorStore.refresh();
  } catch (cause) { error.value = cause instanceof Error ? cause.message : "Could not create schedule"; }
}
async function operation(item: Record<string, unknown>, action: string): Promise<void> {
  const unit = value(item, "Unit");
  const name = unit.replace(/^system-maintenance-job-/, "").replace(/\.timer$/, "");
  if (action === "delete") {
    pendingDelete.value = { item, name };
    return;
  }
  await apiClient.schedule(`schedule.${action}`, name, action === "toggle" ? { enabled: "false" } : {});
  await monitorStore.refresh();
}
async function confirmDelete(): Promise<void> {
  if (!pendingDelete.value) return;
  const { name } = pendingDelete.value;
  pendingDelete.value = null;
  await apiClient.schedule("schedule.delete", name);
  await monitorStore.refresh();
}
</script>

<template>
  <section class="page active">
    <div class="toolbar"><p class="muted">Existing timers are read-only. New jobs use isolated, managed systemd units.</p><button class="primary" @click="showCreate = true">+ New schedule</button></div>
    <article class="panel"><div class="table-wrap"><table><thead><tr><th>UNIT</th><th>NEXT</th><th>LEFT</th><th>LAST</th><th>MANAGED</th><th>ACTIONS</th></tr></thead><tbody><tr v-for="item in monitorStore.snapshot.value.timers" :key="value(item, 'Unit')"><td>{{ value(item, "Unit") }}</td><td>{{ value(item, "Next") }}</td><td>{{ value(item, "Left") }}</td><td>{{ value(item, "Last") }}</td><td><span class="badge" :class="{ good: managed(item) }">{{ managed(item) ? "YES" : "READ ONLY" }}</span></td><td class="actions"><template v-if="managed(item)"><button class="tiny" @click="operation(item, 'run')">run now</button><button class="tiny" @click="operation(item, 'toggle')">disable</button><button class="tiny danger-text" @click="operation(item, 'delete')">delete</button></template></td></tr></tbody></table></div></article>
    <ModalDialog v-if="showCreate" title="Create managed schedule" @close="showCreate = false"><div class="form-grid"><label><span>Name (lowercase)</span><input v-model="form.name" placeholder="daily-report"></label><label><span>Run as user</span><input v-model="form.user"></label><label class="full"><span>OnCalendar expression</span><input v-model="form.calendar" placeholder="Mon..Fri 03:00"></label><label class="full"><span>Allowlisted executable</span><input v-model="form.executable"></label><label class="full"><span>Arguments (JSON array)</span><input v-model="form.args"></label></div><p class="error">{{ error }}</p><div class="modal-actions"><button class="secondary" @click="showCreate = false">Cancel</button><button class="primary" @click="save">Create and enable</button></div></ModalDialog>
    <ConfirmDialog v-if="pendingDelete" title="Delete managed schedule?" :message="`${pendingDelete.name} and its managed systemd units will be removed.`" confirm-label="delete" dangerous @cancel="pendingDelete = null" @confirm="confirmDelete" />
  </section>
</template>
