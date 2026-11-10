import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { CELLS, startStack, type CellName, type Stack } from './stack';

// One person's whole way through the hub, on a real hub, agent and runtime (HRN-761): sign up, an organization and its
// tenant, a team, an invitation to a second person, a machine connected by code, its page opened, a first chat with a
// stand-in for the agent, sign out and in again, a switch between organizations. The same steps in every cell of the
// matrix (E2E_CELL); what a cell does not have (organizations off, no provider) is said, not passed silently.

const cell = (process.env.E2E_CELL ?? 'selfhosted') as CellName;
const shotsDir = process.env.E2E_SHOTS_DIR ?? join('e2e-hub-results', 'shots', cell);
let n = 0;
const shot = async (page: Page, what: string) => {
  mkdirSync(shotsDir, { recursive: true });
  await page.screenshot({
    path: join(shotsDir, `${String(++n).padStart(2, '0')}-${what}.png`),
    fullPage: false,
  });
};
const note = (text: string) => test.info().annotations.push({ type: 'skipped-step', description: text });

test.describe.configure({ mode: 'serial' });
test.skip(!CELLS[cell], `unknown cell ${cell}`);

let stack: Stack;
test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack(cell);
});
test.afterAll(async () => {
  await stack?.stop();
});

/** Signs in with an emailed code; a person who is new lands on the organization form or in a space. */
async function signInByCode(page: Page, email: string) {
  await page.goto(`${stack.hub}/signin`);
  await page.locator('input[type=email]').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await page.locator('.hxa__otp').waitFor();
  await page.waitForTimeout(500); // the log line is written a moment after the answer
  await page.locator('input').first().fill(stack.emailCode(email));
  await page.waitForURL((u) => !/\/signin/.test(u.pathname), { timeout: 20_000 });
}

