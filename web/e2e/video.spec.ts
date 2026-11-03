// A video (a Designly project of the kind `video`) end to end on a standalone runtime of its own: made over the API,
// three scenes written as an agent writes them, read back with `harnessly design video get`, played and sought in the
// editor (a block is hidden before its data-tk-at and shown after it), a stretch commented and sent to the chat, a
// scene's length set from the CLI, both formats rendered and checked with ffprobe, an edit that makes the MP4 outdated
// and a render that draws only that scene again, a guest link that plays both formats and takes a comment on a stretch,
// and the phone. The guest's gateway and the owner's access routes are the deck test's stand-ins (deck-rig.ts).
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect as baseExpect, type Frame, type Page, test } from '@playwright/test';

import { deckGateway } from './deck-rig';
import { type Runtime, startRuntime } from './runtime';
import { BODY_AT, SCENES, sceneFile, scenePageId, stubGuestComments } from './video-rig';

const PASSWORD = 'correct horse';
// A shared machine under load answers slowly (the browser runs at idle priority): every wait here is long.
const expect = baseExpect.configure({ timeout: 180_000 });
/** The harnessly CLI (go build ./cmd/harnessly); E2E_HARNESSLY names a built one. */
const CLI = process.env.E2E_HARNESSLY ?? '';
/** A board engine (a project's support.js): a new project needs one, and the test machine has not fetched it. */
const ENGINE = process.env.RUNTIME_DESIGN_ENGINE_FILE ?? '';

// The renders run at idle priority and wait a little for a busy machine: on a loaded one they take minutes.
test.describe.configure({ mode: 'serial', timeout: 3_600_000 });
test.use({ actionTimeout: 180_000 });

let rt: Runtime;
let page: Page;
let project = '';
let guestUrl = '';
const gw = deckGateway(
  () => rt,
  () => project,
);

const shots = () => process.env.E2E_SHOTS ?? tmpdir();
const lib = () => join(rt.home, 'project', 'design-artifacts', project);
const published = (name: string) => join(lib(), '.media', 'published', name);
const clipsDir = () => join(lib(), '.media', 'video', 'scenes');
const bar = (k: string) => page.locator(`.hx-video__bar [data-k="${k}"]`);
const track = () => page.locator('.hx-video-tl__track');

/** The CLI against the test's runtime, with its token. */
const cli = (...args: string[]) => {
  const token = readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();
  const out = spawnSync(CLI, args, {
    env: { ...process.env, HOME: rt.home, HARNESSLY_URL: rt.url, HARNESSLY_CLI_TOKEN: token },
    encoding: 'utf8',
    timeout: 3_000_000,
  });
  return { code: out.status, out: out.stdout, err: out.stderr };
};

interface IGotScene {
  id: string;
  start: number;
  dur: number;
  auto: boolean;
}
const videoState = () => {
  const got = cli('design', 'video', 'get', project, '--json');
  expect(got.code, got.err).toBe(0);
  return JSON.parse(got.out) as { state: { durMs: number; scenes: IGotScene[] } };
};

const probe = (file: string) => {
  const raw = execFileSync('ffprobe', [
    '-v',
    'error',
    '-select_streams',
    'v:0',
    '-show_entries',
    'stream=width,height',
    '-of',
    'csv=p=0',
    file,
  ])
    .toString()
    .trim();
  const [w, h] = raw.split(',').map(Number);
  return { w, h };
};

/** A scene as the agent writes it: the board and its page. */
const writeScene = (i: number) => {
  const file = `scene-${i + 1}.dc.html`;
  writeFileSync(join(lib(), 'project', file), sceneFile(SCENES[i]));
  writeFileSync(
    join(lib(), 'meta', 'pages', `${scenePageId(i)}.json`),
    JSON.stringify({ id: scenePageId(i), title: SCENES[i].title, file, w: 1920, h: 1080, order: i + 1 }),
  );
};

/** The frame of the scene on screen. */
const shownFrame = async (p: Page): Promise<Frame> => {
  const handle = await p.locator('iframe.hx-video__frame[data-on="1"]').elementHandle({ timeout: 180_000 });
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error('no scene frame');
  return frame;
};

const opacityOf = async (p: Page, block: string) =>
  Number(
    await (
      await shownFrame(p)
    )
      .locator(`[data-block="${block}"]`)
      .evaluate((el) => getComputedStyle(el).opacity),
  );

/** A point of the timeline's track at `ms` of the video. */
const trackAt = async (ms: number, total: number) => {
  const box = await track().boundingBox();
  if (!box) throw new Error('no timeline');
  return { x: box.x + (box.width * ms) / total, y: box.y + box.height / 2 };
};

