import { createApp } from "vue";
import App from "./App.vue";
import "./styles/main.css";
import "./styles/tailwind.css";

const savedTheme = localStorage.getItem("servora.theme");
const initialTheme = savedTheme === "light" || savedTheme === "dark"
  ? savedTheme
  : matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
document.documentElement.dataset.theme = initialTheme;
document.documentElement.classList.toggle("dark", initialTheme === "dark");
document.documentElement.style.colorScheme = initialTheme;

createApp(App).mount("#app");
