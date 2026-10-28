import { renameSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig, type Plugin } from 'vite';

// The hub UI (hub.html + app/hub): a separate entry from the runtime's page, built into dist-hub for the hub binary
// to embed (web/hub_embed.go). The hub serves index.html for each UI route and the files under /assets/, so the page
// is built for the site root and nothing else (no public folder, the icon is inlined in hub.html).
// In development the API and the runtime proxy go to a hub on HUB_URL (default 127.0.0.1:18540).
const hub = process.env.HUB_URL ?? 'http://127.0.0.1:18540';
const outDir = fileURLToPath(new URL('./dist-hub', import.meta.url));

/** Vite names the page after its source; the hub looks for index.html. */
const indexHtml = (): Plugin => ({
  name: 'hub-index-html',
  apply: 'build',
  closeBundle() {
    renameSync(`${outDir}/hub.html`, `${outDir}/index.html`);
  },
});

/** The dev server answers every UI route (not an asset, not the API) with the hub page. */
const spaFallback = (): Plugin => ({
  name: 'hub-spa-fallback',
  apply: 'serve',
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      const url = req.url ?? '/';
      const isPage =
        req.method === 'GET' &&
        !/^\/(api|m|app|kit|@|node_modules|src)\b/.test(url) &&
        !/\.[a-z0-9]+(\?|$)/i.test(url.split('?')[0] ?? '');
      if (isPage) req.url = '/hub.html';
      next();
    });
  },
});

export default defineConfig({
  base: '/',
  publicDir: false,
  plugins: [react(), indexHtml(), spaFallback()],
  resolve: {
    alias: {
      '@kit': fileURLToPath(new URL('./kit', import.meta.url)),
    },
  },
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: { input: fileURLToPath(new URL('./hub.html', import.meta.url)) },
  },
  server: {
    proxy: {
      '/api': { target: hub, changeOrigin: false },
      '/m': { target: hub, ws: true, changeOrigin: false },
    },
  },
});
