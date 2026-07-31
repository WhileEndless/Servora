<script setup lang="ts">
import { monitorStore } from "@/services/MonitorStore";
function value(item: Record<string, unknown>, key: string): string { return String(item[key] ?? "—"); }
</script>
<template><section class="page active"><article class="panel"><div class="panel-title"><span>Active SSH sessions</span><small>LIVE SESSIONS</small></div><div v-if="!monitorStore.snapshot.value.ssh_sessions.length" class="empty">No active remote SSH session was detected.</div><div v-else class="table-wrap"><table><thead><tr><th>USER</th><th>SOURCE</th><th>TTY</th><th>SINCE</th><th>PID</th><th>SESSION</th></tr></thead><tbody><tr v-for="item in monitorStore.snapshot.value.ssh_sessions" :key="value(item, 'ID')"><td>{{ value(item, "User") }}</td><td>{{ value(item, "Remote") }}</td><td>{{ value(item, "TTY") }}</td><td>{{ value(item, "Since") }}</td><td>{{ value(item, "PID") }}</td><td>{{ value(item, "ID") }}</td></tr></tbody></table></div></article></section></template>
