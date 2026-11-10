// The corpus of old apps (fixtures/compat/README.md, HRN-849): views written for earlier versions of harnessly run as
// they are against today's bridge, shim and SDK. Every view must shake hands; the page-tools check must still offer its
// tools. A red test here means an update of the host broke an app that worked: fix the host, never the app.
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IPageTool } from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';

const here = dirname(fileURLToPath(import.meta.url));
const docsApps = join(here, '../../../../../docs/apps');

/** An answer with every list a view reads empty: what a quiet host gives on start. */
const EMPTY = {
  nodes: [],
  boards: [],
  fields: {},
  bindings: [],
  messages: [],
  agents: [],
  value: null,
  version: 0,
};

interface IManifest {
  id: string;
  version: string;
  targets: string[];
  permissions: string[];
  views?: { id: string; entry: string; targets: string[] }[];
}

interface ICorpusView {
  name: string;
  dir: string;
  manifest: IManifest;
  view: { id: string; entry: string; targets: string[] };
}

/** Every local view of every app of the corpus. */
const corpus = (): ICorpusView[] => {
  const roots = [
    join(here, 'fixtures/compat/ptcheck'),
    ...readdirSync(docsApps, { withFileTypes: true })
      .filter((e) => e.isDirectory())
      .map((e) => join(docsApps, e.name)),
  ];
  const out: ICorpusView[] = [];
  for (const dir of roots) {
    const file = join(dir, 'harnessly/app.json');
    if (!existsSync(file)) continue;
    const manifest = JSON.parse(readFileSync(file, 'utf8')) as IManifest;
    for (const view of manifest.views ?? []) {
      if (view.entry.startsWith('https://')) continue;
      out.push({ name: `${manifest.id}/${view.id}`, dir, manifest, view });
    }
  }
  return out;
};

/** The view's page and its classic scripts, as the view server serves them: the SDK at /__hl/app-sdk.js, the rest beside it. */
const pageOf = (c: ICorpusView): { html: string; scripts: string[] } => {
  const entry = join(c.dir, c.view.entry);
  const html = readFileSync(entry, 'utf8');
  const scripts: string[] = [];
  for (const m of html.matchAll(/<script[^>]*\ssrc="([^"]+)"[^>]*><\/script>/g)) {
    const src = m[1];
    if (src === '/__hl/app-sdk.js') scripts.push(classicSdk());
    else if (!/^[a-z]+:/i.test(src)) scripts.push(readFileSync(join(dirname(entry), src), 'utf8'));
  }
  return { html: html.replace(/<script[^>]*\ssrc="[^"]+"[^>]*><\/script>/g, ''), scripts };
};

const run = (c: ICorpusView, handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  const target = c.view.targets[0];
  r.bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: {
      id: c.manifest.id,
      version: c.manifest.version,
      permissions: c.manifest.permissions,
      targets: c.manifest.targets,
    },
    context: {
      app: { id: c.manifest.id, version: c.manifest.version },
      target: target as 'pane',
      view: c.view.id,
      theme: { mode: 'dark', tokens: {} },
      locale: 'ru',
      chat: { id: 'c1' },
      user: null,
    },
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  const { html, scripts } = pageOf(c);
  const dom = r.load(html, NONCE, scripts);
  return { r, dom };
};

describe('the corpus of old apps against today’s host', () => {
  const views = corpus();

  it('has the apps it is meant to hold', () => {
    const names = views.map((v) => v.manifest.id);
    for (const id of ['acme.ptcheck', 'harnessly.cashback', 'harnessly.deck-lint'])
      expect(names).toContain(id);
  });

  it.each(views.map((v) => [v.name, v] as const))('%s shakes hands', async (_name, c) => {
    // Whatever the view asks on start is answered with nothing: the handshake is what is checked here.
    const quiet = new Proxy({}, { get: () => async () => EMPTY }) as IBridgeHandlers;
    const { r } = run(c, quiet);
    await vi.waitFor(() => expect(r.bridge.ready).toBe(true), { timeout: 2000 });
  });

  it('the page-tools check still offers its tools and publishes its context', async () => {
    const c = views.find((v) => v.manifest.id === 'acme.ptcheck')!;
    const offered: IPageTool[][] = [];
    const publish = vi.fn();
    const { dom } = run(c, { pageTools: (tools) => offered.push([...tools]), publish });
    await vi.waitFor(() =>
      expect(dom.window.document.getElementById('state')?.textContent).toBe(
        'тулы зарегистрированы: echo, wipe',
      ),
    );
    await settle(20);
    expect(
      offered
        .at(-1)
        ?.map((t) => t.name)
        .sort(),
    ).toEqual(['echo', 'wipe']);
    expect(publish).toHaveBeenCalled();
  });
});
