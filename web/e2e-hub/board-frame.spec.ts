import { expect, test, type Page } from '@playwright/test';
import { cpSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { startStack, type Stack } from './stack';

// HRN-1482 repro: a Sinopia Design board in the right pane of a chat, a step away to another page of the app and back.
// Every iframe document the page loads is recorded with the headers that decide whether Chrome shows it.

const shotsDir = process.env.E2E_SHOTS_DIR ?? join('e2e-hub-results', 'shots', 'board-frame');
// The runtime script of a Sinopia board (a project's support.js), copied into the seeded project.
const supportJs = process.env.E2E_SUPPORT_JS ?? '';
test.skip(supportJs === '', 'needs E2E_SUPPORT_JS: the support.js of a Sinopia project');
const DESIGN = 'frame-demo';

test.describe.configure({ mode: 'serial' });
let stack: Stack;
let page: Page;
let machineId = '';
let library = '';
const log: string[] = [];
const note = (s: string) => {
  log.push(s);
  console.log(s);
};
const shot = async (what: string) => {
  mkdirSync(shotsDir, { recursive: true });
  await page.screenshot({ path: join(shotsDir, `board-frame-${what}.png`) });
};

test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack('dev', { HUB_FEATURES_DEFAULT: 'ui.hx,runtime.fleet' });
});
test.afterAll(async () => {
  mkdirSync(shotsDir, { recursive: true });
  writeFileSync(join(shotsDir, 'frames.log'), log.join('\n'));
  await stack?.stop();
});

const api = async <T>(method: 'get' | 'post', path: string, data?: unknown): Promise<T> => {
  const res = await page.evaluate(
    async ([m, u, body]) => {
      const r = await fetch(u as string, {
        method: (m as string).toUpperCase(),
        headers: body === null ? {} : { 'content-type': 'application/json' },
        body: body === null ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
      });
      return { ok: r.ok, status: r.status, text: await r.text() };
    },
    [method, path, data === undefined ? null : data] as const,
  );
  expect(res.ok, `${method} ${path}: ${res.status} ${res.text}`).toBeTruthy();
  return JSON.parse(res.text) as T;
};

/** A design project as the library keeps it: meta/ with one numbered page and project/ with the runtime and the board. */
function seedDesign(lib: string) {
  const dir = join(lib, DESIGN);
  const pid = '01ARYZ6S41TSV4RRFFQ69G5FAV';
  mkdirSync(join(dir, 'meta', 'pages'), { recursive: true });
  mkdirSync(join(dir, 'project'), { recursive: true });
  writeFileSync(
    join(dir, '.owner.json'),
    JSON.stringify({ v: 2, user: 'agent', name: 'Owner', private: false, person: 'owner@example.com' }),
  );
  writeFileSync(
    join(dir, 'artifact.json'),
    JSON.stringify({ v: 1, title: 'Frame demo', importedAt: '2026-11-15T10:00:00.000Z', boards: [] }),
  );
  writeFileSync(
    join(dir, 'meta', 'project.json'),
    JSON.stringify({
      v: 1,
      title: 'Frame demo',
      createdAt: '2026-11-15T10:00:00.000Z',
      release: '1792547779-0491',
      kits: [],
      tailwind: false,
      canvasKey: 'harnessly',
    }),
  );
  writeFileSync(
    join(dir, 'meta', 'pages', `${pid}.json`),
    JSON.stringify({ id: pid, title: 'Board', file: 'Board.dc.html', w: null, h: null, order: 1 }),
  );
  cpSync(supportJs, join(dir, 'project', 'support.js'));
  writeFileSync(
    join(dir, 'project', 'Board.dc.html'),
    `<!doctype html><html><head><meta charset="utf-8"><script src="./support.js"></script></head><body><x-dc>
<main style="padding: 32px; font-family: sans-serif"><h1 style="margin:0">Frame demo board</h1><p>Hello from the seeded board</p></main>
</x-dc></body></html>`,
  );
}

test('1. sign in, a machine with a design library', async ({ browser }) => {
  page = await (await browser.newContext()).newPage();
  await page.goto(`${stack.hub}/signin`);
  await page.locator('input[type=email]').fill('owner@example.com');
  await page.getByRole('button', { name: 'Получить код' }).click();
  await page.locator('.hxa__otp').waitFor();
  await page.waitForTimeout(500);
  await page.locator('input').first().fill(stack.emailCode('owner@example.com'));
  await page.waitForURL((u) => !/\/signin/.test(u.pathname), { timeout: 20_000 });
  const me = await api<{ spaces: { id: string }[] }>('get', '/api/me');
  const spaceId =
    me.spaces[0]?.id ??
    (await api<{ space: { id: string } }>('post', '/api/spaces', { name: 'Main' })).space.id;
  const reg = await api<{ code: string }>('post', `/api/spaces/${spaceId}/machines/codes`, {
    profile: 'full',
    confirmFull: true,
  });
  library = join(stack.root, 'library');
  seedDesign(library);
  machineId = (
    await stack.registerMachine({
      code: reg.code,
      name: 'board-box',
      runtimeEnv: {
        RUNTIME_PREVIEW_MODE: 'path',
        RUNTIME_PREVIEW_LISTEN: '',
        RUNTIME_DESIGN_LIBRARY_DIR: library,
        RUNTIME_OWNER_EMAIL: 'owner@example.com',
      },
    })
  ).machineId;
  await expect
    .poll(
      async () =>
        (await api<{ machine: { status: string } }>('get', `/api/machines/${machineId}`)).machine.status,
      { timeout: 60_000 },
    )
    .toBe('online');
});

