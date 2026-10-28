/// <reference lib="dom" />
import { execFileSync, spawn } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import { createServer as listenUnix } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, type Locator, type Page, test } from '@playwright/test';

import { freePort, type Runtime, startRuntime } from './runtime';

// The interface (app/next over /api/v2) as one person's session on a machine of its own: the person signs in, starts a
// chat with the fake Claude from the home screen, talks to it, opens the agent's terminal, then the right pane with its
// tools — an own shell and the browser — and the machine dialogs. The steps share one page and run in order.
//
// The new interface has no test ids: it is found by the design's class names and the agent's own words.

const PASSWORD = 'correct horse';
/** The Machine tab asks for its figures this often (ms). */
const MACHINE_POLL_MS = 2000;
const reply = (text: string) => `Fake agent reply: ${text}`;

/** What the tracker answers to the epics on the person and to the tasks that stand still. */
const DAY_MS = 86_400_000;
const issue = (
  key: string,
  summary: string,
  state: string,
  person: string | null,
  daysAgo: number,
  subtasks?: boolean[],
) => ({
  idReadable: key,
  summary,
  updated: Date.now() - daysAgo * DAY_MS,
  customFields: [
    { name: 'State', value: { name: state } },
    { name: 'Assignee', value: person ? { fullName: person } : null },
  ],
  ...(subtasks ? { subtasks: { issues: subtasks.map((done) => ({ resolved: done ? 1 : null })) } } : {}),
});
const TRACKER_EPICS = [
  issue('ABC-2628', 'Assistant releases', 'In Progress', 'Peter', 1, [true, true, false, false]),
  issue('ABC-2486', 'Parser hub', 'New', null, 4, []),
];
const TRACKER_STALE = [issue('ABC-3039', 'Skeletons in the order list', 'Testing', 'Peter', 9)];

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;
/** A dev server of the machine for the pane's browser: one page with words to find. */
let site: Server;
let sitePort: number;
/** The tracker's stand-in: the machine reads its epics and stale tasks from it with the person's token (test 11). */
let tracker: Server;
const TRACKER_TOKEN = 'e2e-tracker-token';
/** The GitLab's stand-in: the merge requests the machine's token may read (test 7f). The chat's folder holds three
 *  checkouts, the first and the last of them projects named harnessly. */
let gitlab: Server;
const GITLAB_HOST = 'gitlab.example.test';
const GITLAB_TOKEN = 'e2e-gitlab-token';
const GITLAB_MRS: Record<string, string> = {
  'team/harnessly!80': 'Chips that open',
  'other/harnessly!80': 'A fork of the chips',
  'ai/model-gateway!104': 'Queue the models',
};
const GITLAB_CHECKOUTS: Record<string, string> = {
  harnessly: 'team/harnessly',
  'model-gateway': 'ai/model-gateway',
  'harnessly-fork': 'other/harnessly',
};
/** The machine's recognition service is only a socket the runtime finds at start (it announces dictation); the page's
 *  dictation talks to a stand-in the test routes (3d). */
let asr: ReturnType<typeof listenUnix>;

test.beforeAll(async ({ browser }) => {
  const asrSocket = join(mkdtempSync(join(tmpdir(), 'hs-asr-')), 'asr.sock');
  asr = listenUnix();
  await new Promise<void>((ok) => asr.listen(asrSocket, ok));
  const trackerPort = await freePort();
  tracker = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://tracker.test');
    const issues = (rows: object[]) => {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify(rows));
    };
    if (req.headers.authorization !== `Bearer ${TRACKER_TOKEN}`) {
      res.statusCode = 401;
      res.end();
    } else if (url.pathname !== '/api/issues') {
      res.statusCode = 404;
      res.end();
    } else if ((url.searchParams.get('query') ?? '').includes('Type: Epic')) {
      issues(TRACKER_EPICS);
    } else {
      issues(TRACKER_STALE);
    }
  });
  await new Promise<void>((ok) => tracker.listen(trackerPort, '127.0.0.1', ok));
  const gitlabPort = await freePort();
  gitlab = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://gitlab.test');
    const found = /^\/api\/v4\/projects\/([^/]+)(?:\/merge_requests\/(\d+)(\/diffs)?)?$/.exec(url.pathname);
    const project = found?.[1] ? decodeURIComponent(found[1]) : '';
    const title = GITLAB_MRS[`${project}!${found?.[2]}`];
    res.setHeader('Content-Type', 'application/json');
    if (req.headers['private-token'] !== GITLAB_TOKEN) {
      res.statusCode = 401;
      res.end('{}');
    } else if (url.pathname === '/api/v4/user') {
      res.end('{"username":"ann"}');
    } else if (url.pathname === '/api/v4/merge_requests') {
      res.end('[]');
    } else if (found && !found[2] && Object.values(GITLAB_CHECKOUTS).includes(project)) {
      res.end(JSON.stringify({ path_with_namespace: project }));
    } else if (found?.[2] && title && found[3]) {
      res.end(
        JSON.stringify([
          { old_path: 'a.txt', new_path: 'a.txt', diff: '@@ -1 +1 @@\n-old line\n+new line\n' },
        ]),
      );
    } else if (found?.[2] && title) {
      res.end(
        JSON.stringify({
          iid: Number(found[2]),
          title,
          state: 'opened',
          web_url: `https://${GITLAB_HOST}/${project}/-/merge_requests/${found[2]}`,
          changes_count: '1',
        }),
      );
    } else {
      res.statusCode = 404;
      res.end('{"message":"404 Not found"}');
    }
  });
  await new Promise<void>((ok) => gitlab.listen(gitlabPort, '127.0.0.1', ok));
  rt = await startRuntime({
    env: {
      RUNTIME_GITLAB_HOSTS: GITLAB_HOST,
      RUNTIME_GITLAB_API_ORIGIN: `http://127.0.0.1:${gitlabPort}`,
      GITLAB_SELF_TOKEN: GITLAB_TOKEN,
      RUNTIME_WEB_FEATURES: 'chats,design',
      RUNTIME_VOICE_SOCKET: asrSocket,
      RUNTIME_TRACKER_URL: `http://127.0.0.1:${trackerPort}`,
      RUNTIME_TASK_KEY_RE: '^(ABC|EXP)-[0-9]{1,6}$',
      YOUTRACK_TOKEN: TRACKER_TOKEN,
    },
  });
  // The chat's folder is no checkout itself: it holds some, as a folder of many repositories does.
  for (const [dir, project] of Object.entries(GITLAB_CHECKOUTS)) {
    const checkout = join(rt.home, 'project', dir);
    mkdirSync(checkout, { recursive: true });
    execFileSync('git', ['init', '-q', checkout]);
    execFileSync('git', ['-C', checkout, 'remote', 'add', 'origin', `git@${GITLAB_HOST}:${project}.git`]);
  }
  sitePort = await freePort();
  site = createServer((_, res) => {
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end('<!doctype html><title>dev</title><h1>pane preview works</h1>');
  });
  await new Promise<void>((ok) => site.listen(sitePort, '127.0.0.1', ok));
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
  await new Promise((ok) => site?.close(ok));
  await new Promise((ok) => tracker?.close(ok));
  await new Promise((ok) => gitlab?.close(ok));
  await rt?.stop();
  asr?.close();
});

const feed = () => page.locator('.hx-feed');

test('1. the person signs in and lands on the home screen', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();

  await expect(page.locator('.hx-app')).toBeVisible();
  await expect(page.locator('.hx-hello')).toBeVisible();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
  await expect(page.locator('.hx-sidebar')).toBeVisible();
});

test('2. a first message on the home screen starts a chat the agent answers', async () => {
  await page.locator('.hx-composer__input').fill('hello agent');
  await page.locator('.hx-composer__input').press('Enter');

  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await expect(feed().getByText('hello agent', { exact: true })).toBeVisible();
  await expect(feed().getByText(reply('hello agent'))).toBeVisible({ timeout: 60_000 });
  // The sidebar lists it under its first message.
  await expect(page.locator('.hx-sidebar').getByText('hello agent')).toBeVisible();
});

test('3. the composer of the chat sends the next message; to an idle agent it goes straight, never through the queue', async () => {
  await agentIdle();
  await page.evaluate(() => {
    const w = window as unknown as { queued: string[] };
    w.queued = [];
    new MutationObserver(() => {
      const row = document.querySelector('.hx-queue__row');
      if (row) w.queued.push(row.textContent ?? '');
    }).observe(document.body, { childList: true, subtree: true });
  });
  const input = page.locator('.hx-stage .hx-composer__input');
  await input.fill('second message');
  await input.press('Enter');
  await expect(feed().getByText('second message', { exact: true })).toHaveCount(1);
  await expect(feed().getByText(reply('second message'))).toBeVisible({ timeout: 60_000 });
  await expect(feed().getByText('second message', { exact: true })).toHaveCount(1);
  expect(await page.evaluate(() => (window as unknown as { queued: string[] }).queued)).toEqual([]);
});

test('3b. a file goes to the chat from «+», pasted into the field or dropped on it, and leaves with the message', async () => {
  const composer = page.locator('.hx-stage .hx-composer');
  await composer.locator('.hx-plus-btn').click();
  const chooser = page.waitForEvent('filechooser');
  await page.locator('.hx-plus__item').filter({ hasText: 'File from this computer' }).click();
  await (
    await chooser
  ).setFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('picked') });
  await composer.locator('.hx-composer__input').evaluate((field) => {
    const data = new DataTransfer();
    data.items.add(new File(['pasted'], 'pasted.txt', { type: 'text/plain' }));
    field.dispatchEvent(
      new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }),
    );
  });
  await composer.evaluate((root) => {
    const data = new DataTransfer();
    data.items.add(new File(['dropped'], 'dropped.txt', { type: 'text/plain' }));
    root.dispatchEvent(new DragEvent('dragover', { dataTransfer: data, bubbles: true, cancelable: true }));
    root.dispatchEvent(new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true }));
  });
  for (const name of ['notes.txt', 'pasted.txt', 'dropped.txt'])
    await expect(composer.locator('.hx-attach', { hasText: name })).toBeVisible();
  await expect(composer.locator('.hx-attach__err')).toHaveCount(0);
  await composer.locator('.hx-composer__input').fill('with files');
  await composer.locator('.hx-composer__input').press('Enter');
  await expect(feed().getByText('with files', { exact: true })).toBeVisible();
  await expect(composer.locator('.hx-attach')).toHaveCount(0);
  await expect(feed().getByText(reply('with files'))).toBeVisible({ timeout: 60_000 });
});

test('3b2. on a phone «Photos» and «Camera» open pickers that are in the page; a photo past the limit is squeezed and goes up, one this browser cannot read and a HEIC it cannot turn into a JPEG are refused in words', async () => {
  const desktop = page.viewportSize();
  await page.setViewportSize({ width: 390, height: 844 });
  const composer = page.locator('.hx-stage .hx-composer');
  const chip = (name: string) => composer.locator('.hx-attach', { hasText: name });
  const MB = 1024 * 1024;
  const jpeg = (name: string, megabytes: number) => ({
    name,
    mimeType: 'image/jpeg',
    buffer: Buffer.alloc(Math.round(megabytes * MB), 0xff),
  });
  const choose = async (tile: string, files: Parameters<Locator['setInputFiles']>[0]) => {
    await composer.locator('.hx-plus-btn').click();
    const chooser = page.waitForEvent('filechooser');
    await page.locator('.hx-plus__tile').filter({ hasText: tile }).click();
    await (await chooser).setFiles(files);
  };

  // The pickers wait in the page, shut or open menu: a phone reports a choice only for an input that is in it.
  await expect(page.locator('.hx-composer input[type="file"]')).toHaveCount(3);
  await expect(page.locator('input[data-kind="photo"]')).toHaveAttribute('accept', 'image/*');
  await expect(page.locator('input[data-kind="camera"]')).toHaveAttribute('capture', 'environment');

  // A photo of 8 MB from the gallery goes up whole; setting the files on the page's own input works the same.
  await choose('Photos', jpeg('gallery-8mb.jpg', 8));
  await expect(chip('gallery-8mb.jpg')).toHaveAttribute('data-status', 'ready', { timeout: 30_000 });
  await expect(page.locator('.hx-plus__tiles')).toHaveCount(0);
  await page.locator('input[data-kind="camera"]').setInputFiles(jpeg('camera.jpg', 3));
  await expect(chip('camera.jpg')).toHaveAttribute('data-status', 'ready', { timeout: 30_000 });
  // The same file can be picked again.
  await page.locator('input[data-kind="photo"]').setInputFiles(jpeg('camera.jpg', 1));
  await expect(composer.locator('.hx-attach', { hasText: 'camera.jpg' })).toHaveCount(2);

  // Past the machine's 10 MB a picture is squeezed here and goes up under the limit; one this browser cannot read
  // (and a HEIC it cannot turn into a JPEG) is refused in words before a byte is sent.
  const noise = await page.evaluate(async () => {
    const canvas = document.createElement('canvas');
    canvas.width = 4000;
    canvas.height = 3000;
    const context = canvas.getContext('2d');
    if (!context) throw new Error('no canvas');
    const data = context.createImageData(canvas.width, canvas.height);
    for (let at = 0; at < data.data.length; at += 65536)
      crypto.getRandomValues(data.data.subarray(at, Math.min(at + 65536, data.data.length)));
    for (let at = 3; at < data.data.length; at += 4) data.data[at] = 255;
    context.putImageData(data, 0, 0);
    const blob = await new Promise<Blob | null>((ok) => canvas.toBlob(ok, 'image/jpeg', 1));
    if (!blob) throw new Error('no jpeg');
    return [...new Uint8Array(await blob.arrayBuffer())].reduce(
      (text, byte) => text + String.fromCharCode(byte),
      '',
    );
  });
  const big = Buffer.from(noise, 'binary');
  expect(big.length).toBeGreaterThan(10 * MB);
  const sent = page.waitForRequest((r) => r.method() === 'PUT' && r.url().includes('name=big.jpg'));
  await choose('Photos', { name: 'big.jpg', mimeType: 'image/jpeg', buffer: big });
  await expect(chip('big.jpg')).toHaveAttribute('data-status', 'ready', { timeout: 60_000 });
  expect(Number((await (await sent).allHeaders())['content-length'])).toBeLessThanOrEqual(10 * MB);
  await choose('Photos', jpeg('huge.jpg', 11));
  await expect(chip('huge.jpg').locator('.hx-attach__err')).toHaveText(
    'The file is over 10 MB and cannot be sent',
  );
  await choose('Photos', { name: 'IMG_0001.HEIC', mimeType: 'image/heic', buffer: Buffer.alloc(5 * MB, 1) });
  await expect(chip('IMG_0001.HEIC').locator('.hx-attach__err')).toContainText(
    'Could not prepare this photo',
  );

  while ((await composer.locator('.hx-attach__x').count()) > 0)
    await composer.locator('.hx-attach__x').first().click();
  await expect(composer.locator('.hx-attach')).toHaveCount(0);
  if (desktop) await page.setViewportSize(desktop);
});

