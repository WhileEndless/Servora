<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import { apiClient } from "@/services/ApiClient";
import { useI18n } from "@/composables/useI18n";
import type { PackageEventsResponse, PackageFilesResponse, PackageInfo, PackageListResponse } from "@/types";

const { locale } = useI18n();
const view = ref<"inventory" | "changes">("inventory");
const query = ref("");
const status = ref("");
const sort = ref("name");
const eventType = ref("");
const page = ref(1);
const loading = ref(true);
const error = ref("");
const response = ref<PackageListResponse | null>(null);
const events = ref<PackageEventsResponse | null>(null);
const selected = ref<PackageInfo | null>(null);
const files = ref<PackageFilesResponse | null>(null);
const fileQuery = ref("");
const filePage = ref(1);
const detailLoading = ref(false);
let searchTimer: number | undefined;
let pollTimer: number | undefined;

const tr = computed(() => locale.value === "tr");
const pages = computed(() => Math.max(1, Math.ceil((view.value === "inventory" ? response.value?.total ?? 0 : events.value?.total ?? 0) / 100)));

function params(): URLSearchParams {
  const value = new URLSearchParams({ page: String(page.value), per_page: "100", q: query.value });
  if (status.value) value.set("status", status.value);
  value.set("sort", sort.value);
  return value;
}
async function load(): Promise<void> {
  loading.value = true;
  try {
    if (view.value === "inventory") {
      response.value = await apiClient.packages(params());
    } else {
      const value = new URLSearchParams({
        page: String(page.value), per_page: "100", q: query.value,
        from: new Date(Date.now() - 365 * 86_400_000).toISOString(),
      });
      if (eventType.value) value.set("type", eventType.value);
      events.value = await apiClient.packageEvents(value);
    }
    error.value = "";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Package inventory could not be loaded";
  } finally {
    loading.value = false;
  }
}
async function refresh(): Promise<void> {
  try {
    await apiClient.refreshPackages();
    await load();
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Update check could not be started";
  }
}
async function openPackage(item: PackageInfo): Promise<void> {
  selected.value = item;
  files.value = null;
  fileQuery.value = "";
  filePage.value = 1;
  await loadFiles();
}
async function loadFiles(): Promise<void> {
  if (!selected.value) return;
  detailLoading.value = true;
  try {
    files.value = await apiClient.packageFiles(selected.value.id, fileQuery.value, filePage.value);
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Package paths could not be loaded";
  } finally {
    detailLoading.value = false;
  }
}
function switchView(next: "inventory" | "changes"): void {
  view.value = next;
  page.value = 1;
  query.value = "";
  void load();
}
function changePage(next: number): void {
  page.value = Math.max(1, Math.min(pages.value, next));
  void load();
}
function bytes(value: number): string {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GiB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  return `${Math.round(value / 1024)} KiB`;
}
function when(value?: string): string {
  if (!value || value.startsWith("0001-")) return "—";
  return new Date(value).toLocaleString(locale.value);
}
function eventLabel(value: string): string {
  const labels = tr.value
    ? { installed: "Kuruldu", removed: "Kaldırıldı", version_changed: "Sürüm değişti" }
    : { installed: "Installed", removed: "Removed", version_changed: "Version changed" };
  return labels[value as keyof typeof labels] ?? value;
}

watch([status, sort, eventType], () => { page.value = 1; void load(); });
watch(query, () => {
  page.value = 1;
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(load, 250);
});
watch(fileQuery, () => {
  filePage.value = 1;
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(loadFiles, 250);
});
onMounted(() => {
  void load();
  pollTimer = window.setInterval(() => {
    if (view.value === "inventory" && response.value?.status.refreshing) void load();
  }, 3_000);
});
onBeforeUnmount(() => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  if (pollTimer !== undefined) window.clearInterval(pollTimer);
});
</script>

