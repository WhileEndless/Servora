<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import AppHeader from "@/components/layout/AppHeader.vue";
import AppSidebar from "@/components/layout/AppSidebar.vue";
import LoginView from "@/views/LoginView.vue";
import OverviewView from "@/views/OverviewView.vue";
import ProcessesView from "@/views/ProcessesView.vue";
import ServicesView from "@/views/ServicesView.vue";
import DockerView from "@/views/DockerView.vue";
import PackagesView from "@/views/PackagesView.vue";
import NetworkView from "@/views/NetworkView.vue";
import SshView from "@/views/SshView.vue";
import SchedulesView from "@/views/SchedulesView.vue";
import AlertsView from "@/views/AlertsView.vue";
import ActivityView from "@/views/ActivityView.vue";
import SettingsView from "@/views/SettingsView.vue";
import { monitorStore } from "@/services/MonitorStore";
import type { PageName } from "@/types";
import { useI18n } from "@/composables/useI18n";

const restoring = ref(true);
const mobileMenuOpen = ref(false);
const { l } = useI18n();
const viewMap = {
  overview: OverviewView, processes: ProcessesView, services: ServicesView,
  docker: DockerView, network: NetworkView, ssh: SshView,
  packages: PackagesView,
  schedules: SchedulesView, alerts: AlertsView, activity: ActivityView,
  settings: SettingsView,
};
const activeView = computed(() => viewMap[monitorStore.page.value]);
const pages = new Set<PageName>(Object.keys(viewMap) as PageName[]);

function pageFromLocation(): PageName {
  const candidate = window.location.pathname.replace(/^\/+|\/+$/g, "") || "overview";
  return pages.has(candidate as PageName) ? candidate as PageName : "overview";
}

function syncFromHistory(): void {
  monitorStore.setPage(pageFromLocation());
}

onMounted(async () => {
  syncFromHistory();
  window.addEventListener("popstate", syncFromHistory);
  await monitorStore.restore();
  restoring.value = false;
});
onBeforeUnmount(() => window.removeEventListener("popstate", syncFromHistory));

function navigate(page: PageName): void {
  mobileMenuOpen.value = false;
  monitorStore.setPage(page);
  const path = page === "overview" ? "/" : `/${page}`;
  if (window.location.pathname !== path) window.history.pushState({ page }, "", path);
}
</script>

<template>
  <div v-if="restoring" class="boot-screen"><span class="spinner" />{{ l("Loading monitor…", "İzleme ekranı yükleniyor…") }}</div>
  <LoginView v-else-if="!monitorStore.authenticated.value" />
  <div v-else class="app-shell">
    <AppSidebar :page="monitorStore.page.value" :open="mobileMenuOpen" @navigate="navigate" @close="mobileMenuOpen = false" />
    <main>
      <AppHeader
        :page="monitorStore.page.value"
        :user="monitorStore.session.value?.username ?? ''"
        @navigate="navigate"
        @menu="mobileMenuOpen = !mobileMenuOpen"
      />
      <KeepAlive :include="['OverviewView']">
        <component :is="activeView" />
      </KeepAlive>
    </main>
  </div>
</template>
