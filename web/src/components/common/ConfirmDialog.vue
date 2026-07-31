<script setup lang="ts">
defineProps<{
  title: string;
  message: string;
  confirmLabel?: string;
  dangerous?: boolean;
}>();
const emit = defineEmits<{ confirm: []; cancel: [] }>();
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('cancel')">
    <section class="confirm-card" role="alertdialog" aria-modal="true">
      <div class="confirm-icon" :class="{ dangerous }">{{ dangerous ? "!" : "?" }}</div>
      <div>
        <h2>{{ title }}</h2>
        <p>{{ message }}</p>
      </div>
      <div class="modal-actions">
        <button class="secondary" @click="emit('cancel')">Cancel</button>
        <button :class="dangerous ? 'danger' : 'primary'" @click="emit('confirm')">
          {{ confirmLabel ?? "Confirm" }}
        </button>
      </div>
    </section>
  </div>
</template>
