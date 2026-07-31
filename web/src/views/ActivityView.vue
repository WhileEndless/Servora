<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { apiClient } from "@/services/ApiClient";
import { monitorStore } from "@/services/MonitorStore";
import { useI18n } from "@/composables/useI18n";
interface Audit { id: number; time: string; user: string; ip: string; action: string; target: string; success: boolean; result: string }
const items = ref<Audit[]>([]);
const { locale, l } = useI18n();
const filtered = computed(() => items.value.filter((item) => !monitorStore.search.value || JSON.stringify(item).toLowerCase().includes(monitorStore.search.value.toLowerCase())));
onMounted(async () => { items.value = await apiClient.list<Audit>("/activities"); });
</script>
<template><section class="page active"><article class="panel"><div class="panel-title"><span>{{ l("Audit trail", "Denetim geçmişi") }}</span><small>{{ filtered.length }} {{ l("EVENTS", "OLAY") }}</small></div><div class="table-wrap tall"><table><thead><tr><th>{{ l("TIME", "ZAMAN") }}</th><th>{{ l("USER / IP", "KULLANICI / IP") }}</th><th>{{ l("ACTION", "İŞLEM") }}</th><th>{{ l("TARGET", "HEDEF") }}</th><th>{{ l("RESULT", "SONUÇ") }}</th></tr></thead><tbody><tr v-for="item in filtered" :key="item.id"><td>{{ new Date(item.time).toLocaleString(locale) }}</td><td>{{ item.user }} · {{ item.ip }}</td><td>{{ item.action }}</td><td>{{ item.target || "—" }}</td><td><span class="status" :class="item.success ? 'ok' : 'bad'">{{ item.result }}</span></td></tr></tbody></table></div></article></section></template>
