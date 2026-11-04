/// <reference lib="dom" />
import { execFileSync, spawn } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import { mkdirSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import { freePort, startRuntime, type Runtime } from './runtime';
import { RELEASES } from '../app/next/features/whats-new/releases';

let rt: Runtime, site: Server, siteURL: string;
interface Report {
  w: number;
  h: number;
  selected: string;
  confirmed: boolean | null;
  scroll: number;
  doubles?: number;
  range?: string;
  typed?: string;
  entered?: number;
  targets?: Record<string, { x: number; y: number }>;
  select: { x: number; y: number };
  confirm: { x: number; y: number };
}
test.use({ hasTouch: true });
const reports = new Map<string, Report>();
const screenshots = process.env.E2E_SCREENSHOTS;
const fixture = `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Browser fixture</title><style>body{font:18px system-ui;margin:32px;background:#f4f7fc;color:#16273c}
button,select{font:inherit;padding:10px;margin:8px 0;display:block}section{height:1600px}
h1{font-size:28px}article{max-width:600px;padding:24px;background:white;border-radius:16px}</style>
<article><h1>Agent browser playground</h1><p>A local page for tabs, sizes and forms.</p>
<select id="choice"><option value="first">First option</option><option value="second">Second option</option>
<option disabled>Unavailable option</option></select><button id="confirm">Confirm action</button><output id="size"></output></article><section></section>
<script>let confirmed=null;const choice=document.querySelector('#choice');
function point(id){const r=document.querySelector(id).getBoundingClientRect();return {x:r.x+15,y:r.y+15}}
function report(){document.querySelector('#size').textContent=innerWidth+' × '+innerHeight;
fetch('/report?name='+location.pathname,{method:'POST',body:JSON.stringify({w:innerWidth,h:innerHeight,
selected:choice.value,confirmed,scroll:scrollY,select:point('#choice'),confirm:point('#confirm')})})}
document.querySelector('#confirm').onclick=()=>{confirmed=confirm('Continue with this action?');report()};
choice.onchange=report;onresize=report;onscroll=report;report();</script>`;

const inputFixture = `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1">
<style>body{font:18px system-ui;margin:20px;background:#f4f7fc;color:#16273c}input{display:block;margin:14px 0;font:inherit;width:220px}p{margin:12px 0}h1{font-size:24px}</style>
<h1>Remote input</h1><p id="words">Copy these words</p><input id="range" type="range" value="10">
<input id="typed" aria-label="Name" placeholder="Enter your name"><input id="secret" type="password" value="private">
<script>let doubles=0,entered=0;const typed=document.querySelector('#typed'),range=document.querySelector('#range');
function report(){const targets={};for(const id of ['words','range','typed','secret']){const r=document.getElementById(id).getBoundingClientRect();targets[id]={x:r.x+15,y:r.y+r.height/2}};
fetch('/report?name='+location.pathname,{method:'POST',body:JSON.stringify({w:innerWidth,h:innerHeight,scroll:scrollY,doubles,range:range.value,typed:typed.value,entered,targets})})}
document.querySelector('#words').ondblclick=()=>{doubles++;report()};typed.oninput=report;range.oninput=report;typed.onkeydown=e=>{if(e.key==='Enter'){entered++;report()}};onresize=report;report();</script>`;

test.beforeAll(async () => {
  rt = await startRuntime({
    env: {
      RUNTIME_BROWSER_MODE: 'headless',
      RUNTIME_BROWSER_CHROME_ARGS:
        '--disable-background-networking --disable-component-update --disable-sync',
    },
  });
  const port = await freePort();
  siteURL = `http://127.0.0.1:${port}`;
  site = createServer((req, res) => {
    const url = new URL(req.url ?? '/', siteURL);
    if (url.pathname === '/report') {
      let body = '';
      req.on('data', (chunk) => {
        body += String(chunk);
      });
      req.on('end', () => {
        reports.set(url.searchParams.get('name') ?? '', JSON.parse(body) as Report);
        res.end('ok');
      });
      return;
    }
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end(url.pathname.startsWith('/input') ? inputFixture : fixture);
  });
  await new Promise<void>((ok) => site.listen(port, '127.0.0.1', ok));
  if (screenshots) mkdirSync(screenshots, { recursive: true });
});
test.afterAll(async () => {
  // Hosts intentionally outlive the runtime. Visit only this test's temporary profile directories,
  // without following links, since managed profile IDs are random and several profiles can have hosts.
  const stopHosts = (dir: string) => {
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) stopHosts(path);
      if (entry.isFile() && entry.name === 'host.pid') {
        try {
          const pid = Number(readFileSync(path, 'utf8').trim());
          if (Number.isInteger(pid) && pid > 1) process.kill(pid, 'SIGTERM');
        } catch {
          /* host already stopped */
        }
      }
    }
  };
  if (rt) stopHosts(join(rt.home, '.harnessly', 'agent-browser', 'profiles'));
  await rt?.stop();
  if (site) await new Promise<void>((ok) => site.close(() => ok()));
});
async function shot(page: Page, name: string) {
  if (screenshots) await page.screenshot({ path: join(screenshots, `${name}.png`), animations: 'disabled' });
}
async function ready(page: Page) {
  await page.addInitScript((id) => localStorage.setItem('hx:whats-new', id), RELEASES[0]!.id);
  await page.goto(rt.url);
  await page
    .locator('.hx-composer__input, [data-testid="setup-form"], [data-testid="signin-form"]')
    .first()
    .waitFor();
  const login = page.getByTestId('signin-form');
  if (await login.isVisible()) {
    await login.locator('input[name=password]').fill('browser test password');
    await login.locator('button[type=submit]').click();
  }
  const setup = page.getByTestId('setup-form');
  if (await setup.isVisible()) {
    await setup.locator('input[name=password]').fill('browser test password');
    await setup.locator('input[name=confirm]').fill('browser test password');
    await setup.locator('button[type=submit]').click();
  }
  await expect(page.locator('.hx-composer__input')).toBeVisible();
}
async function chat(page: Page, message: string, empty = true) {
  await ready(page);
  await page.getByRole('button', { name: 'New chat', exact: true }).first().click();
  await page.locator('.hx-composer__input').fill(message);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await openBrowser(page, empty);
}
async function openBrowser(page: Page, empty = true) {
  if (!(await page.locator('.hx-pane').isVisible())) await page.locator('.hx-header__tools').click();
  await page.locator('.hx-pane .hx-tab[data-home]').click();
  await page
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="agent-browser"]') })
    .click();
  if (empty) await expect(page.getByText('No tabs', { exact: true })).toBeVisible({ timeout: 90_000 });
}
async function createProjectUI(page: Page, name: string): Promise<string> {
  await page.locator('.hx-sidebar').getByRole('button', { name: 'New project', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'New project', exact: true });
  await dialog.getByLabel('Name', { exact: true }).fill(name);
  await dialog.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const project = page.locator('.hx-sidebar [data-drop="project"]').filter({ hasText: name });
  await expect(project).toHaveCount(1);
  return (await project.getAttribute('data-drop-id'))!;
}
async function navigate(page: Page, name: string) {
  const address = page.locator('.hx-agent-browser .hx-tbrowser__url');
  await address.fill(siteURL + name);
  await address.press('Enter');
  await expect.poll(() => reports.has(name)).toBe(true);
  await expect(page.locator('.hx-agent-browser canvas')).toHaveAttribute('data-shown', 'true');
  await fitReady(page, name);
}
async function waitFrame(page: Page, w: number, h: number) {
  const canvas = page.locator('.hx-agent-browser canvas');
  await expect(canvas).toHaveAttribute('data-page-width', String(w));
  await expect(canvas).toHaveAttribute('data-page-height', String(h));
}
async function fitReady(page: Page, name: string) {
  // The phone pane animates its width; compare against current bounds, not an intermediate pixel.
  await expect
    .poll(async () => {
      const box = (await page.locator('.hx-agent-browser__view').boundingBox())!;
      const report = reports.get(name);
      return (
        report?.w === Math.max(320, Math.floor(box.width)) &&
        report?.h === Math.max(240, Math.floor(box.height))
      );
    })
    .toBe(true);
  const report = reports.get(name)!;
  await waitFrame(page, report.w, report.h);
}
async function clickPage(page: Page, name: string, key: 'select' | 'confirm') {
  const report = reports.get(name)!;
  await waitFrame(page, report.w, report.h);
  const box = (await page.locator('.hx-agent-browser canvas').boundingBox())!;
  await page.mouse.click(
    box.x + (report[key].x * box.width) / report.w,
    box.y + (report[key].y * box.height) / report.h,
  );
}

