import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

const backendOrigin = process.env.SOMASCOPE_DEV_API_ORIGIN ?? "http://localhost:18080";

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      "/api": backendOrigin,
    },
  },
  build: {
    outDir: "dist",
  },
});
