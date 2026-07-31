<script setup lang="ts">
defineProps<{ title: string; wide?: boolean }>();
const emit = defineEmits<{ close: [] }>();
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="modal-card" :class="{ wide }" role="dialog" aria-modal="true">
      <button class="modal-close" @click="emit('close')">×</button>
      <h2>{{ title }}</h2>
      <slot />
    </section>
  </div>
</template>