/** One frame of a bubble's life on the page. */
interface IFrame {
  /** Copies of the bubble in the feed and in the queue's stack. */
  feed: number;
  stack: number;
  /** The feed row it stands in: its element, its index among the rows and its top within the row. */
  row: string | null;
  index: number;
  top: number;
  rows: number;
  /** Its row's turn still works (once it ends, its work folds up and the row is laid out anew). */
  live: boolean;
  /** Something in its row is mid-animation (the fold of a finished turn): the row is moving by design. */
  animating: boolean;
  /** Turns above the bubble's row that still work: a message of the queue must not stand over them. */
  liveAbove: number;
  scrollTop: number;
  scrollHeight: number;
  /** The live indicator («Gathering context», «Working») stands over the last message of the person: never allowed. */
  nowAboveUser: boolean;
  /** A row that stood in the feed when the watch began left the page: the feed was drawn anew. */
  remounted: boolean;
}

/**
 * Watches where the bubble with this text is on every frame, until `stop`: in the feed (which row, how far down it)
 * or in the stack over the field, and whether the feed's rows and scroll held still.
 */
const watchBubble = async (text: string) => {
  await page.evaluate((needle) => {
    const w = window as unknown as { frames: IFrame[]; stopWatch: () => void };
    const before = [...document.querySelectorAll('.hx-feed .hx-turnwrap')];
    // A row is told by its element: a bubble that becomes its turn's prompt keeps the element (the data-key changes).
    const ids = new WeakMap<Element, string>();
    let counter = 0;
    const idOf = (el: Element) => {
      if (!ids.has(el)) ids.set(el, `row-${(counter += 1)}`);
      return ids.get(el) ?? null;
    };
    const frames: IFrame[] = [];
    let handle = 0;
    const tick = () => {
      const feedEl = document.querySelector<HTMLElement>('.hx-feed');
      if (feedEl) {
        const wraps = [...feedEl.querySelectorAll<HTMLElement>('.hx-turnwrap')];
        const inFeed = [...feedEl.querySelectorAll('.hx-bubble')].filter((el) => el.textContent === needle);
        // A row on its way out (its closing animation) is already gone from the stack.
        const inStack = [...document.querySelectorAll('.hx-queue__text')].filter(
          (el) => el.textContent === needle && !el.closest('[data-leaving]'),
        );
        const row = inFeed[0]?.closest<HTMLElement>('.hx-turnwrap') ?? null;
        const index = row ? wraps.indexOf(row) : -1;
        frames.push({
          feed: inFeed.length,
          stack: inStack.length,
          row: row ? idOf(row) : null,
          index,
          top: row && inFeed[0] ? inFeed[0].getBoundingClientRect().top - row.getBoundingClientRect().top : 0,
          rows: wraps.length,
          live: !!row?.querySelector('.hx-work[data-live]'),
          animating: !!row && row.getAnimations({ subtree: true }).length > 0,
          liveAbove: wraps
            .slice(0, index < 0 ? 0 : index)
            .filter((el) => el.querySelector('.hx-work[data-live]')).length,
          scrollTop: feedEl.scrollTop,
          scrollHeight: feedEl.scrollHeight,
          nowAboveUser: (() => {
            const now = feedEl.querySelector('.hx-now');
            const users = feedEl.querySelectorAll('.hx-msg[data-kind="user"]');
            const last = users[users.length - 1];
            return (
              !!now && !!last && (now.compareDocumentPosition(last) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
            );
          })(),
          remounted: before.some((el) => !el.isConnected),
        });
      }
      handle = requestAnimationFrame(tick);
    };
    tick();
    w.frames = frames;
    w.stopWatch = () => cancelAnimationFrame(handle);
  }, text);
  return async (): Promise<IFrame[]> =>
    page.evaluate(() => {
      const w = window as unknown as { frames: IFrame[]; stopWatch: () => void };
      w.stopWatch();
      return w.frames;
    });
};

/**
 * What a message must never do on the page: vanish after it showed, stand twice, go from the feed back to the stack,
 * change its row or move within it, or make the feed jump or be drawn anew. It may go from the stack into the feed once.
 */
const expectSteady = (frames: IFrame[], where: 'feed' | 'stack-then-feed') => {
  expect(frames.filter((f) => f.nowAboveUser).length, 'the live indicator stood over a message').toBe(0);
  const seen = frames.filter((f) => f.feed + f.stack > 0);
  expect(seen.length, 'the bubble was never on the page').toBeGreaterThan(0);
  const first = frames.indexOf(seen[0]);
  const life = frames.slice(first);
  expect(life.filter((f) => f.feed + f.stack === 0).length, 'the bubble vanished after it showed').toBe(0);
  expect(life.filter((f) => f.feed > 1 || f.stack > 1 || (f.feed > 0 && f.stack > 0)).length, 'twice').toBe(
    0,
  );
  const places = life.map((f) => (f.feed > 0 ? 'feed' : 'stack'));
  const changes = places.filter((place, i) => i > 0 && place !== places[i - 1]);
  expect(
    changes.every((place) => place === 'feed'),
    'a bubble went from the feed back to the stack',
  ).toBe(true);
  expect(changes.length).toBeLessThanOrEqual(1);
  if (where === 'feed') expect(places.includes('stack'), 'it stood in the stack').toBe(false);
  else expect(places[0], 'it did not start in the stack').toBe('stack');
  const fed = life.filter((f) => f.feed > 0);
  expect(new Set(fed.map((f) => f.row)).size, 'the bubble changed its row').toBe(1);
  expect(new Set(fed.map((f) => f.index)).size, 'the bubble changed its place in the feed').toBe(1);
  for (const live of [true, false]) {
    const tops = fed.filter((f) => f.live === live && !f.animating).map((f) => f.top);
    if (tops.length > 0)
      expect(Math.max(...tops) - Math.min(...tops), `moved within its row (live: ${live})`).toBeLessThan(2);
  }
  expect(life.filter((f) => f.remounted).length, 'the feed was drawn anew').toBe(0);
  const jumps = life.filter(
    (f, i) => i > 0 && f.scrollTop < life[i - 1].scrollTop - 2 && f.scrollHeight >= life[i - 1].scrollHeight,
  );
  expect(jumps.length, 'the feed jumped up').toBe(0);
};

const SHOTS = process.env.SHOT_DIR;
const shot = async (name: string) => {
  if (SHOTS) await page.screenshot({ path: `${SHOTS}/${name}.png` });
};
const stage = () => ({
  input: page.locator('.hx-stage .hx-composer__input'),
  stack: page.locator('.hx-queue__row'),
});
/** An agent busy with a tool for ~20 s: the Now block shows it. */
const startLongTask = async (text: string) => {
  const { input } = stage();
  await input.fill(text);
  await input.press('Enter');
  await expect(page.locator('.hx-now')).toBeVisible({ timeout: 15_000 });
};
/** The composer no longer offers Stop: the runtime's state says the agent finished (its answer shows a little earlier). */
const agentIdle = () =>
  expect(page.locator('.hx-stage .hx-composer [aria-label="Stop the agent"]')).toHaveCount(0, {
    timeout: 60_000,
  });
const agentFree = async () => {
  await expect(page.locator('.hx-now')).toHaveCount(0, { timeout: 60_000 });
  await expect(page.locator('.hx-queue__row')).toHaveCount(0, { timeout: 60_000 });
  await agentIdle();
};

/** Sent into the working agent right now: the stack holds it «handed» until the agent reads it, then it is the turn's. */
const steerScenario = async (text: string, name: string) => {
  const { input, stack } = stage();
  await startLongTask(`a long task before ${text}`);
  const stop = await watchBubble(text);
  await input.fill(text);
  await input.press('Control+Enter');
  await expect(stack.filter({ hasText: text })).toHaveCount(1, { timeout: 8_000 });
  await expect(stack.filter({ hasText: text })).toContainText('The agent reads it after the current step');
  await shot(`${name}-stack`);
  await expect(feed().getByText(text, { exact: true })).toHaveCount(1, { timeout: 30_000 });
  await expect(stack).toHaveCount(0);
  await shot(`${name}-delivered`);
  await expect(feed().getByText(reply(text))).toBeVisible({ timeout: 60_000 });
  await agentFree();
  // Folded and finished, the message is still there once.
  await expect(feed().getByText(text, { exact: true })).toHaveCount(1);
  await shot(`${name}-done`);
  expectSteady(await stop(), 'stack-then-feed');
};

/** Enter into the working agent: it stands in the stack till the turn ends, then it is a turn of its own, below. */
const queueScenario = async (text: string, name: string) => {
  const { input, stack } = stage();
  await startLongTask(`a long task before ${text}`);
  const stop = await watchBubble(text);
  await input.fill(text);
  await input.press('Enter');
  await expect(stack.filter({ hasText: text })).toHaveCount(1, { timeout: 8_000 });
  // The row is the message, a pencil and a bin: no note, no «Now».
  const row = stack.filter({ hasText: text });
  await expect(row.getByRole('button', { name: 'Edit' })).toBeVisible();
  await expect(row.getByRole('button', { name: 'Remove from the queue' })).toBeVisible();
  await expect(row.getByRole('button', { name: /Now/ })).toHaveCount(0);
  await expect(row.locator('.hx-queue__note')).toHaveCount(0);
  await shot(`${name}-stack`);
  await expect(feed().getByText(text, { exact: true })).toHaveCount(1, { timeout: 60_000 });
  await expect(stack).toHaveCount(0);
  await shot(`${name}-delivered`);
  await expect(feed().getByText(reply(text))).toBeVisible({ timeout: 60_000 });
  await agentFree();
  await expect(feed().getByText(text, { exact: true })).toHaveCount(1);
  const frames = await stop();
  expectSteady(frames, 'stack-then-feed');
  expect(Math.max(...frames.map((f) => f.liveAbove)), 'the bubble stood over a turn that still worked').toBe(
    0,
  );
  // It comes after the reply of the turn it waited for, the last row of the feed.
  const texts = await page.locator('.hx-feed .hx-turnwrap').allInnerTexts();
  const mine = texts.findIndex((row) => row.includes(text) && !row.includes('before'));
  expect(texts.findIndex((row) => row.includes(reply(`a long task before ${text}`)))).toBeLessThan(mine);
  await shot(`${name}-done`);
};

test('3c. «right now» while the agent works waits over the field «handed» and, once the agent has read it, stands once in its turn; it never vanishes', async () => {
  await steerScenario('steer this', 'steer');
});

test('3c2. Enter while the agent works puts the message in the stack; it goes when the turn ends and stands once below the reply, never in between', async () => {
  await queueScenario('then this', 'queue');
});

test('3c3. a message to an idle agent is in the feed at once and stays where it is, never in the stack', async () => {
  await agentFree();
  const { input, stack } = stage();
  const stop = await watchBubble('plain one');
  await input.fill('plain one');
  await input.press('Enter');
  await expect(feed().getByText('plain one', { exact: true })).toHaveCount(1, { timeout: 8_000 });
  await expect(feed().getByText(reply('plain one'))).toBeVisible({ timeout: 60_000 });
  await expect(stack).toHaveCount(0);
  expectSteady(await stop(), 'feed');
});

test('3c4. the socket drops with a message in the stack and again as one is sent: after the reconnect each stands once and is delivered once', async () => {
  await page.addInitScript(() => {
    const w = window as unknown as { sockets: WebSocket[] };
    w.sockets = [];
    const Native = window.WebSocket;
    window.WebSocket = class extends Native {
      constructor(...args: ConstructorParameters<typeof WebSocket>) {
        super(...args);
        w.sockets.push(this);
      }
    };
  });
  await page.reload();
  const { input, stack } = stage();
  const drop = () =>
    page.evaluate(() => (window as unknown as { sockets: WebSocket[] }).sockets.forEach((s) => s.close()));
  await startLongTask('a long task before the drops');
  const stop = await watchBubble('across the drop');
  await input.fill('across the drop');
  await input.press('Enter');
  await expect(stack.filter({ hasText: 'across the drop' })).toHaveCount(1, { timeout: 8_000 });
  await drop();
  await input.fill('sent in the drop');
  await drop();
  await input.press('Enter');
  await expect(stack.filter({ hasText: 'across the drop' })).toHaveCount(1, { timeout: 15_000 });
  await expect(stack.filter({ hasText: 'sent in the drop' })).toHaveCount(1, { timeout: 15_000 });
  await expect(feed().getByText('across the drop', { exact: true })).toHaveCount(1, { timeout: 60_000 });
  await expect(feed().getByText('sent in the drop', { exact: true })).toHaveCount(1, { timeout: 60_000 });
  await expect(feed().getByText(reply('sent in the drop'))).toBeVisible({ timeout: 60_000 });
  await agentFree();
  await expect(feed().getByText('across the drop', { exact: true })).toHaveCount(1);
  await expect(feed().getByText('sent in the drop', { exact: true })).toHaveCount(1);
  expectSteady(await stop(), 'stack-then-feed');
});

test('3c5. the stack comes back from the machine after a reload and on a second tab, once; the message is delivered once', async ({
  browser,
}) => {
  const { input, stack } = stage();
  await startLongTask('a long task before the reload');
  await input.fill('after the reload');
  await input.press('Enter');
  await expect(stack.filter({ hasText: 'after the reload' })).toHaveCount(1, { timeout: 8_000 });
  await page.reload();
  await expect(stack.filter({ hasText: 'after the reload' })).toHaveCount(1, { timeout: 15_000 });
  // Another device of the same person.
  const device = await browser.newContext({ storageState: await page.context().storageState() });
  const other = await device.newPage();
  await other.goto(page.url());
  await expect(other.locator('.hx-queue__row', { hasText: 'after the reload' })).toHaveCount(1, {
    timeout: 15_000,
  });
  await device.close();
  await expect(feed().getByText('after the reload', { exact: true })).toHaveCount(1, { timeout: 60_000 });
  await expect(feed().getByText(reply('after the reload'))).toBeVisible({ timeout: 60_000 });
  await agentFree();
  await expect(feed().getByText('after the reload', { exact: true })).toHaveCount(1);
});

test('3c5b. the pencil of a queued row takes the message off the queue and back into the field; it is never sent', async () => {
  const { input, stack } = stage();
  await startLongTask('a long task before the pencil');
  await input.fill('edit me');
  await input.press('Enter');
  const row = stack.filter({ hasText: 'edit me' });
  await expect(row).toHaveCount(1, { timeout: 8_000 });
  await row.getByRole('button', { name: 'Edit' }).click();
  await expect(input).toHaveValue('edit me', { timeout: 8_000 });
  await expect(stack).toHaveCount(0, { timeout: 8_000 });
  await agentFree();
  await expect(feed().getByText('edit me', { exact: true })).toHaveCount(0);
  await input.fill('');
});

test('3c6. on a phone the same: ⌃↵ hands the message over, Enter queues it; both stand once and never jump', async () => {
  const desktop = page.viewportSize();
  await page.setViewportSize({ width: 390, height: 844 });
  await steerScenario('steer on a phone', 'phone-steer');
  await queueScenario('queue on a phone', 'phone-queue');
  if (desktop) await page.setViewportSize(desktop);
});

/** The gateway answers 502 to every send while `down.on` is set (a rollout); the page's tries are counted. */
const SEND_URL = /\/api\/v2\/cmd\/send\?/;
const rollout = async () => {
  const down = { on: true, tries: 0 };
  await page.route(SEND_URL, (route) => {
    down.tries += 1;
    return down.on
      ? route.fulfill({
          status: 502,
          contentType: 'text/html',
          body: '<html><h1>502 Bad Gateway</h1></html>',
        })
      : route.continue();
  });
  return down;
};
const redAnywhere = () => page.locator('.hx-msg-user[data-send="failed"], [data-tone="fail"]');

test('3c7. a rollout under a sending message (502 from the gateway) is not a refusal: it waits quietly, nothing turns red however long, and it goes by itself, once', async () => {
  await agentFree();
  const { input, stack } = stage();
  const down = await rollout();
  const stop = await watchBubble('through the rollout');
  await input.fill('through the rollout');
  await input.press('Enter');
  const bubble = feed().locator('.hx-msg-user', { hasText: 'through the rollout' });
  await expect(bubble).toHaveCount(1, { timeout: 8_000 });
  await expect(bubble.getByText('Waiting for the machine')).toBeVisible({ timeout: 8_000 });
  await expect(bubble).toHaveAttribute('data-send', 'waiting');
  await expect(stack).toHaveCount(0);
  // Past the 15 s after which the page used to say «not confirmed» with Retry.
  await page.waitForTimeout(16_000);
  await expect(redAnywhere()).toHaveCount(0);
  await expect(bubble.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  expect(down.tries).toBeGreaterThan(3);
  down.on = false;
  await expect(feed().getByText(reply('through the rollout'))).toBeVisible({ timeout: 90_000 });
  await expect(feed().getByText('through the rollout', { exact: true })).toHaveCount(1);
  await agentFree();
  await expect(redAnywhere()).toHaveCount(0);
  await page.unroute(SEND_URL);
  expectSteady(await stop(), 'feed');
});

test('3c8. a message waiting for the machine survives a reload of the page and goes, once, when the machine is back', async () => {
  await agentFree();
  const { input } = stage();
  const down = await rollout();
  await input.fill('after the rollout reload');
  await input.press('Enter');
  const bubble = () => feed().locator('.hx-msg-user', { hasText: 'after the rollout reload' });
  await expect(bubble().getByText('Waiting for the machine')).toBeVisible({ timeout: 8_000 });
  await page.reload();
  await expect(bubble()).toHaveCount(1, { timeout: 15_000 });
  await expect(bubble().getByText('Waiting for the machine')).toBeVisible({ timeout: 15_000 });
  await expect(redAnywhere()).toHaveCount(0);
  down.on = false;
  await expect(feed().getByText(reply('after the rollout reload'))).toBeVisible({ timeout: 90_000 });
  await expect(feed().getByText('after the rollout reload', { exact: true })).toHaveCount(1);
  await agentFree();
  await expect(redAnywhere()).toHaveCount(0);
  await page.unroute(SEND_URL);
});

test('3d. pictures sit above the text and open full size; the queue stands right on the field', async () => {
  const composer = page.locator('.hx-stage .hx-composer');
  const input = composer.locator('.hx-composer__input');
  // A 1×1 PNG, pasted into the field.
  await input.evaluate((field) => {
    const png = Uint8Array.from(
      atob(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
      ),
      (c) => c.charCodeAt(0),
    );
    const data = new DataTransfer();
    data.items.add(new File([png], 'shot.png', { type: 'image/png' }));
    field.dispatchEvent(
      new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }),
    );
  });
  const chip = composer.locator('.hx-attach', { hasText: 'shot.png' });
  await expect(chip.locator('button[data-pic="true"] img')).toBeVisible({ timeout: 15_000 });
  const [chipBox, inputBox] = [await chip.boundingBox(), await input.boundingBox()];
  expect(inputBox!.y).toBeGreaterThanOrEqual(chipBox!.y + chipBox!.height);
  await chip.locator('button[data-pic="true"]').click();
  await expect(page.locator('.hx-imgview')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.hx-imgview')).toHaveCount(0);
  await chip.locator('.hx-attach__x').click();

  await input.fill('a long task again');
  await input.press('Enter');
  await expect(page.locator('.hx-now')).toBeVisible({ timeout: 15_000 });
  await input.fill('wait your turn');
  await input.press('Enter');
  const queue = page.locator('.hx-queue');
  await expect(queue.locator('.hx-queue__row', { hasText: 'wait your turn' })).toBeVisible({
    timeout: 8_000,
  });
  await queue.evaluate((el) => Promise.all(el.getAnimations({ subtree: true }).map((a) => a.finished)));
  const [queueBox, composerBox] = [await queue.boundingBox(), await composer.boundingBox()];
  expect(Math.abs(composerBox!.y - (queueBox!.y + queueBox!.height))).toBeLessThanOrEqual(1);
  if (process.env.SHOT_DIR) await page.screenshot({ path: `${process.env.SHOT_DIR}/queue.png` });
  await expect(feed().getByText(reply('wait your turn'))).toBeVisible({ timeout: 90_000 });
});

test('3e. dictation: the text comes into the field while it is said and the bar follows the voice; stop fixes it, cancel leaves the field as it was, stop-and-send sends it; a machine without live recognition gets the whole recording', async () => {
  // A microphone that hears a steady tone, and a recognizer that answers the way the machine's socket does.
  await page.evaluate(() => {
    navigator.mediaDevices.getUserMedia = async () => {
      const ctx = new AudioContext();
      await ctx.resume();
      const tone = ctx.createOscillator();
      const gain = ctx.createGain();
      gain.gain.value = 0.5;
      const sink = ctx.createMediaStreamDestination();
      tone.connect(gain).connect(sink);
      tone.start();
      return sink.stream;
    };
  });
  let live = true;
  const heard: string[] = [];
  let posts = 0;
  await page.routeWebSocket(/\/api\/voice\?/, (ws) => {
    let chunks = 0;
    ws.onMessage((message) => {
      if (typeof message !== 'string') {
        chunks += 1;
        if (chunks === 1) ws.send(JSON.stringify({ type: 'partial', text: 'dictated' }));
        if (chunks === 3) ws.send(JSON.stringify({ type: 'partial', text: 'dictated words' }));
        return;
      }
      const { type } = JSON.parse(message) as { type: string };
      heard.push(type);
      if (type === 'start')
        ws.send(
          live
            ? JSON.stringify({ type: 'ready' })
            : JSON.stringify({ type: 'error', code: 'unavailable', message: 'No recognizer here' }),
        );
      if (type === 'stop') ws.send(JSON.stringify({ type: 'final', text: 'dictated words, said' }));
    });
  });
  await page.route(/\/api\/v2\/voice\?/, async (route) => {
    posts += 1;
    await route.fulfill({ json: { text: 'whole recording text' } });
  });

  const composer = page.locator('.hx-stage .hx-composer');
  const input = composer.locator('.hx-composer__input');
  const mic = composer.getByRole('button', { name: 'Voice input' });
  const stop = composer.getByRole('button', { name: 'Stop, the text stays' });
  const bar = composer.locator('.hx-recbar');

  // Stop: the partials are in the field before the stop, the final fixes the text after what was typed.
  await input.fill('typed');
  await mic.click();
  await expect(bar).toBeVisible();
  await expect(input).toHaveValue('typed dictated');
  await expect(input).toHaveValue('typed dictated words');
  await expect(composer).toHaveAttribute('data-rec', 'true');
  // The bars follow the tone: some of them rise well above the resting height.
  await expect
    .poll(() =>
      composer
        .locator('.hx-wave[data-live="true"] i')
        .evaluateAll((bars) =>
          Math.max(...bars.map((b) => Number((b as HTMLElement).style.getPropertyValue('--a') || 0))),
        ),
    )
    .toBeGreaterThan(0.4);
  await stop.click();
  await expect(input).toHaveValue('typed dictated words, said');
  await expect(bar).toHaveCount(0);
  expect(heard).toEqual(['start', 'stop']);

  // Cancel: the field is as it was, and the machine is told to drop the dictation.
  heard.length = 0;
  await mic.click();
  await expect(input).toHaveValue('typed dictated words, said dictated');
  await composer.getByRole('button', { name: 'Cancel and restore the text' }).click();
  await expect(bar).toHaveCount(0);
  await expect(input).toHaveValue('typed dictated words, said');
  expect(heard).toEqual(['start', 'abort']);

  // No recognition on the fly: the recording goes whole after the stop.
  live = false;
  heard.length = 0;
  await mic.click();
  await expect(bar).toBeVisible();
  await stop.click();
  await expect(input).toHaveValue('typed dictated words, said whole recording text');
  expect(posts).toBe(1);

  // Stop and send: the dictated text goes as a message.
  live = true;
  await input.fill('');
  await mic.click();
  await expect(input).toHaveValue('dictated');
  await composer.getByRole('button', { name: 'Stop and send' }).click();
  await expect(feed().getByText('dictated words, said', { exact: true })).toBeVisible();
  await expect(feed().getByText(reply('dictated words, said'))).toBeVisible({ timeout: 60_000 });
  await expect(input).toHaveValue('');
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('3f. prompts: ↑ in an empty field shows the earlier message and Enter puts it in the field, Esc closes; a text saved as a quick prompt comes back with «/»', async () => {
  const composer = page.locator('.hx-stage .hx-composer');
  const input = composer.locator('.hx-composer__input');
  const list = composer.locator('.hx-ph');
  const earlier = composer.getByRole('option', { name: 'dictated words, said' });
  await input.fill('');

  // ↑ in the empty field: the last sent message is in the list, Enter puts it in the field, the list leaves.
  await input.focus();
  await page.keyboard.press('ArrowUp');
  await expect(list).toBeVisible();
  await expect(earlier).toBeVisible();
  await expect(earlier).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Enter');
  await expect(input).toHaveValue('dictated words, said');
  await expect(list).toHaveCount(0);

  // With text in the field ↑ moves the cursor: the list stays shut.
  await page.keyboard.press('ArrowUp');
  await expect(list).toHaveCount(0);

  // Esc closes the list and leaves the field alone.
  await input.fill('');
  await page.keyboard.press('ArrowUp');
  await expect(list).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(list).toHaveCount(0);
  await expect(input).toHaveValue('');

  // «+» → Prompts opens it too; the text typed in the field is saved as /probe and found by «/».
  await input.fill('Check the diff against main');
  await composer.getByRole('button', { name: 'Add' }).click();
  await page.getByRole('menuitem', { name: 'Prompts' }).click();
  await expect(list).toBeVisible();
  await list.getByRole('button', { name: 'Save as quick' }).click();
  await list.getByRole('textbox', { name: 'Name of the quick prompt' }).fill('probe');
  await page.keyboard.press('Enter');
  await expect(page.getByText('Quick prompt /probe saved')).toBeVisible();
  await input.focus();
  await page.keyboard.press('Escape');
  await expect(list).toHaveCount(0);
  await input.fill('/pro');
  await expect(list.getByRole('option', { name: '/probe' })).toBeVisible();
  await page.keyboard.press('Tab');
  await expect(input).toHaveValue('Check the diff against main');
  await expect(list).toHaveCount(0);
  await input.fill('');
});

test('3g. text picked in an answer is quoted into the field as a chip, with a remark or as is, and leads the message', async () => {
  const composer = page.locator('.hx-stage .hx-composer');
  const pick = async (text: string) => {
    await feed()
      .getByText(text, { exact: true })
      .last()
      .evaluate((el) => {
        const range = document.createRange();
        range.selectNodeContents(el);
        const sel = window.getSelection()!;
        sel.removeAllRanges();
        sel.addRange(range);
        document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
      });
    await expect(page.locator('.hx-selbar')).toBeVisible();
  };
  const answer = reply('dictated words, said');

  // A remark: the bar asks what is wrong, the chip carries both.
  await pick(answer);
  await page.locator('.hx-selbar').getByRole('button', { name: 'Comment' }).click();
  await page.locator('.hx-selbar textarea').fill('too long');
  await page.locator('.hx-selbar textarea').press('Enter');
  await expect(page.locator('.hx-selbar')).toHaveCount(0);
  await expect(composer.locator('.hx-attach[data-kind="quote"]')).toContainText('too long');
  await composer.locator('.hx-attach[data-kind="quote"] .hx-attach__x').click();
  await expect(composer.locator('.hx-attach[data-kind="quote"]')).toHaveCount(0);

  // A bare quote goes as the head of the message.
  await pick(answer);
  await page.locator('.hx-selbar').getByRole('button', { name: 'Quote' }).click();
  await expect(composer.locator('.hx-attach[data-kind="quote"]')).toHaveCount(1);
  await composer.locator('.hx-composer__input').fill('shorter please');
  await composer.locator('.hx-composer__input').press('Enter');
  await expect(composer.locator('.hx-attach[data-kind="quote"]')).toHaveCount(0);
  await expect(feed().getByText('shorter please').last()).toBeVisible();
  await expect(
    feed()
      .getByText(/Fake agent reply: > Fake agent reply/)
      .last(),
  ).toBeVisible({ timeout: 60_000 });
});

test("3h. what is left of the agent's limits sits by the model: nothing made up when the machine reports none, both windows with the forecast when it does", async () => {
  const composer = page.locator('.hx-stage .hx-composer');
  const chip = composer.locator('.hx-lim__chip');
  // The page asks again when it comes back to the foreground.
  const askAgain = () => page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));

  // The fake machine has no subscription to read: the chip stays, with a dash, and the card says so.
  await expect(chip).toBeVisible();
  await expect(chip).toContainText('—');
  await chip.click();
  await expect(page.locator('.hx-lim__pop')).toContainText('Claude does not report its limits');
  await expect(page.locator('.hx-lim__pop [role=meter]')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page.locator('.hx-lim__pop')).toHaveCount(0);

  // A machine that reports both windows: the rest of each, the reset, the machine's forecast.
  const inSeconds = (s: number) => Date.now() / 1000 + s;
  const forecast = (status: string, usedAtReset: number | null, resetsAt: number) => ({
    status,
    ratePctPerHour: null,
    exhaustAt: null,
    exhaustRange: null,
    usedAtReset,
    confidence: 'high',
    samples: 12,
    spanSec: 3600,
    reason: null,
    lastAt: null,
    resetsAt,
  });
  const fiveReset = inSeconds(2 * 3600 + 30 * 60);
  const weekReset = inSeconds(3 * 86400);
  await page.route(/\/api\/platform\/machines/, (route) =>
    route.fulfill({
      json: {
        machines: [
          {
            id: 'e2e',
            name: 'e2e',
            limits: {
              claude: {
                fiveHour: {
                  usedPct: 6,
                  resetsAt: fiveReset,
                  forecast: forecast('lasts_until_reset', 30, fiveReset),
                },
                sevenDay: {
                  usedPct: 81,
                  resetsAt: weekReset,
                  forecast: forecast('exhausted', null, weekReset),
                },
                updatedAt: Date.now() / 1000,
                plan: 'Claude Max 20x',
                error: null,
              },
              codex: null,
            },
          },
        ],
      },
    }),
  );
  await askAgain();
  await expect(chip).toContainText('5h94%');
  await expect(chip).toContainText('wk19%');
  await chip.click();
  const card = page.locator('.hx-lim__pop');
  await expect(card.locator('[role=meter]')).toHaveCount(2);
  await expect(card).toContainText('94% left');
  await expect(card).toContainText('Resets in 2 h 30 min');
  await expect(card).toContainText('Will last until the reset');
  await expect(card).toContainText('Limit used up');
  await expect(card).toContainText('Claude Max 20x');
  await chip.click();
  await expect(card).toHaveCount(0);
  await page.unroute(/\/api\/platform\/machines/);
});

test("4. the header opens the agent's terminal, which shows the agent's pane", async () => {
  await page.locator('.hx-header__term').click();
  const term = page.locator('.hx-term');
  await expect(term).toHaveAttribute('data-open', 'true');
  await expect(term.locator('.xterm-rows')).toContainText('shorter please', { timeout: 30_000 });
  await page.locator('.hx-term__btn').click();
  await expect(term).toHaveAttribute('data-open', 'false');
});

test('5. the right pane opens the tools and an own shell that runs commands on the machine', async () => {
  await page.locator('.hx-header__tools').click();
  const pane = page.locator('.hx-pane');
  await expect(pane).toBeVisible();
  await expect(page.locator('.hx-stage')).toHaveAttribute('data-pane', 'true');
  const tiles = pane.locator('.hx-tl__tile');
  // The side chat, the plan, the chat's folder, the agent's terminal, an own shell, the browser; then the tools of the
  // Legacy pane: artifacts, the diff, merge requests, subagents, agents' correspondence, the gallery; and Designly.
  for (const icon of ['message-square-text', 'list-checks', 'files', 'square-terminal', 'terminal', 'globe'])
    await expect(tiles.filter({ has: page.locator(`[data-i="${icon}"]`) })).toHaveCount(1);
  for (const icon of [
    'palette',
    'sparkles',
    'git-compare',
    'git-pull-request',
    'workflow',
    'message-square',
    'images',
  ])
    await expect(tiles.filter({ has: page.locator(`[data-i="${icon}"]`) })).toHaveCount(1);

  await tiles.filter({ has: page.locator('[data-i="terminal"]') }).click();
  const shell = pane.locator('.hx-sh .xterm-rows');
  await expect(shell).toContainText('$', { timeout: 30_000 });
  await pane.locator('.hx-sh .xterm-helper-textarea').focus();
  await page.keyboard.type('echo pane-$((6*7))\n');
  await expect(shell).toContainText('pane-42');
});

test('6. a browser tab of the pane lists the servers and opens an address in a preview slot', async () => {
  const pane = page.locator('.hx-pane');
  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="globe"]') })
    .click();
  await expect(pane.locator('.hx-tbrowser')).toBeVisible();
  await expect(pane.locator('.hx-tv__label')).toBeVisible();

  // A dev server of the machine opens in the slot: its own page is in the frame.
  await pane.locator('.hx-tbrowser__url').fill(String(sitePort));
  await pane.locator('.hx-tbrowser__url').press('Enter');
  await expect(pane.locator('.hx-tab[aria-selected="true"]')).toContainText(`localhost:${sitePort}`);
  await expect(pane.frameLocator('.hx-tbrowser__frame').getByRole('heading')).toHaveText(
    'pane preview works',
  );
  // The address shows once in the bar over the page.
  await expect(pane.locator('.hx-pane__sub')).toHaveCount(0);

  // A port nobody listens on says so in words; the server's page comes back by its address.
  await pane.locator('.hx-tbrowser__url').fill('59997');
  await pane.locator('.hx-tbrowser__url').press('Enter');
  await expect(pane.locator('.hx-tbrowser__note')).toContainText('port 59997');
  await expect(pane.locator('.hx-tbrowser__frame')).toHaveCount(0);
  await pane.locator('.hx-tbrowser__url').fill(String(sitePort));
  await pane.locator('.hx-tbrowser__url').press('Enter');
  await expect(pane.frameLocator('.hx-tbrowser__frame').getByRole('heading')).toHaveText(
    'pane preview works',
  );
});

