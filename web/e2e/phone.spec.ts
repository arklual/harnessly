// The interface on a phone (390 px, touch) on a runtime of its own (HRN-510): a video project opened in the chat's pane
// draws its player and its scenes, the pane's strip is one row of equal chips, the tools home keeps its search field
// opaque over what scrolls under it, and «Сделано в этом чате» names its documents and shows each once.
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect as baseExpect, type Locator, type Page, test } from '@playwright/test';

import { deckGateway } from './deck-rig';
import { type Runtime, startRuntime } from './runtime';
import { SCENES, sceneFile, scenePageId, stubGuestComments } from './video-rig';

const PASSWORD = 'correct horse';
// A shared machine under load answers slowly: every wait here is long.
const expect = baseExpect.configure({ timeout: 120_000 });
/** A board engine (a project's support.js): a new project needs one, and the test machine has not fetched it. */
const ENGINE = process.env.RUNTIME_DESIGN_ENGINE_FILE ?? '';
const shots = () => {
  const dir = process.env.E2E_SHOTS ?? tmpdir();
  mkdirSync(dir, { recursive: true });
  return dir;
};

test.describe.configure({ mode: 'serial', timeout: 900_000 });
test.use({ actionTimeout: 60_000 });

let rt: Runtime;
let page: Page;
let project = '';
let conv = '';
const gw = deckGateway(
  () => rt,
  () => project,
);

/** The documents of the chat: the first is met twice, by the portal's address and by the platform's reader. */
const DOCS = [
  { slug: 'sinopia-video', title: 'Sinopia: видео — план и итоги' },
  { slug: 'launch-notes', title: 'Заметки к запуску' },
];
/** Enough more of them that the tools home scrolls on a phone and the cards pass under the search field. */
const EXTRA = Array.from({ length: 6 }, (_, i) => ({
  slug: `extra-${i + 1}`,
  title: `Extra document ${i + 1}`,
}));
interface IPage {
  ref: string;
  at: number;
  kind?: string;
}
const MADE = (
  [
    { ref: `https://ai.example.test/agents/docs/${DOCS[0].slug}`, at: 50 },
    { ref: `https://a.example.test/a/${DOCS[0].slug}`, at: 40 },
    { ref: `https://ai.example.test/agents/docs/${DOCS[1].slug}`, at: 30 },
    { ref: 'https://b.example.test/b/flow?k=SECRETKEY', at: 20, kind: 'board' },
    ...EXTRA.map((x, i) => ({ ref: `https://ai.example.test/agents/docs/${x.slug}`, at: 10 - i })),
  ] satisfies IPage[]
).map((x: IPage) => ({
  kind: x.kind ?? 'doc',
  ref: x.ref,
  title: '',
  op: 'changed',
  at: x.at,
  cover: null,
  designKind: null,
}));

/** The runtime's answers the chat's block reads, in the page: the made list and the titles of its links. */
const stubMade = async (p: Page, state: { metaCalls: number }) => {
  await p.route(/\/conversations\/[^/]+\/made(\?|$)/, (route) =>
    route.fulfill({ json: { items: MADE, complete: true } }),
  );
  await p.route(/\/link-meta\?/, (route) => {
    state.metaCalls += 1;
    // The first answers are lost, as on a weak mobile network: the page asks again by itself.
    if (state.metaCalls <= 4) return route.fulfill({ status: 502, body: 'bad gateway' });
    const url = new URL(route.request().url()).searchParams.get('url') ?? '';
    const doc = [...DOCS, ...EXTRA].find((x) => url.includes(`/${x.slug}`));
    if (doc) return route.fulfill({ json: { kind: 'artifact', title: doc.title, description: null } });
    return route.fulfill({ json: { kind: 'artifact', title: 'Flow of the clip', description: null } });
  });
};

const stubDoc = async (p: Page) => {
  await p.route('**/api/platform/docs/**', (route) => {
    const rest = new URL(route.request().url()).pathname.split('/api/platform/docs/')[1] ?? '';
    if (rest === DOCS[0].slug)
      return route.fulfill({
        json: {
          doc: {
            slug: DOCS[0].slug,
            title: DOCS[0].title,
            kind: 'artifact',
            url: 'https://a.example.test/a/x',
            bound: true,
            owner: null,
            machineId: null,
            sourcePath: null,
            openThreads: 0,
            unread: 0,
            lastActivityAt: new Date().toISOString(),
          },
          threads: [],
        },
      });
    if (rest.endsWith('/versions')) return route.fulfill({ json: { versions: [] } });
    if (rest.endsWith('/content'))
      return route.fulfill({
        contentType: 'text/html',
        body: '<!doctype html><body style="font:16px sans-serif;padding:20px"><h1>Sinopia doc</h1><p>The document.</p></body>',
      });
    return route.fulfill({ json: {} });
  });
};

