import { computed, ref } from "vue";

export type Theme = "light" | "dark";
const STORAGE_KEY = "servora.theme";
const stored = localStorage.getItem(STORAGE_KEY);
const theme = ref<Theme>(
  stored === "light" || stored === "dark"
    ? stored
    : window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light",
);

function applyTheme(value: Theme): void {
  document.documentElement.dataset.theme = value;
  document.documentElement.classList.toggle("dark", value === "dark");
  document.documentElement.style.colorScheme = value;
}
applyTheme(theme.value);

export function useTheme() {
  const isDark = computed(() => theme.value === "dark");
  function setTheme(value: Theme): void {
    theme.value = value;
    localStorage.setItem(STORAGE_KEY, value);
    applyTheme(value);
  }
  function toggleTheme(): void {
    setTheme(isDark.value ? "light" : "dark");
  }
  return { theme, isDark, setTheme, toggleTheme };
}