test('6b. the Machine tab shows the load, and stops a heavy process of the user on the machine', async () => {
  const pane = page.locator('.hx-pane');
  let asks = 0;
  page.on('request', (req) => {
    if (new URL(req.url()).pathname.endsWith('/api/v2/machine/stats')) asks += 1;
  });
  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="cpu"]') })
    .click();
  const mach = pane.locator('.hx-mach');
  await expect(mach).toBeVisible();
  await expect(pane.locator('.hx-tab[aria-selected="true"]')).toContainText('Machine');
  // The tiles in the design's order, the level, the cores, the disk with its parts and both charts.
  await expect(mach.locator('.hx-ch-kpi__label')).toHaveText(['Processor', 'Memory', 'Disk', 'Network']);
  await expect(mach.locator('.hx-lvl')).toBeVisible();
  expect(await mach.locator('.hx-ch-heat i').count()).toBeGreaterThan(0);
  await expect(mach.locator('.hx-ch-stack__part').last()).toHaveAttribute('data-free', 'true');
  await expect(mach.locator('.hx-ch-frame')).toHaveCount(2);

  // A process that burns a core, as the runtime's own user: it is the heaviest, and its row stops it.
  const marker = `e2e-burner-${process.pid}`;
  const burner = spawn(process.execPath, ['-e', 'for(;;){}', marker], { stdio: 'ignore' });
  const ended = new Promise<string | null>((ok) => burner.once('exit', (_code, signal) => ok(signal)));
  try {
    const row = mach.locator('.hx-mach__proc').filter({ hasText: marker });
    await expect(row).toBeVisible({ timeout: 30_000 });
    await row.getByRole('button', { name: /^Stop / }).click();
    // The machine answers, the row leaves and the process is gone.
    await expect(page.locator('.hx-toast').filter({ hasText: 'stopped' })).toBeVisible();
    await expect(row).toHaveCount(0, { timeout: 10_000 });
    expect(await ended).toBe('SIGTERM');
  } finally {
    burner.kill('SIGKILL');
  }

  // The tab goes with its polling: nothing asks the machine for figures once it is closed.
  await pane.locator('.hx-tab[aria-selected="true"]').hover();
  await pane.locator('.hx-tab[aria-selected="true"] .hx-tab__x').click();
  await expect(mach).toHaveCount(0);
  const asked = asks;
  await page.waitForTimeout(MACHINE_POLL_MS + 1500);
  expect(asks).toBe(asked);
});

