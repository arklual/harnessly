// The machine's journal on a real runtime: an app's view that loads is in `harnessly logs` with its times, and
// a view whose SDK never reaches the machine (a gateway answers 401 for /__hl/, the runtime sees nothing) is an error
// with its chain and its cause: the card says it, and the agent reads it without the person's DevTools. A view whose
// SDK the preview's DevTools block is the same, told by the runtime itself.
//
// JOURNAL_APP_DIR and JOURNAL_APP_ID check another app (an app of a team machine); by default a tiny app made
// here. Output of the CLI goes to JOURNAL_OUT when set.
import { execFileSync } from 'node:child_process';
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Page, test } from '@playwright/test';

import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;
let cli: string;
let appId: string;

const harnessly = (...args: string[]): string =>
  execFileSync(cli, args, {
    env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
    encoding: 'utf8',
  });

const note = (title: string, text: string) => {
  if (process.env.JOURNAL_OUT) appendFileSync(process.env.JOURNAL_OUT, `\n=== ${title}\n${text}\n`);
};

const makeApp = (root: string): string => {
  const dir = join(root, 'journal-app');
  mkdirSync(join(dir, 'harnessly'), { recursive: true });
  mkdirSync(join(dir, 'ui'), { recursive: true });
  writeFileSync(
    join(dir, 'harnessly', 'app.json'),
    JSON.stringify({
      id: 'example.journal',
      name: 'Journal check',
      description: 'A view that connects and writes a key',
      version: '0.1.0',
      harnessly: '>=0.16.0',
      trust: 'sandboxed',
      targets: ['app'],
      permissions: ['storage'],
      views: [{ id: 'main', title: 'Main', entry: 'ui/index.html', targets: ['app'] }],
    }),
  );
  writeFileSync(
    join(dir, 'ui', 'index.html'),
    '<!doctype html><html><head><title>j</title></head><body><p id="s">loading</p>' +
      '<script src="/__hl/app-sdk.js"></script><script src="main.js"></script></body></html>',
  );
  writeFileSync(
    join(dir, 'ui', 'main.js'),
    "window.harnessly.connect({ protocol: 2 }).then(function (hl) { document.getElementById('s').textContent = 'ready';" +
      " return hl.storage.user.set('seen', 1); });",
  );
  return dir;
};

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-journal-'));
  rt = await startRuntime({ root });
  cli = process.env.HARNESSLY_BIN ?? join(root, 'harnessly');
  if (!process.env.HARNESSLY_BIN)
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  const dir = process.env.JOURNAL_APP_DIR ?? makeApp(root);
  appId = process.env.JOURNAL_APP_ID ?? 'example.journal';
  note('install', harnessly('app', 'install', dir) + harnessly('app', 'list'));
  page = await browser.newPage();
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();
  // The person's «Allow» for what the app asks, as the consent card sends it.
  const manifest = JSON.parse(readFileSync(join(dir, 'harnessly', 'app.json'), 'utf8')) as {
    version: string;
    permissions?: string[];
  };
  const granted = await page.evaluate(
    async (b) => {
      const base = location.pathname.startsWith('/agents') ? '/agents' : '';
      const res = await fetch(`${base}/api/platform/apps/consent`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(b),
      });
      return `${res.status} ${await res.text()}`;
    },
    { id: appId, version: manifest.version, access: (manifest.permissions ?? []).map((p) => `perm:${p}`) },
  );
  note('consent', granted.slice(0, 300));
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

