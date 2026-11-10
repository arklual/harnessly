import { expect, test, type Browser, type Page, type Response } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { startStack, type Stack } from './stack';

// HRN-879, spec v10 («смотреть — только через хаб»): the machine is switched off, and the person still has the whole
// chat. A real hub with its copy of the chats (HUB_HISTORY, on by default), the fleet's workspace at /agents/
// (runtime.fleet), one machine with the stand-in agent. A chat is made while the machine is on; then its agent and
// runtime go, and the page, opened anew, draws the list and the feed from the hub alone, and a message waits.
// One cell is enough: what is checked is the page and the hub's copy, not the hub's settings.

const cell = process.env.E2E_CELL ?? 'dev';
const shotsDir = process.env.E2E_SHOTS_DIR ?? join('e2e-hub-results', 'shots', cell);
const shot = async (page: Page, what: string) => {
  mkdirSync(shotsDir, { recursive: true });
  await page.screenshot({ path: join(shotsDir, `offline-${what}.png`), fullPage: false });
};

test.describe.configure({ mode: 'serial' });
test.skip(cell !== 'dev', 'the machine-off chat is checked in the dev cell only');

let stack: Stack;
let page: Page;
let machineId = '';
/** Where the page reads the hub's list of chats (…/history/conversations), seen in hubList. */
let listUrl = '';