const panel = (command: Record<string, string>) => {
  const token = readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();
  return page.request
    .get(`${rt.url}/api/v2/conversations/${conv}/legacy`)
    .then((legacy) => legacy.json() as Promise<{ sessionName: string }>)
    .then(({ sessionName }) =>
      page.request.post(`${rt.url}/api/cli/panel`, {
        headers: { 'x-cli-token': token },
        data: { session: sessionName, command },
      }),
    );
};

const pane = () => page.locator('.hx-pane');
const strip = () => pane().locator('.hx-strip');
const chips = () => strip().locator('.hx-tochat, .hx-strip__nav .hx-tab');

/** What a chip looks like to the finger and the eye. */
const lookOf = (chip: Locator) =>
  chip.evaluate((el) => {
    const css = getComputedStyle(el);
    const box = el.getBoundingClientRect();
    return {
      height: Math.round(box.height),
      radius: css.borderTopLeftRadius,
      size: css.fontSize,
      weight: css.fontWeight,
      background: css.backgroundColor,
    };
  });

test.beforeAll(async ({ browser }) => {
  test.skip(ENGINE === '', 'RUNTIME_DESIGN_ENGINE_FILE names a board engine');
  const root = mkdtempSync(join(tmpdir(), 'hs-phone-e2e-'));
  const secretFile = join(root, 'secret');
  writeFileSync(secretFile, gw.secret);
  rt = await startRuntime({
    root,
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      AGENT_SECRET_FILE: secretFile,
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
      RUNTIME_DESIGN_ENGINE_FILE: ENGINE,
    },
  });
  await gw.start();
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    colorScheme: 'dark',
  });
  page = await ctx.newPage();
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
  // the notices of the fake agent's replies come down over the header: they are taken away like a person would
  await page.addLocatorHandler(
    page.locator('.hx-notices .hx-notice__x').first(),
    async (x) => {
      // It may be gone already, as it comes and goes by itself.
      await x.dispatchEvent('click', undefined, { timeout: 2000 }).catch(() => undefined);
    },
    { noWaitAfter: true },
  );
  await gw.stubAccess(page);
  await stubGuestComments(page, gw);
  await stubDoc(page);
});

test.afterAll(async () => {
  await page?.context().close();
  await gw.stop();
  await rt?.stop();
});

test('1. a chat and a video project of three scenes', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  const made = await page.evaluate(async () => {
    const res = await fetch('/api/v2/designly/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: 'Launch clip',
        brief: 'A three-scene launch clip',
        kind: 'video',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
        video: { formats: ['16:9', '9:16'], seconds: null, subs: false },
      }),
    });
    return { status: res.status, body: (await res.json()) as { id?: string } };
  });
  expect(made.status, JSON.stringify(made.body)).toBe(200);
  project = made.body.id ?? '';
  const lib = join(rt.home, 'project', 'design-artifacts', project);
  await expect
    .poll(
      () =>
        page.evaluate(async (id) => {
          const res = await fetch('/api/v2/designly/library');
          const body = (await res.json()) as { artifacts?: { id: string; kind?: string }[] };
          return body.artifacts?.find((a) => a.id === id)?.kind ?? 'absent';
        }, project),
      { timeout: 300_000 },
    )
    .toBe('video');
  for (const old of readdirSync(join(lib, 'meta', 'pages'))) {
    // The template's own first page, if any, gives way to the fixture's scenes.
    if (!old.startsWith('01JVDE')) execFileSync('rm', ['-f', join(lib, 'meta', 'pages', old)]);
  }
  SCENES.forEach((scene, i) => {
    const file = `scene-${i + 1}.dc.html`;
    writeFileSync(join(lib, 'project', file), sceneFile(scene));
    writeFileSync(
      join(lib, 'meta', 'pages', `${scenePageId(i)}.json`),
      JSON.stringify({ id: scenePageId(i), title: scene.title, file, w: 1920, h: 1080, order: i + 1 }),
    );
  });
  await page.locator('.hx-composer__input').fill('hello agent');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  conv = new URL(page.url()).pathname.split('/').pop() ?? '';
  await expect(page.getByText('Fake agent reply: hello agent')).toBeVisible();
});

