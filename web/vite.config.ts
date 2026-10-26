import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

// The runtime serves the built page under its app base path and rewrites <base href>; relative asset URLs follow it.
// In development the API and sockets go to a runtime on RUNTIME_URL (default 127.0.0.1:8022).
const runtime = process.env.RUNTIME_URL ?? 'http://127.0.0.1:8022';

// One module per language, `virtual:dictionary/<lang>`: every app/locales/<lang>/*.json merged over the English ones,
// so a missing text falls back to English already in the build. The page loads only its own language's module; the
// JSON goes in as one JSON.parse string, which a phone parses faster than the same object written as code.
const LOCALES = fileURLToPath(new URL('./app/locales/', import.meta.url));
const DICTIONARY = 'virtual:dictionary/';
const dictionaries = (): Plugin => ({
  name: 'harnessly-dictionaries',
  resolveId: (id) => (id.startsWith(DICTIONARY) ? `\0${id}` : undefined),
  load(id) {
    if (!id.startsWith(`\0${DICTIONARY}`)) return undefined;
    const lang = id.slice(DICTIONARY.length + 1);
    const read = (dir: string) =>
      readdirSync(LOCALES + dir)
        .filter((file) => file.endsWith('.json'))
        .sort()
        .map((file) => {
          this.addWatchFile(LOCALES + dir + '/' + file);
          return JSON.parse(readFileSync(LOCALES + dir + '/' + file, 'utf8')) as Record<string, unknown>;
        });
    const merged = Object.assign({}, ...read('en'), ...(lang === 'en' ? [] : read(lang)));
    return `export default JSON.parse(${JSON.stringify(JSON.stringify(merged))});`;
  },
  handleHotUpdate({ file, server }) {
    if (!file.startsWith(LOCALES)) return undefined;
    for (const lang of ['en', 'ru']) {
      const module = server.moduleGraph.getModuleById(`\0${DICTIONARY}${lang}`);
      if (module) server.moduleGraph.invalidateModule(module);
    }
    server.ws.send({ type: 'full-reload' });
    return [];
  },
});

// The dictionary is imported from inside the app code, so the browser would see it only after the first chunk ran: one
// more round trip before any text, which a phone on a slow line feels. A preload for the page's language starts it at
// once, next to the first chunk; the language comes from the config the runtime writes into the page.
const preloadDictionary = (): Plugin => ({
  name: 'harnessly-preload-dictionary',
  transformIndexHtml: {
    order: 'post',
    handler(html, { bundle }) {
      if (!bundle) return html;
      const files: Record<string, string> = {};
      for (const chunk of Object.values(bundle)) {
        const id = chunk.type === 'chunk' ? chunk.facadeModuleId : null;
        if (id?.startsWith(`\0${DICTIONARY}`)) files[id.slice(DICTIONARY.length + 1)] = chunk.fileName;
      }
      if (!files.en || !files.ru) return html;
      const script = `<script>try{var c=JSON.parse(document.querySelector('meta[name="hl-config"]').content||'{}');var l=document.createElement('link');l.rel='modulepreload';l.href='./'+(c.locale==='ru'?${JSON.stringify(files.ru)}:${JSON.stringify(files.en)});document.head.appendChild(l)}catch(e){}</script>`;
      // Ahead of the page's own module script and its preloads, so the browser asks for the dictionary first.
      const at = html.indexOf('<script type="module"');
      return at < 0
        ? html.replace('</head>', `${script}\n</head>`)
        : html.slice(0, at) + script + '\n' + html.slice(at);
    },
  },
});

// The modules the interface (next/entry) needs at once, sorted by who else needs them: read from the module graph after
// the build has walked it, before the chunks are cut. What the page's start (main.tsx) also needs is «start», what the
// guest page (guest/main.tsx, a design opened by link) also needs is «shared», the rest is «shell»: a guest then loads
// start and shared, not the whole interface, and the start does not wait for the interface.
const shellModules = new Set<string>();
const sharedModules = new Set<string>();
const startModules = new Set<string>();
const collectShell = (): Plugin => ({
  name: 'harnessly-shell-modules',
  apply: 'build',
  buildEnd() {
    const reach = (from: string) => {
      const seen = new Set<string>();
      const walk = [from];
      while (walk.length > 0) {
        const id = walk.pop() as string;
        if (seen.has(id)) continue;
        seen.add(id);
        walk.push(...(this.getModuleInfo(id)?.importedIds ?? []));
      }
      return seen;
    };
    const ids = [...this.getModuleIds()];
    const main = ids.find((id) => id.endsWith('/app/main.tsx'));
    const entry = ids.find((id) => /\/app\/next\/entry\.tsx?$/.test(id));
    const guest = ids.find((id) => id.endsWith('/app/guest/main.tsx'));
    shellModules.clear();
    sharedModules.clear();
    startModules.clear();
    if (!main || !entry) return;
    const start = reach(main);
    const guests = guest ? reach(guest) : new Set<string>();
    for (const id of reach(entry)) {
      if (start.has(id)) startModules.add(id);
      else (guests.has(id) ? sharedModules : shellModules).add(id);
    }
    for (const id of guests) if (start.has(id)) startModules.add(id);
    startModules.delete(main);
  },
});

