<script setup lang="ts">
import { useI18n } from "@/composables/useI18n";
defineProps<{ title: string; wide?: boolean }>();
const emit = defineEmits<{ close: [] }>();
const { l } = useI18n();
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="modal-card" :class="{ wide }" role="dialog" aria-modal="true">
      <button class="modal-close" :aria-label="l('Close', 'Kapat')" @click="emit('close')">×</button>
      <h2>{{ title }}</h2>
      <slot />
    </section>
  </div>
</template>
