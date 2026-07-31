<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { monitorStore } from "@/services/MonitorStore";
import { apiClient } from "@/services/ApiClient";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import ModalDialog from "@/components/common/ModalDialog.vue";
import type { DockerImageInfo, DockerImagesResponse, DockerResponse, DockerSummary } from "@/types";

const view = ref<"containers" | "images">("containers");
const message = ref("");
const loading = ref(true);
const loadError = ref("");
const response = ref<DockerResponse | null>(null);
const pending = ref<{ item: Record<string, unknown>; verb: string } | null>(null);
const selectedImage = ref<DockerImageInfo | null>(null);
const imageResponse = ref<DockerImagesResponse | null>(null);
const imageLoading = ref(false);
const imageError = ref("");
let refreshTimer: number | undefined;
let imageTimer: number | undefined;
const emptyDocker: DockerSummary = {
  server_version: "", storage_driver: "", containers: 0,
  containers_running: 0, containers_stopped: 0, containers_paused: 0, images: 0,
};
const sourceItems = computed(() => response.value?.items ?? monitorStore.snapshot.value.containers ?? []);
const items = computed(() => sourceItems.value.filter((item) => {
  const query = monitorStore.search.value.toLowerCase();
  return !query || JSON.stringify(item).toLowerCase().includes(query);
}));
const docker = computed(() => response.value?.summary ?? monitorStore.snapshot.value.docker ?? emptyDocker);
const available = computed(() => response.value?.available ?? monitorStore.snapshot.value.capabilities.docker ?? false);
const dockerError = computed(() => response.value?.errors?.[0] ?? loadError.value ?? monitorStore.snapshot.value.errors
  ?.find((item) => item.startsWith("Docker: "))?.slice(8) ?? "");
const freshness = computed(() => {
  const raw = response.value?.freshness ?? monitorStore.snapshot.value.freshness.docker;
  if (!raw || raw.startsWith("0001-")) return "Waiting for first Docker probe";
  return `Last checked ${new Date(raw).toLocaleTimeString()}`;
});
const images = computed(() => (imageResponse.value?.items ?? []).filter((item) => {
  const query = monitorStore.search.value.toLowerCase();
  return !query || JSON.stringify(item).toLowerCase().includes(query);
}));
async function loadImages(): Promise<void> {
  imageLoading.value = true;
  try {
    imageResponse.value = await apiClient.dockerImages();
    imageError.value = "";
  } catch (error) {
    imageError.value = error instanceof Error ? error.message : "Docker image request failed";
  } finally {
    imageLoading.value = false;
  }
}
function switchView(next: "containers" | "images"): void {
  view.value = next;
  if (next === "images") void loadImages();
}
function imageName(item: DockerImageInfo): string {
  return item.references?.join(", ") || "<dangling>";
}
function shortID(id: string): string { return id.replace("sha256:", "").slice(0, 12); }
async function load(): Promise<void> {
  try {
    response.value = await apiClient.docker();
    loadError.value = "";
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : "Docker status request failed";
  } finally {
    loading.value = false;
  }
}
function text(item: Record<string, unknown>, key: string): string { return String(item[key] ?? "—"); }
function bytes(raw: unknown): string { const value = Number(raw) || 0; return value > 1e9 ? `${(value / 1e9).toFixed(1)} GB` : value > 1e6 ? `${(value / 1e6).toFixed(1)} MB` : `${(value / 1e3).toFixed(0)} KB`; }
async function control(item: Record<string, unknown>, verb: string): Promise<void> {
  if (["stop", "restart", "pause"].includes(verb)) {
    pending.value = { item, verb };
    return;
  }
  await execute(item, verb);
}
async function execute(item: Record<string, unknown>, verb: string): Promise<void> {
  const id = text(item, "ID");
  try { await apiClient.action("docker", id, { verb }); message.value = `${verb} requested`; await load(); }
  catch (error) { message.value = error instanceof Error ? error.message : "Action failed"; }
}
async function confirmAction(): Promise<void> {
  if (!pending.value) return;
  const action = pending.value; pending.value = null;
  await execute(action.item, action.verb);
}
onMounted(() => {
  void load();
  refreshTimer = window.setInterval(load, 5_000);
  imageTimer = window.setInterval(() => {
    if (view.value === "images" && !imageLoading.value) void loadImages();
  }, 60_000);
});
onBeforeUnmount(() => {
  if (refreshTimer !== undefined) window.clearInterval(refreshTimer);
  if (imageTimer !== undefined) window.clearInterval(imageTimer);
});
</script>

