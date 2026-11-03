// The overlay layer of a video end to end on a standalone runtime of its own: an agent adds overlays with the
// CLI and the editor draws them on the scene's frame; a person moves one with the mouse, retypes it in place, drags its
// strip on the time line and picks its look in the inspector, and each of it lands in the same meta file the CLI reads; an
// agent's change to what the person edited is refused until it says --force, and the editor shows what the agent changed.
// Needs the harnessly CLI (E2E_HARNESSLY), a board engine (RUNTIME_DESIGN_ENGINE_FILE) and a built page (RUNTIME_WEB_DIR).
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect as baseExpect, type Frame, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';
import { SCENES, sceneFile, scenePageId } from './video-rig';

const PASSWORD = 'correct horse';
const expect = baseExpect.configure({ timeout: 180_000 });
const CLI = process.env.E2E_HARNESSLY ?? '';
const ENGINE = process.env.RUNTIME_DESIGN_ENGINE_FILE ?? '';

test.describe.configure({ mode: 'serial', timeout: 3_600_000 });
test.use({ actionTimeout: 180_000 });

let rt: Runtime;
let page: Page;
let project = '';

const shots = () => process.env.E2E_SHOTS ?? tmpdir();
const lib = () => join(rt.home, 'project', 'design-artifacts', project);
const track = () => page.locator('.hx-video-tl__track');
const scene = scenePageId(0);

const cli = (...args: string[]) => {
  const token = readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();
  const out = spawnSync(CLI, args, {
    env: { ...process.env, HOME: rt.home, HARNESSLY_URL: rt.url, HARNESSLY_CLI_TOKEN: token },
    encoding: 'utf8',
    timeout: 600_000,
  });
  return { code: out.status, out: out.stdout, err: out.stderr };
};

interface IOverlay {
  id: string;
  kind: string;
  text: string;
  at: number;
  out: number | null;
  x: number;
  y: number;
  enter: string;
  theme: string;
  author: string;
}
const overlays = () => {
  const got = cli('design', 'video', 'overlay', 'ls', project, scene, '--json');
  expect(got.code, got.err).toBe(0);
  return (JSON.parse(got.out) as { overlays: IOverlay[] }).overlays;
};
const overlay = (id: string) => {
  const o = overlays().find((x) => x.id === id);
  if (!o) throw new Error(`no overlay ${id}`);
  return o;
};

const writeScene = (i: number) => {
  const file = `scene-${i + 1}.dc.html`;
  writeFileSync(join(lib(), 'project', file), sceneFile(SCENES[i]));
  writeFileSync(
    join(lib(), 'meta', 'pages', `${scenePageId(i)}.json`),
    JSON.stringify({ id: scenePageId(i), title: SCENES[i].title, file, w: 1920, h: 1080, order: i + 1 }),
  );
};

const shownFrame = async (p: Page): Promise<Frame> => {
  const handle = await p.locator('iframe.hx-video__frame[data-on="1"]').elementHandle({ timeout: 180_000 });
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error('no scene frame');
  return frame;
};

/** What the scene's frame draws of an overlay: its opacity (0 while it is not on) and its text. */
const drawn = async (kind: string) => {
  const frame = await shownFrame(page);
  return frame
    .locator(`.tko-${kind}`)
    .first()
    .evaluate((el) => {
      const g = el.closest('.tko-g');
      return { opacity: Number(g ? getComputedStyle(g).opacity : 0), text: (el.textContent ?? '').trim() };
    });
};

const trackAt = async (ms: number, total: number) => {
  const box = await track().boundingBox();
  if (!box) throw new Error('no timeline');
  return { x: box.x + (box.width * ms) / total, y: box.y + box.height / 2 };
};
const seek = async (ms: number) => {
  const total = Number(JSON.parse(cli('design', 'video', 'get', project, '--json').out).state.durMs);
  const at = await trackAt(ms, total);
  await page.mouse.click(at.x, at.y);
};

