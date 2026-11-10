// Sinopia as a builtin app (HRN-808), on a live runtime and page:
//   1–2. the editor's tools of the page (WebMCP form, through its SDK) and its screen reach the chat's agent: the page
//        reports them to the runtime, and an agent's call (show_board) turns the deck and comes back with the answer;
//   3.   the Cashback card of a site (HRN-524): a click on an element with data-hl-field opens the app's inspector on
//        that field, and the value written there goes back into the board.
// The agent's side of page_call (the runtime holding the call until the page takes it) is covered in Go
// (internal/apps/pagetools_test.go); here the call is handed to the page as the runtime hands it.
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Frame, type Page, type Request, test } from '@playwright/test';

import { agreeApp, makeSlides, SLIDES } from './deck-rig';
import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
const SINOPIA = 'harnessly.sinopia';

test.describe.configure({ mode: 'serial' });
test.setTimeout(120_000);

let rt: Runtime;
let page: Page;
let token = '';

interface IReport {
  conversation: string;
  apps: { app: string; tools: { name: string }[]; context?: Record<string, unknown> }[];
}

const pathOf = (req: Request) => new URL(req.url()).pathname;

const sinopiaOf = (req: Request) => {
  const body = req.postDataJSON() as IReport;
  return body.apps.find((a) => a.app === SINOPIA) ?? null;
};

/** The next report of the page in which Sinopia's part passes the check. */
const nextReport = (p: Page, ok: (s: NonNullable<ReturnType<typeof sinopiaOf>>) => boolean) =>
  p.waitForRequest(
    (req) => {
      if (!pathOf(req).endsWith('/apps/page-tools') || req.method() !== 'POST') return false;
      const s = sinopiaOf(req);
      return s !== null && ok(s);
    },
    { timeout: 30_000 },
  );

const deckFrame = async (p: Page): Promise<Frame> => {
  const handle = await p.locator('iframe.hx-deck__frame').last().elementHandle({ timeout: 30_000 });
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error('no frame in the slide');
  return frame;
};

const create = async (p: Page, kind: 'slides' | 'site', title: string) => {
  const made = await p.evaluate(
    async ({ kind, title }) => {
      const res = await fetch('/api/v2/designly/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title,
          brief: title,
          kind,
          provider: 'claude',
          model: null,
          effort: null,
          designSystem: null,
        }),
      });
      return (await res.json()) as { id?: string; conv?: string };
    },
    { kind, title },
  );
  expect(made.conv).toMatch(/^tab-/);
  return { id: made.id ?? '', conv: made.conv ?? '' };
};

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-spa-'));
  rt = await startRuntime({
    root,
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
      ...(process.env.RUNTIME_DESIGN_RELEASES_DIR
        ? { RUNTIME_DESIGN_RELEASES_DIR: process.env.RUNTIME_DESIGN_RELEASES_DIR }
        : {}),
    },
  });
  let cli = process.env.HARNESSLY_BIN;
  if (!cli) {
    cli = join(root, 'harnessly');
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  }
  execFileSync(cli, ['app', 'install', join(repo, 'docs', 'apps', 'cashback')], {
    env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
    stdio: 'inherit',
  });
  token = readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();
  page = await browser.newPage();
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  await agreeApp(page, 'harnessly.cashback');
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

let deck = { id: '', conv: '' };

test('1. the open deck offers its tools to the chat’s agent and publishes the slide on screen', async () => {
  deck = await create(page, 'slides', 'Page tools deck');
  await makeSlides(rt, deck.id, SLIDES);
  const first = nextReport(
    page,
    (s) =>
      ['show_board', 'reload_board'].every((n) => s.tools.some((t) => t.name === n)) &&
      (s.context?.board as { number?: number } | null)?.number === 1,
  );
  await page.goto(`${rt.url}/agents/designly/${deck.id}?chat=${deck.conv}`);
  await expect((await deckFrame(page)).locator('h1')).toHaveText(SLIDES[0].head, { timeout: 30_000 });
  const report = await first;
  expect(report.postDataJSON().conversation).toBe(deck.conv);
  // The runtime keeps it: Sinopia is the host's own, on in no chat's list of apps.
  const res = await report.response();
  expect(await res?.json()).toMatchObject({ kept: 1 });
  const s = sinopiaOf(report)!;
  expect(s.context).toMatchObject({
    document: { id: deck.id, kind: 'slides' },
    boards: { count: SLIDES.length },
  });

  // A block selected on the slide is in what the agent reads.
  const selected = nextReport(
    page,
    (r) => Array.isArray(r.context?.selection) && r.context.selection.length > 0,
  );
  await (await deckFrame(page)).locator('p').click();
  const sel = sinopiaOf(await selected)!;
  expect((sel.context?.selection as { text?: string }[])[0].text).toContain(SLIDES[0].body);
});

