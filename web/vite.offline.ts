import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { build, type Plugin } from 'vite';
import { fileURLToPath } from 'node:url';

// Install the reader and its complete module graph as one version. A failed install leaves the old worker active.
export const offlineReader = (): Plugin => ({
  name: 'harnessly-build-offline',
  apply: 'build',
  async closeBundle() {
    const root = fileURLToPath(new URL('./', import.meta.url));
    await build({
      root,
      configFile: false,
      base: './',
      publicDir: false,
      resolve: { alias: { '@': resolve(root, 'app') } },
      plugins: [manifest()],
      build: {
        outDir: 'dist',
        emptyOutDir: false,
        rolldownOptions: { input: resolve(root, 'offline.html') },
      },
    });
  },
});
const manifest = (): Plugin => ({
  name: 'harnessly-offline-reader',
  apply: 'build',
  writeBundle(options, bundle) {
    const entry = Object.values(bundle).find(
      (file) => file.type === 'chunk' && file.facadeModuleId?.endsWith('/offline.html'),
    );
    if (!entry) this.error('Missing offline reader entry');
    const files = new Set<string>(['offline.html']);
    function visit(name: string) {
      if (files.has(name)) return;
      files.add(name);
      const file = bundle[name];
      if (file?.type !== 'chunk') return;
      for (const child of [
        ...file.imports,
        ...file.dynamicImports,
        ...(file.viteMetadata?.importedCss ?? []),
        ...(file.viteMetadata?.importedAssets ?? []),
      ])
        visit(child);
    }
    visit(entry.fileName);
    const dir = options.dir!;
    const hash = createHash('sha256');
    for (const file of files) hash.update(readFileSync(resolve(dir, file)));
    const worker = resolve(dir, 'sw.js');
    writeFileSync(
      worker,
      readFileSync(worker, 'utf8').replace(
        '/* offline manifest */ null',
        JSON.stringify({ version: hash.digest('hex').slice(0, 16), files: [...files] }),
      ),
    );
  },
});