test('7. closing the shell tab ends the shell on the machine; Esc closes the pane', async () => {
  const pane = page.locator('.hx-pane');
  await pane
    .locator('.hx-tab')
    .filter({ has: page.locator('[data-i="terminal"]') })
    .click();
  await pane.locator('.hx-tab[aria-selected="true"] .hx-tab__x').click();
  await expect(pane.locator('.hx-tab').filter({ has: page.locator('[data-i="terminal"]') })).toHaveCount(0);

  const conv = new URL(page.url()).pathname.split('/').pop() ?? '';
  const list = await page.request.get(`${rt.url}/api/v2/conversations/${conv}/terminals`);
  expect(await list.json()).toEqual([]);

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
  await expect(page.locator('.hx-stage')).not.toHaveAttribute('data-pane', 'true');
});

test("7b. the pane's plan shows no steps of a fake agent; its files open the chat's folder and a file in it", async () => {
  await page.locator('.hx-header__tools').click();
  const pane = page.locator('.hx-pane');
  // The pane comes back with its last tab (the browser): the tools are behind «+».
  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="list-checks"]') })
    .click();
  // A plan that failed has a retry link; the fake agent's plan is merely empty.
  await expect(pane.locator('[data-tool="plan"] .hx-tv__empty')).toBeVisible({ timeout: 10_000 });
  await expect(pane.locator('[data-tool="plan"] .hx-tv__link')).toHaveCount(0);

  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="files"]') })
    .click();
  await expect(pane.locator('.hx-disk__crumbs')).toBeVisible();
  const file = pane
    .locator('.hx-tv__row')
    .filter({ has: page.locator('[data-i="file-text"]') })
    .first();
  await expect(file).toBeVisible();
  await file.click();
  // The folder holds one file, README.md, and a markdown file opens as an editor (7b2).
  await expect(pane.locator('.hx-fed .cm-content')).toContainText('# demo project');

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
});

const project = () => join(rt.home, 'project');

/** The chat's folder in the right pane, opened from the header and from the launcher's «Files» tile. */
const openFiles = async () => {
  await page.locator('.hx-header__tools').click();
  const pane = page.locator('.hx-pane');
  const have = pane.locator('.hx-tab').filter({ has: page.locator('[data-i="files"]') });
  if ((await have.count()) > 0) await have.click();
  else {
    await pane.locator('.hx-tab[data-home]').click();
    await pane
      .locator('.hx-tl__tile')
      .filter({ has: page.locator('[data-i="files"]') })
      .click();
  }
  // The tab comes back with the file it had open: the folder is where to start.
  await pane.locator('.hx-disk__crumb').first().click();
  return pane;
};

test('7b2. a markdown file opens as an editor: edited, saved with Ctrl+S, still there after a reload, read as a page', async () => {
  const pane = await openFiles();
  await pane.locator('.hx-tv__row', { hasText: 'README.md' }).click();
  const editor = pane.locator('.hx-fed');
  await expect(editor.locator('.cm-content')).toContainText('# demo project');
  await expect(editor.locator('.hx-fed__lang')).toHaveText('Markdown');
  await expect(editor.locator('.hx-fed__state')).toHaveText('Saved');
  await expect(pane.getByRole('button', { name: 'Save' })).toBeDisabled();

  await editor.locator('.cm-content').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('\nEdited in the pane.\n');
  await expect(editor.locator('.hx-fed__state')).toHaveText('Modified');
  await expect(pane.locator('.hx-tab[data-dirty]')).toHaveCount(1);
  await expect(pane.getByRole('button', { name: 'Save' })).toBeEnabled();
  await page.keyboard.press('Control+s');
  await expect(editor.locator('.hx-fed__state')).toHaveText('Saved');
  await expect(pane.locator('.hx-tab[data-dirty]')).toHaveCount(0);
  expect(readFileSync(join(project(), 'README.md'), 'utf8')).toBe('# demo project\n\nEdited in the pane.\n');

  await editor.getByRole('radio', { name: 'Preview' }).click();
  await expect(editor.locator('.hx-fed__page h1')).toHaveText('demo project');
  await expect(editor.locator('.hx-fed__page')).toContainText('Edited in the pane.');
  await editor.getByRole('radio', { name: 'Edit' }).click();
  await expect(editor.locator('.hx-fed__page')).toHaveCount(0);

  await page.reload();
  const again = await openFiles();
  await again.locator('.hx-tv__row', { hasText: 'README.md' }).click();
  await expect(again.locator('.hx-fed .cm-content')).toContainText('Edited in the pane.');

  // «/» offers the markdown blocks; a pick lands in the text.
  await again.locator('.hx-fed .cm-content').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('/tab');
  await expect(page.locator('.cm-tooltip-autocomplete')).toContainText('Table');
  // The list ignores a pick in the first moments after it opens (a key typed with the last letter must not take it).
  await page.waitForTimeout(300);
  await page.keyboard.press('Enter');
  await expect(again.locator('.hx-fed .cm-content')).toContainText('| --- | --- |');
  await page.keyboard.press('Control+s');
  await expect(again.locator('.hx-fed__state')).toHaveText('Saved');

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(page.locator('.hx-pane')).toHaveCount(0);
});