test('2. a video project in the pane draws the player and its scenes, not an empty screen', async () => {
  await page.getByRole('button', { name: 'Tools · side chat' }).click();
  await expect(pane()).toBeVisible();
  expect((await panel({ kind: 'open', tab: 'design', target: project })).ok()).toBe(true);
  await expect(pane().locator('.hx-video')).toBeVisible();
  // Never empty: the shell says it is on its way until the video is read, then the player and the scenes stand.
  await expect(pane().locator('.hx-video-player')).toBeVisible();
  await expect(pane().locator('.hx-video-scenes li')).toHaveCount(SCENES.length);
  const slide = await pane().locator('.hx-video__slide').boundingBox();
  expect(slide?.width ?? 0).toBeGreaterThan(100);
  expect(slide?.width ?? 0).toBeLessThanOrEqual(390);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await page.screenshot({ path: join(shots(), 'video-tab-phone.png') });
});

test('3. a document tab shows its viewer on a phone, and the strip is one row of equal chips', async () => {
  expect((await panel({ kind: 'open', tab: 'artifacts', target: `/agents/docs/${DOCS[0].slug}` })).ok()).toBe(
    true,
  );
  // A tab the agent opens while another is on screen comes up behind it: the person turns to it.
  const open = strip().locator('.hx-strip__tabs .hx-tab');
  await expect(open).toHaveCount(2);
  await open.last().tap();
  await expect(pane().locator('.td-app')).toBeVisible();
  await expect(pane().locator('.td-bar h1')).toHaveText(DOCS[0].title);
  await page.screenshot({ path: join(shots(), 'strip-phone.png') });

  const all = chips();
  await expect(all).toHaveCount(4);
  const looks = await Promise.all((await all.all()).map(lookOf));
  // «Чат», the tools and the open tabs: one height, one radius, one size and weight of type.
  expect(new Set(looks.map((x) => x.height)).size).toBe(1);
  expect(new Set(looks.map((x) => x.radius)).size).toBe(1);
  expect(new Set(looks.map((x) => x.size)).size).toBe(1);
  expect(new Set(looks.map((x) => x.weight)).size).toBe(1);
  expect(looks[0].radius).not.toBe('999px');
  // Only the open tab has a tone of its own, and its close button is as tall as a finger needs.
  const selected = strip().locator('.hx-tab[aria-selected="true"]');
  await expect(selected).toHaveCount(1);
  const [chat, tools] = looks;
  const own = await lookOf(selected);
  expect(own.background).not.toBe(chat.background);
  expect(tools.background).toBe(chat.background);
  const close = await selected.locator('.hx-tab__x').boundingBox();
  expect(close?.height ?? 0).toBeGreaterThanOrEqual(32);
  // The strip never makes the page wider than the phone.
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
});

test('4. the tools home: the search field is opaque over the cards, and what the chat made has real names, each once', async () => {
  const state = { metaCalls: 0 };
  await stubMade(page, state);
  page.on('request', (r) => /made|link-meta/.test(r.url()) && console.log('REQ', r.url()));
  // The tools home has not been on screen yet: what it asks of the runtime is answered here.
  await strip().locator('.hx-tab[data-home]').click();
  await expect(pane().locator('.hx-tl__search')).toBeVisible();
  const cards = pane().locator('.hx-made__card');
  // The page is one thing met by two addresses: three cards and the extra ones, not four and those.
  await expect(cards).toHaveCount(3 + EXTRA.length);
  // The lost answers are asked again by themselves: the real titles come.
  await expect(cards.nth(0).locator('.hx-made__name')).toHaveText(DOCS[0].title);
  await expect(cards.nth(1).locator('.hx-made__name')).toHaveText(DOCS[1].title);
  await expect(cards.nth(2).locator('.hx-made__name')).toHaveText('Flow of the clip');
  await expect(cards.last().locator('.hx-made__name')).toHaveText(EXTRA[EXTRA.length - 1].title);
  expect(await cards.allTextContents()).not.toContain('SECRETKEY');
  expect((await pane().locator('.hx-made').textContent()) ?? '').not.toContain('SECRETKEY');
  await page.screenshot({ path: join(shots(), 'tools-phone.png') });

  // Scrolled until the cards stand under the field: it is one opaque colour, and it is what a finger meets there.
  const field = pane().locator('.hx-tl__search');
  await pane()
    .locator('.hx-pane__body')
    .evaluate((el) => el.scrollTo(0, 260));
  await page.screenshot({ path: join(shots(), 'tools-phone-scrolled.png') });
  const probe = await field.evaluate((el) => {
    const css = getComputedStyle(el);
    const box = el.getBoundingClientRect();
    const hit = document.elementFromPoint(box.left + 24, box.top + box.height / 2);
    return {
      colour: css.backgroundColor,
      layered: css.backgroundImage,
      hitsField: hit !== null && el.contains(hit),
    };
  });
  const alpha = /rgba?\([^)]*?,\s*([\d.]+)\)$/.exec(probe.colour)?.[1];
  expect(alpha === undefined || Number(alpha) === 1, probe.colour).toBe(true);
  expect(probe.hitsField).toBe(true);
});