test('2. the agent’s show_board turns the deck, and its answer goes back', async () => {
  let handed = false;
  await page.route('**/apps/page-calls?**', async (route) => {
    if (handed) return route.continue();
    handed = true;
    await route.fulfill({
      json: { calls: [{ id: 'call-1', app: SINOPIA, tool: 'show_board', arguments: { board: 3 } }] },
    });
  });
  const answered = page.waitForRequest((r) => pathOf(r).endsWith('/apps/page-calls/answer'), {
    timeout: 30_000,
  });
  const answer = (await answered).postDataJSON() as {
    id: string;
    result?: { shown?: string; number?: number };
  };
  expect(answer.id).toBe('call-1');
  expect(answer.result?.number).toBe(3);
  await expect((await deckFrame(page)).locator('h1')).toHaveText(SLIDES[2].head, { timeout: 30_000 });
  await nextReport(page, (r) => (r.context?.board as { number?: number } | null)?.number === 3);
  await page.unroute('**/apps/page-calls?**');
});

test('3. the Cashback card: a bound element opens the inspector on its field, and its value goes back to the board', async () => {
  const site = await create(page, 'site', 'Cashback site');
  const file = 'cashback.dc.html';
  const res = await fetch(`${rt.url}/api/cli/design/pages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'x-cli-token': token },
    body: JSON.stringify({ action: 'create', project: site.id, title: 'Кешбэк', file }),
  });
  expect(res.ok, await res.clone().text()).toBe(true);
  const card = readFileSync(
    join(repo, 'docs', 'apps', 'cashback', 'templates', 'cashback-card', 'board.dc.html'),
    'utf8',
  );
  writeFileSync(join(rt.home, 'project', 'design-artifacts', site.id, 'project', file), card);

  await page.goto(`${rt.url}/agents/designly/${site.id}?chat=${site.conv}`);
  const handle = await page.locator('iframe.hx-site__iframe').first().elementHandle({ timeout: 30_000 });
  const board = await handle?.contentFrame();
  if (!board) throw new Error('no board frame');
  const title = board.locator('[data-hl-field="title"]');
  await expect(title).toHaveText('Кафе и рестораны', { timeout: 30_000 });
  await title.click();
  // The inspector of the app opens on the clicked field.
  const inspector = page.frameLocator('.hx-sinopia iframe').last();
  await expect(inspector.locator('#field')).toHaveText('Название категории', { timeout: 30_000 });
  await expect(inspector.locator('#value')).toHaveValue('Кафе и рестораны');
  await inspector.locator('#value').fill('Супермаркеты');
  await inspector.locator('#save').click();
  await expect(inspector.locator('#note')).toHaveText('Записано на доску.', { timeout: 15_000 });
  // The value is written into the board's element, in its file.
  const written = join(rt.home, 'project', 'design-artifacts', site.id, 'project', file);
  await expect
    .poll(() => /data-hl-field="title"[^>]*>Супермаркеты</.test(readFileSync(written, 'utf8')), {
      timeout: 15_000,
    })
    .toBe(true);
  // And the board shows it: as it is open now, or once it is loaded again.
  const shown = async () => {
    const h = await page.locator('iframe.hx-site__iframe').first().elementHandle();
    const f = await h?.contentFrame();
    return f
      ?.locator('[data-hl-field="title"]')
      .textContent()
      .catch(() => null);
  };
  const live = await expect
    .poll(shown, { timeout: 10_000 })
    .toBe('Супермаркеты')
    .then(
      () => true,
      () => false,
    );
  if (!live) {
    console.log('the open board did not take the written value until reloaded');
    await page.reload();
    await expect.poll(shown, { timeout: 30_000 }).toBe('Супермаркеты');
  }
});