test('managed browser tabs, real viewport, dialogs, select and control', async ({ page, context }) => {
  test.setTimeout(180_000);
  const notices: string[] = [];
  page.on('websocket', (ws) =>
    ws.on('framereceived', (frame) => {
      if (typeof frame.payload === 'string' && frame.payload.includes('"t":"notice"'))
        notices.push(frame.payload);
    }),
  );
  await chat(page, 'Browser test');
  const pane = page.locator('.hx-agent-browser');
  await expect(pane.locator('.hx-agent-browser__note')).not.toContainText(/Chrome\/|sandbox:|renderer probe/);
  await shot(page, 'empty');
  await pane.getByRole('button', { name: 'New tab', exact: true }).first().click();
  await expect(pane.locator('.hx-tbrowser__url')).toBeFocused();
  await navigate(page, '/first');
  for (const name of ['/second', '/third']) {
    await pane.getByRole('button', { name: 'New tab', exact: true }).click();
    await navigate(page, name);
  }
  await expect(pane.getByRole('tab')).toHaveCount(3);
  expect(notices).toEqual([]);
  await expect(pane.getByRole('alert')).toHaveCount(0, { timeout: 500 });
  await shot(page, 'three-tabs');
  await pane
    .getByTitle(siteURL + '/third', { exact: true })
    .locator('..')
    .getByRole('button', { name: 'Close tab: Browser fixture' })
    .click();
  await expect(pane.getByRole('tab')).toHaveCount(2);
  await pane.getByTitle(siteURL + '/second', { exact: true }).click({ button: 'middle' });
  await expect(pane.getByRole('tab')).toHaveCount(1);
  await expect(pane.locator('.hx-tbrowser__url')).toHaveValue(siteURL + '/first');
  await pane.getByRole('button', { name: 'Viewport size' }).click();
  await shot(page, 'size-menu');
  await pane.getByRole('button', { name: '1280×800', exact: true }).click();
  await expect.poll(() => reports.get('/first')?.w).toBe(1280);
  await expect.poll(() => reports.get('/first')?.h).toBe(800);
  await page.setViewportSize({ width: 1200, height: 850 });
  await expect.poll(() => reports.get('/first')?.w).toBe(1280);
  await pane.getByRole('button', { name: 'Viewport size' }).click();
  await pane.getByRole('spinbutton', { name: 'Width', exact: true }).fill('900');
  await pane.getByRole('spinbutton', { name: 'Height', exact: true }).fill('700');
  await pane.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect.poll(() => reports.get('/first')?.w).toBe(900);
  await expect.poll(() => reports.get('/first')?.h).toBe(700);
  await waitFrame(page, 900, 700);
  const handle = await pane.getByRole('button', { name: 'Drag to resize' }).boundingBox();
  expect(handle).not.toBeNull();
  await page.mouse.move(handle!.x + handle!.width / 2, handle!.y + handle!.height / 2);
  await page.mouse.down();
  await page.mouse.move(handle!.x + handle!.width / 2 + 20, handle!.y + handle!.height / 2 + 20);
  await page.mouse.up();
  await expect.poll(() => reports.get('/first')?.w ?? 0).toBeGreaterThan(900);
  await pane.getByRole('button', { name: 'Viewport size' }).click();
  await pane.getByRole('button', { name: 'Phone · 390×844' }).click();
  await expect.poll(() => reports.get('/first')?.w).toBe(390);
  await waitFrame(page, 390, 844);
  await shot(page, 'phone-mode');
  await pane.getByRole('button', { name: 'Viewport size' }).click();
  await pane.getByRole('button', { name: 'Fit panel', exact: true }).click();
  await fitReady(page, '/first');
  await clickPage(page, '/first', 'confirm');
  await expect(pane.getByRole('dialog')).toBeVisible();
  await shot(page, 'dialog');
  await pane.getByRole('button', { name: 'OK', exact: true }).click();
  await expect.poll(() => reports.get('/first')?.confirmed).toBe(true);
  await expect(pane.getByRole('alert')).toHaveCount(0);
  await clickPage(page, '/first', 'select');
  await expect(pane.getByRole('listbox')).toBeVisible();
  await shot(page, 'select');
  await pane.getByRole('option', { name: 'Second option' }).click();
  await expect.poll(() => reports.get('/first')?.selected).toBe('second');
  await pane.getByRole('button', { name: 'Take control' }).click();
  await expect(pane.getByText('You have control')).toBeVisible();
  await pane.getByRole('button', { name: 'Return to agent' }).click();
  await expect(pane.getByText('Agent has control')).toBeVisible();
  const other = await context.newPage();
  await chat(other, 'Another browser chat');
  await other
    .locator('.hx-agent-browser')
    .getByRole('button', { name: 'New tab', exact: true })
    .first()
    .click();
  await navigate(other, '/other');
  await expect(pane.locator('summary')).toHaveText('Other chats (1)');
  await pane.locator('summary').click();
  await pane.locator('.hx-agent-browser__others').getByRole('tab').click();
  await expect(pane.getByRole('button', { name: 'Follow agent' })).toBeVisible();
  await shot(page, 'other-chats');
  await pane.getByRole('button', { name: 'Follow agent' }).click();
  await expect(pane.locator('.hx-tbrowser__url')).toHaveValue(siteURL + '/first');
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(pane).toBeVisible();
  await fitReady(page, '/first');
  await shot(page, 'phone-width');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const touchReport = reports.get('/first')!;
  const touchBox = (await pane.locator('canvas').boundingBox())!;
  await page.touchscreen.tap(
    touchBox.x + (touchReport.select.x * touchBox.width) / touchReport.w,
    touchBox.y + (touchReport.select.y * touchBox.height) / touchReport.h,
  );
  await expect(pane.getByRole('listbox')).toBeVisible();
  await pane.getByRole('option', { name: 'First option', exact: true }).click();
  await expect.poll(() => reports.get('/first')?.selected).toBe('first');
  const cdp = await context.newCDPSession(page);
  const x = touchBox.x + touchBox.width / 2,
    y = touchBox.y + touchBox.height * 0.8;
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
  for (let n = 1; n <= 5; n++)
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [{ x, y: y - n * 25 }],
    });
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await expect.poll(() => reports.get('/first')?.scroll ?? 0).toBeGreaterThan(50);
  await cdp.detach();
  await other.close();
});

