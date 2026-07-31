<script setup lang="ts">
import { ref } from "vue";
import { monitorStore } from "@/services/MonitorStore";
import { useI18n } from "@/composables/useI18n";
import ThemeToggle from "@/components/common/ThemeToggle.vue";

const username = ref("");
const password = ref("");
const error = ref("");
const busy = ref(false);
const { t } = useI18n();

async function submit(): Promise<void> {
  busy.value = true; error.value = "";
  try {
    await monitorStore.login(username.value, password.value);
    password.value = "";
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Login failed";
  } finally { busy.value = false; }
}
</script>

<template>
  <div class="login-shell">
    <ThemeToggle />
    <section class="login-card">
      <div class="brand-mark"><img src="/assets/servora-logo.png" alt="Servora"></div>
      <p class="eyebrow">SERVORA · SYSTEM OPERATIONS</p>
      <h1>{{ t("signIn") }}</h1>
      <p class="muted">{{ t("pamHint") }}</p>
      <form @submit.prevent="submit">
        <label><span>{{ t("username") }}</span><input v-model="username" autocomplete="username" required></label>
        <label><span>{{ t("password") }}</span><input v-model="password" type="password" autocomplete="current-password" required></label>
        <p class="error" role="alert">{{ error }}</p>
        <button class="primary wide" :disabled="busy">{{ busy ? "…" : t("signInButton") }}</button>
      </form>
    </section>
  </div>
</template>