// One file of styles cascades in the order of its modules, and the bundler orders a chunk's modules its own way
// (the base came after the features, the narrow-screen rules lost to the base ones). The interface's stylesheets go
// in the order of app/next/styles/order.ts instead, in whichever chunk holds them: the CSS plugin reads the chunk's
// modules in a later hook of the same chunk object, so the slots of the listed stylesheets are refilled in that order
// here; the check after the CSS plugin stops the build if the bundler ever hands the CSS plugin a copy.
const SHELL_STYLES = fileURLToPath(new URL('./app/next/styles/order.ts', import.meta.url));
const orderShellStyles = (): Plugin[] => {
  const wanted = new Map<string, string[]>();
  const order: Plugin = {
    name: 'harnessly-shell-style-order',
    apply: 'build',
    renderChunk: {
      order: 'pre',
      handler(_code, chunk) {
        const order = this.getModuleInfo(SHELL_STYLES)?.importedIds ?? [];
        const modules = chunk.modules;
        const ids = Object.keys(modules);
        const listed = new Set(order.filter((id) => id in modules));
        if (listed.size === 0) return null;
        const queue = order.filter((id) => listed.has(id));
        const result = ids.map((id) => (listed.has(id) ? queue.shift()! : id));
        const entries = result.map((id) => [id, modules[id]] as const);
        for (const id of ids) delete modules[id];
        for (const [id, info] of entries) modules[id] = info;
        wanted.set(chunk.fileName, result);
        return null;
      },
    },
  };
  const check: Plugin = {
    name: 'harnessly-shell-style-order-check',
    apply: 'build',
    renderChunk: {
      order: 'post',
      handler(_code, chunk) {
        const result = wanted.get(chunk.fileName);
        if (result && Object.keys(chunk.modules).join('\n') !== result.join('\n'))
          this.error(`the order of the interface's styles did not hold in ${chunk.fileName}`);
        return null;
      },
    },
  };
  return [order, check];
};

// Over HTTP/1.1 (the phone reaches the page through Teleport, which offers no HTTP/2) every file is a round trip of
// its own, and an interface split into a hundred and fifty small files waits on the line rather than on the code. React,
// the icons and the UI primitives go out as one file each, and what the interface needs at once as one more; what it
// opens later (the right pane's tabs, the terminal) stays in its own files.
// The stylesheets of shell and shared go to one «styles» file: split between the two, the shared half would load
// first on the interface and break the cascade of order.ts. The groups take only the modules listed for them
// (includeDependenciesRecursively off); by default a group pulls in everything its modules import, and the shell
// group swallowed what the guest page shares with it.
const CSS = /\.css(\?|$)/;
const CODE_SPLITTING = {
  groups: [
    {
      name: 'styles',
      test: (id: string) => CSS.test(id) && (shellModules.has(id) || sharedModules.has(id)),
      priority: 2,
      includeDependenciesRecursively: false,
    },
    {
      name: 'shell',
      test: (id: string) => shellModules.has(id),
      priority: 1,
      includeDependenciesRecursively: false,
    },
    {
      name: 'shared',
      test: (id: string) => sharedModules.has(id),
      priority: 1,
      includeDependenciesRecursively: false,
    },
    {
      name: 'start',
      test: (id: string) => startModules.has(id),
      priority: 1,
      includeDependenciesRecursively: false,
    },
    { name: 'react', test: /[\\/]node_modules[\\/](react|react-dom|scheduler)[\\/]/, priority: 4 },
    { name: 'icons', test: /[\\/]lucide-react[\\/]/, priority: 3 },
    {
      name: 'ui',
      test: /[\\/]node_modules[\\/](@base-ui|@floating-ui|use-sync-external-store|tabbable|reselect)[\\/]/,
      priority: 2,
    },
  ],
};

export default defineConfig({
  base: './',
  plugins: [react(), tailwindcss(), dictionaries(), preloadDictionary(), collectShell(), orderShellStyles()],
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
    chunkSizeWarningLimit: 900,
    rolldownOptions: {
      input: {
        main: fileURLToPath(new URL('./index.html', import.meta.url)),
        guest: fileURLToPath(new URL('./guest.html', import.meta.url)),
      },
      output: { codeSplitting: CODE_SPLITTING },
    },
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
