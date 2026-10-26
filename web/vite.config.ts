import { fileURLToPath } from 'node:url';

import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The runtime serves the built page under its app base path and rewrites <base href>; relative asset URLs follow it.
// In development the API and sockets go to a runtime on RUNTIME_URL (default 127.0.0.1:8022).
const runtime = process.env.RUNTIME_URL ?? 'http://127.0.0.1:8022';

export default defineConfig({
  base: './',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@kit': fileURLToPath(new URL('./kit', import.meta.url)),
      '@/': fileURLToPath(new URL('./app/', import.meta.url)),
      'next/router': fileURLToPath(new URL('./app/platform/router.ts', import.meta.url)),
      'next/head': fileURLToPath(new URL('./app/platform/head.tsx', import.meta.url)),
      'next/dynamic': fileURLToPath(new URL('./app/platform/dynamic.tsx', import.meta.url)),
      'next-themes': fileURLToPath(new URL('./app/platform/theme.tsx', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    // The guest page is a second entry of the same build; the runtime serves its files without a session by the manifest.
    manifest: true,
    rollupOptions: {
      input: {
        main: fileURLToPath(new URL('./index.html', import.meta.url)),
        guest: fileURLToPath(new URL('./guest.html', import.meta.url)),
      },
    },
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
    include: ['app/**/*.test.{ts,tsx}', 'kit/**/*.test.{ts,tsx}'],
    css: false,
    // The default cache lives in node_modules, which a worktree shares with the checkout it links to; a transformed
    // module (import.meta.glob of the locales above all) cached by another worktree then shows that one's files here.
    fsModuleCachePath: fileURLToPath(new URL('./.vitest-cache', import.meta.url)),
  },
});
