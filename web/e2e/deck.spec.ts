// A deck (a Designly project of the kind `slides`) end to end on a standalone runtime: the editor, notes, a comment,
// the show, the share popover, the guest page of a link and the exports, then the phone. The gateway of the guest
// link is a small stand-in of its own (it signs the runtime's public token the way the gateway does and serves the
// guest app with its config); the share links are answered by a stand-in of the gateway's access routes.
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, type Frame, type Page, test } from '@playwright/test';

import { deckGateway, pageId, SLIDES, slideFile } from './deck-rig';
import { type Runtime, startRuntime } from './runtime';

const PASSWORD = 'correct horse';

test.describe.configure({ mode: 'serial' });

let rt: Runtime;
let page: Page;
let project = '';
/** The guest gateway's stand-in (/s/<token>[/app|/f|/m|/comments|/seen]) and the owner's access routes. */
const gw = deckGateway(
  () => rt,
  () => project,
);
const links = gw.links;
const stubAccess = gw.stubAccess;

const frameOf = async (p: Page, slot = 'iframe.hx-deck__frame'): Promise<Frame> => {
  const handle = await p.locator(slot).last().elementHandle({ timeout: 30_000 });
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error(`no frame in ${slot}`);
  return frame;
};

test.beforeAll(async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-deck-e2e-'));
  const secretFile = join(root, 'secret');
  writeFileSync(secretFile, gw.secret);
  rt = await startRuntime({
    root,
    env: {
      RUNTIME_WEB_FEATURES: 'chats,design',
      AGENT_SECRET_FILE: secretFile,
      RUNTIME_DESIGN_CHROME: process.env.RUNTIME_DESIGN_CHROME ?? '/usr/bin/google-chrome',
    },
  });
  await gw.start();
  page = await browser.newPage({ acceptDownloads: true });
  await stubAccess(page);
});

test.afterAll(async () => {
  await page?.close();
  await gw.stop();
  await rt?.stop();
});

test('1. a deck made over the API opens in the editor with its five slides', async () => {
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
        title: 'Quarterly review',
        brief: 'A five-slide quarterly review',
        kind: 'slides',
        provider: 'claude',
        model: null,
        effort: null,
        designSystem: null,
      }),
    });
    return { status: res.status, body: (await res.json()) as { id?: string } };
  });
  expect(made.status, JSON.stringify(made.body)).toBe(200);
  project = made.body.id ?? '';
  expect(project).not.toBe('');

  // The agent's work, written as its CLI would: five boards and their pages.
  const lib = join(rt.home, 'project', 'design-artifacts', project);
  SLIDES.forEach((s, i) => {
    const file = `slide-${i + 1}.dc.html`;
    writeFileSync(join(lib, 'project', file), slideFile(s));
    writeFileSync(
      join(lib, 'meta', 'pages', `${pageId(i)}.json`),
      JSON.stringify({ id: pageId(i), title: s.title, file, w: 1920, h: 1080, order: i + 1 }),
    );
  });
  expect(JSON.parse(readFileSync(join(lib, 'meta', 'deck.json'), 'utf8')).v).toBe(1);

  await page.goto(`${rt.url}/agents/designly/${project}`);
  await expect(page.locator('.hx-deck')).toBeVisible({ timeout: 30_000 });
  const frame = await frameOf(page);
  await expect(frame.locator('h1')).toHaveText(SLIDES[0].head, { timeout: 30_000 });
  // The release notes the page shows once would cover the stage.
  const news = page.getByRole('button', { name: 'Got it' });
  if (await news.isVisible()) await news.click();
  await page.screenshot({ path: join(process.env.E2E_SHOTS ?? tmpdir(), 'deck-1-editor.png') });
});

const shots = () => process.env.E2E_SHOTS ?? tmpdir();
const lib = () => join(rt.home, 'project', 'design-artifacts', project);
const deck = () => page.locator('.hx-deck');
const bar = (k: string) => page.locator(`.hx-deck__bar [data-k="${k}"]`);
const NEW_TITLE = 'Acme yearly review';

test('2. a title edited in place on the slide is saved into the board and stays after a reload', async () => {
  const frame = await frameOf(page);
  const title = frame.locator('h1');
  await title.click();
  await expect(title).toHaveAttribute('contenteditable', /true|plaintext-only/);
  await page.keyboard.press('Control+A');
  await page.keyboard.type(NEW_TITLE);
  // Leaving the text saves it.
  await frame.locator('p').click();
  await expect
    .poll(() => readFileSync(join(lib(), 'project', 'slide-1.dc.html'), 'utf8'), { timeout: 20_000 })
    .toContain(NEW_TITLE);
  await page.reload();
  await expect((await frameOf(page)).locator('h1')).toHaveText(NEW_TITLE, { timeout: 30_000 });
});