test('7b3. a code file: the language by its name, words complete, a file changed on disk is a conflict, leaving asks', async () => {
  writeFileSync(join(project(), 'main.ts'), 'const alphabet = 1;\n');
  const pane = await openFiles();
  await pane.locator('.hx-tv__row', { hasText: 'main.ts' }).click();
  const editor = pane.locator('.hx-fed');
  await expect(editor.locator('.hx-fed__lang')).toHaveText('TypeScript');
  await editor.locator('.cm-content').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('alp');
  await expect(page.locator('.cm-tooltip-autocomplete')).toContainText('alphabet');
  await page.keyboard.press('Escape');
  // Esc went to the editor's completions: the pane stays.
  await expect(pane).toBeVisible();

  // The disk moved on while the text was open: the save is refused in the runtime's words, the text is not lost.
  writeFileSync(join(project(), 'main.ts'), 'const changed = 2;\n');
  await page.keyboard.press('Control+s');
  const notice = editor.locator('.hx-fed__notice[data-kind="conflict"]');
  await expect(notice).toContainText('changed after it was opened');
  expect(readFileSync(join(project(), 'main.ts'), 'utf8')).toBe('const changed = 2;\n');
  await notice.getByRole('button', { name: 'Load from disk' }).click();
  await expect(editor.locator('.cm-content')).toContainText('const changed = 2;');
  await expect(editor.locator('.hx-fed__state')).toHaveText('Saved');

  await editor.locator('.cm-content').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// more');
  await pane.locator('.hx-disk__crumb').first().click();
  const ask = page.locator('.hx-leave');
  await expect(ask).toContainText('There are unsaved changes');
  await ask.getByRole('button', { name: 'Stay' }).click();
  await expect(ask).toHaveCount(0);
  await expect(editor.locator('.cm-content')).toContainText('// more');
  await pane.locator('.hx-disk__crumb').first().click();
  await ask.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(pane.locator('.hx-tv__row', { hasText: 'main.ts' })).toBeVisible();
  expect(readFileSync(join(project(), 'main.ts'), 'utf8')).toBe('const changed = 2;\n// more');

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(page.locator('.hx-pane')).toHaveCount(0);
});

test('7c. the side chat opens over the window, docks into the pane and comes out of it; Esc closes it', async () => {
  await page.locator('.hx-header__tools').click();
  const pane = page.locator('.hx-pane');
  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="message-square-text"]') })
    .click();
  const card = page.locator('.hx-sidechat');
  await expect(card).toBeVisible();
  await expect(card.locator('.hx-sidechat__input')).toBeFocused();

  await card.getByRole('button', { name: 'To the right pane' }).click();
  await expect(card).toHaveCount(0);
  await expect(pane.locator('.hx-tab[aria-selected="true"]')).toContainText('Side chat');
  await expect(pane.locator('.hx-sidedock')).toBeVisible();

  await pane.getByRole('button', { name: 'Over the window' }).click();
  await expect(card).toBeVisible();
  await expect(pane.locator('.hx-sidedock')).toHaveCount(0);

  await card.locator('.hx-sidechat__input').press('Escape');
  await expect(card).toHaveCount(0);
  // Esc went to the window: the pane under it stays.
  await expect(pane).toBeVisible();
  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
});

/** Opens a tool of the pane from its launcher, by the tool's icon. */
async function openTool(icon: string) {
  const pane = page.locator('.hx-pane');
  await pane.locator('.hx-tab[data-home]').click();
  await pane
    .locator('.hx-tl__tile')
    .filter({ has: page.locator(`[data-i="${icon}"]`) })
    .click();
  await expect(pane.locator('.hx-tab[aria-selected="true"]').locator(`[data-i="${icon}"]`)).toBeVisible();
}

test("7d. the Legacy pane's tools open as tabs: artifacts, the diff, merge requests, subagents, correspondence, gallery", async () => {
  const input = page.locator('.hx-stage .hx-composer__input');
  await input.fill('share the links');
  await input.press('Enter');
  await expect(feed().getByText('release notes')).toBeVisible({ timeout: 60_000 });

  await page.locator('.hx-header__tools').click();
  const pane = page.locator('.hx-pane');
  // The design has no overview: Legacy's is not among the tabs.
  await expect(pane.locator('.hx-tab').filter({ has: page.locator('[data-i="layers"]') })).toHaveCount(0);

  await openTool('sparkles');
  await expect(pane.locator('.tw-rpane-panel-artifacts')).toContainText('release notes');
  await openTool('git-pull-request');
  await expect(pane.locator('.tw-rpane-panel-gitlab')).toBeVisible();
  await openTool('git-compare');
  await expect(pane.locator('.tw-rpane-panel-changes')).toBeVisible();
  await openTool('workflow');
  await expect(pane.locator('.tw-rpane-panel-subagents')).toBeVisible();
  await openTool('message-square');
  await expect(pane.locator('.tw-rpane-panel-peers')).toBeVisible();
  await openTool('images');
  await expect(pane.locator('.tw-rpane-panel-gallery')).toBeVisible();

  await pane
    .locator('.hx-tab')
    .filter({ has: page.locator('[data-i="sparkles"]') })
    .click();
  await expect(pane.locator('.tw-rpane-panel-artifacts')).toContainText('release notes');
  // A Legacy tab closes like the pane's own.
  const gallery = pane.locator('.hx-tab').filter({ has: page.locator('[data-i="images"]') });
  await gallery.click();
  await gallery.locator('.hx-tab__x').click();
  await expect(pane.locator('.hx-tab').filter({ has: page.locator('[data-i="images"]') })).toHaveCount(0);

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
});

test('7e. what the agent asks its pane to show (`harnessly open`) opens there: a dev server in the browser, a tool by its kind', async () => {
  const conv = new URL(page.url()).pathname.split('/').pop() ?? '';
  const legacy = await page.request.get(`${rt.url}/api/v2/conversations/${conv}/legacy`);
  expect(legacy.ok()).toBe(true);
  const { sessionName } = (await legacy.json()) as { sessionName: string };
  const token = readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();
  const ask = (command: Record<string, string>) =>
    page.request.post(`${rt.url}/api/cli/panel`, {
      headers: { 'x-cli-token': token },
      data: { session: sessionName, command },
    });

  expect((await ask({ kind: 'open', tab: 'browser', target: String(sitePort) })).ok()).toBe(true);
  const pane = page.locator('.hx-pane');
  await expect(pane).toBeVisible({ timeout: 15_000 });
  await expect(pane.locator('.hx-tab[aria-selected="true"]')).toContainText(`localhost:${sitePort}`);
  await expect(pane.frameLocator('.hx-tbrowser__frame').getByRole('heading')).toHaveText(
    'pane preview works',
  );

  // With the pane closed, a tool comes forward with it (an open pane gets it behind, unread, as in Legacy).
  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
  expect((await ask({ kind: 'open', tab: 'gitlab' })).ok()).toBe(true);
  await expect(pane.locator('.hx-tab[aria-selected="true"] [data-i="git-pull-request"]')).toBeVisible({
    timeout: 15_000,
  });
  await expect(pane.locator('.tw-rpane-panel-gitlab')).toBeVisible();

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
});

test('7f. a chip opens what it names in its own view: a task, a merge request of the project the text names, a site', async () => {
  const input = page.locator('.hx-stage .hx-composer__input');
  await input.fill('your references');
  await input.press('Enter');
  const answer = feed().locator('.hx-ref').filter({ hasText: 'ABC-12' }).last();
  await expect(answer).toBeVisible({ timeout: 60_000 });

  await answer.click();
  const pane = page.locator('.hx-pane');
  await expect(pane.locator('.tw-rpane-panel-task')).toBeVisible({ timeout: 15_000 });
  await expect(pane.locator('.hx-tab[aria-selected="true"]')).toContainText('ABC-12');
  await expect(pane.locator('.hx-tbrowser')).toHaveCount(0);

  // The merge requests of the answer in a folder of many repositories: the project is the word before the number.
  // «model-gateway !104» is one project's, «harnessly !80» is two projects' (they ask which), and what no project
  // has («!999») or a tool's output made up («!502») is text, not a chip that opens nothing.
  const merge = (label: string) => feed().locator('.hx-ref[data-type="mr"]').filter({ hasText: label });
  await expect(merge('!104')).toBeVisible({ timeout: 15_000 });
  await expect(merge('!80')).toBeVisible();
  await expect(merge('!999')).toHaveCount(0);
  await expect(merge('!502')).toHaveCount(0);
  await expect(feed().getByText(/!999 and !502 are typos/)).toBeVisible();

  await merge('!104').click();
  await expect(pane.locator('.tw-rpane-panel-gitlab')).toContainText('Queue the models', { timeout: 15_000 });

  await merge('!80').click();
  const choices = page.getByRole('menu', { name: 'Which project?' });
  await expect(choices.getByRole('menuitem')).toHaveText([
    'team/harnesslyChips that open',
    'other/harnesslyA fork of the chips',
  ]);
  await choices.getByRole('menuitem', { name: /other\/harnessly/ }).click();
  await expect(choices).toHaveCount(0);
  await expect(pane.locator('.tw-rpane-panel-gitlab')).toContainText('A fork of the chips', {
    timeout: 15_000,
  });

  await feed().locator('.hx-ref').filter({ hasText: 'example.com' }).last().click();
  await expect(pane.locator('.hx-tab[aria-selected="true"]')).toContainText('example.com');

  // Closing the tabs one by one never leaves the pane empty: the last gives way to the tools, as «+» opens them.
  for (let left = await pane.locator('.hx-tab').count(); left > 0; left--) {
    const current = pane.locator('.hx-tab[aria-selected="true"]');
    if ((await current.count()) === 0 || (await pane.locator('.hx-tl').count()) > 0) break;
    await current.hover();
    await current.locator('.hx-tab__x').click();
  }
  await expect(pane.locator('.hx-tl')).toBeVisible();

  await page.locator('.hx-header').click();
  await page.keyboard.press('Escape');
  await expect(pane).toHaveCount(0);
});

test('8. the new project dialog lists folders of the machine', async () => {
  await page.locator('.hx-nav__add').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('project', { exact: false }).first()).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
});

test('8b. «More» in the sidebar opens the statistics and the efficiency as pages', async () => {
  await page.locator('.hx-nav__item').filter({ hasText: 'More' }).click();
  await page.locator('.hx-nav__kid').filter({ hasText: 'Statistics' }).click();
  await expect(page.locator('.hx-lscreen[data-screen="stats"]')).toBeVisible();
  await expect(page).toHaveURL(/\/stats$/);
  await page.locator('.hx-nav__kid').filter({ hasText: 'Efficiency' }).click();
  await expect(page.locator('.hx-lscreen[data-screen="efficiency"]')).toBeVisible();
  await page.locator('.hx-nav__item').filter({ hasText: 'New chat' }).click();
  await expect(page.locator('.hx-lscreen')).toHaveCount(0);
});

test('8c. the sidebar folds to a rail and opens again: the window has its place at once and slides there on the compositor, its content keeps one width; a toggle in the middle goes on from where the edge is', async () => {
  const app = page.locator('.hx-app');
  const probe = () =>
    page.evaluate(() => {
      const el = (sel: string) => document.querySelector<HTMLElement>(sel);
      const slides = document
        .getAnimations()
        .map((a) => (a.effect as KeyframeEffect | null)?.target)
        .filter((t): t is HTMLElement => t instanceof HTMLElement);
      return {
        move: el('.hx-app')?.dataset.move ?? null,
        mainLeft: el('.hx-main')?.getBoundingClientRect().left ?? -1,
        bodyWidth: el('.hx-main__body')?.getBoundingClientRect().width ?? -1,
        sideWidth: el('.hx-sidebar')?.getBoundingClientRect().width ?? -1,
        bodySlides: slides.filter((t) => t.classList.contains('hx-main__body')).length,
        mainSlides: slides.filter((t) => t.classList.contains('hx-main')).length,
        clip: el('.hx-main') ? getComputedStyle(el('.hx-main') as HTMLElement).clipPath : '',
      };
    });
  const settled = async () => {
    await expect(app).not.toHaveAttribute('data-move', /.*/);
    return probe();
  };

  const open = await probe();
  expect(open.sideWidth).toBe(232);
  await page.locator('.hx-brand__close').click();
  await expect(app).toHaveAttribute('data-side', 'closed');
  // At the first frame: the grid is the rail's already, the window is only drawn where it was.
  const folding = await probe();
  expect(folding.mainLeft).toBe(58);
  expect(folding.move).toBe('on');
  expect(folding.bodySlides).toBe(1);
  expect(folding.mainSlides).toBe(1);
  expect(folding.bodyWidth).toBe(open.bodyWidth + 174);
  // The sidebar keeps its width under the window's edge until the edge has come.
  expect(folding.sideWidth).toBe(232);
  const rail = await settled();
  expect(rail.sideWidth).toBe(58);
  expect(rail.bodySlides + rail.mainSlides).toBe(0);
  expect(rail.clip).toBe('none');
  expect(rail.bodyWidth).toBe(open.bodyWidth + 174);

  await page.keyboard.press('Control+b');
  await expect(app).toHaveAttribute('data-side', 'open');
  const opening = await probe();
  expect(opening.mainLeft).toBe(232);
  expect(opening.sideWidth).toBe(232);
  expect(opening.move).toBe('on');
  expect(opening.bodyWidth).toBe(open.bodyWidth);
  // Back before the slide is over: the slide that goes on starts from the edge where it is, not from the far end.
  await page.waitForTimeout(150);
  await page.keyboard.press('Control+b');
  await expect(app).toHaveAttribute('data-side', 'closed');
  expect((await probe()).bodySlides).toBe(1);
  await settled();
  await page.keyboard.press('Control+b');
  const back = await settled();
  expect(back.sideWidth).toBe(232);
  expect(back.mainLeft).toBe(232);
  expect(back.bodyWidth).toBe(open.bodyWidth);
});