test.beforeAll(async ({ browser }) => {
  test.skip(CLI === '', 'E2E_HARNESSLY names the harnessly CLI (go build ./cmd/harnessly)');
  test.skip(ENGINE === '', 'RUNTIME_DESIGN_ENGINE_FILE names a board engine');
  const root = mkdtempSync(join(tmpdir(), 'hs-video-e2e-'));
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
  page = await browser.newPage({ acceptDownloads: true });
  // The release notes the page shows once would cover the player.
  await page.addLocatorHandler(page.getByRole('button', { name: 'Got it' }), (b) => b.click());
  // the notices of the fake agent's replies come down over the bar: they are taken away like a person would
  await page.addLocatorHandler(
    page.locator('.hx-notices .hx-notice__x').first(),
    async (x) => {
      // a notice that is sliding away is never "stable", and one that left must not hold the test up
      await x.dispatchEvent('click', undefined, { timeout: 2_000 }).catch(() => undefined);
    },
    { noWaitAfter: true },
  );
  await gw.stubAccess(page);
  await stubGuestComments(page, gw);
});

test.afterAll(async () => {
  await page?.close();
  await gw.stop();
  await rt?.stop();
});

test('1. a video made over the API gets three scenes and the CLI reads them', async () => {
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
  expect(existsSync(join(lib(), 'meta', 'video.json'))).toBe(true);
  for (const old of readdirSync(join(lib(), 'meta', 'pages'))) {
    // The template's own first page, if any, gives way to the fixture's scenes.
    if (!old.startsWith('01JVDE')) execFileSync('rm', ['-f', join(lib(), 'meta', 'pages', old)]);
  }
  SCENES.forEach((_, i) => writeScene(i));

  const state = videoState().state;
  expect(state.scenes.map((s) => s.id)).toEqual(SCENES.map((_, i) => scenePageId(i)));
  // Lengths are the runtime's: automatic, at least 3 s, each scene starting where the one before ends.
  for (const [i, s] of state.scenes.entries()) {
    expect(s.auto).toBe(true);
    expect(s.dur).toBeGreaterThanOrEqual(3000);
    expect(s.start).toBe(i === 0 ? 0 : state.scenes[i - 1].start + state.scenes[i - 1].dur);
  }
});

test('2. the editor plays, and a seek shows a block only from its data-tk-at', async () => {
  // The library lists the project as a video before the page opens it.
  const kind = () =>
    page.evaluate(async (id) => {
      const res = await fetch('/api/v2/designly/library');
      const body = (await res.json()) as { artifacts?: { id: string; kind?: string }[] };
      return `${res.status} ${body.artifacts?.find((a) => a.id === id)?.kind ?? 'absent'}`;
    }, project);
  await expect.poll(kind, { timeout: 300_000 }).toBe('200 video');
  // A runtime that has just started has no preview slots yet: the editor would open on its failure.
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

  const time = page.locator('.hx-video__time');
  await page.getByRole('button', { name: 'Play' }).click();
  await expect
    .poll(async () => (await time.textContent())?.startsWith('0:00') === false, { timeout: 180_000 })
    .toBe(true);
  // On a slow machine the clip may have played to its end already.
  const main = page.locator('.hx-video__btn[data-main="1"]');
  if ((await main.getAttribute('aria-label')) === 'Pause') await main.click();
  await expect(main).toHaveAttribute('aria-label', 'Play');

  const total = videoState().state.durMs;
  const before = await trackAt(BODY_AT * 1000 - 700, total);
  await page.mouse.click(before.x, before.y);
  await expect.poll(() => opacityOf(page, 'Body'), { timeout: 180_000 }).toBe(0);
  const after = await trackAt(BODY_AT * 1000 + 1200, total);
  await page.mouse.click(after.x, after.y);
  await expect.poll(() => opacityOf(page, 'Body'), { timeout: 180_000 }).toBe(1);
  await page.screenshot({ path: join(shots(), 'video-2-editor-1440.png') });
});

