import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The runtime serves the built page under its app base path and rewrites <base href>; relative asset URLs follow it.
// In development the API and sockets go to a runtime on RUNTIME_URL (default 127.0.0.1:8022).
const runtime = process.env.RUNTIME_URL ?? 'http://127.0.0.1:8022';

export default defineConfig({
  base: './',
  plugins: [react()],
  resolve: {
    alias: {
      '@kit': fileURLToPath(new URL('./kit', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
  server: {
    proxy: {
      '/api': { target: runtime, ws: true, changeOrigin: false },
      '/agents/api': { target: runtime, ws: true, changeOrigin: false },
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.{ts,tsx}', 'kit/**/*.test.{ts,tsx}'],
    css: false,
  },
});
