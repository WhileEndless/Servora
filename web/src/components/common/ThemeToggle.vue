<script setup lang="ts">
import { useTheme } from "@/composables/useTheme";
import { useI18n } from "@/composables/useI18n";
const { isDark, toggleTheme } = useTheme();
const { l } = useI18n();
</script>

<template>
  <button class="theme-toggle" type="button" :aria-label="isDark ? l('Switch to light theme', 'Aydınlık temaya geç') : l('Switch to dark theme', 'Karanlık temaya geç')" :title="isDark ? l('Light theme', 'Aydınlık tema') : l('Dark theme', 'Karanlık tema')" @click="toggleTheme">
    <svg v-if="isDark" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M2 12h2m16 0h2M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42" /></svg>
    <svg v-else viewBox="0 0 24 24" aria-hidden="true"><path d="M20.5 15.1A9 9 0 0 1 8.9 3.5 9 9 0 1 0 20.5 15.1Z" /></svg>
  </button>
</template>