test('3. a stretch of the timeline gets a comment with a marker, and goes to the chat as a video annotation', async () => {
  const total = videoState().state.durMs;
  const from = await trackAt(500, total);
  const to = await trackAt(2000, total);
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(to.x, to.y, { steps: 8 });
  await page.mouse.up();
  const actions = page.locator('.hx-video-tl__actions');
  await expect(actions).toBeVisible();
  await actions.getByRole('button', { name: 'Comment' }).click();
  const field = page.getByRole('textbox', { name: 'Comment text' });
  await expect(field).toBeVisible();
  await field.fill('Bring the title in sooner');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.hx-video-tl__mark')).toHaveCount(1, { timeout: 180_000 });
  await expect(page.locator('.hx-video-tl__mark')).toHaveAttribute('data-range', '1');

  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(to.x, to.y, { steps: 8 });
  await page.mouse.up();
  await actions.getByRole('button', { name: 'Send to chat' }).click();
  await expect(page.getByText('Timeline selection').first()).toBeVisible();
  await page.keyboard.type('Tighten this stretch');
  await page.keyboard.press('Enter');
  // The agent got the stretch as a block of its own kind.
  const said = () => {
    const dir = join(rt.home, '.claude', 'projects');
    if (!existsSync(dir)) return '';
    return readdirSync(dir, { recursive: true, encoding: 'utf8' })
      .filter((f) => f.endsWith('.jsonl'))
      .map((f) => readFileSync(join(dir, f), 'utf8'))
      .join('\n');
  };
  await expect.poll(() => /kind=\\?"video\\?"/.test(said()), { timeout: 180_000 }).toBe(true);
  expect(said()).not.toMatch(/kind=\\?"deck\\?"/);
});

test('4. a scene length set from the CLI resizes its tile', async () => {
  const width = () =>
    page
      .locator('.hx-video-tl__tile')
      .nth(1)
      .evaluate((el) => el.getBoundingClientRect().width);
  const was = await width();
  const set = cli('design', 'video', 'scene', 'set', project, scenePageId(1), '--dur', '5s');
  expect(set.code, set.err).toBe(0);
  const s = videoState().state.scenes[1];
  expect(s.dur).toBe(5000);
  expect(s.auto).toBe(false);
  await expect.poll(width, { timeout: 180_000 }).toBeGreaterThan(was + 10);
});

test('5. both formats render; ffprobe agrees on the sizes and the editor says the MP4s are current', async () => {
  for (const [format, name, w, h] of [
    ['16:9', 'video.mp4', 1920, 1080],
    ['9:16', 'video-9x16.mp4', 1080, 1920],
  ] as const) {
    const run = cli('design', 'video', 'render', project, '--format', format, '--wait');
    expect(run.code, `${run.out}\n${run.err}`).toBe(0);
    expect(probe(published(name))).toEqual({ w, h });
  }
  const draft = cli('design', 'video', 'render', project, '--format', '16:9', '--draft', '--wait');
  expect(draft.code, `${draft.out}\n${draft.err}`).toBe(0);
  const renders = join(lib(), '.media', 'video', 'renders', '16x9');
  const newest = readdirSync(renders)
    .filter((f) => f.endsWith('.mp4'))
    .map((f) => join(renders, f))
    .sort((a, b) => statSync(b).mtimeMs - statSync(a).mtimeMs)[0];
  expect(probe(newest)).toEqual({ w: 960, h: 540 });

  await bar('render').click();
  const pop = page.getByRole('dialog', { name: 'Render' });
  await expect(pop.locator('[data-format="16:9"] .hx-video-render__badge')).toHaveText('Current', {
    timeout: 180_000,
  });
  await expect(pop.locator('[data-format="9:16"] .hx-video-render__badge')).toHaveText('Current');
  await page.screenshot({ path: join(shots(), 'video-5-render-1440.png') });
});

test('6. an edited scene makes the MP4 outdated, and the render draws only that scene again', async () => {
  const keep = new Map(
    readdirSync(clipsDir())
      .filter((f) => f.endsWith('.mp4'))
      .map((f) => [f, statSync(join(clipsDir(), f)).ino]),
  );
  writeFileSync(
    join(lib(), 'project', 'scene-3.dc.html'),
    sceneFile({ ...SCENES[2], body: 'Exports, sharing and the phone' }),
  );
  const pop = page.getByRole('dialog', { name: 'Render' });
  await expect(pop.locator('[data-format="16:9"] .hx-video-render__badge')).toHaveText('Outdated', {
    timeout: 180_000,
  });
  const run = cli('design', 'video', 'render', project, '--format', '16:9', '--wait');
  expect(run.code, `${run.out}\n${run.err}`).toBe(0);
  await expect(pop.locator('[data-format="16:9"] .hx-video-render__badge')).toHaveText('Current', {
    timeout: 180_000,
  });
  const now = readdirSync(clipsDir()).filter((f) => f.endsWith('.mp4'));
  const fresh = now.filter((f) => !keep.has(f));
  // One new 16:9 clip (the edited scene); the other two were joined as they were, the very same files.
  expect(fresh).toHaveLength(1);
  const state = videoState().state;
  for (const s of state.scenes.slice(0, 2)) {
    const keys = (s as unknown as { keys: Record<string, string> }).keys;
    const clip = `${keys['16:9']}.mp4`;
    expect(keep.get(clip), clip).toBe(statSync(join(clipsDir(), clip)).ino);
  }
  await page.keyboard.press('Escape');
});

