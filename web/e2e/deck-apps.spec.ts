// An app of «Валидатор презентаций» in the slide editor (HRN-612): its checks' marks are drawn on the blocks of the
// slide, a block selected on the slide brings the app's bar over the field, and the bar's item sends the task and the
// block's words to the deck's chat. The agent is the stand-in CLI, so what is checked is that the message gets there.
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Frame, type Page, test } from '@playwright/test';

import { pageId, slideFile } from './deck-rig';
import { type Runtime, startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
/** More words than the example's rule lets a block have (40). */
const WORDY = Array.from({ length: 55 }, (_, i) => `word${i}`).join(' ');

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;
let project = '';
let conv = '';

const frameOf = async (p: Page): Promise<Frame> => {
  const handle = await p.locator('iframe.hx-deck__frame').last().elementHandle({ timeout: 30_000 });
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error('no frame in the slide');
  return frame;
};

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-dka-'));
  rt = await startRuntime({
    root,
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
      // The board engine of a machine that has it (the test makes a deck, which needs it).
      ...(process.env.RUNTIME_DESIGN_RELEASES_DIR
        ? { RUNTIME_DESIGN_RELEASES_DIR: process.env.RUNTIME_DESIGN_RELEASES_DIR }
        : {}),
    },
  });
  // The example is installed as a person does with the CLI.
  let cli = process.env.HARNESSLY_BIN;
  if (!cli) {
    cli = join(root, 'harnessly');
    execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
  }
  execFileSync(cli, ['app', 'install', join(repo, 'docs', 'apps', 'deck-lint')], {
    env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
    stdio: 'inherit',
  });
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

test('1. a slide with a long text gets a mark on the block and the panel counts its words whole', async () => {
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-app')).toBeVisible();

  const made = await page.evaluate(async () => {
    const res = await fetch('/api/v2/designly/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: 'Lint deck',
        brief: 'Two slides',
        kind: 'slides',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
      }),
    });
    return { status: res.status, body: (await res.json()) as { id?: string; conv?: string } };
  });
  expect(made.status, JSON.stringify(made.body)).toBe(200);
  project = made.body.id ?? '';
  conv = made.body.conv ?? '';
  expect(project).not.toBe('');
  expect(conv).toMatch(/^tab-/);

  const lib = join(rt.home, 'project', 'design-artifacts', project);
  [
    { title: 'Short', head: 'A short slide', body: 'Few words here' },
    { title: 'Wordy', head: 'A wordy slide', body: WORDY },
  ].forEach((s, i) => {
    const file = `slide-${i + 1}.dc.html`;
    writeFileSync(join(lib, 'project', file), slideFile(s));
    writeFileSync(
      join(lib, 'meta', 'pages', `${pageId(i)}.json`),
      JSON.stringify({ id: pageId(i), title: s.title, file, w: 1920, h: 1080, order: i + 1 }),
    );
  });

  // The deck is opened from its chat (`?chat=`), as the person comes to it.
  await page.goto(`${rt.url}/agents/designly/${project}?chat=${conv}`);
  await expect(page.locator('.hx-deck')).toBeVisible({ timeout: 30_000 });
  // The release notes show once and cover the strip: they come a moment after the deck.
  await page
    .getByRole('button', { name: 'Got it' })
    .click({ timeout: 5_000 })
    .catch(() => undefined);
  await page.locator('.hx-deck-th').nth(1).click();

  // The error mark is drawn over the block, in the slide's own frame.
  const frame = await frameOf(page);
  await expect(frame.locator('[data-tk-library-mark] > div')).toHaveCount(1, { timeout: 30_000 });
  // The panel's count is the rule's (55), not the count of a clipped text.
  await expect(page.locator('.hx-sinopia__panel iframe')).toBeVisible();
  const panel = page.frameLocator('.hx-sinopia__panel iframe');
  await expect(panel.locator('.dl__n').first()).toHaveText('55', { timeout: 30_000 });
});

test('2. a block selected on the slide brings the app’s bar, and «Сократить» sends the words to the chat', async () => {
  const frame = await frameOf(page);
  await expect(page.locator('.hx-sinopia__bar')).toHaveCount(0);
  await frame.locator('p').click();
  const bar = page.locator('.hx-sinopia__bar');
  await expect(bar).toBeVisible();
  await bar.getByRole('button', { name: 'Сократить' }).click();
  // The task, the block's id and its words are in the chat's message.
  const chat = page.locator('.hx-deck');
  await expect(chat.getByText('Сократи выбранный текст до сути').first()).toBeVisible({ timeout: 30_000 });
  await expect(chat.getByText(/word0 word1 word2/).first()).toBeVisible();
  await page.screenshot({ path: join(process.env.E2E_SHOTS ?? tmpdir(), 'deck-apps-sent.png') });
});

test('3. leaving the block takes the bar away', async () => {
  const frame = await frameOf(page);
  await frame.locator('h1').click();
  await expect(page.locator('.hx-sinopia__bar')).toBeVisible();
  // A click on the empty slide deselects.
  await frame.locator('main').click({ position: { x: 1800, y: 1000 } });
  await expect(page.locator('.hx-sinopia__bar')).toHaveCount(0, { timeout: 10_000 });
});