test('2. the board in the pane, away and back', async () => {
  // every iframe document: url, status, the headers that can refuse it, and what the browser said it was doing
  let phase = 'open';
  const frames: { url: string; xfo: string; csp: string }[] = [];
  page.on('response', async (r) => {
    const req = r.request();
    if (req.resourceType() !== 'document' || req.frame() === page.mainFrame()) return;
    const h = r.headers();
    frames.push({ url: r.url(), xfo: h['x-frame-options'] ?? '', csp: h['content-security-policy'] ?? '' });
    const rq = await req.allHeaders().catch(() => ({}) as Record<string, string>);
    note(
      `[${phase}] ${r.status()} ${r.url()}\n   xfo=${h['x-frame-options'] ?? '-'} csp=${h['content-security-policy'] ?? '-'}\n   sec-fetch-dest=${rq['sec-fetch-dest']} mode=${rq['sec-fetch-mode']} site=${rq['sec-fetch-site']} user=${rq['sec-fetch-user'] ?? '-'} referer=${rq['referer'] ?? '-'}`,
    );
  });
  const fresh = page.getByRole('button', { name: /new chat|новый чат/i }).first();
  await expect(async () => {
    await page.goto(`${stack.hub}/agents/`);
    await expect(fresh).toBeVisible({ timeout: 10_000 });
  }).toPass({ timeout: 120_000 });
  await fresh.click();
  const box = page.getByRole('textbox').last();
  await box.fill('hello');
  await box.press('Enter');
  await expect(page.getByText('Fake agent reply: hello').first()).toBeVisible({ timeout: 90_000 });
  await shot('0-chat');
  const chatUrl = page.url();
  note(`chat url ${chatUrl}`);

  // the pane: its button at the top right of the chat, then the Sinopia Design tool and the design's card
  await page
    .getByRole('button', { name: /got it|понятно/i })
    .click({ timeout: 5_000 })
    .catch(() => {});
  await page.mouse.click(1397, 74);
  await page.waitForTimeout(1500);
  await shot('1-pane');
  note(
    'buttons: ' +
      JSON.stringify(
        await page
          .getByRole('button')
          .evaluateAll((els) =>
            els.map((e) => e.getAttribute('aria-label') || e.textContent?.trim()).filter(Boolean),
          ),
      ),
  );
  await page
    .getByText(/Sinopia Design/)
    .last()
    .click({ timeout: 15_000 });
  await page.waitForTimeout(1500);
  await shot('1b-library');
  await page
    .getByText(/Frame demo/)
    .first()
    .click({ timeout: 30_000 });
  await page.waitForTimeout(6000);
  await shot('1c-clicked');
  const frame = page.locator('iframe.hx-site__iframe');
  await expect(frame).toBeVisible({ timeout: 60_000 });
  await expect(
    page.frameLocator('iframe.hx-site__iframe').getByText('Hello from the seeded board'),
  ).toBeVisible({ timeout: 60_000 });
  await shot('2-open');

  // (a) mark the frame's own DOM node before leaving; (b)/(c) are read from the responses recorded below
  await page.evaluate(() => {
    for (const el of document.querySelectorAll('iframe.hx-site__iframe'))
      (el as HTMLElement).dataset.e2eMark = '1';
  });
  const before = frames.length;
  phase = 'after-return';
  // another page of the app by the app's own navigation (no reload): the new-chat page, then the way back
  await page.getByRole('button', { name: 'New chat' }).first().click();
  await page.waitForTimeout(3000);
  note(`away url ${page.url()}`);
  await shot('3-away');
  // the way back as a person takes it: the chat in the sidebar's list (the browser's back leaves the SPA's history alone)
  await page
    .getByRole('button', { name: /expand sidebar/i })
    .first()
    .click()
    .catch(() => {});
  await page.getByText('hello', { exact: true }).first().click();
  await page.waitForTimeout(8000);
  await shot('4-back');
  note(`back url ${page.url()}`);
  const marked = await page.evaluate(
    () => document.querySelectorAll('iframe.hx-site__iframe[data-e2e-mark="1"]').length,
  );
  const total = await page.evaluate(() => document.querySelectorAll('iframe.hx-site__iframe').length);
  note(
    `marked iframes after return: ${marked} of ${total}; iframe document responses after return: ${frames.length - before}`,
  );
  const shown = await page
    .frameLocator('iframe.hx-site__iframe')
    .getByText('Hello from the seeded board')
    .isVisible()
    .catch(() => false);
  note(`board visible after return: ${shown}`);
  const refused = frames.filter((f) => /DENY/i.test(f.xfo) || /frame-ancestors\s+'none'/i.test(f.csp));
  note(`refusing answers: ${refused.length} ${refused.map((f) => f.url).join(' ')}`);
  // diagnostic: the slot document of the first opening and a slot key the runtime never gave, each as an iframe
  // navigation (a fetch cannot carry Sec-Fetch-Dest: iframe), read by the same response recorder
  const slots = frames.filter((f) => f.url.includes('/__pv/'));
  const stale = slots[0]?.url;
  const bogus = stale?.replace(/__pv\/(\d+)-[0-9a-f]+/, '__pv/$1-00000000000000000000000000000000');
  for (const [what, u] of [
    ['first-slot-key', stale],
    ['unknown-slot-key', bogus],
  ] as const) {
    if (!u) continue;
    phase = `probe:${what}`;
    await page.evaluate((src) => {
      const f = document.createElement('iframe');
      f.src = src;
      f.style.cssText = 'position:fixed;width:10px;height:10px;left:0;top:0';
      document.body.appendChild(f);
    }, u);
    await page.waitForTimeout(2500);
  }
  expect.soft(marked, '(a) the same iframe node').toBeGreaterThan(0);
  expect.soft(frames.length - before, '(b) no new frame document after return').toBe(0);
  expect.soft(refused, '(c) no frame answer refuses framing').toEqual([]);
});