<template>
  <section class="page active">
    <p class="success">{{ message }}</p>
    <div class="network-page-header docker-page-header">
      <div><p class="eyebrow">DOCKER</p><h2>{{ view === "containers" ? "Containers" : "Local images" }}</h2><p>{{ view === "containers" ? "Runtime state and resource usage." : "Content-addressed images, tags, sizes and container usage." }}</p></div>
      <div class="network-view-switch"><button :class="{ active: view === 'containers' }" @click="switchView('containers')">Containers</button><button :class="{ active: view === 'images' }" @click="switchView('images')">Images</button></div>
    </div>
    <template v-if="view === 'containers'">
    <article class="docker-hero panel">
      <div class="docker-engine">
        <span class="live-pulse" :class="{ offline: !available }" />
        <div><small>DOCKER ENGINE</small><h2>{{ loading ? "Checking…" : available ? `v${docker.server_version || "detected"}` : "Unavailable" }}</h2><p>{{ freshness }}</p></div>
      </div>
      <dl>
        <div><dt>RUNNING</dt><dd>{{ docker.containers_running }}</dd></div>
        <div><dt>STOPPED</dt><dd>{{ docker.containers_stopped }}</dd></div>
        <div><dt>IMAGES</dt><dd>{{ docker.images }}</dd></div>
        <div><dt>STORAGE</dt><dd>{{ docker.storage_driver || "—" }}</dd></div>
      </dl>
    </article>
    <div v-if="loading && !response" class="empty"><span class="spinner" /><b>Checking Docker daemon…</b></div>
    <div v-else-if="!available" class="empty error-state"><b>Docker inventory is unavailable</b><span>{{ dockerError || "The command is missing, the daemon is stopped, or the privileged agent cannot reach its socket." }}</span><small>The collector retries automatically every five seconds.</small><button class="secondary retry-button" @click="load">Retry now</button></div>
    <div v-else-if="!items.length" class="empty"><b>Docker is connected — no containers exist</b><span>The daemon reported {{ docker.images }} image(s), but this host currently has no running or stopped containers.</span><small>This is a valid empty inventory, not a loading failure.</small></div>
    <div v-else class="cards"><article v-for="item in items" :key="text(item, 'ID')" class="card"><div class="panel-title"><h3>{{ text(item, "Name") }}</h3><span class="badge good">{{ text(item, "State") }}</span></div><p class="meta">{{ text(item, "Image") }}</p><div class="stat-pair"><div><small>CPU</small><b>{{ Number(item.CPU || 0).toFixed(1) }}%</b></div><div><small>MEMORY</small><b>{{ bytes(item.Memory) }} / {{ bytes(item.MemoryLimit) }}</b></div><div><small>NETWORK</small><b>↓ {{ bytes(item.NetRX) }} · ↑ {{ bytes(item.NetTX) }}</b></div><div><small>BLOCK I/O</small><b>↓ {{ bytes(item.BlockRead) }} · ↑ {{ bytes(item.BlockWrite) }}</b></div><div><small>STATUS</small><b>{{ text(item, "Status") }}</b></div><div><small>PORTS</small><b>{{ text(item, "Ports") || "—" }}</b></div></div><div class="button-row"><button v-for="verb in ['start','restart','stop','pause','unpause']" :key="verb" class="tiny" @click="control(item, verb)">{{ verb }}</button></div></article></div>
    </template>
    <template v-else>
      <div class="docker-image-summary">
        <article><small>IMAGES</small><b>{{ imageResponse?.items?.length ?? 0 }}</b></article>
        <article><small>IN USE</small><b>{{ imageResponse?.items?.filter(item => (item.container_names?.length ?? 0) > 0).length ?? 0 }}</b></article>
        <article><small>DANGLING</small><b>{{ imageResponse?.items?.filter(item => item.dangling).length ?? 0 }}</b></article>
        <article><small>LAST CHECKED</small><b>{{ imageResponse?.freshness ? new Date(imageResponse.freshness).toLocaleTimeString() : "—" }}</b></article>
      </div>
      <p v-if="imageError || imageResponse?.errors?.length" class="notice error-state">{{ imageError || imageResponse?.errors?.[0] }}</p>
      <div v-if="imageLoading && !imageResponse" class="detail-loading"><span class="spinner" /> Loading Docker images…</div>
      <div v-else-if="!imageResponse?.available" class="empty error-state"><b>Docker image inventory is unavailable</b><span>{{ imageResponse?.errors?.[0] || imageError }}</span><button class="secondary retry-button" @click="loadImages">Retry now</button></div>
      <div v-else-if="!images.length" class="empty"><b>No local images</b><span>Images will appear after the first pull or build.</span></div>
      <article v-else class="panel flush">
        <div class="table-wrap tall"><table class="interactive-table"><thead><tr><th>IMAGE</th><th>ID</th><th>DIGEST</th><th>CREATED</th><th>SIZE</th><th>USED BY</th><th>STATE</th></tr></thead><tbody>
          <tr v-for="image in images" :key="image.id" class="clickable-row" @click="selectedImage = image"><td><b>{{ imageName(image) }}</b></td><td><code>{{ shortID(image.id) }}</code></td><td><code>{{ image.repo_digests?.[0]?.split('@')[1]?.slice(0, 19) || "—" }}</code></td><td>{{ new Date(image.created_at).toLocaleString() }}</td><td>{{ bytes(image.size_bytes) }}</td><td>{{ image.container_names?.join(", ") || "—" }}</td><td><span class="badge" :class="{ good: image.container_names?.length, neutral: !image.container_names?.length }">{{ image.dangling ? "DANGLING" : image.container_names?.length ? "IN USE" : "UNUSED" }}</span></td></tr>
        </tbody></table></div>
      </article>
    </template>
    <ModalDialog v-if="selectedImage" title="Docker image details" wide @close="selectedImage = null">
      <div class="detail-hero package-detail-hero"><div><span class="badge" :class="{ good: selectedImage.container_names.length }">{{ selectedImage.dangling ? "DANGLING" : selectedImage.container_names.length ? "IN USE" : "UNUSED" }}</span><h3>{{ imageName(selectedImage) }}</h3><p><code>{{ selectedImage.id }}</code></p></div><div class="detail-metrics"><div><small>SIZE</small><b>{{ bytes(selectedImage.size_bytes) }}</b></div><div><small>CREATED</small><b>{{ new Date(selectedImage.created_at).toLocaleString() }}</b></div><div><small>REFERENCES</small><b>{{ selectedImage.references.length }}</b></div><div><small>CONTAINERS</small><b>{{ selectedImage.container_names.length }}</b></div></div></div>
      <section><h4>Repository digests</h4><div class="code-list"><code v-for="digest in selectedImage.repo_digests" :key="digest">{{ digest }}</code><p v-if="!selectedImage.repo_digests.length" class="subtle-empty">No repository digest is recorded for this local image.</p></div></section>
      <section class="section-gap"><h4>Containers using this image</h4><div class="code-list"><code v-for="name in selectedImage.container_names" :key="name">{{ name }}</code><p v-if="!selectedImage.container_names.length" class="subtle-empty">No container currently references this image.</p></div></section>
    </ModalDialog>
    <ConfirmDialog v-if="pending" :title="`${pending.verb} container?`" :message="`${text(pending.item, 'Name')} will be ${pending.verb === 'stop' ? 'stopped' : pending.verb + 'd'}. This action is audited.`" :confirm-label="pending.verb" :dangerous="pending.verb === 'stop'" @cancel="pending = null" @confirm="confirmAction" />
  </section>
</template>