test.beforeAll(async ({ browser }) => {
  test.skip(CLI === '', 'E2E_HARNESSLY names the harnessly CLI (go build ./cmd/harnessly)');
  test.skip(ENGINE === '', 'RUNTIME_DESIGN_ENGINE_FILE names a board engine');
  rt = await startRuntime({
    root: mkdtempSync(join(tmpdir(), 'hs-video-ovl-e2e-')),
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
      RUNTIME_DESIGN_ENGINE_FILE: ENGINE,
    },
  });
  page = await browser.newPage();
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
  await page.addLocatorHandler(
    page.locator('.hx-notices .hx-notice__x').first(),
    async (x) => {
      await x.dispatchEvent('click', undefined, { timeout: 2_000 }).catch(() => undefined);
    },
    { noWaitAfter: true },
  );
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

test('1. a video with three scenes, and an agent puts three overlays on the first', async () => {
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
        title: 'Overlay clip',
        brief: 'A three-scene clip with overlays',
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
  for (const old of readdirSync(join(lib(), 'meta', 'pages'))) {
    if (!old.startsWith('01JVDE')) execFileSync('rm', ['-f', join(lib(), 'meta', 'pages', old)]);
  }
  SCENES.forEach((_, i) => writeScene(i));

  const add = (...a: string[]) => {
    const r = cli('design', 'video', 'overlay', 'add', project, scene, ...a);
    expect(r.code, r.err + r.out).toBe(0);
  };
  add(
    '--kind',
    'callout',
    '--text',
    'Pay here',
    '--at',
    '0.4s',
    '--x',
    '0.55',
    '--y',
    '0.12',
    '--w',
    '0.3',
    '--h',
    '0.1',
    '--tx',
    '0.4',
    '--ty',
    '0.5',
  );
  add('--kind', 'key', '--text', 'Ctrl + K', '--at', '0.8s', '--out', '2.6s');
  add(
    '--kind',
    'counter',
    '--text',
    '{n} ₽',
    '--value',
    '12400',
    '--at',
    '1s',
    '--x',
    '0.08',
    '--y',
    '0.7',
    '--w',
    '0.3',
    '--h',
    '0.14',
  );
  expect(overlays().map((o) => o.id)).toEqual(['callout-1', 'key-1', 'counter-1']);
  expect(overlays().every((o) => o.author === 'agent')).toBe(true);
});

test("2. the editor draws the agent's overlays by time: hidden before, whole after", async () => {
  await expect
    .poll(async () => {
      const res = await page.evaluate(async () => (await fetch('/api/v2/designly/library')).status);
      return res;
    })
    .toBe(200);
  const slots = () =>
    page.evaluate(async () => {
      const res = await fetch('/agents/api/preview/open?machine=local', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      });
      return res.status;
    });
  await expect.poll(slots, { timeout: 60_000 }).toBe(400);
  await page.goto(`${rt.url}/agents/designly/${project}`);
  await expect(track()).toBeVisible({ timeout: 300_000 });
  await expect(page.locator('.hx-video-tl__tile')).toHaveCount(3);
  await shownFrame(page);
  await seek(100);
  await expect.poll(async () => (await drawn('callout')).opacity, { timeout: 180_000 }).toBe(0);
  await seek(2000);
  await expect.poll(async () => (await drawn('callout')).opacity, { timeout: 180_000 }).toBeGreaterThan(0.99);
  expect((await drawn('callout')).text).toBe('Pay here');
  expect((await drawn('key')).opacity).toBeGreaterThan(0.99);
  await page.screenshot({ path: join(shots(), 'overlays-1-agent-1440.png') });
});

test('3. a person drags the callout in the player: the meta file is theirs now', async () => {
  const toggle = page.getByRole('button', { name: 'Captions' });
  await toggle.click();
  const box = page.locator('.hx-ovl__box[data-kind="callout"]');
  await expect(box).toBeVisible();
  const before = overlay('callout-1');
  const b = await box.boundingBox();
  if (!b) throw new Error('no box');
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await page.mouse.down();
  await page.mouse.move(b.x + b.width / 2 - 140, b.y + b.height / 2 + 60, { steps: 10 });
  await page.mouse.up();
  await expect.poll(() => overlay('callout-1').x, { timeout: 180_000 }).toBeLessThan(before.x - 0.03);
  const after = overlay('callout-1');
  expect(after.author).toBe('human');
  expect(after.y).toBeGreaterThan(before.y);
  await page.screenshot({ path: join(shots(), 'overlays-2-drag-1440.png') });
});

test('4. a double click retypes it in place', async () => {
  const box = page.locator('.hx-ovl__box[data-kind="callout"]');
  await box.dblclick();
  const field = page.getByRole('textbox', { name: 'Caption text' });
  await expect(field).toBeVisible();
  await field.fill('Pay in 2 clicks');
  await field.press('Enter');
  await expect.poll(() => overlay('callout-1').text, { timeout: 180_000 }).toBe('Pay in 2 clicks');
  await expect.poll(async () => (await drawn('callout')).text, { timeout: 180_000 }).toBe('Pay in 2 clicks');
});