test('3. the notes typed under the slide are saved for that slide', async () => {
  const notes = page.getByRole('textbox', { name: 'Speaker notes for slide 1' });
  await notes.click();
  await page.keyboard.type('Welcome everyone, this is the yearly review of Acme.');
  await page.locator('.hx-deck__stage').click({ position: { x: 4, y: 4 } });
  const file = join(lib(), 'meta', 'notes', `${pageId(0)}.md`);
  await expect
    .poll(() => {
      try {
        return readFileSync(file, 'utf8');
      } catch {
        return '';
      }
    })
    .toContain('yearly review of Acme');
  await page.reload();
  await expect(page.getByRole('textbox', { name: 'Speaker notes for slide 1' })).toContainText(
    'yearly review of Acme',
    { timeout: 30_000 },
  );
});

test('4. a comment picked on the slide is kept with its place and counted on the Comments tab', async () => {
  await bar('comment').click();
  await expect(deck()).toHaveAttribute('data-pick', '1');
  const frame = await frameOf(page);
  await frame.locator('p').click();
  const text = page.getByRole('textbox', { name: 'Comment text' });
  await expect(text).toBeVisible();
  await text.fill('Make the subtitle bigger');
  await page.getByRole('button', { name: 'Send', exact: true }).last().click();
  const dir = join(lib(), 'meta', 'comments');
  await expect
    .poll(() => {
      try {
        return readdirSync(dir).filter((f) => f.endsWith('.json')).length;
      } catch {
        return 0;
      }
    })
    .toBe(1);
  const saved = JSON.parse(readFileSync(join(dir, readdirSync(dir)[0]), 'utf8')) as Record<string, unknown>;
  expect(saved.text).toBe('Make the subtitle bigger');
  expect(saved.page).toBe(pageId(0));
  expect(saved.resolved).toBe(false);
  await expect(page.getByRole('tab', { name: /Comments/ })).toContainText('1');
  await page.screenshot({ path: join(shots(), 'deck-4-comment.png') });
});

test('5. the show steps through the slides and goes back to the editor', async () => {
  await bar('show').click();
  await expect(deck()).toHaveAttribute('data-screen', 'show');
  const where = deck().getByText(/^\d \/ 5$/);
  await page.keyboard.press('ArrowRight');
  await expect(where).toHaveText('2 / 5');
  await expect.poll(async () => (await frameOf(page)).locator('h1').textContent()).toBe(SLIDES[1].head);
  await page.keyboard.press('ArrowRight');
  await expect(where).toHaveText('3 / 5');
  await page.keyboard.press('ArrowLeft');
  await expect(where).toHaveText('2 / 5');
  await expect.poll(async () => (await frameOf(page)).locator('h1').textContent()).toBe(SLIDES[1].head);
  await page.screenshot({ path: join(shots(), 'deck-5-show.png') });
  await page.keyboard.press('Escape');
  await expect(deck()).toHaveAttribute('data-screen', 'edit');
});

let guestUrl = '';

test('6. the share popover makes a link for anyone at once, and the PDF for guests follows', async () => {
  await bar('share').click();
  const pop = page.getByRole('dialog', { name: 'Share' });
  await expect(pop).toBeVisible();
  await pop.getByRole('radio', { name: 'Anyone with the link' }).click();
  await expect(pop.locator('.hx-deck-share__link')).toBeVisible({ timeout: 15_000 });
  expect(links).toHaveLength(1);
  guestUrl = String(links[0].url);
  expect(guestUrl).toContain(`${gw.url()}/s/`);
  await page.screenshot({ path: join(shots(), 'deck-6-share.png') });
  // The PDF a guest may download is published beside the deck.
  await expect
    .poll(() => {
      try {
        return readFileSync(join(lib(), '.media', 'published', 'deck.pdf'))
          .subarray(0, 5)
          .toString();
      } catch {
        return '';
      }
    })
    .toBe('%PDF-');
  await page.keyboard.press('Escape');
  await expect(pop).toBeHidden();
});

