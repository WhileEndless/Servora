<script setup lang="ts">
import { onMounted, ref } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import { apiClient } from "@/services/ApiClient";

interface AlertEvent { id: string; name: string; severity: string; state: string; message: string; started_at: string; updated_at: string }
interface AlertRule { id: string; name: string; source: string; operator: string; threshold: number; for_seconds: number; cooldown_seconds: number; severity: string; target_ids: string; enabled: boolean; notify_recovery: boolean }
interface Target { id: string; name: string; provider: string; chat_id: string; secret_ref: string; enabled: boolean }

const tab = ref<"events" | "rules" | "targets">("events");
const events = ref<AlertEvent[]>([]);
const rules = ref<AlertRule[]>([]);
const targets = ref<Target[]>([]);
const modal = ref<"rule" | "target" | null>(null);
const error = ref("");
const message = ref("");
const selectedTargets = ref<string[]>([]);
const pendingDelete = ref<string | null>(null);
const rule = ref({ name: "", source: "memory", operator: ">=", threshold: 90, for_seconds: 300, cooldown_seconds: 900, severity: "warning", target_ids: "", enabled: true, notify_recovery: true });
const target = ref({ name: "", chat_id: "", token: "", enabled: true });
const metricSources = [
  ["cpu", "CPU usage (%)"], ["memory", "Memory usage (%)"], ["swap", "Swap usage (%)"],
  ["load", "System load"], ["disk", "Disk usage (%)"], ["processes", "Process count"],
  ["containers", "Container count"], ["network_total_1h", "Network total · last hour (bytes)"],
  ["network_total_24h", "Network total · last 24 hours (bytes)"],
  ["network_rx_24h", "Network received · last 24 hours (bytes)"],
  ["network_tx_24h", "Network sent · last 24 hours (bytes)"],
] as const;

async function load(): Promise<void> {
  [events.value, rules.value, targets.value] = await Promise.all([
    apiClient.list<AlertEvent>("/alerts"),
    apiClient.list<AlertRule>("/alert-rules"),
    apiClient.list<Target>("/notification-targets"),
  ]);
}
async function saveRule(): Promise<void> {
  error.value = "";
  try {
    await apiClient.create("/alert-rules", {
      name: rule.value.name, source: rule.value.source, operator: rule.value.operator,
      threshold: Number(rule.value.threshold), for_seconds: Number(rule.value.for_seconds),
      cooldown_seconds: Number(rule.value.cooldown_seconds), severity: rule.value.severity,
      target_ids: selectedTargets.value.join(","), enabled: true, notify_recovery: true,
    });
    modal.value = null; await load();
  } catch (cause) { error.value = cause instanceof Error ? cause.message : "Could not save rule"; }
}
async function saveTarget(): Promise<void> {
  error.value = "";
  try {
    await apiClient.create("/notification-targets", {
      name: target.value.name, provider: "telegram", chat_id: target.value.chat_id,
      token: target.value.token, enabled: true,
    });
    target.value.token = ""; modal.value = null; await load();
  } catch (cause) { error.value = cause instanceof Error ? cause.message : "Could not save destination"; }
}
async function remove(): Promise<void> {
  if (!pendingDelete.value) return;
  const path = pendingDelete.value;
  pendingDelete.value = null;
  await apiClient.remove(path);
  await load();
}
async function test(id: string): Promise<void> {
  message.value = "";
  try {
    await apiClient.create(`/notification-targets/${id}/test`, {});
    message.value = "Telegram accepted the test message.";
  } catch (cause) { message.value = cause instanceof Error ? cause.message : "Telegram test failed"; }
}
async function acknowledge(id: string): Promise<void> { await apiClient.create(`/alerts/${id}/acknowledge`, {}); await load(); }
function targetNames(ids: string): string {
  if (!ids) return "Dashboard only";
  const names = ids.split(",").map((id) => targets.value.find((item) => item.id === id)?.name ?? id);
  return names.join(", ");
}
onMounted(load);
</script>