test('server error remains visible after socket closes', async ({ page }) => {
  await page.routeWebSocket('**/api/agent-browser?*', (ws) => {
    ws.send(
      JSON.stringify({
        t: 'status',
        state: 'error',
        error: 'Chrome could not start: test failure',
        profile: { id: 'common', name: 'Common' },
      }),
    );
    setTimeout(() => ws.close(), 100);
  });
  // This test isolates the error view; the successful browser flow above uses the real socket throughout.
  await chat(page, 'Browser error test', false);
  await expect(page.getByText('Chrome could not start: test failure')).toBeVisible();
  await shot(page, 'error');
});

test('double click, drag, clipboard and phone keyboard reach the page', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: rt.url });
  await chat(page, 'Input test');
  const pane = page.locator('.hx-agent-browser');
  await pane.getByRole('button', { name: 'New tab', exact: true }).first().click();
  await navigate(page, '/input');
  const canvas = pane.locator('canvas');
  const target = async (key: string) => {
    const r = reports.get('/input')!;
    await waitFrame(page, r.w, r.h);
    const b = (await canvas.boundingBox())!,
      p = r.targets![key]!;
    return { x: b.x + (p.x * b.width) / r.w, y: b.y + (p.y * b.height) / r.h, scale: b.width / r.w };
  };
  let p = await target('words');
  await page.mouse.dblclick(p.x, p.y);
  await expect.poll(() => reports.get('/input')?.doubles).toBe(1);
  await canvas.press('Control+c');
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('Copy');
  p = await target('range');
  await page.mouse.move(p.x + 7 * p.scale, p.y);
  await page.mouse.down();
  await page.mouse.move(p.x + 175 * p.scale, p.y, { steps: 12 });
  await page.mouse.up();
  await expect.poll(() => Number(reports.get('/input')?.range)).toBeGreaterThan(70);
  p = await target('typed');
  await page.mouse.click(p.x, p.y);
  await canvas.pressSequentially('sample');
  await canvas.press('Control+a');
  await canvas.press('Control+c');
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('sample');
  p = await target('secret');
  await page.mouse.click(p.x, p.y);
  await canvas.press('Control+a');
  await canvas.press('Control+c');
  await page.waitForTimeout(200);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('sample');
  await page.setViewportSize({ width: 390, height: 844 });
  await fitReady(page, '/input');
  p = await target('typed');
  await page.touchscreen.tap(p.x, p.y);
  await canvas.press('End');
  await pane.getByRole('button', { name: 'Keyboard', exact: true }).click();
  const keyboard = pane.getByLabel('Remote keyboard input');
  await expect(keyboard).toBeFocused();
  await keyboard.pressSequentially(' phone');
  await expect(keyboard).toHaveValue('');
  await expect.poll(() => reports.get('/input')?.typed).toBe('sample phone');
  await keyboard.press('Backspace');
  await keyboard.press('Enter');
  await expect.poll(() => reports.get('/input')?.typed).toBe('sample phon');
  await expect.poll(() => reports.get('/input')?.entered).toBe(1);
  await shot(page, 'phone-keyboard');
});

