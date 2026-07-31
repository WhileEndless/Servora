<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { apiClient } from "@/services/ApiClient";
import { monitorStore } from "@/services/MonitorStore";
interface Audit { id: number; time: string; user: string; ip: string; action: string; target: string; success: boolean; result: string }
const items = ref<Audit[]>([]);
const filtered = computed(() => items.value.filter((item) => !monitorStore.search.value || JSON.stringify(item).toLowerCase().includes(monitorStore.search.value.toLowerCase())));
onMounted(async () => { items.value = await apiClient.list<Audit>("/activities"); });
</script>
<template><section class="page active"><article class="panel"><div class="panel-title"><span>Audit trail</span><small>{{ filtered.length }} EVENTS</small></div><div class="table-wrap tall"><table><thead><tr><th>TIME</th><th>USER / IP</th><th>ACTION</th><th>TARGET</th><th>RESULT</th></tr></thead><tbody><tr v-for="item in filtered" :key="item.id"><td>{{ new Date(item.time).toLocaleString() }}</td><td>{{ item.user }} · {{ item.ip }}</td><td>{{ item.action }}</td><td>{{ item.target || "—" }}</td><td><span class="status" :class="item.success ? 'ok' : 'bad'">{{ item.result }}</span></td></tr></tbody></table></div></article></section></template>
