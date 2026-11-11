import { expect, test, type Browser, type Locator, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { CELLS, startStack, type CellName, type Stack } from './stack';

// Several people in one organization on a real hub, agent and runtime: the owner connects a machine, invites people
// into the tenant and its team, makes one of them a lead; then each of them walks every screen of their menu, and the
// member and the lead open the machine and write in it. Every screen's failed calls and page errors are written to
// e2e-hub-results/team-<cell>.json: the report of «screen, button, works or not».

const cell = (process.env.E2E_CELL ?? 'cloud') as CellName;
const shotsDir = process.env.E2E_SHOTS_DIR ?? join('e2e-hub-results', 'shots-team', cell);
let n = 0;
const shot = async (page: Page, what: string) => {
  mkdirSync(shotsDir, { recursive: true });
  await page.screenshot({ path: join(shotsDir, `${String(++n).padStart(2, '0')}-${what}.png`) });
};

interface Row {
  who: string;
  screen: string;
  action: string;
  ok: boolean;
  detail?: string;
}
const rows: Row[] = [];
const record = (r: Row) => {
  rows.push(r);
  test.info().annotations.push({
    type: r.ok ? 'ok' : 'FAIL',
    description: `${r.who} · ${r.screen} · ${r.action}${r.detail ? ` — ${r.detail}` : ''}`,
  });
};

test.describe.configure({ mode: 'serial' });
// Organizations with teams live in the hosted service and in a company's own hub; the other cells have none.
test.skip(!['cloud', 'selfhosted'].includes(cell) || !CELLS[cell], `no organizations in cell ${cell}`);

// The public hub's features (the prod values of the hub), unless a run asks for others.
const features = process.env.E2E_FEATURES ?? 'ui.hx,env,catalog,machine.resources,org.events';

let stack: Stack;
test.beforeAll(async () => {
  test.setTimeout(600_000);
  stack = await startStack(cell, { HUB_FEATURES_DEFAULT: features, HUB_ACCESS_ENFORCE: 'on' });
});
test.afterAll(async () => {
  mkdirSync('e2e-hub-results', { recursive: true });
  writeFileSync(join('e2e-hub-results', `team-${cell}.json`), JSON.stringify(rows, null, 2));
  await stack?.stop();
});

const api = async <T>(page: Page, method: string, path: string, data?: unknown): Promise<T> => {
  const res = await page.evaluate(
    async ([m, u, body]) => {
      const r = await fetch(u as string, {
        method: m as string,
        headers: body === null ? {} : { 'content-type': 'application/json' },
        body: body === null ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
      });
      return { ok: r.ok, status: r.status, text: await r.text() };
    },
    [method, path, data === undefined ? null : data] as const,
  );
  expect(res.ok, `${method} ${path}: ${res.status} ${res.text}`).toBeTruthy();
  return (res.text ? JSON.parse(res.text) : {}) as T;
};

async function signIn(page: Page, email: string) {
  if (stack.cell.oidc) {
    // The company's provider approves at once as the person its cookie names: one provider, several people, each in a
    // browser of their own. (A route that adds login_hint misses the authorize request: it comes by a redirect.)
    await page
      .context()
      .addCookies([{ name: 'fake_oidc_email', value: encodeURIComponent(email), url: stack.oidc }]);
    await page.getByRole('button', { name: /Через Example SSO/ }).click();
    return;
  }
  await page.locator('input[type=email]').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await page.locator('.hxa__otp').waitFor();
  await page.waitForTimeout(500);
  await page.locator('input').first().fill(stack.emailCode(email));
}

/** A person of their own browser, with the failed hub calls and page errors of what they do collected. */
interface Person {
  name: string;
  email: string;
  page: Page;
  id: string;
  errors: string[];
  /** What a machine's own page asks of the hub's workspace and gets 404 for (see CENTRAL_ONLY). */
  central: Set<string>;
}

/**
 * The chat history, the move of a chat and the chats that wait for a machine are the hub's, served on the
 * workspace's way in only: a machine's own page runs the machine's code (its own origin in the cloud), so the hub
 * does not hand it the person's history or queue of every machine. Its 404 there is the answer the page expects (it
 * asks once and reads «not here»), not a failure.
 */
const CENTRAL_ONLY =
  /^\/m\/[^/]+\/api\/(platform|v2)\/(history\/(conversations|sidebar)$|chat-move\/status$|offline-chats$)/;

/** Puts a failed request of a person's page among the errors, or among the central-only answers. */
const noteFailed = (p: Person, method: string, path: string, search: string, status: number) => {
  if (status === 404 && CENTRAL_ONLY.test(path)) p.central.add(path.replace(/^\/m\/[^/]+/, ''));
  else p.errors.push(`${method} ${path}${search} → ${status}`);
};
async function person(browser: Browser, name: string, email: string): Promise<Person> {
  const page = await (await browser.newContext()).newPage();
  const p: Person = { name, email, page, id: '', errors: [], central: new Set() };
  page.on('response', (r) => {
    const u = new URL(r.url());
    if (r.status() >= 400 && (u.pathname.startsWith('/api/') || u.pathname.startsWith('/m/')))
      noteFailed(p, r.request().method(), u.pathname, u.search, r.status());
  });
  page.on('pageerror', (e) => p.errors.push(`pageerror: ${e.message.slice(0, 200)}`));
  return p;
}
const drain = (p: Person) => p.errors.splice(0).join('; ');

let owner: Person, lead: Person, member: Person, plain: Person;
let slug = '';
let tenantSlug = '';
let spaceId = '';
let teamId = '';
let machineId = '';
const at = (section: string) => `${stack.hub}/o/${slug}/${tenantSlug}/${section}`;

test('1. the owner signs up, makes the organization and connects a machine', async ({ browser }) => {
  owner = await person(browser, 'owner', 'owner@example.com');
  await owner.page.goto(`${stack.hub}/signin`);
  await owner.page.locator('.hxa__card').waitFor();
  await signIn(owner.page, owner.email);
  await owner.page.waitForURL((u) => !/\/signin/.test(u.pathname), { timeout: 20_000 });
  await owner.page.goto(`${stack.hub}/org/new`);
  await owner.page.locator('input.hx-set__input').first().fill('Acme');
  await owner.page.locator('input.hx-set__input').nth(1).fill('Main');
  await owner.page.getByRole('button', { name: 'Создать организацию' }).click();
  await owner.page.waitForURL(/\/o\//, { timeout: 20_000 });
  [slug, tenantSlug] = owner.page.url().split('/o/')[1].split(/[/?#]/);
  const org = await api<{ tenants: { id: string; slug: string }[] }>(owner.page, 'GET', `/api/orgs/${slug}`);
  spaceId = org.tenants[0].id;
  tenantSlug = org.tenants[0].slug || tenantSlug;
  owner.id = (await api<{ user: { id: string } }>(owner.page, 'GET', '/api/me')).user.id;
  const t = await api<{ team?: { id: string }; id?: string }>(owner.page, 'POST', `/api/orgs/${slug}/teams`, {
    tenantId: spaceId,
    name: 'Core',
  });
  teamId = (t.team ?? t).id as string;
  const reg = await api<{ code: string }>(owner.page, 'POST', `/api/spaces/${spaceId}/machines/codes`, {
    profile: 'full',
    confirmFull: true,
  });
  machineId = (await stack.registerMachine({ code: reg.code, name: 'team-box' })).machineId;
  await expect
    .poll(
      async () =>
        (await api<{ machine: { status: string } }>(owner.page, 'GET', `/api/machines/${machineId}`)).machine
          .status,
      { timeout: 60_000 },
    )
    .toBe('online');
  record({
    who: 'owner',
    screen: 'signup → org/new → machine code',
    action: 'organization, team, machine online',
    ok: true,
  });
});

/** Accepts an invitation link as a new person; returns them signed in. */
async function accept(browser: Browser, name: string, email: string, link: string): Promise<Person> {
  const p = await person(browser, name, email);
  await p.page.goto(`${stack.hub}${new URL(link).pathname}`);
  await p.page.locator('.hxa__card').waitFor();
  await p.page.getByRole('link', { name: /Войти и присоединиться/ }).click();
  await signIn(p.page, email);
  await p.page.waitForURL(/invite/, { timeout: 20_000 });
  await p.page.getByRole('button', { name: 'Присоединиться' }).click();
  await p.page.waitForURL((u) => !/invite/.test(u.pathname), { timeout: 20_000 });
  p.id = (await api<{ user: { id: string } }>(p.page, 'GET', '/api/me')).user.id;
  return p;
}

/** The owner invites through «Команды» → «Пригласить»: by mail (the cell has no mail, so the sheet shows the link) or by a link. */
async function inviteFromScreen(how: 'mail' | 'link', team: string | null, email?: string): Promise<string> {
  const pg = owner.page;
  drain(owner);
  await pg.goto(at('teams'));
  await pg.getByRole('button', { name: 'Пригласить' }).first().click();
  const sheet = pg
    .getByRole('dialog', { name: 'Пригласить в тенант' })
    .or(pg.locator('.md').filter({ hasText: 'Пригласить в тенант' }))
    .first();
  await sheet.waitFor();
  const teamGroup = sheet.getByRole('radiogroup', { name: 'Команда' });
  if (await teamGroup.isVisible().catch(() => false))
    await teamGroup.getByRole('radio', { name: team ?? 'Без команды' }).click();
  if (how === 'mail') {
    await sheet.getByLabel('Почта коллеги').fill(email!);
    await sheet.getByRole('button', { name: 'Отправить' }).click();
  } else {
    await sheet.getByRole('button', { name: 'Создать ссылку' }).click();
  }
  const link = sheet.getByLabel('Ссылка');
  const shown = await link.waitFor({ timeout: 15_000 }).then(
    () => true,
    () => false,
  );
  const url = shown ? await link.inputValue() : '';
  await shot(pg, `owner-invite-${how}-${email ?? 'link'}`);
  record({
    who: 'owner',
    screen: 'teams → Пригласить',
    action:
      how === 'mail'
        ? `по почте ${email}${team ? ` в «${team}»` : ''}`
        : `ссылка${team ? ` в «${team}»` : ''}`,
    ok: shown,
    detail: shown ? undefined : drain(owner) || 'ссылки нет в окне',
  });
  await pg.keyboard.press('Escape');
  return url;
}

test('2. people join through the screen: by mail into the team, by a link without a team; one is made a lead', async ({
  browser,
}) => {
  member = await accept(
    browser,
    'member',
    'member@example.com',
    await inviteFromScreen('mail', 'Core', 'member@example.com'),
  );
  lead = await accept(
    browser,
    'lead',
    'lead@example.com',
    await inviteFromScreen('mail', 'Core', 'lead@example.com'),
  );
  plain = await accept(browser, 'plain', 'plain@example.com', await inviteFromScreen('link', null));

  const people = await api<{ members: { userId: string }[] }>(
    owner.page,
    'GET',
    `/api/orgs/${slug}/scopes/team/${teamId}/members`,
  );
  const inTeam = people.members.map((m) => m.userId);
  record({
    who: 'owner',
    screen: 'teams',
    action: 'приглашённые в «Core» в команде',
    ok: inTeam.includes(member.id) && inTeam.includes(lead.id),
    detail: `в команде: ${inTeam.length}`,
  });

  // The lead is made through the person's card: «Лид команды» → «Сохранить».
  drain(owner);
  await owner.page.goto(`${at('teams')}?p=${lead.id}`);
  const roles = owner.page
    .getByRole('radiogroup')
    .filter({ has: owner.page.getByRole('radio', { name: 'Лид команды' }) });
  await roles.getByRole('radio', { name: 'Лид команды' }).click();
  await owner.page.getByRole('button', { name: 'Сохранить' }).click();
  const made = await expect
    .poll(
      async () => {
        const m = await api<{ members: { userId: string; grants?: { role: string }[] }[] }>(
          owner.page,
          'GET',
          `/api/orgs/${slug}/scopes/team/${teamId}/members`,
        );
        return m.members.some((x) => x.userId === lead.id && (x.grants ?? []).some((g) => g.role === 'lead'));
      },
      { timeout: 15_000 },
    )
    .toBe(true)
    .then(
      () => true,
      () => false,
    );
  await shot(owner.page, 'owner-make-lead');
  record({
    who: 'owner',
    screen: 'teams → карточка человека',
    action: 'Лид команды → Сохранить',
    ok: made,
    detail: made ? undefined : drain(owner) || 'роль не появилась',
  });
  if (!made)
    await api(owner.page, 'PUT', `/api/orgs/${slug}/grants`, {
      userId: lead.id,
      scope: { kind: 'team', id: teamId },
      role: 'lead',
      granted: true,
    });
});

const SECTIONS: Record<string, string[]> = {
  owner: ['overview', 'analytics', 'machines', 'teams', 'env', 'accounts', 'settings'],
  lead: ['overview', 'analytics', 'machines', 'team', 'env', 'accounts'],
  member: ['overview', 'analytics', 'machines', 'env'],
  plain: ['overview', 'analytics', 'machines', 'env'],
};

test('3. every person walks every screen of their menu', async () => {
  for (const p of [owner, lead, member, plain]) {
    for (const s of SECTIONS[p.name]) {
      drain(p);
      await p.page.goto(at(s));
      await p.page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {});
      await p.page.waitForTimeout(800);
      const text = (await p.page.locator('body').innerText()).slice(0, 4000);
      const refused = /Не удалось|Это выше вашего уровня|Ошибка|forbidden/i.exec(text)?.[0];
      const errs = drain(p);
      await shot(p.page, `${p.name}-${s}`);
      record({
        who: p.name,
        screen: s,
        action: 'open',
        ok: !errs && !refused,
        detail: [refused && `on screen: «${refused}»`, errs].filter(Boolean).join(' | ') || undefined,
      });
    }
  }
});

/** Whether the element shows up within ms (isVisible does not wait, whatever its timeout says). */
const shows = (l: Locator, ms: number) =>
  l.waitFor({ state: 'visible', timeout: ms }).then(
    () => true,
    () => false,
  );

/** The machines screen as a flat list: grouped by team, a machine sits inside a folded group. */
async function machinesList(page: Page) {
  await page.goto(at('machines'));
  // A member's screen has no switch: it waits for the list or the switch, whichever comes.
  const flat = page.getByRole('radio', { name: 'Списком' });
  await shows(flat.or(page.getByText('team-box')).first(), 15_000);
  if (await flat.isVisible()) await flat.click();
}

test('4. who sees the machine in «Машины»', async () => {
  for (const p of [owner, lead, member, plain]) {
    await machinesList(p.page);
    const seen = await shows(p.page.getByText('team-box').first(), 15_000);
    record({
      who: p.name,
      screen: 'machines',
      action: 'sees team-box',
      ok: seen,
      detail: seen ? undefined : drain(p) || 'not on the screen',
    });
  }
});

/** Opens the machine from the machines screen and sends a message in a new chat. */
async function openAndWrite(p: Person) {
  await machinesList(p.page);
  await p.page
    .getByText('team-box')
    .first()
    .click({ timeout: 15_000 })
    .catch(() => {});
  const trust = p.page.getByRole('button', { name: 'Доверять машине' });
  if (await trust.isVisible().catch(() => false)) await trust.click();
  const open = p.page
    .getByRole('button', { name: /^Открыть/ })
    .or(p.page.getByRole('link', { name: /^Открыть/ }));
  if (!(await shows(open.first(), 10_000))) {
    record({
      who: p.name,
      screen: 'machines',
      action: 'button «Открыть»',
      ok: false,
      detail: drain(p) || 'no button',
    });
    return;
  }
  const popup = p.page
    .context()
    .waitForEvent('page', { timeout: 8_000 })
    .catch(() => null);
  await open.first().dispatchEvent('click');
  const rt = (await popup) ?? p.page;
  await rt.waitForLoadState('domcontentloaded');
  rt.on('response', (r) => {
    if (r.status() >= 400) {
      const u = new URL(r.url());
      noteFailed(p, `runtime ${r.request().method()}`, u.pathname, '', r.status());
    }
  });
  const want = stack.cell.runtimeDomain ? `${machineId}.${stack.cell.runtimeDomain}` : `/m/${machineId}/`;
  const opened = await expect
    .poll(() => rt.url(), { timeout: 30_000 })
    .toContain(want)
    .then(
      () => true,
      () => false,
    );
  record({
    who: p.name,
    screen: 'machines',
    action: 'button «Открыть» → runtime',
    ok: opened,
    detail: opened ? undefined : `${rt.url()} ${drain(p)}`,
  });
  if (!opened) return;
  const chat = rt.getByRole('button', { name: /new chat|новый чат/i }).first();
  const ready = await expect(async () => {
    if (!(await chat.isVisible())) await rt.reload();
    await expect(chat).toBeVisible({ timeout: 5_000 });
  })
    .toPass({ timeout: 90_000 })
    .then(
      () => true,
      () => false,
    );
  if (!ready) {
    await shot(rt, `${p.name}-runtime-no-chat`);
    record({
      who: p.name,
      screen: 'runtime',
      action: 'button «Новый чат» visible',
      ok: false,
      detail: drain(p),
    });
    return;
  }
  await chat.click();
  const box = rt.getByRole('textbox').last();
  const editable = await box.isEditable({ timeout: 10_000 }).catch(() => false);
  if (!editable) {
    await shot(rt, `${p.name}-runtime-readonly`);
    record({
      who: p.name,
      screen: 'runtime',
      action: 'type into the composer',
      ok: false,
      detail: `composer not editable ${drain(p)}`,
    });
    return;
  }
  const words = `hello from ${p.name}`;
  await box.fill(words);
  await box.press('Enter');
  const sent = await expect(rt.locator('body'))
    .toContainText(words, { timeout: 60_000 })
    .then(
      () => true,
      () => false,
    );
  await shot(rt, `${p.name}-runtime-chat`);
  record({
    who: p.name,
    screen: 'runtime',
    action: 'send a message',
    ok: sent && !p.errors.length,
    detail: drain(p) || (sent ? undefined : 'the message did not show up'),
  });
  if (p.central.size)
    record({
      who: p.name,
      screen: 'runtime',
      action: 'history and chat move: only through the workspace',
      ok: true,
      detail: `404 by design: ${[...p.central].sort().join(', ')}`,
    });
  p.central.clear();
  if (rt !== p.page) await rt.close();
}

test('5. the owner, the lead and the member open the machine and write in it', async () => {
  test.setTimeout(600_000);
  for (const p of [owner, lead, member, plain]) await openAndWrite(p);
});

test('6. the machine shared with the team only: the plain member loses it, the team keeps it', async () => {
  // In the cloud a machine opens on its own domain: the pages come back to the hub first.
  for (const p of [owner, member, plain]) await p.page.goto(at('machines'));
  await api(owner.page, 'PATCH', `/api/machines/${machineId}`, { access: [{ kind: 'team', id: teamId }] });
  for (const p of [member, plain]) {
    const r = await p.page.evaluate(
      async (id) => (await fetch(`/api/machines/${id}/open`)).status,
      machineId,
    );
    record({
      who: p.name,
      screen: 'api',
      action: 'open a team-only machine',
      ok: p === member ? r === 200 : r === 403,
      detail: `status ${r}`,
    });
  }
  await api(owner.page, 'PATCH', `/api/machines/${machineId}`, { access: [] });
});

test('7. the result', async () => {
  const bad = rows.filter((r) => !r.ok);
  console.log(`\n${rows.length} checks, ${bad.length} failed`);
  for (const r of rows)
    console.log(
      `${r.ok ? 'ok  ' : 'FAIL'} ${r.who} · ${r.screen} · ${r.action}${r.detail ? ` — ${r.detail}` : ''}`,
    );
  expect(bad.map((r) => `${r.who} · ${r.screen} · ${r.action} — ${r.detail ?? ''}`)).toEqual([]);
});
