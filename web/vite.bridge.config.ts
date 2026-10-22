import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';

// The preview slot's page bridge: one self-contained classic script next to the page's files (dist/preview-bridge.js),
// which the runtime serves at /__tk/bridge.js of every slot.
export default defineConfig({
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    copyPublicDir: false,
    lib: {
      entry: fileURLToPath(new URL('./app/lib/hl-preview/bridge-entry.ts', import.meta.url)),
      formats: ['iife'],
      name: 'hlPreviewBridge',
      fileName: () => 'preview-bridge.js',
    },
  },
});