test('7. a guest link plays the MP4 in both formats and takes a comment on a stretch the owner sees', async ({
  browser,
}) => {
  await bar('share').click();
  const share = page.getByRole('dialog', { name: 'Share' });
  await expect(share).toBeVisible();
  await share.getByRole('radio', { name: 'Anyone with the link' }).click();
  await expect(share.locator('.hx-deck-share__link')).toBeVisible({ timeout: 180_000 });
  expect(gw.links).toHaveLength(1);
  expect(gw.links[0].view).toMatchObject({ v: 1, deck: 'video', video: true, format: '16:9' });
  await share.getByRole('radio', { name: '9:16' }).click();
  await expect.poll(() => (gw.links[0].view as { format?: string }).format).toBe('9:16');
  await share.getByRole('radio', { name: '16:9' }).click();
  await expect.poll(() => (gw.links[0].view as { format?: string }).format).toBe('16:9');
  guestUrl = String(gw.links[0].url);
  await page.keyboard.press('Escape');

  const guest = await browser.newPage();
  try {
    await guest.goto(guestUrl);
    const video = guest.locator('.hx-vg__stage video');
    const readyState = () => video.evaluate((v: HTMLVideoElement) => v.readyState);
    await expect.poll(readyState, { timeout: 180_000 }).toBeGreaterThanOrEqual(2);
    expect(await video.getAttribute('src')).toMatch(/\/m\/video\.mp4$/);
    await guest.getByRole('button', { name: '9:16' }).click();
    await expect(video).toHaveAttribute('src', /\/m\/video-9x16\.mp4$/);
    await expect.poll(readyState, { timeout: 180_000 }).toBeGreaterThanOrEqual(2);
    expect(await video.evaluate((v: HTMLVideoElement) => [v.videoWidth, v.videoHeight])).toEqual([
      1080, 1920,
    ]);
    await guest.screenshot({ path: join(shots(), 'video-7-guest-916-1440.png') });

    const strip = guest.locator('.hx-vg__strip');
    const box = await strip.boundingBox();
    if (!box) throw new Error('no comment strip');
    await guest.mouse.move(box.x + box.width * 0.05, box.y + box.height / 2);
    await guest.mouse.down();
    await guest.mouse.move(box.x + box.width * 0.2, box.y + box.height / 2, { steps: 8 });
    await guest.mouse.up();
    const text = guest.getByRole('textbox', { name: 'Comment' });
    await expect(text).toBeVisible();
    await text.fill('Too fast here');
    await guest.getByRole('button', { name: 'Send' }).click();
    const named = guest.getByRole('textbox', { name: 'Your name' });
    await expect(named).toBeVisible();
    await named.fill('Gita');
    await guest.getByRole('button', { name: 'Continue' }).click();
    await expect.poll(() => gw.guestComments.length, { timeout: 180_000 }).toBe(1);
    const anchor = gw.guestComments[0].anchor as { state: Record<string, unknown> };
    expect(anchor.state).toMatchObject({ v: 'video', scene: scenePageId(0), format: '9:16' });
    expect(typeof anchor.state.t1).toBe('number');
  } finally {
    await guest.close();
  }

  // The owner sees the guest's comment on the timeline, with its time.
  await page.reload();
  await expect(page.locator('.hx-video-tl__mark')).toHaveCount(2, { timeout: 180_000 });
  await bar('comment').click();
  await expect(page.getByText('Too fast here')).toBeVisible({ timeout: 180_000 });
  await expect(page.locator('.hx-video-tl__mark[data-range="1"]')).toHaveCount(2);
});

test('8. on a phone the editor shows the player and the scenes', async ({ browser }) => {
  const phone = await browser.newContext({
    storageState: await page.context().storageState(),
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  const p = await phone.newPage();
  try {
    await gw.stubAccess(p);
    await stubGuestComments(p, gw);
    await p.goto(`${rt.url}/agents/designly/${project}`);
    await expect(p.locator('.hx-video-player')).toBeVisible({ timeout: 180_000 });
    await shownFrame(p);
    const box = await p.locator('.hx-video__slide').boundingBox();
    expect(box?.width ?? 0).toBeLessThanOrEqual(390);
    // No page wider than the phone.
    expect(await p.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    await p.screenshot({ path: join(shots(), 'video-8-phone.png') });
  } finally {
    await phone.close();
  }
});
