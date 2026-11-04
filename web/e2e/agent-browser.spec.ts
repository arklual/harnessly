/// <reference lib="dom" />
import { createServer, type Server } from 'node:http';
import { mkdirSync, readFileSync } from 'node:fs';
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
    res.end(fixture);
  });
  await new Promise<void>((ok) => site.listen(port, '127.0.0.1', ok));
  if (screenshots) mkdirSync(screenshots, { recursive: true });
});
test.afterAll(async () => {
  // Hosts intentionally outlive the runtime, so terminate only the host in this test's temporary data directory.
  try {
    process.kill(
      Number(
        readFileSync(join(rt.home, '.harnessly', 'agent-browser', 'profiles', 'common', 'host.pid'), 'utf8'),
      ),
      'SIGTERM',
    );
  } catch {
    /* no host was started */
  }
  await rt?.stop();
  await new Promise<void>((ok) => site?.close(() => ok()));
});
async function shot(page: Page, name: string) {
  if (screenshots) await page.screenshot({ path: join(screenshots, `${name}.png`), animations: 'disabled' });
}
async function chat(page: Page, message: string, empty = true) {
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
  await page.getByRole('button', { name: 'New chat', exact: true }).first().click();
  await page.locator('.hx-composer__input').fill(message);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  if (!(await page.locator('.hx-pane').isVisible())) await page.locator('.hx-header__tools').click();
  await page.locator('.hx-pane .hx-tab[data-home]').click();
  await page
    .locator('.hx-tl__tile')
    .filter({ has: page.locator('[data-i="agent-browser"]') })
    .click();
  if (empty) await expect(page.getByText('No tabs', { exact: true })).toBeVisible({ timeout: 90_000 });
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
  const box = (await page.locator('.hx-agent-browser__view').boundingBox())!;
  const w = Math.max(320, Math.floor(box.width)),
    h = Math.max(240, Math.floor(box.height));
  await expect.poll(() => reports.get(name)?.w).toBe(w);
  await expect.poll(() => reports.get(name)?.h).toBe(h);
  await waitFrame(page, w, h);
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
  await chat(page, 'Browser test');
  const pane = page.locator('.hx-agent-browser');
  await pane.getByRole('button', { name: 'New tab', exact: true }).first().click();
  await expect(pane.locator('.hx-tbrowser__url')).toBeFocused();
  await navigate(page, '/first');
  for (const name of ['/second', '/third']) {
    await pane.getByRole('button', { name: 'New tab', exact: true }).click();
    await navigate(page, name);
  }
  await expect(pane.getByRole('tab')).toHaveCount(3);
  await shot(page, 'three-tabs');
  await pane.getByRole('button', { name: 'Close tab: Browser fixture' }).last().click();
  await expect(pane.getByRole('tab')).toHaveCount(2);
  await pane.getByRole('tab').last().click({ button: 'middle' });
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