test('7. the guest opens the link and steps through the slides', async ({ browser }) => {
  const guest = await browser.newPage();
  const errors: string[] = [];
  guest.on('pageerror', (e) => errors.push(e.message));
  try {
    await guest.goto(guestUrl);
    const handle = await guest.locator('iframe').first().elementHandle({ timeout: 30_000 });
    const frame = await handle?.contentFrame();
    if (!frame) throw new Error('no slide on the guest page');
    await expect(frame.locator('h1')).toHaveText(NEW_TITLE, { timeout: 30_000 });
    // The board ran: no error of its script on the guest page.
    expect(errors).toEqual([]);
    await guest.screenshot({ path: join(shots(), 'deck-7-guest-1440.png') });
    await guest.keyboard.press('ArrowRight');
    await expect
      .poll(async () => {
        const h = await guest.locator('iframe').last().elementHandle();
        return (await h?.contentFrame())?.locator('h1').textContent();
      })
      .toBe(SLIDES[1].head);
    await guest.setViewportSize({ width: 390, height: 844 });
    await guest.waitForTimeout(600);
    await guest.screenshot({ path: join(shots(), 'deck-7-guest-390.png') });
  } finally {
    await guest.close();
  }
});

const exported = async (item: string) => {
  await bar('more').click();
  const [download] = await Promise.all([
    page.waitForEvent('download', { timeout: 120_000 }),
    page.getByRole('menuitem', { name: new RegExp(`^${item}`) }).click(),
  ]);
  const path = await download.path();
  return { name: download.suggestedFilename(), bytes: readFileSync(path) };
};

test('8. the deck exports as a PDF of five pages and as a PPTX of five slides with the edited title', async () => {
  const pdf = await exported('PDF');
  expect(pdf.name).toMatch(/\.pdf$/);
  expect(pdf.bytes.subarray(0, 5).toString()).toBe('%PDF-');
  expect(pdf.bytes.toString('latin1').match(/\/Type\s*\/Page\b/g)).toHaveLength(5);

  const pptx = await exported('PowerPoint');
  expect(pptx.name).toMatch(/\.pptx$/);
  const dir = mkdtempSync(join(tmpdir(), 'hs-deck-pptx-'));
  writeFileSync(join(dir, 'deck.pptx'), pptx.bytes);
  const names = execFileSync('unzip', ['-Z1', join(dir, 'deck.pptx')])
    .toString()
    .split('\n');
  expect(names.filter((n) => /^ppt\/slides\/slide\d+\.xml$/.test(n))).toHaveLength(5);
  const first = execFileSync('unzip', ['-p', join(dir, 'deck.pptx'), 'ppt/slides/slide1.xml']).toString();
  expect(first).toContain(NEW_TITLE);
  const notes = names.filter((n) => /^ppt\/notesSlides\/notesSlide\d+\.xml$/.test(n));
  expect(notes.length).toBeGreaterThan(0);
  const said = notes.map((n) => execFileSync('unzip', ['-p', join(dir, 'deck.pptx'), n]).toString()).join('');
  expect(said).toContain('yearly review of Acme');
});

test('9. on a phone the editor edits a text in a sheet, shows the notes in a sheet and counts the comments', async ({
  browser,
}) => {
  const phone = await browser.newContext({
    storageState: await page.context().storageState(),
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  const p = await phone.newPage();
  try {
    await stubAccess(p);
    await p.goto(`${rt.url}/agents/designly/${project}`);
    const dock = p.locator('.hx-deck-mdock');
    await expect(dock).toBeVisible({ timeout: 30_000 });
    await expect(dock.getByRole('button', { name: 'Comments' })).toContainText('1');
    // The deck opens on the slide the show ended on (the second); the pager goes back to the first.
    await expect
      .poll(async () => (await frameOf(p)).locator('h1').textContent(), { timeout: 30_000 })
      .toBe(SLIDES[1].head);
    await p.getByRole('button', { name: 'Previous slide' }).tap();
    await expect.poll(async () => (await frameOf(p)).locator('h1').textContent()).toBe(NEW_TITLE);
    await p.waitForTimeout(500);
    const frame = await frameOf(p);
    await p.screenshot({ path: join(shots(), 'deck-9-phone.png') });

    // A tap on a text opens its sheet: the agent's field and «Done».
    await frame.locator('h1').tap();
    const edit = p.getByRole('dialog', { name: /^Edit: / });
    await expect(edit).toBeVisible();
    await p.screenshot({ path: join(shots(), 'deck-9-phone-edit.png') });
    await edit.getByRole('button', { name: 'Done' }).tap();
    await expect(edit).toBeHidden();

    // The notes under the slide open in their sheet with the text.
    await p.getByRole('button', { name: 'Notes for slide 1: edit' }).tap();
    const notes = p.getByRole('dialog', { name: 'Notes for slide 1' });
    await expect(notes).toBeVisible();
    await expect(notes.getByRole('textbox', { name: 'Notes text' })).toHaveValue(/yearly review of Acme/);
    await p.screenshot({ path: join(shots(), 'deck-9-phone-notes.png') });
    await notes.getByRole('button', { name: 'Done' }).tap();
    await expect(notes).toBeHidden();
  } finally {
    await phone.close();
  }
});