test('9. Designly opens from the sidebar with its library', async () => {
  const toDesignly = () =>
    page
      .locator('.hx-nav__item')
      .filter({ has: page.locator('[data-i="palette"]') })
      .click();
  // From the start page the field flies up to Designly's; from Designly «New chat» flies it back. The copy is gone
  // once it has landed.
  await page
    .locator('.hx-nav__item')
    .filter({ has: page.locator('[data-i="square-pen"]') })
    .click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  await toDesignly();
  await expect(page).toHaveURL(/\/designly$/);
  await expect(page.locator('.hx-dl')).toBeVisible();
  await expect(page.locator('.hx-dl[data-fly]')).toHaveCount(1);
  await expect(page.locator('.hx-ghost')).toHaveCount(0);
  await page
    .locator('.hx-nav__item')
    .filter({ has: page.locator('[data-i="square-pen"]') })
    .click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  await expect(page.locator('.hx-ghost')).toHaveCount(0);
  // The field came to the start page from Designly: it stands in its place, it does not rise from a chat's.
  await expect(page.locator('.hx-stage[data-phase="home"]')).not.toHaveAttribute('data-back', 'chat');
  await toDesignly();
  await expect(page.locator('.hx-dl')).toBeVisible();
  // «Draw» with nothing written is not refused: it takes the person back to the field.
  await page.locator('.hx-dl__composer .hx-round[data-variant="accent"]').click();
  await expect(page.locator('.hx-dl__input')).toBeFocused();
  // The agent that draws is picked as in the chat.
  await expect(page.locator('.hx-dl__model .hx-model')).toBeVisible();

  // On a phone the kinds are a strip: the edge with more behind it is marked and the arrow steps to it.
  const size = page.viewportSize();
  await page.setViewportSize({ width: 360, height: 740 });
  const track = page.locator('.hx-hs__track');
  await expect(track).toHaveAttribute('data-hs-r', '');
  await expect(track).not.toHaveAttribute('data-hs-l', '');
  await page.locator('.hx-hs__arrow[data-dir="next"]').click();
  await expect(track).toHaveAttribute('data-hs-l', '');
  if (size) await page.setViewportSize(size);
});

test('10. a new design opens with its chat; an edit picked on its page goes to the agent as a note', async () => {
  // The library's template: one board (a `.dc.html` file of project/) with words to pick.
  const template = join(rt.home, 'project', 'design-artifacts', '.template');
  mkdirSync(join(template, 'project'), { recursive: true });
  writeFileSync(
    join(template, 'artifact.json'),
    JSON.stringify({
      v: 1,
      title: 'template',
      boards: [{ title: 'main', path: 'main.dc.html' }],
    }),
  );
  // The board declares settings the way the design runtime does (its registry and `__dcSetProps`); the library's script
  // offers them to the site, which draws them in «Live settings».
  const settings = `<script>
    window.__dcUpdate = () => {};
    window.__dcRootName = () => 'Root';
    window.__dcRegistry = { Root: { propOverrides: {}, propsMeta: {
      theme: { label: 'Theme', section: 'Look', editor: 'enum', options: ['dark', 'light'], default: 'dark' },
      cards: { label: 'Cards in a row', section: 'State', editor: 'number', min: 2, max: 6, step: 1, default: 4 },
      hot: { label: 'Hot bar', section: 'State', editor: 'boolean', default: true },
    } } };
    window.__dcSetProps = (name, next) => { window.__dcRegistry[name].propOverrides = next; };
  </script>`;
  writeFileSync(
    join(template, 'project', 'main.dc.html'),
    `<!doctype html><title>main</title>${settings}<h1 id="hero">Coffee for everyone</h1><p>Menu</p><div style="height:2400px"></div>`,
  );

  await page.locator('.hx-dl__input').fill('Coffee shop landing');
  await page.locator('.hx-dl__composer .hx-round[data-variant="accent"]').click();
  const card = page.locator('.hx-dl-card').first();
  await expect(card).toBeVisible({ timeout: 20_000 });
  // The runtime wrote who made the project: its card carries the owner's avatar, it is in «All» and «Mine», and not
  // in «Shared» (the teammates' designs).
  await expect(card.locator('.hx-dl-card__who')).toHaveCount(1);
  const tabs = page.locator('.hx-dl__tab');
  await expect(tabs).toHaveText(['All', 'Mine', 'Shared']);
  await expect(tabs.nth(0)).toHaveAttribute('aria-selected', 'true');
  await tabs.nth(2).click();
  await expect(tabs.nth(2)).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.hx-dl-card')).toHaveCount(0);
  await expect(page.locator('.hx-dl__empty')).toBeVisible();
  await tabs.nth(1).click();
  await expect(page.locator('.hx-dl-card')).toHaveCount(1);
  await tabs.nth(0).click();
  await expect(page.locator('.hx-dl-card')).toHaveCount(1);
  await card.click();
  await expect(page).toHaveURL(/\/designly\/[^/]+$/);

  // The pieces of the site are dragged by their grabs: the field to an edge docks the chat there, the border of the
  // chat takes its width, the chat's head takes it to the other side.
  const section = page.locator('.hx-site');
  const pull = async (grab: Locator, to: (box: { x: number; y: number }) => { x: number; y: number }) => {
    // The site slides when its mode changes: the grab is taken where it has stopped.
    let box = await grab.boundingBox();
    for (let i = 0; i < 40; i++) {
      await page.waitForTimeout(100);
      const next = await grab.boundingBox();
      const still = box && next && box.x === next.x && box.y === next.y;
      box = next;
      if (still) break;
    }
    if (!box) throw new Error('nothing to grab');
    const from = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
    const end = to(from);
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.mouse.move(end.x, end.y, { steps: 12 });
    await page.mouse.up();
  };
  await expect(section).toHaveAttribute('data-min', 'true');
  await page.locator('.hx-island__mini').click();
  await expect(section).toHaveAttribute('data-min', 'false');

  // ⌘R (Ctrl+R here) reloads the page of the design, as the button's tip says: what the old page kept is gone.
  const kept = () =>
    page
      .frameLocator('.hx-site__iframe')
      .locator('h1')
      .evaluate(() => (window as unknown as { kept?: boolean }).kept === true)
      .catch(() => false);
  await page
    .frameLocator('.hx-site__iframe')
    .locator('h1')
    .evaluate(() => {
      (window as unknown as { kept?: boolean }).kept = true;
    });
  await expect.poll(kept).toBe(true);
  await page.keyboard.press('Control+r');
  await expect.poll(kept, { timeout: 15_000 }).toBe(false);
  await expect(page.frameLocator('.hx-site__iframe').locator('h1')).toBeVisible();
  const bounds = await section.boundingBox();
  if (!bounds) throw new Error('no site');
  await pull(page.locator('.hx-island__grab'), (from) => ({
    x: bounds.x + bounds.width - 24,
    y: from.y - 120,
  }));
  await expect(section).toHaveAttribute('data-mode', 'chat');
  await expect(section).toHaveAttribute('data-dock', 'right');
  // A phone has one column: there the chat is never docked to the right.
  const desktop = page.viewportSize();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(section).toHaveAttribute('data-dock', 'left');
  if (desktop) await page.setViewportSize(desktop);
  await expect(section).toHaveAttribute('data-dock', 'right');
  const widthOf = () => section.evaluate((el) => (el as HTMLElement).style.getPropertyValue('--site-chat'));
  const width0 = await widthOf();
  await pull(page.locator('.hx-site__split'), (from) => ({ x: from.x - 80, y: from.y }));
  await expect.poll(widthOf).not.toBe(width0);
  await pull(page.locator('.hx-site__chat-head'), (from) => ({ x: bounds.x + 24, y: from.y + 200 }));
  await expect(section).toHaveAttribute('data-dock', 'left');
  // Let go of the border close to the edge: the chat closes and the field stays over the site.
  await pull(page.locator('.hx-site__split'), (from) => ({ x: from.x - 400, y: from.y }));
  await expect(section).toHaveAttribute('data-mode', 'island');

  // The chat beside the site holds the design's conversation: the brief and the agent's answer.
  await page.locator('.hx-site-modes__item[aria-label="Chat"]').click();
  const log = page.locator('.hx-site__log');
  await expect(log.locator('.hx-site-msg[data-who="agent"]').first()).toBeVisible({ timeout: 60_000 });

  // The page scrolls: the site says so for a moment (the island narrows meanwhile), then it is calm again.
  await page
    .frameLocator('.hx-site__iframe')
    .locator('h1')
    .evaluate(() => window.scrollTo(0, 300));
  await expect(section).toHaveAttribute('data-scrolling', 'true');
  await expect(section).toHaveAttribute('data-scrolling', 'false', { timeout: 10_000 });

  // The browser bar's «Pick» is the island's switch: it turns picking on over the page, and off again.
  const pickTool = page.locator('.hx-browser__tool');
  await expect(pickTool).toHaveAttribute('aria-disabled', 'false', { timeout: 20_000 });
  await pickTool.click();
  await expect(section).toHaveAttribute('data-pick', 'true');
  await expect(pickTool).toHaveAttribute('aria-pressed', 'true');
  await pickTool.click();
  await expect(section).toHaveAttribute('data-pick', 'false');
  await expect(pickTool).toHaveAttribute('aria-pressed', 'false');

  // The device menu: «Overview» shows the page in several sizes side by side, a card takes its device in a case, and
  // an iPhone draws its status bar, which the menu's switch takes away.
  const device = page.locator('.hx-devopt').filter({ hasText: /^Overview/ });
  const deviceMenu = async () => {
    await page.locator('.hx-browser__dev').click();
    await expect(page.locator('.hx-devmenu')).toBeVisible();
  };
  await deviceMenu();
  await device.click();
  await expect(page.locator('.hx-ovw__card')).toHaveCount(4);
  await expect(page.locator('.hx-ovw__frame')).toHaveCount(4);
  await expect(page.frameLocator('.hx-ovw__frame').first().getByRole('heading')).toHaveText(
    'Coffee for everyone',
  );
  await page.locator('.hx-ovw__label').filter({ hasText: 'iPhone 15 Pro' }).click();
  await expect(page.locator('.hx-ovw')).toHaveCount(0);
  await expect(page.locator('.hx-vp[data-os="ios"] .hx-pbar')).toBeVisible();
  await expect(page.locator('.hx-vp[data-ui="ios26"] .hx-psafari')).toBeVisible();
  await deviceMenu();
  await expect(page.locator('.hx-devmenu .hx-dl-pop__label')).toHaveText(['Browser', 'Device']);
  await page.locator('.hx-devchrome').click();
  await expect(page.locator('.hx-pbar')).toHaveCount(0);
  await page.locator('.hx-devchrome').click();
  await expect(page.locator('.hx-pbar')).toBeVisible();
  await page
    .locator('.hx-devopt')
    .filter({ hasText: /^Responsive/ })
    .click();
  await expect(page.locator('.hx-pbar')).toHaveCount(0);
  await expect(page.locator('.hx-devmenu')).toHaveCount(0);

  // «More» offers picking too, with its key.
  await page.locator('.hx-browser__bar [aria-label="More"]').click();
  await expect(page.locator('.hx-more__item').filter({ hasText: 'Inspect element' })).toContainText('⇧⌘C');
  await page.keyboard.press('Escape');
  await expect(page.locator('.hx-more')).toHaveCount(0);

  // «Edits»: a click on the page's block opens its note; the bar sends the notes as one message.
  await page.locator('.hx-site-modes__item[aria-label="Edits"]').click();
  await expect(page.locator('.hx-site')).toHaveAttribute('data-pick', 'true', { timeout: 20_000 });
  await page.frameLocator('.hx-site__iframe').locator('#hero').click();
  const note = page.locator('.hx-note');
  await expect(note).toBeVisible();
  await expect(page.locator('.hx-site__pins .hx-lasso[data-on="true"]')).toBeVisible();
  // The card's settings of the block: the page wears them at once, and a reset gives the block back.
  const hero = page.frameLocator('.hx-site__iframe').locator('#hero');
  const look = () =>
    hero.evaluate((el) => {
      const style = getComputedStyle(el);
      return [style.borderTopLeftRadius, style.opacity, style.backgroundColor];
    });
  await expect(note.locator('.hx-ann__tune')).toHaveAttribute('aria-pressed', 'true');
  await expect(note.locator('.hx-ann__tag')).toHaveText('<h1>');
  await expect(note.locator('.hx-ann__reset')).toHaveAttribute('data-on', 'false');
  const before = await look();
  await note.locator('.hx-ann__step button[aria-label="More"]').click();
  await note.locator('.hx-ann__range input').fill('50');
  await note.locator('.hx-ann__val').click();
  await note.locator('.hx-ann__swb[aria-label="#2F80ED"]').click();
  await expect.poll(look).toEqual(['2px', '0.5', 'rgb(47, 128, 237)']);
  await expect(note.locator('.hx-ann__reset')).toHaveAttribute('data-on', 'true');
  await note.locator('.hx-ann__reset').click();
  await expect.poll(look).toEqual(before);
  await note.locator('.hx-ann__step button[aria-label="More"]').click();
  await note.locator('.hx-ann__range input').fill('50');
  await expect.poll(async () => (await look()).slice(0, 2)).toEqual(['2px', '0.5']);
  await note.locator('.hx-ann__input').fill('Make it bigger');
  await note.locator('.hx-ann__ok').click();
  await expect(note).toHaveCount(0);
  // It stays on the page while the note is there.
  await expect.poll(async () => (await look()).slice(0, 2)).toEqual(['2px', '0.5']);
  // The note stays on the page as a pin with its number; the bar counts it.
  await expect(page.locator('.hx-site__pins .hx-npin__dot')).toHaveText('1');
  await expect(page.locator('.hx-notesbar')).toContainText('1 edit');
  // In the island the notes are one strip over the field: its count, and a list with the words, where the note is
  // taken back to its card.
  await page.locator('.hx-site-modes__item[aria-label="Island"]').click();
  await expect(page.locator('.hx-anns__main')).toContainText('1');
  await page.locator('.hx-anns__main').click();
  await expect(page.locator('.hx-anns__list .hx-anns__text')).toHaveText('Make it bigger');
  await expect(page.locator('.hx-anns__list .hx-anns__diff')).toHaveText(['Radius → 2 px', 'Opacity → 50%']);
  await page.locator('.hx-anns__act[aria-label="Edit"]').click();
  await expect(page.locator('.hx-note .hx-ann__input')).toHaveValue('Make it bigger');
  await page.locator('.hx-note .hx-ann__cancel').click();
  await expect(page.locator('.hx-note')).toHaveCount(0);
  await page.locator('.hx-site-modes__item[aria-label="Edits"]').click();
  await page.locator('.hx-notesbar__send').click();
  // Sent: the agent changes the source, and the page gives the block back what it had.
  await expect.poll(look).toEqual(before);

  await page.locator('.hx-site-modes__item[aria-label="Chat"]').click();
  const sent = log.locator('.hx-site-msg[data-who="user"] .hx-site-chip').last();
  await expect(sent).toContainText('#1 h1 · «Coffee for everyone»', { timeout: 30_000 });
});