/** A call to the hub from inside the page: its cookies and the browser's own name resolution (the machines' test domain is mapped there). */
const api = async <T>(page: Page, method: 'get' | 'post', path: string, data?: unknown): Promise<T> => {
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

let owner: Page;
let orgsOn = false;
let slug = '';
let scope = '';
const machinesUrl = () => (orgsOn ? `${stack.hub}/o/${slug}/${scope}/machines` : '');
let spaceId = '';
let machineId = '';
let invitePath = '';
let teamId = '';

test('1. the first person signs up', async ({ browser }) => {
  owner = await (await browser.newContext()).newPage();
  await owner.goto(`${stack.hub}/signin`);
  await owner.locator('.hxa__card').waitFor();
  const config = await api<{ orgs: string }>(owner, 'get', '/api/config');
  orgsOn = config.orgs === 'on';
  await shot(owner, 'signin');
  // The first account of an installation is its administrator: the address is in HUB_ADMIN_EMAILS in every cell.
  await signInByCode(owner, 'owner@example.com');
  await shot(owner, 'signed-in');
});

test('2. an organization and its tenant', async () => {
  if (!orgsOn) {
    note('organizations are not on in this cell: the person has a space of their own');
    const me = await api<{ spaces: { id: string }[] }>(owner, 'get', '/api/me');
    spaceId =
      me.spaces[0]?.id ??
      (await api<{ space: { id: string } }>(owner, 'post', '/api/spaces', { name: 'Main' })).space.id;
    return;
  }
  await owner.goto(`${stack.hub}/org/new`);
  await owner.locator('.hxa__card').waitFor();
  await owner.locator('input.hx-set__input').first().fill('Example Org');
  await owner.locator('input.hx-set__input').nth(1).fill('Main');
  await shot(owner, 'org-new');
  await owner.getByRole('button', { name: 'Создать организацию' }).click();
  await owner.waitForURL(/\/o\//, { timeout: 20_000 });
  const [orgSlug, firstScope] = owner.url().split('/o/')[1].split(/[/?#]/);
  slug = orgSlug;
  const org = await api<{ tenants: { id: string }[] }>(owner, 'get', `/api/orgs/${slug}`);
  spaceId = org.tenants[0].id;
  scope = firstScope || ((org.tenants[0] as { slug?: string }).slug ?? spaceId);
  await shot(owner, 'org-home');
});

test('3. a team and an invitation for a second person', async () => {
  if (orgsOn) {
    const t = await api<{ team?: { id: string }; id?: string }>(owner, 'post', `/api/orgs/${slug}/teams`, {
      tenantId: spaceId,
      name: 'Core',
    });
    teamId = (t.team ?? t).id as string;
  } else note('no teams without organizations');
  const inv = await api<{ invite?: { url: string }; url?: string }>(
    owner,
    'post',
    `/api/spaces/${spaceId}/invites/link`,
    { maxUses: 3, ...(teamId ? { teamId } : {}) },
  );
  invitePath = new URL((inv.invite ?? inv).url as string).pathname;
  expect(invitePath).toMatch(/^\/invite\//);
});

test('4. the second person accepts: by the provider where there is one, else by email', async ({
  browser,
}) => {
  const guest = await (await browser.newContext()).newPage();
  await guest.goto(`${stack.hub}${invitePath}`);
  await guest.locator('.hxa__card').waitFor();
  await shot(guest, 'invite');
  await guest.getByRole('link', { name: /Войти и присоединиться/ }).click();
  if (stack.cell.oidc) {
    await guest.getByRole('button', { name: /Через Example SSO/ }).click();
    await guest.waitForURL((u) => /invite/.test(u.pathname) || !/\/signin/.test(u.pathname), {
      timeout: 20_000,
    });
  } else {
    await guest.locator('input[type=email]').fill('guest@example.com');
    await guest.getByRole('button', { name: 'Получить код' }).click();
    await guest.locator('.hxa__otp').waitFor();
    await guest.waitForTimeout(500);
    await guest.locator('input').first().fill(stack.emailCode('guest@example.com'));
    await guest.waitForURL(/invite/, { timeout: 20_000 });
  }
  await guest.locator('.hxa__card').waitFor();
  await shot(guest, 'invite-signed-in');
  await guest.getByRole('button', { name: 'Присоединиться' }).click();
  await guest.waitForURL((u) => !/invite/.test(u.pathname), { timeout: 20_000 });
  await shot(guest, 'invite-joined');
  const me = await api<{ spaces: { id: string }[] }>(guest, 'get', '/api/me');
  expect(me.spaces.map((s) => s.id)).toContain(spaceId);
  await guest.context().close();
});

test('5. a machine is connected by its code', async () => {
  const reg = await api<{ code: string }>(owner, 'post', `/api/spaces/${spaceId}/machines/codes`, {
    profile: 'full',
    confirmFull: true,
  });
  const m = await stack.registerMachine({ code: reg.code, name: 'journey-box' });
  machineId = m.machineId;
  if (orgsOn) {
    await owner.goto(machinesUrl());
    await expect(owner.getByText('journey-box').first()).toBeVisible();
  }
  await expect
    .poll(
      async () =>
        (await api<{ machine: { status: string } }>(owner, 'get', `/api/machines/${machineId}`)).machine
          .status,
      { timeout: 60_000 },
    )
    .toBe('online');
  await shot(owner, 'machines');
});

test('6. its page opens (a machine may have to be trusted first)', async () => {
  if (!orgsOn) {
    note('no organizations: the page is opened by its address, not from the machines list');
    await owner.goto(`${stack.hub}${stack.cell.runtimeDomain ? '' : `/m/${machineId}/`}`);
    (globalThis as { __rt?: Page }).__rt = owner;
    const chat = owner.getByRole('button', { name: /chat|чат/i }).first();
    await expect(async () => {
      if (!(await chat.isVisible())) await owner.reload();
      await expect(chat).toBeVisible({ timeout: 5_000 });
    }).toPass({ timeout: 90_000 });
    return;
  }
  await owner.goto(machinesUrl());
  await owner.getByText('journey-box').first().click();
  const trust = owner.getByRole('button', { name: 'Доверять машине' });
  if (await trust.isVisible().catch(() => false)) {
    await shot(owner, 'machine-waits-for-trust');
    await trust.click();
  }
  // Trusting makes the agent call the hub again; the page opened in that moment says "the machine is off".
  await expect
    .poll(
      async () =>
        (await api<{ machine: { status: string } }>(owner, 'get', `/api/machines/${machineId}`)).machine
          .status,
      { timeout: 60_000 },
    )
    .toBe('online');
  const open = owner
    .getByRole('button', { name: /^Открыть/ })
    .or(owner.getByRole('link', { name: /^Открыть/ }));
  await expect(open.first()).toBeVisible();
  const popup = owner
    .context()
    .waitForEvent('page', { timeout: 8_000 })
    .catch(() => null);
  // The button sits in the row under the detail layer on a short viewport, so it is clicked as the page does, not by pointer.
  await open.first().dispatchEvent('click');
  const page = (await popup) ?? owner;
  await page.waitForLoadState('domcontentloaded');
  const want = stack.cell.runtimeDomain ? `${machineId}.${stack.cell.runtimeDomain}` : `/m/${machineId}/`;
  await expect.poll(() => page.url(), { timeout: 30_000 }).toContain(want);
  const chat = page.getByRole('button', { name: /chat|чат/i }).first();
  await expect(async () => {
    if (!(await chat.isVisible())) await page.reload();
    await expect(chat).toBeVisible({ timeout: 5_000 });
  }).toPass({ timeout: 90_000 });
  await shot(page, 'runtime-open');
  test.info().annotations.push({ type: 'runtime-url', description: page.url() });
  (globalThis as { __rt?: Page }).__rt = page;
});

test('7. a first chat with the stand-in agent', async () => {
  const page = (globalThis as { __rt?: Page }).__rt!;
  await page
    .getByRole('button', { name: /new chat|новый чат/i })
    .first()
    .click();
  const box = page.getByRole('textbox').last();
  await box.fill('hello');
  await box.press('Enter');
  await expect(page.locator('body')).toContainText(/\S/, { timeout: 60_000 });
  await shot(page, 'first-chat');
});

test('8. sign out and sign in again', async () => {
  // With organizations the sign-out button is in the shell; without them it is on the settings page.
  await owner.goto(orgsOn ? machinesUrl() : `${stack.hub}/settings`);
  await owner.getByRole('button', { name: 'Выйти' }).click();
  await owner.waitForURL(/\/signin/, { timeout: 20_000 });
  await shot(owner, 'signed-out');
  await signInByCode(owner, 'owner@example.com');
  await shot(owner, 'signed-in-again');
});

test('9. a second organization and a switch between them', async () => {
  if (!orgsOn) return note('no organizations in this cell');
  await owner.goto(`${stack.hub}/org/new`);
  await owner.locator('.hxa__card').waitFor();
  await owner.locator('input.hx-set__input').first().fill('Second Org');
  await owner.locator('input.hx-set__input').nth(1).fill('Side');
  await owner.getByRole('button', { name: 'Создать организацию' }).click();
  await owner.waitForURL((u) => /\/o\//.test(u.pathname) && !u.pathname.includes(`/o/${slug}`), {
    timeout: 20_000,
  });
  await shot(owner, 'second-org');
  await owner.goto(`${stack.hub}/o/${slug}/`);
  await expect(owner.locator('body')).toContainText('Example Org', { timeout: 20_000 });
  await shot(owner, 'back-to-first');
});
