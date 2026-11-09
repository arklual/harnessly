/// <reference lib="dom" />
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { type Browser, type BrowserContext, type Cookie, expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

// HRN-901 and HRN-902: the page does not wait for a machine that is starting or switched off.
//
// 1. A runtime with RESTART_TABS agent tabs (each with a transcript of RESTART_MB) is stopped with its tmux server, as a
//    reboot does, the disk's cache of its files is let go, and it is started again: the first screen and the chat list
//    are there within 2 s of the start, while the tabs are still being read.
// 2. Two machines, the person's home one switched off while the cluster has not noticed yet: the list is drawn from the
//    other machine and the cluster's copy within 2 s on a browser that keeps nothing.
//
// E2E_DISK_ROOT puts the runtime's home on a disk (the cache of a tmpfs cannot be let go); RESTART_TABS and RESTART_MB
// change the size. The times are printed (restart-load: …) for the record.

const PASSWORD = 'correct horse';
const TABS = Number(process.env.RESTART_TABS ?? 120);
const MB = Number(process.env.RESTART_MB ?? 3);
const LIMIT_MS = 2000;
// How long a measured load is waited for (a runtime before the fix took minutes; its time is printed all the same).
const WAIT_MS = Number(process.env.RESTART_WAIT_MS ?? 30_000);
const title = (i: number) => `Restart chat ${String(i).padStart(3, '0')}`;

/** RESTART_TABS Legacy agent tabs in one workspace, each with a Claude transcript of RESTART_MB that names no title. */
function seed(root: string): string[] {
  const home = join(root, 'home');
  const data = join(home, '.harnessly');
  const project = join(home, 'project');
  const transcripts = join(home, '.claude', 'projects', project.replace(/[^A-Za-z0-9]/g, '-'));
  mkdirSync(join(data, 'workspaces', 'ws-load'), { recursive: true });
  mkdirSync(transcripts, { recursive: true });
  mkdirSync(project, { recursive: true });
  const filler = JSON.stringify({
    type: 'assistant',
    timestamp: '2026-11-01T10:00:00.000Z',
    message: { content: [{ type: 'text', text: 'x'.repeat(4000) }], stop_reason: 'end_turn' },
  });
  const lines = Math.max(1, Math.floor((MB * 1024 * 1024) / (filler.length + 1)));
  const files: string[] = [];
  const tabs = [];
  for (let i = 1; i <= TABS; i++) {
    const id = `tab-load${String(i).padStart(4, '0')}`;
    const session = `11111111-2222-4333-8444-${String(i).padStart(12, '0')}`;
    const path = join(transcripts, `${session}.jsonl`);
    const at = new Date(Date.UTC(2026, 10, 1, 9, 0, i)).toISOString();
    const head = JSON.stringify({ type: 'user', timestamp: at, message: { content: title(i) } });
    writeFileSync(path, `${head}\n${`${filler}\n`.repeat(lines)}`);
    files.push(path);
    tabs.push({
      id,
      sessionName: `hs-ws-load-pane-p1-${id}`,
      name: '',
      order: i,
      cwd: project,
      panelType: 'claude-code',
      cliState: 'idle',
      lastUserMessage: title(i),
      agentState: { providerId: 'claude', sessionId: session, jsonlPath: path, summary: title(i) },
    });
  }
  writeFileSync(
    join(data, 'workspaces.json'),
    JSON.stringify({
      workspaces: [{ id: 'ws-load', name: 'Load', directories: [project] }],
      sidebarCollapsed: false,
      sidebarWidth: 240,
    }),
  );
  writeFileSync(
    join(data, 'workspaces', 'ws-load', 'layout.json'),
    JSON.stringify({
      root: { type: 'pane', id: 'pane-p1', tabs, activeTabId: tabs[0]!.id },
      activePaneId: 'pane-p1',
    }),
  );
  return files;
}

/** Lets the disk's cache of every file under dir go (GNU dd's nocache), as after a reboot. */
function dropCache(dir: string): void {
  const walk = (d: string): string[] =>
    readdirSync(d, { withFileTypes: true }).flatMap((e) =>
      e.isDirectory() ? walk(join(d, e.name)) : e.isFile() ? [join(d, e.name)] : [],
    );
  for (const f of walk(dir)) {
    try {
      execFileSync('dd', [`if=${f}`, 'iflag=nocache', 'count=0', 'status=none']);
    } catch {
      // a file that went meanwhile
    }
  }
}

async function signUp(page: Page, url: string) {
  await page.goto(`${url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
}

/** Setting the password writes a new pair's secret that the running process does not take: the next start asks again. */
async function signIn(page: Page, url: string) {
  await expect(async () => {
    await page.goto(`${url}/`, { timeout: 5000 });
  }).toPass({ timeout: 30_000 });
  const login = page.getByTestId('signin-form');
  await login.locator('input[name=password]').fill(PASSWORD);
  await login.locator('button[type=submit]').click();
  await expect(page.locator('.hx-composer__input')).toBeVisible();
}

/** Loads the page until it answers (the runtime is starting), then waits for the sidebar to show a chat of `pattern`;
 * returns when the page had loaded. */
async function listShown(page: Page, url: string, pattern: RegExp, limitMs: number): Promise<number> {
  for (const until = Date.now() + limitMs; ;) {
    try {
      await page.goto(`${url}/`, { timeout: 5000 });
      break;
    } catch (err) {
      if (Date.now() > until) throw err;
      await page.waitForTimeout(50);
    }
  }
  const loaded = Date.now();
  await expect(page.locator('.hx-sidebar').getByText(pattern).first()).toBeVisible({ timeout: limitMs });
  return loaded;
}

/** A browser that keeps nothing but the session. */
const freshContext = (browser: Browser) =>
  browser.newContext({ storageState: { cookies: session, origins: [] } });

let rt: Runtime;
let files: string[] = [];
let session: Cookie[] = [];

test.beforeAll(async () => {
  test.setTimeout(10 * 60_000);
  const root = mkdtempSync(join(process.env.E2E_DISK_ROOT ?? tmpdir(), 'hs-web-restart-'));
  files = seed(root);
  rt = await startRuntime({ root, env: { RUNTIME_WEB_FEATURES: 'chats' } });
});

test.afterAll(async () => {
  await rt?.stop();
});

/** The person signed in on a runtime that took the Legacy tabs over. A failed test starts the next one on a runtime of
 *  its own (a new worker), so each test asks for it. */
async function prepare(browser: Browser): Promise<{ context: BrowserContext; page: Page }> {
  if (session.length) {
    const context = await freshContext(browser);
    const page = await context.newPage();
    await listShown(page, rt.url, /Restart chat \d+/, WAIT_MS);
    return { context, page };
  }
  const context = await browser.newContext();
  const page = await context.newPage();
  await signUp(page, rt.url);
  // The first start takes the Legacy tabs over; the list has them all once it is done.
  await expect(page.locator('.hx-sidebar').getByText(title(TABS))).toBeVisible({ timeout: 5 * 60_000 });
  await expect(page.locator('.hx-sidebar').getByText(title(TABS - 1))).toBeVisible();
  // A first restart takes the secret written at sign-up; the measured ones keep this session.
  await rt.restart();
  await signIn(page, rt.url);
  await page.reload();
  await expect(page.locator('.hx-sidebar').getByText(title(TABS))).toBeVisible({ timeout: 5 * 60_000 });
  session = (await context.storageState()).cookies;
  return { context, page };
}

test('a runtime with 100+ tabs restarts: the first screen and the chat list within 2 s', async ({
  browser,
}) => {
  test.setTimeout(10 * 60_000);
  const { context, page } = await prepare(browser);
  // The page keeps the list it had (IndexedDB); the runtime is stopped with its tmux server, as a reboot does.
  const spawned = await rt.restart(() => dropCache(rt.home));
  const loaded = await listShown(page, rt.url, /Restart chat \d+/, WAIT_MS);
  const listMs = Date.now() - spawned;
  console.log(
    `restart-load: ${TABS} tabs × ${MB} MB, list shown ${listMs} ms after the start (the page loaded at ${loaded - spawned} ms)`,
  );
  expect(listMs).toBeLessThan(LIMIT_MS);
  await context.close();
  expect(files.length).toBe(TABS);
});

test('one of two machines is off and the cluster has not noticed: the first screen and the list within 2 s', async ({
  browser,
}) => {
  test.setTimeout(10 * 60_000);
  await (await prepare(browser)).context.close();
  const seedContext = await freshContext(browser);
  const seedPage = await seedContext.newPage();
  // The live machine's own records stand in for the other machine's in the cluster's copy.
  let records: Array<Record<string, unknown>> = [];
  await seedPage.routeWebSocket(/\/socket/, (ws) => {
    const server = ws.connectToServer();
    server.onMessage((m) => {
      try {
        const doc = JSON.parse(String(m)) as {
          ch?: string;
          data?: { conversations?: Array<Record<string, unknown>> };
        };
        if (doc.ch === 'convs' && doc.data?.conversations?.length) records = doc.data.conversations;
      } catch {
        // not a JSON frame
      }
      ws.send(m);
    });
    ws.onMessage((m) => server.send(m));
  });
  await listShown(seedPage, rt.url, /Restart chat \d+/, WAIT_MS);
  await expect.poll(() => records.length, { timeout: WAIT_MS }).toBeGreaterThan(0);
  const offRecords = records
    .slice(0, 5)
    .map((c, i) => ({ ...c, id: `tab-off${i}`, title: `Off machine chat ${i}`, machineId: 'machine-off' }));

  const context = await freshContext(browser);
  await seedContext.close();
  const page = await context.newPage();
  // The person's machines: the home one first, switched off (nothing behind it answers), then this runtime.
  await page.route(/\/api\/v2\/machines(\?|$)/, async (route) => {
    const res = await route.fetch();
    const own = (await res.json()) as Array<Record<string, unknown>>;
    await route.fulfill({
      json: [
        { ...own[0], id: 'machine-off', name: 'off', host: 'off.example.com', reach: 'online' },
        { ...own[0], id: 'machine-live', name: 'live' },
      ],
    });
  });
  await page.route(/[?&]machine=machine-off/, () => {
    // a machine that took the connection and went silent: no answer at all
  });
  await page.routeWebSocket(/machine=machine-off/, () => {
    // opened, and never a welcome
  });
  // The cluster's copy: on, both machines still online by its word (the 45 s before it notices).
  await page.route(/\/history\/conversations(\?|$)/, (route) =>
    route.fulfill({
      json: {
        enabled: true,
        machines: [
          { id: 'machine-off', name: 'off', state: 'online' },
          { id: 'machine-live', name: 'live', state: 'online' },
        ],
        conversations: offRecords,
        moved: [],
      },
    }),
  );
  await page.route(/\/history\/sidebar(\?|$)/, (route) =>
    route.fulfill({ json: { enabled: false, rev: 0, doc: null } }),
  );

  const started = Date.now();
  await page.goto(`${rt.url}/`);
  await expect(
    page
      .locator('.hx-sidebar')
      .getByText(/Restart chat \d+/)
      .first(),
  ).toBeVisible({ timeout: WAIT_MS });
  const liveMs = Date.now() - started;
  await expect(page.locator('.hx-sidebar').getByText('Off machine chat 0')).toBeVisible({ timeout: WAIT_MS });
  const offMs = Date.now() - started;
  console.log(`restart-load: home machine off, live chats ${liveMs} ms, the off machine's chats ${offMs} ms`);
  expect(liveMs).toBeLessThan(LIMIT_MS);
  expect(offMs).toBeLessThan(LIMIT_MS);
  await context.close();
});