test('agent viewport remains fixed when the panel opens and resizes', async ({ page }) => {
  const binary = join(rt.root, 'browser-review');
  execFileSync('go', ['build', '-o', binary, './e2e/fixtures/browser-agent']);
  const child = spawn(binary, [], { stdio: ['ignore', 'pipe', 'inherit'] });
  const base = await new Promise<string>((resolve, reject) => {
    child.stdout.once('data', (data) => resolve(String(data).trim()));
    child.once('error', reject);
    child.once('exit', (code) => reject(new Error(`Browser fixture exited: ${code}`)));
  });
  let browserConv = '';
  try {
    await page.routeWebSocket('**/api/agent-browser?*', (ws) => {
      const url = new URL(ws.url());
      browserConv = url.searchParams.get('conv')!;
      // Bridge the real fixture socket; Playwright's connectToServer only connects
      // to the original URL and cannot redirect to a different runtime.
      const upstream = new WebSocket(base.replace('http:', 'ws:') + '/api/agent-browser' + url.search);
      upstream.binaryType = 'arraybuffer';
      upstream.onmessage = (event) =>
        ws.send(typeof event.data === 'string' ? event.data : Buffer.from(event.data as ArrayBuffer));
      ws.onMessage((message) =>
        upstream.send(typeof message === 'string' ? message : Uint8Array.from(message)),
      );
      ws.onClose(() => upstream.close());
      upstream.onclose = () => {
        void ws.close();
      };
    });
    await chat(page, 'Agent viewport test');
    const conv = browserConv;
    const response = await fetch(base + '/new?' + new URLSearchParams({ conv, url: siteURL + '/agent' }));
    expect(response.status).toBe(204);
    const pane = page.locator('.hx-agent-browser');
    await expect.poll(() => reports.get('/agent')?.w).toBe(1280);
    await waitFrame(page, 1280, 800);
    await expect(pane.getByRole('button', { name: 'Viewport size' })).toHaveText('1280×800 ▾');
    const box = (await pane.locator('canvas').boundingBox())!;
    expect(box.width).toBeLessThan(1280);
    const area = (await pane.locator('.hx-agent-browser__view').boundingBox())!;
    expect(Math.abs(box.y - area.y)).toBeLessThan(1);
    await shot(page, 'agent-fixed');
    await page.setViewportSize({ width: 1100, height: 800 });
    await page.waitForTimeout(500);
    expect(reports.get('/agent')?.w).toBe(1280);
    expect(reports.get('/agent')?.h).toBe(800);
  } finally {
    child.kill('SIGTERM');
    await new Promise<void>((resolve) => child.once('exit', () => resolve()));
  }
});

