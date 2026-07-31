<script setup lang="ts">
import { computed } from "vue";
import { monitorStore } from "@/services/MonitorStore";
import { useI18n, type Locale, type MessageKey } from "@/composables/useI18n";
import type { PageName } from "@/types";
import ThemeToggle from "@/components/common/ThemeToggle.vue";

const props = defineProps<{ page: PageName; user: string }>();
const emit = defineEmits<{ navigate: [page: PageName]; menu: [] }>();
const { locale, setLocale, t, l } = useI18n();
const title = computed(() => {
  const translated = ["overview", "processes", "services", "packages", "network", "schedules", "alerts", "activity", "settings"];
  return translated.includes(props.page) ? t(props.page as MessageKey) : props.page.toUpperCase();
});
</script>

<template>
  <header>
    <div class="header-title">
      <button class="mobile-menu-button" type="button" :aria-label="l('Open menu', 'Menüyü aç')" @click="emit('menu')">☰</button>
      <div><p class="eyebrow">{{ l("LIVE HOST TELEMETRY", "CANLI HOST TELEMETRİSİ") }}</p><h1>{{ title }}</h1></div>
    </div>
    <div class="header-actions">
      <label class="search">
        <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" /></svg>
        <input :placeholder="t('search')" @input="monitorStore.setSearch(($event.target as HTMLInputElement).value)">
      </label>
      <select class="language-picker" :value="locale" :aria-label="l('Language', 'Dil')" @change="setLocale(($event.target as HTMLSelectElement).value as Locale)">
        <option value="en">EN</option><option value="tr">TR</option>
      </select>
      <ThemeToggle />
      <button class="icon-button" :title="l('Refresh', 'Yenile')" :aria-label="l('Refresh', 'Yenile')" @click="monitorStore.refresh()">↻</button>
      <button class="user-chip" @click="emit('navigate', 'settings')"><span>{{ user.charAt(0).toUpperCase() }}</span><b>{{ user }}</b></button>
    </div>
  </header>
</template>