<template>
  <section class="page active">
    <div class="integration-banner">
      <div class="integration-icon">✈</div>
      <div><strong>Telegram notifications</strong><p>{{ targets.length ? `${targets.length} destination configured. Assign destinations while creating an alert rule.` : "No destination configured yet. Add a bot token and chat or group ID to receive alerts." }}</p></div>
      <button class="secondary" @click="modal = 'target'">{{ targets.length ? "Add destination" : "Configure Telegram" }}</button>
    </div>
    <p v-if="message" class="notice">{{ message }}</p>
    <div class="toolbar"><div class="tabs"><button :class="{ active: tab === 'events' }" @click="tab = 'events'">Events</button><button :class="{ active: tab === 'rules' }" @click="tab = 'rules'">Rules</button><button :class="{ active: tab === 'targets' }" @click="tab = 'targets'">Telegram</button></div><div><button class="secondary" @click="modal = 'target'">+ Telegram</button> <button class="primary" @click="modal = 'rule'">+ New rule</button></div></div>
    <div v-if="tab === 'events'"><div v-if="!events.length" class="empty">No alert event has been recorded.</div><article v-for="item in events" :key="item.id" class="alert-card" :class="[item.state, `severity-${item.severity}`]"><div class="alert-event-copy"><div class="alert-event-title"><b>{{ item.name }}</b><span class="severity-label" :class="item.severity">{{ item.severity }}</span></div><p>{{ item.message }}</p><small>{{ new Date(item.updated_at).toLocaleString() }}</small></div><button v-if="item.state === 'firing'" class="tiny" @click="acknowledge(item.id)">Acknowledge</button><span v-else class="alert-state" :class="item.state"><i />{{ item.state === "resolved" ? "Recovered" : item.state }}</span></article></div>
    <article v-else-if="tab === 'rules'" class="panel"><div class="table-wrap"><table><thead><tr><th>NAME</th><th>CONDITION</th><th>FOR</th><th>SEVERITY</th><th>TARGETS</th><th></th></tr></thead><tbody><tr v-for="item in rules" :key="item.id"><td>{{ item.name }}</td><td>{{ item.source }} {{ item.operator }} {{ item.threshold }}</td><td>{{ item.for_seconds }}s</td><td>{{ item.severity }}</td><td>{{ targetNames(item.target_ids) }}</td><td><button class="tiny danger-text" @click="pendingDelete = `/alert-rules/${item.id}`">delete</button></td></tr></tbody></table></div></article>
    <div v-else class="cards"><article v-for="item in targets" :key="item.id" class="card"><div class="panel-title"><h3>{{ item.name }}</h3><span class="badge good">TELEGRAM</span></div><p class="meta">Chat ID: {{ item.chat_id }}</p><div class="button-row"><button class="tiny" @click="test(item.id)">send test</button><button class="tiny danger-text" @click="pendingDelete = `/notification-targets/${item.id}`">delete</button></div></article></div>
    <ModalDialog v-if="modal === 'rule'" title="Create alert rule" @close="modal = null"><div class="form-grid"><label class="full"><span>Name</span><input v-model="rule.name"></label><label><span>Metric</span><select v-model="rule.source"><option v-for="source in metricSources" :key="source[0]" :value="source[0]">{{ source[1] }}</option></select></label><label><span>Operator</span><select v-model="rule.operator"><option v-for="op in ['>','>=','<','<=','==']" :key="op">{{ op }}</option></select></label><label><span>Threshold{{ rule.source.startsWith('network_') ? ' (bytes)' : '' }}</span><input v-model.number="rule.threshold" type="number"></label><label><span>Required duration (s)</span><input v-model.number="rule.for_seconds" type="number"></label><label><span>Severity</span><select v-model="rule.severity"><option>info</option><option>warning</option><option>critical</option></select></label><fieldset class="full target-picker"><legend>Notification destinations</legend><label v-for="item in targets" :key="item.id"><input v-model="selectedTargets" type="checkbox" :value="item.id"><span><b>{{ item.name }}</b><small>Telegram · {{ item.chat_id }}</small></span></label><p v-if="!targets.length" class="muted">No Telegram destination yet. The alert will remain visible on the dashboard.</p></fieldset></div><p class="error">{{ error }}</p><div class="modal-actions"><button class="secondary" @click="modal = null">Cancel</button><button class="primary" @click="saveRule">Save rule</button></div></ModalDialog>
    <ModalDialog v-if="modal === 'target'" title="Connect Telegram" @close="modal = null"><div class="setup-steps"><span>1</span><p>Create a bot with <b>@BotFather</b> and copy its token.</p><span>2</span><p>Add the bot to the target group, then enter the group or user chat ID.</p><span>3</span><p>Save and use <b>send test</b> before assigning the destination to a rule.</p></div><div class="form-grid"><label class="full"><span>Destination name</span><input v-model="target.name" placeholder="Operations group"></label><label class="full"><span>Chat, user or group ID</span><input v-model="target.chat_id" placeholder="-1001234567890"></label><label class="full"><span>Bot token (stored write-only)</span><input v-model="target.token" type="password" autocomplete="off" placeholder="123456:ABC…"></label></div><p class="error">{{ error }}</p><div class="modal-actions"><button class="secondary" @click="modal = null">Cancel</button><button class="primary" @click="saveTarget">Save destination</button></div></ModalDialog>
    <ConfirmDialog v-if="pendingDelete" title="Delete this configuration?" message="This item will be permanently removed. Existing audit records remain available." confirm-label="delete" dangerous @cancel="pendingDelete = null" @confirm="remove" />
  </section>
</template>