test('profile switching, Browser settings and an access ask outside the browser panel', async ({ page }) => {
  // Exercise the page contract independently of Chrome: backend profile isolation is covered by the runtime tests.
  const profiles = [
    {
      id: 'work',
      name: 'Work account',
      color: '#3377cc',
      kind: 'managed',
      createdAt: 1,
      lastUsedAt: 0,
      usedBy: [],
      path: '/data/browser/work',
      running: true,
      projects: ['demo'],
    },
    {
      id: 'other',
      name: 'Other account',
      color: '#cc7733',
      kind: 'managed',
      createdAt: 1,
      lastUsedAt: 0,
      usedBy: [],
      path: '/data/browser/other',
      running: false,
      projects: [],
    },
    {
      id: 'private',
      name: 'Private account',
      color: '#8877cc',
      kind: 'managed',
      createdAt: 1,
      lastUsedAt: 0,
      usedBy: [],
      path: '/data/browser/private',
      running: false,
      projects: [],
    },
  ];
  let current = profiles[0]!,
    hasTabs = true,
    sockets = 0;
  let asks: { id: string; conv: string; title: string; why: string; at: number }[] = [];
  const actions: Record<string, unknown>[] = [];
  await page.route('**/api/agent-browser/profiles?*', async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (request.method() === 'POST') {
      const action = request.postDataJSON() as Record<string, unknown>;
      actions.push(action);
      if (action.action === 'use') {
        current = profiles.find((p) => p.id === action.id)!;
        hasTabs = false;
      }
      if (action.action === 'answer') asks = [];
      await route.fulfill({ json: { ok: true } });
      return;
    }
    await route.fulfill({
      json: url.searchParams.has('conv')
        ? {
            profiles: profiles.slice(0, 2),
            current,
            hasTabs,
          }
        : { profiles, projects: { demo: { default: 'work', granted: ['other'] } }, asks, noSandbox: false },
    });
  });
  await page.routeWebSocket('**/api/agent-browser?*', (ws) => {
    sockets++;
    ws.send(JSON.stringify({ t: 'status', state: 'up', profile: current }));
    ws.send(
      JSON.stringify({
        t: 'tabs',
        tabs: hasTabs
          ? [
              {
                id: 'previous-tab',
                title: 'Previous account tab',
                url: 'https://example.test',
                owner: 'this',
                by: 'agent',
                viewport: { mode: 'fixed', w: 1280, h: 800, dpr: 1 },
                crashed: false,
                control: 'agent',
                agentAt: 0,
              },
            ]
          : [],
        current: hasTabs ? 'previous-tab' : '',
        agentTab: '',
        following: true,
        gen: 1,
      }),
    );
  });
  await chat(page, 'Profile access test', false);
  const picker = page.getByRole('button', { name: 'Browser profile', exact: true });
  await expect(picker).toContainText('Work account');
  await picker.click();
  await expect(page.getByRole('menuitemradio')).toHaveCount(2);
  await shot(page, 'profiles-menu');
  await page.keyboard.press('Escape');
  await picker.click();
  await page.getByRole('menuitemradio', { name: 'Other account', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Switch browser profile?' })).toBeVisible();
  expect(actions).toHaveLength(0);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(picker).toContainText('Work account');
  await picker.click();
  await page.getByRole('menuitemradio', { name: 'Other account', exact: true }).click();
  await page.getByRole('button', { name: 'Switch profile', exact: true }).click();
  await expect(picker).toContainText('Other account');
  await expect(page.getByRole('tab', { name: 'Previous account tab' })).toHaveCount(0);
  await expect.poll(() => sockets).toBeGreaterThan(1);
  expect(actions[0]).toMatchObject({ action: 'use', id: 'other' });
  await picker.click();
  await page.getByRole('menuitem', { name: 'Browser settings…', exact: true }).click();
  await expect(page.locator('.hx-set__title')).toHaveText('Browser');
  await expect(page.getByText('/data/browser/private', { exact: true })).toBeVisible();
  await shot(page, 'profiles-settings');
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByLabel('Profile source')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await shot(page, 'profiles-phone');
  await page.setViewportSize({ width: 1440, height: 900 });
  await createProjectUI(page, 'Browser UI project');
  const projectSettings = page
    .locator('.hx-sidebar')
    .getByRole('button', { name: /^Settings of/ })
    .first();
  await projectSettings.click();
  await expect(page.getByRole('group', { name: 'Browser access' })).toBeVisible();
  await shot(page, 'profiles-project-field');
  await page
    .getByRole('dialog', { name: 'Project settings', exact: true })
    .getByRole('button', { name: 'Close', exact: true })
    .click();
  // The Browser settings page has no browser panel open; asks still arrive through the shell's global host.
  asks = [
    {
      id: 'request-account',
      conv: 'requesting-chat',
      title: 'Staging login task',
      why: 'Please allow the staging account',
      at: 1,
    },
  ];
  await expect(page.getByRole('dialog', { name: 'Browser profile access' })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('Staging login task', { exact: true })).toBeVisible();
  await expect(page.getByText('Please allow the staging account', { exact: true })).toBeVisible();
  await shot(page, 'profiles-card');
  const ask = page.getByRole('dialog', { name: 'Browser profile access' });
  await ask.getByRole('combobox').selectOption('private');
  await ask.getByRole('button', { name: 'Only this chat', exact: true }).click();
  await expect(ask).toHaveCount(0);
  expect(actions.at(-1)).toMatchObject({
    action: 'answer',
    askId: 'request-account',
    id: 'private',
    scope: 'chat',
  });
});

test('real project profile switch closes old tabs and reconnects to the selected profile', async ({
  page,
}) => {
  test.setTimeout(180_000);
  let connections = 0;
  const statusProfiles: string[] = [];
  page.on('websocket', (socket) => {
    if (!new URL(socket.url()).pathname.endsWith('/api/agent-browser')) return;
    connections++;
    socket.on('framereceived', (frame) => {
      if (typeof frame.payload !== 'string') return;
      const message = JSON.parse(frame.payload) as { t: string; state?: string; profile?: { id: string } };
      if (message.t === 'status' && message.state === 'up' && message.profile)
        statusProfiles.push(message.profile.id);
    });
  });
  await ready(page);
  const projectName = 'Browser isolation project';
  const project = await createProjectUI(page, projectName);
  await page.getByRole('button', { name: `New chat in “${projectName}”`, exact: true }).click();
  await page.locator('.hx-composer__input').fill('Real profile switch');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  const conv = new URL(page.url()).pathname.split('/').at(-1)!;
  await openBrowser(page);
  const endpoint = rt.url + '/api/agent-browser/profiles?machine=local';
  const initialResponse = await page.request.get(endpoint + '&conv=' + encodeURIComponent(conv));
  expect(initialResponse.ok()).toBe(true);
  const initial = (await initialResponse.json()) as { current: { id: string; name: string } };
  const change = async (data: Record<string, unknown>) => {
    const response = await page.request.post(endpoint, { data, headers: { Origin: rt.url } });
    expect(response.ok(), await response.text()).toBe(true);
  };
  await change({ action: 'create', name: 'Second project account', color: '#b87333' });
  const settings = (await (await page.request.get(endpoint)).json()) as {
    profiles: { id: string; name: string }[];
  };
  const extra = settings.profiles.find((profile) => profile.name === 'Second project account')!;
  expect(extra).toBeTruthy();
  expect(extra.id).not.toBe(initial.current.id);
  await change({ action: 'project', project, default: initial.current.id, granted: [extra.id] });
  const pane = page.locator('.hx-agent-browser');
  await pane.getByRole('button', { name: 'New tab', exact: true }).first().click();
  await navigate(page, '/real-profile');
  await expect(pane.getByRole('tab')).toHaveCount(1);
  const before = connections;
  await pane.getByRole('button', { name: 'Browser profile', exact: true }).click();
  await page.getByRole('menuitemradio', { name: extra.name, exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Switch browser profile?' })).toBeVisible();
  await page.getByRole('button', { name: 'Switch profile', exact: true }).click();
  await expect(pane.locator('canvas')).toHaveAttribute('data-shown', 'false');
  await expect(pane.getByRole('tab')).toHaveCount(0);
  await expect.poll(() => connections).toBeGreaterThan(before);
  await expect.poll(() => statusProfiles.at(-1)).toBe(extra.id);
  await expect(pane.getByRole('button', { name: 'Browser profile', exact: true })).toContainText(extra.name);
  const selected = (await (
    await page.request.get(endpoint + '&conv=' + encodeURIComponent(conv))
  ).json()) as { current: { id: string }; hasTabs: boolean };
  expect(selected.current.id).toBe(extra.id);
  expect(selected.hasTabs).toBe(false);
  await expect(pane.getByText('No tabs', { exact: true })).toBeVisible();
});
