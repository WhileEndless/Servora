<script setup lang="ts">
import { monitorStore } from "@/services/MonitorStore";
import { useI18n } from "@/composables/useI18n";
const { l } = useI18n();
function value(item: Record<string, unknown>, key: string): string { return String(item[key] ?? "—"); }
</script>
<template><section class="page active"><article class="panel"><div class="panel-title"><span>{{ l("Active SSH sessions", "Aktif SSH oturumları") }}</span><small>{{ l("LIVE SESSIONS", "CANLI OTURUMLAR") }}</small></div><div v-if="!monitorStore.snapshot.value.ssh_sessions.length" class="empty">{{ l("No active remote SSH session was detected.", "Aktif uzak SSH oturumu algılanmadı.") }}</div><div v-else class="table-wrap"><table><thead><tr><th>{{ l("USER", "KULLANICI") }}</th><th>{{ l("SOURCE", "KAYNAK") }}</th><th>TTY</th><th>{{ l("SINCE", "BAŞLANGIÇ") }}</th><th>PID</th><th>{{ l("SESSION", "OTURUM") }}</th></tr></thead><tbody><tr v-for="item in monitorStore.snapshot.value.ssh_sessions" :key="value(item, 'ID')"><td>{{ value(item, "User") }}</td><td>{{ value(item, "Remote") }}</td><td>{{ value(item, "TTY") }}</td><td>{{ value(item, "Since") }}</td><td>{{ value(item, "PID") }}</td><td>{{ value(item, "ID") }}</td></tr></tbody></table></div></article></section></template>