test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack('dev', { HUB_FEATURES_DEFAULT: 'ui.hx,runtime.fleet' });
});
test.afterAll(async () => {
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

const machineStatus = async () =>
  (await api<{ machine: { status: string } }>('get', `/api/machines/${machineId}`)).machine.status;

/** The hub's list of chats as the page reads it (GET …/history/conversations), asked by opening the page anew. */
/**
 * Another device of the same person: the session's cookies and nothing the first browser kept of the chats (its
 * IndexedDB drew the feed by itself, which says nothing about where the page reads from).
 */
async function anotherDevice(browser: Browser): Promise<void> {
  const cookies = await page.context().cookies();
  page = await (await browser.newContext({ storageState: { cookies, origins: [] } })).newPage();
  // On the hub's origin, for the calls to its API; the page itself is opened by the step.
  await page.goto(`${stack.hub}/api/config`);
}

async function hubList(): Promise<{ conversations: { id: string; title?: string }[] }> {
  let list: { conversations: { id: string; title?: string }[] } = { conversations: [] };
  // A fresh page of the hub with its home machine hung can take a while to start (seen locally: a blank page for
  // 30 s, the hub's own matter, not the copy's): it is opened again until the list comes.
  await expect(async () => {
    const answered = page.waitForResponse(
      (r: Response) =>
        /\/history\/conversations$/.test(new URL(r.url()).pathname) && r.request().method() === 'GET',
      { timeout: 30_000 },
    );
    await page.goto(`${stack.hub}/agents/`);
    const res = await answered;
    listUrl = res.url();
    list = (await res.json()) as typeof list;
  }).toPass({ timeout: 120_000 });
  return list;
}

test('1. a person signs in and connects a machine', async ({ browser }) => {
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
  machineId = (await stack.registerMachine({ code: reg.code, name: 'offline-box' })).machineId;
  await expect.poll(machineStatus, { timeout: 60_000 }).toBe('online');
});

test('2. a chat is made while the machine is on, and the hub keeps it', async () => {
  const fresh = page.getByRole('button', { name: /new chat|новый чат/i }).first();
  await expect(async () => {
    await page.goto(`${stack.hub}/agents/`);
    await expect(fresh).toBeVisible({ timeout: 10_000 });
  }).toPass({ timeout: 120_000 });
  await fresh.click();
  const box = page.getByRole('textbox').last();
  await box.fill('hello from the machine');
  await box.press('Enter');
  await expect(page.getByText('Fake agent reply: hello from the machine').first()).toBeVisible({
    timeout: 90_000,
  });
  // A tool whose output is past the 4 KB an event carries (HRN-879).
  await box.fill('bigoutput please');
  await box.press('Enter');
  await expect(page.getByText('Fake agent reply: bigoutput please').first()).toBeVisible({
    timeout: 90_000,
  });
  // The agent sends the chat to the hub on its own beat: the copy has it before the machine goes.
  await expect
    .poll(async () => (await hubList()).conversations.length, { timeout: 90_000, intervals: [2_000] })
    .toBeGreaterThan(0);
  // …and the tool's whole output, read from the runtime apart from the stream.
  let seen = '';
  await expect
    .poll(
      async () => {
        const { conversations } = await api<{ conversations: { id: string }[] }>('get', listUrl);
        seen = `${conversations.length} chats`;
        for (const c of conversations) {
          const chat = await api<{ entries: { id: string; t: string; output?: { truncated: unknown } }[] }>(
            'get',
            `${listUrl}/${encodeURIComponent(c.id)}`,
          );
          const tools = chat.entries.filter((x) => x.t === 'tool');
          seen += `; ${c.id}: ${tools.length} tools, ${JSON.stringify(tools.map((x) => x.output?.truncated ?? null))}`;
          for (const e of tools.filter((x) => x.output?.truncated)) {
            const whole = await api<{ output: { text: string; truncated: unknown } }>(
              'get',
              `${listUrl}/${encodeURIComponent(c.id)}/entries/${encodeURIComponent(e.id)}/output`,
            );
            seen += ` whole ${whole.output.text.length} ${JSON.stringify(whole.output.truncated)}`;
            if (whole.output.text.includes('END-OF-BIG-OUTPUT')) return true;
          }
        }
        console.log(`the hub's copy: ${seen}`);
        return false;
      },
      { timeout: 90_000, intervals: [2_000] },
    )
    .toBe(true);
  await shot(page, 'on');
});

test('3. the runtime hangs while its agent is on: the chat still opens whole from the hub', async ({
  browser,
}) => {
  // The case of a machine under load: its agent keeps the link and the hub calls it online, but the runtime answers
  // nothing. Reading goes through the hub, so the chat opens all the same (spec v10).
  stack.hangRuntime(true);
  try {
    await anotherDevice(browser);
    // The cluster's gateway calls a machine off only after 45 s without its beats; the hub knows
    // sooner. The list says «online» here as the gateway would in that window: the page must not wait on the machine.
    await page.route(/\/history\/conversations$/, async (route) => {
      const res = await route.fetch();
      const body = (await res.json()) as { machines?: { state: string }[] };
      for (const m of body.machines ?? []) m.state = 'online';
      await route.fulfill({ response: res, json: body });
    });
    expect(await machineStatus()).toBe('online');
    const list = await hubList();
    expect(list.conversations.length).toBeGreaterThan(0);
    const row = page.getByText('hello from the machine').first();
    await expect(row).toBeVisible({ timeout: 30_000 });
    await row.click();
    await expect(page.getByText('Fake agent reply: hello from the machine').first()).toBeVisible({
      timeout: 20_000,
    });
    await shot(page, 'hung-chat');
  } finally {
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    stack.hangRuntime(false);
  }
});

test('4. the machine is off: the list and the whole chat open from the hub, a message waits', async ({
  browser,
}) => {
  await stack.stopMachine();
  await expect.poll(machineStatus, { timeout: 120_000, intervals: [2_000] }).not.toBe('online');
  await anotherDevice(browser);

  // A page opened anew, with nothing of the machine to ask: the list is the hub's.
  const list = await hubList();
  expect(list.conversations.length).toBeGreaterThan(0);
  const row = page.getByText('hello from the machine').first();
  await expect(row).toBeVisible({ timeout: 30_000 });
  await shot(page, 'off-list');

  // The chat opens whole: the person's message and the agent's answer, no «it opens when the machine answers».
  await row.click();
  await expect(page.getByText('Fake agent reply: hello from the machine').first()).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByText(/откроется, когда машина|когда машина ответит/i)).toHaveCount(0);
  await shot(page, 'off-chat');

  // A tool's output past 4 KB opens whole: «show all» reads the rest from the hub, not from the machine.
  await expect(page.getByText('Fake agent reply: bigoutput please').first()).toBeVisible({ timeout: 30_000 });
  // The turn's steps are folded under «Worked … · N actions».
  await page
    .getByText(/\d+ (actions|действ)/)
    .last()
    .click();
  await page.locator('.hx-tool__head', { hasText: 'cat big.log' }).last().click();
  await expect(page.getByText(/Показано \d+ из \d+|Showing \d+ of \d+/).first()).toBeVisible({
    timeout: 15_000,
  });
  await expect(page.getByText('END-OF-BIG-OUTPUT')).toHaveCount(0);
  await page
    .getByRole('button', { name: /Показать всё|Show all/ })
    .last()
    .click();
  await expect(page.getByText('END-OF-BIG-OUTPUT').first()).toBeVisible({ timeout: 15_000 });
  await shot(page, 'off-whole-output');

  // A message to the machine that is off is kept and waits for it: it is on screen, not lost.
  const box = page.getByRole('textbox').last();
  await expect(box).toBeEditable({ timeout: 15_000 });
  await box.fill('while the machine sleeps');
  await box.press('Enter');
  await expect(page.getByText('while the machine sleeps').first()).toBeVisible({ timeout: 15_000 });
  await expect(
    page.getByText(/Ждёт машину|Отправится, когда|Waiting for the machine|Goes out when/).first(),
  ).toBeVisible({
    timeout: 90_000,
  });
  await shot(page, 'off-queued');
});
