<script setup lang="ts">
import { useI18n } from "@/composables/useI18n";
defineProps<{
  title: string;
  message: string;
  confirmLabel?: string;
  dangerous?: boolean;
}>();
const emit = defineEmits<{ confirm: []; cancel: [] }>();
const { l } = useI18n();
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
        <button class="secondary" @click="emit('cancel')">{{ l("Cancel", "İptal") }}</button>
        <button :class="dangerous ? 'danger' : 'primary'" @click="emit('confirm')">
          {{ confirmLabel ?? l("Confirm", "Onayla") }}
        </button>
      </div>
    </section>
  </div>
</template>
