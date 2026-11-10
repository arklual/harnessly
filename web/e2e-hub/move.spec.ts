import { expect, test, type Page } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { RELEASES } from '../app/next/features/whats-new/releases';
import { startStack, type Stack } from './stack';

// HRN-883: a machine of an older install, its data still on its disk (the files people put into chats, the handoff
// notes), connects to a hub that keeps a store. Its runtime moves the data into the hub (docs/migrations/0001-data-to-hub.md),
// the hub keeps the journal, «Машины» shows how far it is. One object is broken (a folder where a file should be): the
// move stops on it with its code, the card offers «Повторить» and «Продолжить с агентом», the agent's chat opens with
// the brief; once the cause is fixed, «Повторить» finishes the move, and the hub keeps it with the machine off.

const cell = process.env.E2E_CELL ?? 'dev';
const shotsDir = process.env.E2E_SHOTS_DIR ?? join('e2e-hub-results', 'shots', cell);
const shot = async (page: Page, what: string) => {
  mkdirSync(shotsDir, { recursive: true });
  await page.screenshot({ path: join(shotsDir, `move-${what}.png`), fullPage: false });
};

test.describe.configure({ mode: 'serial' });
test.skip(cell !== 'dev', 'the move is checked in the dev cell only');

let stack: Stack;
let page: Page;
let machineId = '';
let dataDir = '';

interface IMove {
  total: number;
  verified: number;
  pending: number;
  failed: number;
  skipped: number;
  failures?: { kind: string; object: string; code?: string }[];
}

test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack('dev', { HUB_FEATURES_DEFAULT: 'ui.hx,runtime.fleet' });
});
test.afterAll(async () => {
  await stack?.stop();
});