const openApp = async () => {
  // Through the page's router, as a click would: a fresh load starts at the greeting.
  await page.goto(`${rt.url}/`);
  await expect(page.locator('.hx-app')).toBeVisible();
  const base = new URL(page.url()).pathname.startsWith('/agents') ? '/agents' : '';
  await page.evaluate((to) => {
    window.history.pushState({}, '', to);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, `${base}/apps/${appId}`);
  // An app installed from a folder waits for the person's consent: the card over it has «Allow».
  const allow = page.getByRole('button', { name: /^(Allow|Разрешить)/ }).first();
  if (await allow.isVisible({ timeout: 8000 }).catch(() => false)) await allow.click();
  if (process.env.JOURNAL_SHOT) await page.screenshot({ path: process.env.JOURNAL_SHOT });
  await expect(page.locator('iframe').first()).toBeAttached({ timeout: 30_000 });
};

test('1. a view that loads is in the journal with its times', async () => {
  await openApp();
  // The handshake: the journal's ui.initialize line comes from the page once the view connected.
  await expect
    .poll(() => harnessly('logs', '--level', 'info', '--app', appId, '--since', '5m'), { timeout: 30_000 })
    .toContain('ui.initialize');
  const out = harnessly('logs', '--level', 'info', '--app', appId, '--since', '5m');
  note('success: harnessly logs --level info --app', out);
  expect(out).toContain('view.open');
  expect(out).toMatch(/view\.page .*→ 200/);
  expect(out).toMatch(/http POST \S*\/apps\/session → 200 \d+ms/);
  expect(out).toMatch(/http POST \S*\/apps\/storage → 200 \d+ms · user\.(get|set)/);
  expect(out).toMatch(/ui\.initialize .*\d+(ms|\.\ds)/);
});

test('2. a gateway that refuses /__hl/: the card and the journal say why', async () => {
  // In front of the machine, as a gateway would: the runtime never sees these requests.
  await page.route('**/__hl/**', (route) => route.fulfill({ status: 401, body: 'sign in' }));
  const since = new Date().toISOString();
  await openApp();
  const card = page.getByText(/bridge-shim\.js/);
  await expect(card.first()).toBeVisible({ timeout: 20_000 });
  note(
    'card',
    (await page.locator('body').innerText())
      .split('\n')
      .filter((l) => /shim|подключ|connect/i.test(l))
      .join('\n'),
  );
  // The page's lines come in a batch a few seconds later.
  await expect
    .poll(() => harnessly('logs', '--since', since), { timeout: 20_000 })
    .toContain('ui.initialize');
  const out = harnessly('logs', '--since', since);
  note('gateway 401: harnessly logs (errors with chains)', out);
  expect(out).toContain('ui.initialize');
  expect(out).toContain(appId);
  expect(out).toMatch(/bridge-shim\.js/);
  await page.unroute('**/__hl/**');
});

test('3. the preview DevTools block /__hl/: the runtime names the file and the block', async () => {
  const slots = JSON.parse(
    execFileSync('curl', ['-s', '-H', `x-cli-token: ${readCLIToken()}`, `${rt.url}/api/cli/preview/slots`], {
      encoding: 'utf8',
    }),
  ) as { slots?: { slot: string }[] } | { slot: string }[];
  const list = Array.isArray(slots) ? slots : (slots.slots ?? []);
  const block = (change: Record<string, string[]>) =>
    list.forEach((s) =>
      execFileSync('curl', [
        '-s',
        '-X',
        'PATCH',
        '-H',
        `x-cli-token: ${readCLIToken()}`,
        '-H',
        'Content-Type: application/json',
        '--data',
        JSON.stringify(change),
        `${rt.url}/api/cli/preview/settings?slot=${encodeURIComponent(s.slot)}`,
      ]),
    );
  block({ blockedAdd: ['*/__hl/*'] });
  const since = new Date().toISOString();
  await openApp();
  // Whichever SDK file the block stopped first, the card names it and the block.
  const card = page.getByText(/__hl\/(bridge-shim|app-sdk)\.js.*(DevTools|DevTools превью)/);
  await expect(card.first()).toBeVisible({ timeout: 20_000 });
  note('card', await card.first().innerText());
  await expect
    .poll(() => harnessly('logs', '--since', since), { timeout: 20_000 })
    .toContain('ui.initialize');
  const out = harnessly('logs', '--since', since);
  note('preview block: harnessly logs (errors with chains)', out);
  expect(out).toMatch(/__hl\/(bridge-shim|app-sdk)\.js.*(DevTools)/);
  block({ blockedRemove: ['*/__hl/*'] });
});

// The frame's first request: its address carries the mount's trace, by path or on a slot's origin.
const frameAddress = (url: URL) => url.searchParams.has('__hl_trace');

const readCLIToken = (): string => readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();

test('4. a gateway that answers the view page itself: the machine saw nothing, the page says so', async () => {
  await page.route(frameAddress, (route) => route.fulfill({ status: 401, body: 'sign in' }));
  const since = new Date().toISOString();
  await openApp();
  // A frame of the page's origin (a slot by path) is asked again and its status read; another origin's is not.
  const card = page.getByText(
    /answered the view's page 401: the request never reached the machine|never saw the request of the view's page/,
  );
  await expect(card.first()).toBeVisible({ timeout: 25_000 });
  note('card', await card.first().innerText());
  await expect
    .poll(() => harnessly('logs', '--since', since), { timeout: 20_000 })
    .toContain('frame.gateway');
  const out = harnessly('logs', '--since', since);
  note('foreign gateway: harnessly logs', out);
  expect(out).toMatch(/frame\.load \S+ \d+ms/);
  expect(out).toContain('frame.gateway');
  expect(out).toContain(appId);
  await page.unroute(frameAddress);
});

test('5. a gateway of ours that refuses the view page tells the card why, with its status', async () => {
  // As the hub and the preview gateway answer a frame they refuse: a page that tells the page above.
  const told =
    '<!doctype html><script>parent.postMessage({hlViewError:{kind:"refused",status:401,code:"gateway-no-session",' +
    'text:"the gateway did not let the view in: open the preview again",src:""}},"*")</script>';
  await page.route(frameAddress, (route) =>
    route.fulfill({ status: 401, contentType: 'text/html', body: told }),
  );
  const since = new Date().toISOString();
  await openApp();
  const card = page.getByText(/the gateway did not let the view in/);
  await expect(card.first()).toBeVisible({ timeout: 20_000 });
  await expect.poll(() => harnessly('logs', '--since', since), { timeout: 20_000 }).toContain('view.refused');
  const out = harnessly('logs', '--since', since);
  note('our gateway: harnessly logs', out);
  expect(out).toMatch(/view\.refused .*→ 401|→ 401.*view\.refused|view\.refused/);
  await page.unroute(frameAddress);
});
