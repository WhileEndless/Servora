import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "node:path";

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { "@": resolve(__dirname, "src") } },
  build: {
    outDir: resolve(__dirname, "../internal/app/static"),
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    proxy: {
      "/api": { target: "https://127.0.0.1:8443", secure: false },
    },
  },
});