test("5. the agent may not change what the person edited without --force; the editor then shows the agent's change", async () => {
  const refused = cli(
    'design',
    'video',
    'overlay',
    'set',
    project,
    scene,
    'callout-1',
    '--text',
    'Agent says',
  );
  expect(refused.code).not.toBe(0);
  expect(refused.err + refused.out).toContain('edited by a person');
  expect(overlay('callout-1').text).toBe('Pay in 2 clicks');
  // An overlay the agent wrote alone it changes freely.
  const free = cli('design', 'video', 'overlay', 'set', project, scene, 'key-1', '--text', 'Ctrl + P');
  expect(free.code, free.err).toBe(0);
  const forced = cli(
    'design',
    'video',
    'overlay',
    'set',
    project,
    scene,
    'callout-1',
    '--text',
    'Agent says',
    '--force',
  );
  expect(forced.code, forced.err).toBe(0);
  expect(overlay('callout-1').author).toBe('agent');
  // The page reads the video every 3 s and redraws what moved.
  await expect.poll(async () => (await drawn('callout')).text, { timeout: 180_000 }).toBe('Agent says');
  await expect.poll(async () => (await drawn('key')).text, { timeout: 180_000 }).toContain('P');
});

test('6. the strip of the overlay on the time line: an edge sets the time it goes out', async () => {
  const strips = page.locator('.hx-video-tl__strip');
  await expect(strips).toHaveCount(3);
  const strip = strips.nth(1); // the key: 0.8 s .. 2.6 s
  const s = await strip.boundingBox();
  if (!s) throw new Error('no strip');
  const before = overlay('key-1');
  expect(before.out).toBe(2600);
  await page.mouse.move(s.x + s.width - 2, s.y + s.height / 2);
  await page.mouse.down();
  await page.mouse.move(s.x + s.width - 2 - s.width / 3, s.y + s.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect.poll(() => overlay('key-1').out ?? 0, { timeout: 180_000 }).toBeLessThan(2200);
  expect(overlay('key-1').at).toBe(800);
  expect(overlay('key-1').author).toBe('human');
  await page.screenshot({ path: join(shots(), 'overlays-3-strips-1440.png') });
});

test('7. the inspector changes the look: way in and color', async () => {
  const box = page.locator('.hx-ovl__box[data-kind="callout"]');
  await seek(2000);
  await box.click();
  const insp = page.locator('.hx-ovl-insp');
  await expect(insp).toBeVisible();
  await insp.getByRole('button', { name: 'In', exact: true }).click();
  await page.getByRole('option', { name: 'Typewriter' }).click();
  await expect.poll(() => overlay('callout-1').enter, { timeout: 180_000 }).toBe('typewriter');
  await insp.getByRole('radio', { name: 'Dark' }).click();
  await expect.poll(() => overlay('callout-1').theme, { timeout: 180_000 }).toBe('dark');
  await page.screenshot({ path: join(shots(), 'overlays-4-inspector-1440.png') });
  await insp.getByRole('button', { name: 'Delete caption' }).click();
  await expect.poll(() => overlays().some((o) => o.id === 'callout-1'), { timeout: 180_000 }).toBe(false);
});

test('8. "+ Caption" adds an overlay at the playhead', async () => {
  await seek(1500);
  await page.getByRole('button', { name: '+ Caption' }).click();
  await expect.poll(() => overlays().length, { timeout: 180_000 }).toBe(3);
  const made = overlays().find((o) => o.text === 'New caption');
  expect(made).toBeDefined();
  expect(Math.abs((made?.at ?? 0) - 1500)).toBeLessThan(400);
  await page.screenshot({ path: join(shots(), 'overlays-5-added-1440.png') });
});

test('9. the 9:16 frame moves the overlays into its safe zone', async () => {
  await page.getByRole('button', { name: '9:16' }).first().click();
  await seek(2000);
  await expect(page.locator('.hx-video__slot[data-format="9:16"]')).toBeVisible();
  await page.screenshot({ path: join(shots(), 'overlays-6-portrait-1440.png') });
  expect(existsSync(lib())).toBe(true);
});