const call = async (method: 'get' | 'post', path: string, data?: unknown) =>
  page.evaluate(
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

const api = async <T>(method: 'get' | 'post', path: string, data?: unknown): Promise<T> => {
  const res = await call(method, path, data);
  expect(res.ok, `${method} ${path}: ${res.status} ${res.text}`).toBeTruthy();
  return JSON.parse(res.text) as T;
};

/** The hub's journal of the machine's move; null until the machine reported. */
const hubMove = async (): Promise<IMove | null> => {
  const res = await call('get', `/api/machines/${machineId}/migration`);
  return res.ok ? (JSON.parse(res.text) as IMove) : null;
};

const brokenUpload = 'u-broken';
const retry = /^(Retry|Повторить)$/;
const withAgent = /^(Continue with an agent|Продолжить с агентом)$/;

/** An older install's data folder: two uploads with their refs, a handoff note, and one upload that cannot be read. */
const oldInstall = (dir: string) => {
  const uploads = join(dir, 'v2', 'uploads');
  const drafts = join(dir, 'v2', 'handoffs', 'drafts');
  mkdirSync(uploads, { recursive: true });
  mkdirSync(drafts, { recursive: true });
  const upload = (id: string, name: string, body: string | null) => {
    if (body === null) mkdirSync(join(uploads, id), { recursive: true });
    else writeFileSync(join(uploads, id), body);
    writeFileSync(join(uploads, `${id}.json`), JSON.stringify({ id, name, mime: 'text/plain' }));
  };
  upload('u-notes', 'notes.txt', 'the notes a person put into a chat\n');
  upload('u-plan', 'plan.md', '# plan\n\n1. move the data\n');
  upload(brokenUpload, 'broken.txt', null);
  writeFileSync(join(drafts, 'h-1.jsonl'), '{"t":"handoff","text":"carry on with the move"}\n');
};

test('1. a machine of an older install connects, and its data moves into the hub', async ({ browser }) => {
  page = await (await browser.newContext()).newPage();
  // «Что нового» is seen: its card would lie over the buttons.
  await page.addInitScript((id) => localStorage.setItem('hx:whats-new', id), RELEASES[0]!.id);
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
  const m = await stack.registerMachine({
    code: reg.code,
    name: 'old-box',
    seed: oldInstall,
    runtimeEnv: { RUNTIME_DATA_MOVE_DELAY: '1s' },
  });
  machineId = m.machineId;
  dataDir = m.dataDir;

  // Three objects reach the hub; the broken one stops with its code, nothing else waits.
  let last: IMove | null = null;
  await expect
    .poll(
      async () => {
        last = await hubMove();
        return last && `${last.verified}/${last.total} failed ${last.failed} pending ${last.pending}`;
      },
      { timeout: 180_000, intervals: [2_000] },
    )
    .toBe('3/4 failed 1 pending 0');
  expect(last!.failures?.map((f) => `${f.kind}/${f.object} ${f.code}`)).toEqual([
    `uploads/${brokenUpload} MIG-READ`,
  ]);
});

test('2. «Машины» shows that the move stopped, with the code and the two buttons', async () => {
  await expect(async () => {
    await page.goto(`${stack.hub}/agents/settings/machines`);
    await expect(page.locator('.hx-mmove[data-stopped="true"]')).toBeVisible({ timeout: 15_000 });
  }).toPass({ timeout: 120_000 });
  // The runtime's page speaks the runtime's language (RUNTIME_LOCALE=en in the stack).
  const card = page.locator('.hx-mmove');
  await expect(card).toContainText(
    /stopped: 1 object failed \(3 of 4 in the hub\)|остановился: 1 объект с ошибкой \(3 из 4/,
  );
  await expect(card).toContainText('MIG-READ ×1');
  await expect(card).toContainText(`uploads/${brokenUpload}`);
  await expect(card.getByRole('button', { name: retry })).toBeVisible();
  await shot(page, 'stopped');
});

test('3. «Продолжить с агентом» opens a chat on the machine with the brief', async () => {
  await page.locator('.hx-mmove').getByRole('button', { name: withAgent }).click();
  const box = page.getByRole('textbox').last();
  await expect(box).toBeVisible({ timeout: 20_000 });
  await expect(box).toHaveValue(/harnessly migrate status --json/, { timeout: 20_000 });
  await expect(box).toHaveValue(new RegExp(`uploads/${brokenUpload}: MIG-READ`));
  await expect(box).toHaveValue(/0001-data-to-hub/);
  // Sent, it is a chat on that machine; the chat says it too, in the bar above the composer, with the same buttons.
  await box.press('Enter');
  await expect(page.getByText(/Fake agent reply/).first()).toBeVisible({ timeout: 90_000 });
  const bar = page.locator('.hx-dockbar').filter({ hasText: 'MIG-READ' });
  await expect(bar).toBeVisible({ timeout: 30_000 });
  await expect(bar.getByRole('button', { name: retry })).toBeVisible();
  await shot(page, 'agent-brief');
});

test('4. the cause is fixed, «Повторить» finishes the move', async () => {
  // What the agent would do on the machine: the folder in place of the file goes, the file comes back.
  const path = join(dataDir, 'v2', 'uploads', brokenUpload);
  rmSync(path, { recursive: true, force: true });
  writeFileSync(path, 'the file is back\n');

  await page.goto(`${stack.hub}/agents/settings/machines`);
  const card = page.locator('.hx-mmove[data-stopped="true"]');
  await expect(card).toBeVisible({ timeout: 60_000 });
  await card.getByRole('button', { name: retry }).click();
  await expect
    .poll(
      async () => {
        const mv = await hubMove();
        return mv && `${mv.verified}/${mv.total} failed ${mv.failed}`;
      },
      { timeout: 120_000, intervals: [2_000] },
    )
    .toBe('4/4 failed 0');
  // Over: the card has nothing more to say.
  await page.goto(`${stack.hub}/agents/settings/machines`);
  await expect(page.locator('.hx-mrow-wrap').first()).toBeVisible({ timeout: 30_000 });
  await expect(page.locator('.hx-mmove')).toHaveCount(0, { timeout: 30_000 });
  await shot(page, 'done');
});

test('5. the machine is off: the hub still has every moved object', async () => {
  await stack.stopMachine();
  const mv = await hubMove();
  expect(mv && `${mv.verified}/${mv.total} failed ${mv.failed} pending ${mv.pending}`).toBe(
    '4/4 failed 0 pending 0',
  );
});