test('10b. the page is shown with its live settings and its DevTools: the elements, the console, the network and the server of the page', async () => {
  const section = page.locator('.hx-site');
  const frame = page.frameLocator('.hx-site__iframe');
  const overrides = () =>
    page
      .frameLocator('.hx-site__iframe')
      .locator('h1')
      .evaluate(
        () =>
          (window as unknown as { __dcRegistry: { Root: { propOverrides: Record<string, unknown> } } })
            .__dcRegistry.Root.propOverrides,
      );

  // «Live settings»: the board's own settings in a panel over the page; a change is seen on the page at once.
  const tweaksButton = page.locator('.hx-browser__bar [aria-label="Live settings"]');
  await expect(tweaksButton).toHaveAttribute('aria-disabled', 'false', { timeout: 30_000 });
  await tweaksButton.click();
  const tweaks = page.locator('.hx-tweaks');
  await expect(tweaks).toBeVisible();
  await expect(tweaks.locator('.hx-tweaks__group')).toHaveText(['Look', 'State']);
  await expect(tweaks.locator('.hx-tweak__opt')).toHaveText(['dark', 'light']);
  await tweaks.locator('.hx-tweak__opt', { hasText: 'light' }).click();
  await expect.poll(overrides).toMatchObject({ theme: 'light' });
  await expect(tweaks.locator('.hx-tweak__opt[aria-pressed="true"]')).toHaveText('light');
  await tweaks.locator('.hx-switch').click();
  await expect.poll(overrides).toMatchObject({ hot: false });
  await expect(tweaks.locator('.hx-switch')).toHaveAttribute('aria-checked', 'false');
  await tweaks.locator('.hx-tweak__slider').fill('5');
  await expect.poll(overrides).toMatchObject({ cards: 5 });
  await expect(tweaks.locator('.hx-tweak__val')).toHaveText('5');
  // The head moves the panel, and it keeps the place.
  const head = await tweaks.locator('.hx-tweaks__head').boundingBox();
  if (!head) throw new Error('no head');
  await page.mouse.move(head.x + 40, head.y + 12);
  await page.mouse.down();
  await page.mouse.move(head.x - 160, head.y + 80, { steps: 8 });
  await page.mouse.up();
  await expect(tweaks).toHaveAttribute('data-moved', 'true');
  await tweaks.locator('.hx-tweaks__head .hx-head-btn').click();
  await expect(tweaks).toHaveCount(0);

  // DevTools: the page's console, with its errors counted on the tab, and its network.
  const devButton = page.locator('.hx-browser__bar [aria-label="Developer tools"]');
  await devButton.click();
  const dev = page.locator('.hx-devtools');
  await expect(dev).toHaveAttribute('data-open', 'true');
  await expect(section).toHaveAttribute('data-dev', 'true');
  await expect(dev.locator('.hx-devtools__tab')).toContainText(['Elements', 'Console', 'Network', 'Server']);
  await frame.locator('h1').evaluate(() => {
    console.error('boom from the page');
    void fetch('/missing-for-devtools').catch(() => undefined);
  });
  const row = dev.locator('.hx-devrow[data-lv="error"]');
  await expect(row).toContainText('boom from the page', { timeout: 30_000 });
  await expect(
    dev.locator('.hx-devtools__tab', { hasText: 'Console' }).locator('.hx-devtools__badge'),
  ).toHaveText('1');
  await dev.locator('.hx-devtools__tab', { hasText: 'Network' }).click();
  const missing = dev.locator('.hx-devrow', { hasText: 'missing-for-devtools' });
  await expect(missing).toContainText('404', { timeout: 30_000 });
  await dev.locator('.hx-devfilter input').fill('missing-for');
  await expect(dev.locator('.hx-devrow[data-tab="network"]')).toHaveCount(1);
  await dev.locator('.hx-devtype', { hasText: 'Images' }).click();
  await expect(dev.locator('.hx-devtools__empty')).toHaveText('Nothing found');
  await dev.locator('.hx-devtype', { hasText: 'All' }).click();
  // The conditions are the slot's: the runtime takes them, the chip says so.
  await dev.locator('.hx-devchip', { hasText: 'No throttling' }).click();
  await page.locator('.hx-site-menu--up .hx-option', { hasText: 'Slow 4G' }).click();
  await expect(dev.locator('.hx-devchip').first()).toContainText('Slow 4G');
  await dev.locator('.hx-devchip').first().click();
  await page.locator('.hx-site-menu--up .hx-option', { hasText: 'No throttling' }).click();
  await expect(dev.locator('.hx-devchip').first()).toContainText('No throttling');
  // The elements: the page's tree from the board's own script, opened by node; a row is lit on the page while the pointer
  // is on it, and a taken node shows its box, place and styles.
  // E2E_SHOTS names a folder for pictures of the two tabs, to set beside the design's; the page is shown without the chat.
  const shots = process.env.E2E_SHOTS;
  const shoot = async (name: string) => {
    if (!shots) return;
    await page.locator('.hx-site-modes__item[aria-label="Island"]').click();
    await expect(section).toHaveAttribute('data-mode', 'island');
    // The site slides and the page is laid out anew; the picture is taken when both are still.
    await page.waitForTimeout(1500);
    await page.screenshot({ path: join(shots, `${name}.png`) });
    await page.locator('.hx-site-modes__item[aria-label="Chat"]').click();
    await expect(section).toHaveAttribute('data-mode', 'chat');
  };
  await dev.locator('.hx-devtools__tab', { hasText: 'Elements' }).click();
  const els = dev.locator('.hx-els');
  const tag = (name: string) =>
    els.locator('.hx-els__item', { has: page.locator('.hx-els__tag', { hasText: `<${name}` }) });
  await expect(els.locator('.hx-els__row').first()).toContainText('<html', { timeout: 30_000 });
  // The document and its body are open; the head is not.
  await expect(tag('body')).toBeVisible();
  await expect(tag('h1')).toContainText('#hero');
  await expect(tag('h1')).toContainText('Coffee for everyone');
  await expect(tag('title')).toHaveCount(0);
  await tag('head').locator('.hx-els__twist').click();
  await expect(tag('title')).toBeVisible();
  await tag('head').locator('.hx-els__twist').click();
  await expect(tag('title')).toHaveCount(0);
  // The page's own markup is the page's: the library's panel and the light are not in the tree.
  await expect(els.locator('.hx-els__row', { hasText: 'data-tk' })).toHaveCount(0);
  const mark = frame.locator('[data-tk-library-mark]');
  await expect(mark).toBeHidden();
  await tag('h1').hover();
  const heroBox = await frame.locator('#hero').boundingBox();
  await expect(mark).toBeVisible();
  await expect.poll(async () => (await mark.boundingBox())?.width).toBeCloseTo(heroBox?.width ?? -1, 0);
  await tag('h1').locator('.hx-els__row').click();
  const side = els.locator('.hx-els__side');
  await expect(side.locator('.hx-els__sel')).toHaveText('h1#hero');
  await expect(side.locator('.hx-els__label')).toHaveText('Coffee for everyone');
  await expect(side.locator('.hx-els__dim')).toHaveText(
    new RegExp(`^${Math.round(heroBox?.width ?? 0)} × ${Math.round(heroBox?.height ?? 0)}$`),
  );
  await expect(side.locator('.hx-els__kv dt')).toContainText([
    'Position',
    'Viewport',
    'File',
    'display',
    'font-size',
  ]);
  await expect(side.locator('.hx-els__kv dd', { hasText: 'main.dc.html' })).toBeVisible();
  await shoot('elements');
  await els.locator('.hx-els__tree').hover({ position: { x: 4, y: 4 } });
  // «Inspect» picks a node by a click on the page, which acts on nothing else; it and «Pick» do not go together.
  const inspect = dev.locator('.hx-devtools__head [aria-label="Inspect element"]');
  await inspect.click();
  await expect(inspect).toHaveAttribute('aria-pressed', 'true');
  await frame.locator('p').hover();
  await expect.poll(async () => (await mark.boundingBox())?.height).toBeGreaterThan(0);
  await frame.locator('p').click();
  await expect(inspect).toHaveAttribute('aria-pressed', 'false');
  await expect(side.locator('.hx-els__sel')).toHaveText('p');
  await expect(side.locator('.hx-els__label')).toHaveText('Menu');
  await expect(tag('p').locator('.hx-els__row')).toHaveAttribute('aria-selected', 'true');
  await inspect.click();
  await page.locator('.hx-browser__tool').click();
  await expect(inspect).toHaveAttribute('aria-pressed', 'false');
  await expect(section).toHaveAttribute('data-pick', 'true');
  await page.locator('.hx-browser__tool').click();
  await expect(section).toHaveAttribute('data-pick', 'false');
  await inspect.click();
  await page.keyboard.press('Escape');
  await expect(inspect).toHaveAttribute('aria-pressed', 'false');
  // The node goes to the field.
  await side.locator('.hx-els__send').click();
  await expect(page.locator('.hx-island__input')).toHaveValue(/^Elements: p «Menu» · \d+ × \d+$/);
  // Another tab takes the light off the page.
  await dev.locator('.hx-devtools__tab', { hasText: 'Console' }).click();
  await expect(mark).toBeHidden();

  // The server: the runtime serves the board itself, so its record is the board's requests to it, what the page logged
  // to the library and the last check; a kind of record it does not keep is said not to exist.
  await frame.locator('h1').evaluate(() => {
    setTimeout(() => {
      throw new Error('boom for the server tab');
    });
  });
  await dev.locator('.hx-devtools__tab', { hasText: 'Server' }).click();
  const server = dev.locator('.hx-devrow[data-tab="server"]');
  await expect(server.filter({ hasText: /GET \/a\/[^ ]+\/p\/main\.dc\.html → 200/ }).first()).toBeVisible({
    timeout: 30_000,
  });
  const boom = server.filter({ hasText: 'boom for the server tab' });
  await expect(boom).toHaveAttribute('data-lv', 'error', { timeout: 30_000 });
  // The tab's badge counts the error rows (the failed request for the missing file is one of them), and «Errors» keeps them.
  const serverBadge = dev.locator('.hx-devtools__tab', { hasText: 'Server' }).locator('.hx-devtools__badge');
  await expect(serverBadge).toHaveText(/^[1-9]\d*$/);
  await expect(dev.locator('.hx-devtools__foot')).toContainText('no build or import log');
  const errors = Number(await serverBadge.textContent());
  await dev.locator('.hx-devtype', { hasText: 'Errors' }).click();
  await expect(server).toHaveCount(errors);
  await expect(server.filter({ hasText: 'missing-for-devtools → 404' })).toHaveCount(1);
  if (shots) await frame.getByRole('button', { name: 'Hide' }).click();
  await shoot('server');
  await boom.hover();
  await boom.locator('.hx-devrow__send').click();
  await expect(page.locator('.hx-island__input')).toHaveValue(/Server: Error: boom for the server tab/);
  await dev.locator('.hx-devtype', { hasText: 'All' }).click();
  await dev.locator('.hx-devtools__head [aria-label="Clear"]').click();
  await expect(boom).toHaveCount(0);
  await expect(serverBadge).toHaveCount(0);
  await dev.locator('.hx-devtools__tab', { hasText: 'Network' }).click();

  // A line goes to the field.
  await missing.hover();
  await missing.locator('.hx-devrow__send').click();
  await expect(page.locator('.hx-island__input')).toHaveValue(/Network: GET .*missing-for-devtools → 404/);
  // «Clear» hides what was there; ⌥⌘I shuts the panel.
  // The page may ask for more meanwhile, so what is checked is that the cleared line is gone.
  await dev.locator('.hx-devtools__head [aria-label="Clear"]').click();
  await expect(missing).toHaveCount(0);
  await page.keyboard.press('Control+Alt+KeyI');
  await expect(dev).toHaveAttribute('data-open', 'false');
});

