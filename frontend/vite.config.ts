import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => ({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: mode === 'diagnostic-smoke' ? { outDir: 'dist-diagnostic-smoke', rollupOptions: { input: 'diagnostic-smoke/index.html' } } : {},
  plugins: [react(), wails("./bindings")],
}));