<template>
  <section class="page active package-intelligence">
    <div class="network-page-header">
      <div>
        <p class="eyebrow">{{ tr ? "SİSTEM ENVANTERİ" : "SYSTEM INVENTORY" }}</p>
        <h2>{{ view === "inventory" ? (tr ? "Kurulu paketler" : "Installed packages") : (tr ? "Paket değişimleri" : "Package changes") }}</h2>
        <p>{{ tr ? "Kurulu sürümleri, güncelleme adaylarını ve dosya konumlarını izleyin." : "Track installed versions, update candidates and file locations." }}</p>
      </div>
      <div class="network-view-switch">
        <button :class="{ active: view === 'inventory' }" @click="switchView('inventory')">{{ tr ? "Envanter" : "Inventory" }}</button>
        <button :class="{ active: view === 'changes' }" @click="switchView('changes')">{{ tr ? "Değişimler" : "Changes" }}</button>
      </div>
    </div>

    <div v-if="view === 'inventory'" class="package-summary-grid">
      <article><small>{{ tr ? "KURULU" : "INSTALLED" }}</small><b>{{ response?.summary.installed ?? 0 }}</b></article>
      <article><small>{{ tr ? "GÜNCELLEME" : "UPDATES" }}</small><b class="warning-text">{{ response?.summary.updates ?? 0 }}</b></article>
      <article><small>{{ tr ? "BİLİNMİYOR" : "UNKNOWN" }}</small><b>{{ response?.summary.unknown ?? 0 }}</b></article>
      <article><small>HOST</small><b>{{ response?.status.hostname || "—" }}</b><span>{{ (response?.status.manager || "—").toUpperCase() }}</span></article>
    </div>

    <div class="network-toolbar package-toolbar">
      <div class="tabs" v-if="view === 'inventory'">
        <button :class="{ active: status === '' }" @click="status = ''">{{ tr ? "Tümü" : "All" }}</button>
        <button :class="{ active: status === 'updates' }" @click="status = 'updates'">{{ tr ? "Güncellemeler" : "Updates" }}</button>
        <button :class="{ active: status === 'unknown' }" @click="status = 'unknown'">{{ tr ? "Bilinmeyen" : "Unknown" }}</button>
      </div>
      <select v-if="view === 'inventory'" v-model="sort" class="package-sort">
        <option value="name">{{ tr ? "Ada göre" : "Sort by name" }}</option>
        <option value="status">{{ tr ? "Duruma göre" : "Sort by status" }}</option>
        <option value="size">{{ tr ? "Boyuta göre" : "Sort by size" }}</option>
        <option value="changed">{{ tr ? "Değişim zamanına göre" : "Sort by changed time" }}</option>
      </select>
      <select v-else v-model="eventType" class="package-event-filter">
        <option value="">{{ tr ? "Tüm değişimler" : "All changes" }}</option>
        <option value="installed">{{ tr ? "Kurulanlar" : "Installed" }}</option>
        <option value="removed">{{ tr ? "Kaldırılanlar" : "Removed" }}</option>
        <option value="version_changed">{{ tr ? "Sürüm değişimleri" : "Version changes" }}</option>
      </select>
      <label class="usage-search">
        <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" /></svg>
        <input v-model="query" :placeholder="tr ? 'Paket, kaynak veya açıklama ara…' : 'Search package, source or description…'">
      </label>
      <button v-if="view === 'inventory'" class="secondary" :disabled="response?.status.refreshing" @click="refresh">
        {{ response?.status.refreshing ? (tr ? "Kontrol ediliyor…" : "Checking…") : (tr ? "Güncellemeleri kontrol et" : "Check for updates") }}
      </button>
    </div>

    <p v-if="response?.status.error && view === 'inventory'" class="notice warning-state">{{ response.status.error }}</p>
    <p v-if="error" class="notice error-state">{{ error }}</p>
    <div v-if="view === 'inventory'" class="inventory-freshness">
      <span>{{ tr ? "Son envanter" : "Inventory scanned" }}: <b>{{ when(response?.status.inventory_scanned_at) }}</b></span>
      <span>{{ tr ? "Depo metadatası" : "Repository metadata" }}: <b>{{ when(response?.status.metadata_refreshed_at) }}</b></span>
    </div>

    <article class="panel flush usage-panel">
      <div v-if="loading && !(response || events)" class="detail-loading"><span class="spinner" /> {{ tr ? "Paketler yükleniyor…" : "Loading packages…" }}</div>
      <div v-else-if="view === 'inventory' && !response?.items.length" class="empty"><b>{{ tr ? "Eşleşen paket yok" : "No matching packages" }}</b></div>
      <div v-else-if="view === 'inventory'" class="table-wrap tall">
        <table class="interactive-table package-table">
          <thead><tr><th>{{ tr ? "PAKET" : "PACKAGE" }}</th><th>{{ tr ? "KURULU SÜRÜM" : "INSTALLED" }}</th><th>{{ tr ? "ADAY SÜRÜM" : "CANDIDATE" }}</th><th>{{ tr ? "MİMARİ" : "ARCH" }}</th><th>{{ tr ? "BOYUT" : "SIZE" }}</th><th>{{ tr ? "DURUM" : "STATUS" }}</th></tr></thead>
          <tbody><tr v-for="item in response?.items" :key="item.id" class="clickable-row" @click="openPackage(item)">
            <td><b>{{ item.name }}</b><small class="cell-subtitle">{{ item.summary || item.source || "—" }}</small></td>
            <td><code>{{ item.installed_version }}</code></td>
            <td><code :class="{ 'warning-text': item.candidate_version }">{{ item.candidate_version || "—" }}</code></td>
            <td>{{ item.architecture }}</td><td>{{ bytes(item.installed_size_bytes) }}</td>
            <td><span class="badge" :class="{ good: item.update_state === 'current', bad: item.update_state === 'update_available' }">{{ item.update_state.replace(/_/g, " ").toUpperCase() }}</span></td>
          </tr></tbody>
        </table>
      </div>
      <div v-else-if="!events?.items.length" class="empty"><b>{{ tr ? "Henüz paket değişimi yok" : "No package changes recorded yet" }}</b></div>
      <div v-else class="table-wrap tall">
        <table><thead><tr><th>{{ tr ? "ZAMAN" : "TIME" }}</th><th>{{ tr ? "OLAY" : "EVENT" }}</th><th>{{ tr ? "PAKET" : "PACKAGE" }}</th><th>{{ tr ? "ÖNCEKİ" : "OLD" }}</th><th>{{ tr ? "YENİ" : "NEW" }}</th></tr></thead>
          <tbody><tr v-for="item in events?.items" :key="item.id"><td>{{ when(item.time) }}</td><td><span class="badge">{{ eventLabel(item.event_type) }}</span></td><td><b>{{ item.name }}</b> · {{ item.architecture }}</td><td><code>{{ item.old_version || "—" }}</code></td><td><code>{{ item.new_version || "—" }}</code></td></tr></tbody>
        </table>
      </div>
    </article>
    <div class="package-pagination"><button class="tiny" :disabled="page <= 1" @click="changePage(page - 1)">←</button><span>{{ page }} / {{ pages }}</span><button class="tiny" :disabled="page >= pages" @click="changePage(page + 1)">→</button></div>

    <ModalDialog v-if="selected" :title="selected.name" wide @close="selected = null">
      <div class="detail-hero package-detail-hero">
        <div><span class="badge">{{ selected.manager.toUpperCase() }}</span><h3>{{ selected.name }} · {{ selected.architecture }}</h3><p>{{ selected.summary || selected.source || "—" }}</p></div>
        <div class="detail-metrics"><div><small>{{ tr ? "KURULU" : "INSTALLED" }}</small><b>{{ selected.installed_version }}</b></div><div><small>{{ tr ? "ADAY" : "CANDIDATE" }}</small><b>{{ selected.candidate_version || "—" }}</b></div><div><small>{{ tr ? "BOYUT" : "SIZE" }}</small><b>{{ bytes(selected.installed_size_bytes) }}</b></div><div><small>HOST</small><b>{{ response?.status.hostname || "—" }}</b></div></div>
      </div>
      <section class="package-files">
        <div class="section-heading"><div><h3>{{ tr ? "Kurulum yolları" : "Installed paths" }}</h3><p>{{ files?.total ?? 0 }} {{ tr ? "dosya ve dizin" : "files and directories" }}</p></div><input v-model="fileQuery" :placeholder="tr ? 'Yollarda ara…' : 'Search paths…'"></div>
        <div v-if="detailLoading" class="detail-loading"><span class="spinner" /></div>
        <div v-else class="code-list package-code-list"><code v-for="file in files?.items" :key="file">{{ file }}</code></div>
        <div class="package-pagination"><button class="tiny" :disabled="filePage <= 1" @click="filePage--; loadFiles()">←</button><span>{{ filePage }} / {{ Math.max(1, Math.ceil((files?.total ?? 0) / 200)) }}</span><button class="tiny" :disabled="filePage * 200 >= (files?.total ?? 0)" @click="filePage++; loadFiles()">→</button></div>
      </section>
    </ModalDialog>
  </section>
</template>