test('11. «Apps» is the first of «More»: the tracker’s epics and stale tasks from the machine, GitLab (the stand-in of 7f) holds nothing for the person, and picked rows go to a new chat as mentions', async () => {
  const kids = page.locator('.hx-nav__kid');
  // «More» keeps its state between visits: it is opened only when it is closed.
  const openApps = async () => {
    if (!(await kids.first().isVisible()))
      await page.locator('.hx-nav__item').filter({ hasText: 'More' }).click();
    await kids.first().click();
  };
  await openApps();
  await expect(kids).toHaveText(['Apps', 'Efficiency', 'Statistics']);
  await expect(page).toHaveURL(/\/apps$/);
  const apps = page.locator('.hx-feedapp');
  await expect(apps).toBeVisible();
  // The screen has its own strip: no chat header over it.
  await expect(page.locator('.hx-header')).toHaveCount(0);
  await expect(apps.getByRole('heading', { name: 'Apps' })).toBeVisible();
  await expect(apps.getByRole('tab')).toHaveText(['Tracker', 'GitLab']);
  await expect(apps.getByRole('tab', { name: 'Tracker' })).toHaveAttribute('aria-selected', 'true');

  // What the tracker holds, by section, with the words of each row.
  const rows = apps.getByRole('checkbox');
  await expect(rows).toHaveCount(3);
  await expect(apps.locator('.hx-feedapp__sec-title')).toHaveText(['My epics', 'Standing still']);
  await expect(apps.locator('.hx-feedapp__sec-hint')).toHaveText('More than 5 days without a move');
  await expect(rows.nth(0)).toContainText('ABC-2628');
  await expect(rows.nth(0)).toContainText('Assistant releases');
  await expect(rows.nth(0)).toContainText('2 of 4 tasks done');
  await expect(rows.nth(0).locator('.hx-feedapp__tag')).toHaveText('In progress');
  await expect(rows.nth(1)).toContainText('The epic has no tasks yet');
  await expect(rows.nth(2)).toContainText('Peter · 9 days without a move');
  await expect(rows.nth(2).locator('.hx-feedapp__tag')).toHaveAttribute('data-tone', 'warn');

  // Picking: the bar names one row by its id and several by their number; clearing lets it go.
  await rows.nth(0).click();
  await expect(rows.nth(0)).toHaveAttribute('aria-checked', 'true');
  const bar = apps.locator('.hx-feedapp__bar');
  await expect(bar).toContainText('ABC-2628');
  await rows.nth(2).press(' ');
  await expect(bar).toContainText('2 tasks');
  await bar.getByRole('button', { name: 'Clear selection' }).click();
  await expect(rows.nth(0)).toHaveAttribute('aria-checked', 'false');
  await expect(bar).toHaveCount(0);

  // GitLab is the stand-in of 7f, which knows the person and holds no merge request for them: it says so, in both
  // sections, instead of showing nothing.
  await apps.getByRole('tab', { name: 'GitLab' }).click();
  await expect(apps.getByText('Nobody waits for your review')).toBeVisible();
  await expect(apps.getByText('Nothing stands still')).toBeVisible();
  await expect(rows).toHaveCount(0);
  await apps.getByRole('tab', { name: 'Tracker' }).click();

  // The row's own button, and then the bar's: the chips come to the start page's field and lead the message.
  await rows.nth(1).hover();
  await rows.nth(1).getByRole('button', { name: 'Discuss in a new chat' }).click();
  await expect(page).toHaveURL(/\/$/);
  const chips = page.locator('.hx-composer .hx-attach[data-kind="quote"]');
  await expect(chips).toHaveCount(1);
  await expect(chips).toContainText('ABC-2486 · Parser hub');
  await expect(page.locator('.hx-composer__input')).toBeFocused();

  await openApps();
  await rows.nth(0).click();
  await rows.nth(2).click();
  await apps.locator('.hx-feedapp__bar').getByRole('button', { name: 'Discuss in a new chat' }).click();
  await expect(chips).toHaveCount(2);
  await expect(chips.nth(0)).toContainText('ABC-2628 · Assistant releases');
  await expect(chips.nth(1)).toContainText('ABC-3039 · Skeletons in the order list');
  await chips.nth(1).locator('.hx-attach__x').click();
  await expect(chips).toHaveCount(1);
  await page.locator('.hx-composer__input').fill('which first?');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(chips).toHaveCount(0);
  await expect(page).toHaveURL(/\/c\//);
  await expect(
    feed()
      .getByText(/ABC-2628 · Assistant releases — http:\/\/127\.0\.0\.1:\d+\/issue\/ABC-2628/)
      .first(),
  ).toBeVisible({
    timeout: 60_000,
  });
  await expect(feed().getByText('which first?').first()).toBeVisible();
});

test('12. Settings opens from the profile menu, the Apps screen and the machine card; its sections write to the person and to the agents’ files on the machine', async () => {
  const set = page.locator('.hx-set');
  const nav = set.locator('.hx-set__nav-item');
  const row = (label: string) =>
    set.locator('.hx-set__row').filter({ has: page.getByText(label, { exact: true }) });
  const claudeFile = join(rt.home, '.claude', 'settings.json');
  const written = () => JSON.parse(readFileSync(claudeFile, 'utf8')) as Record<string, unknown>;

  // A skill the agent finds on the machine, to be switched off from the page.
  mkdirSync(join(rt.home, '.claude', 'skills', 'e2e-skill'), { recursive: true });
  writeFileSync(
    join(rt.home, '.claude', 'skills', 'e2e-skill', 'SKILL.md'),
    '---\nname: e2e-skill\ndescription: Says hello in the test\n---\nHello.\n',
  );

  // 1. From the profile menu at the bottom of the sidebar.
  await page.goto(`${rt.url}/`);
  // The page lives under the installation's base (the runtime does not serve a deep link outside it).
  const base = new URL(page.url()).pathname.replace(/\/$/, '');
  const at = (path: string) => `${rt.url}${base}${path}`;
  await page.locator('.hx-user__open').click();
  await page.getByRole('menuitem', { name: 'Settings' }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(set).toBeVisible();
  await expect(page.locator('.hx-header')).toHaveCount(0);
  await expect(nav).toHaveText([
    'General',
    'Models and keys',
    'Claude Code',
    'Codex',
    'Skills and MCP',
    'Machines',
    'Apps',
  ]);
  await expect(set.getByRole('heading', { name: 'General' })).toBeVisible();

  // General: the person's own settings go to the runtime and stay across a reload.
  const enter = row('Enter sends').getByRole('switch');
  await expect(enter).toHaveAttribute('aria-checked', 'true');
  await enter.click();
  await expect(enter).toHaveAttribute('aria-checked', 'false');
  await set.getByRole('radio', { name: 'Larger' }).click();
  await expect(page.locator('.hx-root')).toHaveAttribute('data-text', 'l');
  await page.reload();
  await expect(row('Enter sends').getByRole('switch')).toHaveAttribute('aria-checked', 'false');
  await expect(page.locator('.hx-root')).toHaveAttribute('data-text', 'l');
  // Language and the editor are not the person's to change here: they say so instead of pretending.
  await expect(row('Language').getByRole('button').first()).toBeDisabled();
  await expect(row('Open files in')).toContainText('Not available yet');

  // With «Enter sends» off, Enter in the field makes a new line instead of sending.
  await page.goto(`${rt.url}/`);
  const field = page.locator('.hx-composer__input');
  await field.fill('first line');
  await field.press('Enter');
  await expect(page).not.toHaveURL(/\/c\//);
  await expect(field).toHaveValue(/first line\n/);
  await field.fill('');

  // Back on: the settings are the person's, not the page's.
  await page.goto(at('/settings'));
  await row('Enter sends').getByRole('switch').click();
  await expect(row('Enter sends').getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  await set.getByRole('radio', { name: 'Normal' }).click();
  await expect(page.locator('.hx-root')).toHaveAttribute('data-text', 'm');

  // 2. Claude Code: the file on the machine changes with the page, and nothing else in it.
  await nav.filter({ hasText: 'Claude Code' }).click();
  await expect(page).toHaveURL(/\/settings\/claude$/);
  await expect(set.getByRole('heading', { name: 'Claude Code' })).toBeVisible();
  await expect(set.locator('.hx-set__lead')).toContainText('.claude/settings.json');
  const thinking = row('Always think before answering').getByRole('switch');
  await expect(thinking).toHaveAttribute('aria-checked', 'false');
  await thinking.click();
  await expect(thinking).toHaveAttribute('aria-checked', 'true');
  expect(written().alwaysThinkingEnabled).toBe(true);
  await row('Thinking depth').getByRole('radio', { name: 'High', exact: true }).click();
  await expect(row('Thinking depth').getByRole('radio', { name: 'High', exact: true })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  expect(written().effortLevel).toBe('high');
  await row('Default mode').getByRole('button').click();
  await set.getByRole('option', { name: /Plan first/ }).click();
  await expect(row('Default mode').getByRole('button')).toContainText('Plan first');
  expect(written().permissions).toMatchObject({ defaultMode: 'plan' });
  // Switched off again, the key goes with the switch.
  await thinking.click();
  await expect(thinking).toHaveAttribute('aria-checked', 'false');
  expect(written().alwaysThinkingEnabled).toBeFalsy();

  // Codex has its own page and its own file.
  await nav.filter({ hasText: 'Codex' }).click();
  await expect(page).toHaveURL(/\/settings\/codex$/);
  await expect(set.locator('.hx-set__lead')).toContainText('.codex/config.toml');
  await row('Fast mode').getByRole('switch').click();
  await expect(row('Fast mode').getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  expect(readFileSync(join(rt.home, '.codex', 'config.toml'), 'utf8')).toMatch(/^service_tier\s*=\s*"fast"/m);

  // 3. Skills: what the machine has; off is a ban the agent obeys.
  await nav.filter({ hasText: 'Skills and MCP' }).click();
  await expect(page).toHaveURL(/\/settings\/skills$/);
  const skill = set.locator('.hx-ext').filter({ hasText: 'e2e-skill' });
  await expect(skill).toContainText('Says hello in the test');
  await expect(skill.getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  await skill.getByRole('switch').click();
  await expect(skill.getByRole('switch')).toHaveAttribute('aria-checked', 'false');
  expect(JSON.stringify(written().permissions)).toContain('Skill(e2e-skill)');
  await set.getByRole('radio', { name: 'Off', exact: true }).click();
  await expect(skill).toBeVisible();
  await set.getByRole('radio', { name: 'On', exact: true }).click();
  await expect(skill).toHaveCount(0);
  await set.getByRole('radio', { name: 'All', exact: true }).click();
  await skill.getByRole('switch').click();
  await expect(skill.getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  expect(JSON.stringify(written().permissions ?? {})).not.toContain('Skill(e2e-skill)');

  // Models and keys: the agents' sign-in is theirs, so the page says how, and never asks for a key.
  await nav.filter({ hasText: 'Models and keys' }).click();
  await expect(set.locator('.hx-acct')).toHaveCount(2);
  await expect(set.getByText('claude auth login')).toBeVisible();

  // 4. Machines: the list is the cluster’s, and this machine is the main one.
  await nav.filter({ hasText: 'Machines' }).click();
  await expect(page).toHaveURL(/\/settings\/machines$/);
  await expect(set.getByText('Main', { exact: true })).toBeVisible();

  // An unknown section is not a page: it is the home screen.
  await page.goto(at('/settings/nowhere'));
  await expect(page.locator('.hx-hello')).toBeVisible();

  // 5. From the Apps screen: the button next to its title.
  await page.goto(at('/apps'));
  await page.getByRole('button', { name: 'Connected apps' }).click();
  await expect(page).toHaveURL(/\/settings\/apps$/);
  await expect(set.getByRole('heading', { name: 'Apps' })).toBeVisible();
  await expect(set.locator('.hx-app-card')).toHaveCount(2);
  await expect(set.locator('.hx-app-card').first()).toContainText('Connected');
  await set.locator('.hx-app-card').first().getByRole('button', { name: 'Open' }).click();
  await expect(page).toHaveURL(/\/apps$/);

  // 6. From the machine card: «Set up machines».
  await page.locator('.hx-machine__chip').click();
  await page.getByRole('button', { name: 'Set up machines' }).click();
  await expect(page).toHaveURL(/\/settings\/machines$/);
  await expect(nav.filter({ hasText: 'Machines' })).toHaveAttribute('aria-current', 'true');
});

test('13. segmented controls fit their choices except the mobile plan gate', async () => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  if ((await setup.count()) > 0) {
    await setup.locator('input[name=password]').fill(PASSWORD);
    await setup.locator('input[name=confirm]').fill(PASSWORD);
    await setup.locator('button[type=submit]').click();
    await expect(page.locator('.hx-app')).toBeVisible();
  }
  const base = new URL(page.url()).pathname;
  await page.goto(`${rt.url}${base}?dev=chat&scene=handoff-sheet&chrome=0&lang=en`);

  const handoff = page.locator('.hx-hsheet .hx-segmented');
  await expect(handoff).toBeVisible();
  const handoffBox = await handoff.boundingBox();
  const handoffButtonWidths = await handoff
    .locator('button')
    .evaluateAll((buttons) =>
      buttons.reduce((width, button) => width + button.getBoundingClientRect().width, 0),
    );
  expect(handoffBox).not.toBeNull();
  expect(Math.abs(handoffBox!.width - handoffButtonWidths - 4)).toBeLessThanOrEqual(1);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${rt.url}${base}?dev=chat&scene=permission&chrome=0&lang=en`);

  const plan = page.locator('.hx-gate[data-gate="plan"]');
  const actions = plan.locator('.hx-gate__acts');
  const planModes = actions.locator('.hx-segmented');
  await expect(planModes).toBeVisible();
  const actionsBox = await actions.boundingBox();
  const planModesBox = await planModes.boundingBox();
  expect(actionsBox).not.toBeNull();
  expect(planModesBox).not.toBeNull();
  expect(Math.abs(planModesBox!.width - actionsBox!.width)).toBeLessThanOrEqual(1);
});
