import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { type Browser, expect, type Locator, type Page, test } from '@playwright/test';

// The agents page on a fresh machine, as one person's first session: set the password, land on the home screen, start
// a conversation with the fake Claude, talk to it through the composer and its terminal, stop it and resume it, and
// open the right pane. The steps share one page and run in order.
//
// The copy comes from the en/ru dictionaries, so the page is found by test ids and the agents' own words, not its
// labels. The selectors live in `ui` below.

const PASSWORD = 'correct horse';

const ui = (page: Page) => {
  const main = page.locator('main');
  const composer = main.getByTestId('composer');
  const termbar = main.getByTestId('terminal-bar');
  const pane = page.getByTestId('right-pane');
  return {
    main,
    setupForm: page.getByTestId('setup-form'),
    signInForm: page.getByTestId('signin-form'),
    sidebar: page.getByRole('navigation').first(),
    home: main.getByTestId('home'),
    homeComposer: main.getByTestId('home-composer'),
    homeSend: main.getByTestId('home-send'),
    homeCrumb: main.getByTestId('home-crumb'),
    timeline: main.getByTestId('timeline'),
    composer,
    composerInput: composer.getByTestId('composer-input'),
    composerSend: composer.getByTestId('composer-send'),
    termbar,
    termbarToggle: termbar.getByTestId('terminal-bar-toggle'),
    terminal: termbar.getByTestId('terminal'),
    sessionEnded: main.getByTestId('session-ended'),
    paneToggle: main.getByTestId('pane-toggle'),
    pane,
    paneHide: pane.getByTestId('right-pane-hide'),
  };
};

/** The page's first screen behind the runtime's root, which redirects to the app base path. */
async function openPage(page: Page) {
  await page.goto(`${process.env.E2E_URL}/`);
  await expect(page).toHaveURL(/\/$/);
}

async function signIn(page: Page) {
  const { signInForm, main } = ui(page);
  await signInForm.locator('input[name=password]').fill(PASSWORD);
  await signInForm.locator('button[type=submit]').click();
  await expect(main).toBeVisible();
}

/** Sends a message through the conversation's composer once it can send. */
async function send(page: Page, text: string) {
  const { composerInput, composerSend } = ui(page);
  await composerInput.fill(text);
  await expect(composerSend).toBeEnabled();
  await composerInput.press('Enter');
}

/** Opens a folding header (aria-expanded) that may or may not be open already, and waits until it is. */
async function expand(header: Locator) {
  await expect(header).toBeVisible();
  if ((await header.getAttribute('aria-expanded')) !== 'true') await header.click();
  await expect(header).toHaveAttribute('aria-expanded', 'true');
}

/** The fake Claude answers every message with one command (ls -la) and this reply. */
const reply = (text: string) => `Fake agent reply: ${text}`;

const agentMessage = (timeline: Locator, text: string) =>
  timeline.locator('.agent-message').filter({ hasText: reply(text) });

test.describe.configure({ mode: 'serial' });

let browser: Browser;
let page: Page;

test.beforeAll(async ({ browser: b }) => {
  browser = b;
  page = await browser.newPage();
});

test.afterAll(async () => {
  await page?.close();
});

test('1. first-run setup sets the password and signs in; a new browser signs in with it', async () => {
  test.skip(!!process.env.E2E_SIGNED_UP, 'a running page is past its first run');
  const { setupForm, signInForm, main } = ui(page);
  await openPage(page);

  await expect(setupForm).toBeVisible();
  await expect(setupForm.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(setupForm.locator('button[type=submit]')).toBeDisabled();
  // Passwords that differ are refused in place.
  await setupForm.locator('input[name=password]').fill(PASSWORD);
  await setupForm.locator('input[name=confirm]').fill(`${PASSWORD}!`);
  await setupForm.locator('button[type=submit]').click();
  await expect(setupForm.getByRole('alert')).toBeVisible();
  await setupForm.locator('input[name=confirm]').fill(PASSWORD);
  await setupForm.locator('button[type=submit]').click();
  await expect(main).toBeVisible();
  await expect(setupForm).toHaveCount(0);

  // Another browser has no session: it gets the sign-in form, without the repeat field, and the password works.
  const other = await browser.newPage();
  try {
    await openPage(other);
    await expect(ui(other).signInForm).toBeVisible();
    await signIn(other);
    await expect(ui(other).home).toBeVisible();
  } finally {
    await other.close();
  }
  await expect(signInForm).toHaveCount(0);
});

test('2. the home screen renders', async () => {
  const { home, homeComposer, homeSend, sidebar } = ui(page);
  if (process.env.E2E_SIGNED_UP) {
    await openPage(page);
    await signIn(page);
  }
  await expect(home).toBeVisible();
  await expect(home.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(homeComposer.getByRole('textbox')).toBeVisible();
  // Claude and Codex to choose from, Claude by default.
  const kinds = homeComposer.getByRole('radiogroup').getByRole('radio');
  await expect(kinds).toHaveCount(2);
  await expect(kinds.first()).toBeChecked();
  // Nothing typed, nothing to send.
  await expect(homeSend).toBeDisabled();
  await expect(sidebar).toBeVisible();
});

test('3. a first message on the home screen starts a conversation', async () => {
  const { home, homeComposer, homeSend, timeline, composerInput, sidebar } = ui(page);
  await homeComposer.getByRole('textbox').fill('hello agent');
  await expect(homeSend).toBeEnabled();
  await homeSend.click();

  // The page addresses the new conversation and shows it in place of the home screen.
  await expect(page).toHaveURL(/[?&]c=agent/);
  await expect(home).toHaveCount(0);
  await expect(timeline.getByText('hello agent', { exact: true })).toBeVisible();
  await expect(composerInput).toBeVisible();
  // The sidebar lists it under its first message.
  await expect(sidebar.getByRole('link', { name: /hello agent/ })).toBeVisible();
  // The agent took the message as its first prompt.
  await expect(agentMessage(timeline, 'hello agent')).toBeVisible();
});

test('4. the composer sends a message to the agent', async () => {
  const { timeline } = ui(page);
  await send(page, 'second message');
  await expect(timeline.getByText('second message', { exact: true })).toBeVisible();
});

test("5. the turn renders with the agent's answer and its steps", async () => {
  const { timeline, composerSend } = ui(page);
  await expect(agentMessage(timeline, 'second message')).toBeVisible();
  // The turn that carries the answer folds its work under a line; unfolded, the command shows what ran.
  const turn = timeline
    .getByTestId('turn')
    .filter({ has: page.locator('.agent-message', { hasText: reply('second message') }) });
  await expand(turn.getByTestId('turn-actions-toggle'));
  await expand(turn.getByTestId('tool-group-toggle'));
  await expect(turn.getByTestId('tool-target')).toContainText('ls -la');
  // The answer is in, so the composer is back to sending.
  await expect(composerSend).toHaveAttribute('data-kind', 'send');
  // Both turns stay in order.
  await expect(timeline.locator('.agent-message')).toHaveCount(2);
  await expect(timeline.locator('.agent-message').first()).toContainText(reply('hello agent'));
});

test('5a. a finished answer on screen counts as seen: no eye on its row, after a reload too', async () => {
  const { sidebar, timeline } = ui(page);
  // The row is named after the latest message.
  const row = sidebar.getByRole('link', { name: /second message/ });
  await expect(agentMessage(timeline, 'second message')).toBeVisible();
  await expect(row.locator('.tw-orb-done')).toBeVisible();
  await expect(row.locator('.tw-orb-ready')).toHaveCount(0);
  await page.reload();
  await expect(agentMessage(ui(page).timeline, 'second message')).toBeVisible();
  await expect(row.locator('.tw-orb-done')).toBeVisible();
  await expect(row.locator('.tw-orb-ready')).toHaveCount(0);
});

test("6. the terminal bar opens the agent's terminal, which reaches the agent", async () => {
  const { termbarToggle, terminal, timeline } = ui(page);
  await expect(termbarToggle).toHaveAttribute('aria-expanded', 'false');
  await expect(terminal).toHaveCount(0);
  await termbarToggle.click();
  await expect(termbarToggle).toHaveAttribute('aria-expanded', 'true');
  await expect(terminal).toHaveAttribute('data-state', 'open');
  // It is the agent's own session.
  await expect(terminal.locator('.xterm-rows')).toContainText('fake claude ready');

  // A line typed there goes to the agent, and its turn lands in the timeline.
  await terminal.click();
  await page.keyboard.type('typed in the terminal');
  await page.keyboard.press('Enter');
  await expect(agentMessage(timeline, 'typed in the terminal')).toBeVisible();

  await termbarToggle.click();
  await expect(termbarToggle).toHaveAttribute('aria-expanded', 'false');
  await expect(terminal).toHaveCount(0);
});

test('7. a stopped session resumes where it was', async () => {
  const { termbarToggle, terminal, sessionEnded, timeline, composerInput } = ui(page);
  // Stop the agent: end its input in its terminal, as quitting it there would.
  await termbarToggle.click();
  await expect(terminal).toHaveAttribute('data-state', 'open');
  await terminal.click();
  await page.keyboard.press('Control+D');
  await expect(sessionEnded).toBeVisible();
  await termbarToggle.click();
  await expect(terminal).toHaveCount(0);

  // The conversation stays readable while the agent is down, and survives a reload.
  await page.reload();
  await expect(sessionEnded).toBeVisible();
  await expect(agentMessage(timeline, 'second message')).toBeVisible();

  await sessionEnded.getByRole('button').click();
  await expect(sessionEnded).toHaveCount(0);
  await expect(composerInput).toBeVisible();
  await send(page, 'after resume');
  await expect(agentMessage(timeline, 'after resume')).toBeVisible();
  // The same session went on: the earlier turns are still there, and the agent was started with --resume.
  await expect(agentMessage(timeline, 'hello agent')).toBeVisible();
  if (process.env.E2E_HOME) {
    const calls = readFileSync(join(process.env.E2E_HOME, 'claude-calls.log'), 'utf8').trim().split('\n');
    expect(calls.at(-1)).toMatch(/^--resume [0-9a-f-]{36} /);
  }
});

test('8. the right pane opens beside the conversation and hides again', async () => {
  const { paneToggle, pane, paneHide } = ui(page);
  await expect(pane).toHaveCount(0);
  await paneToggle.click();
  await expect(pane).toBeVisible();
  await expect(paneToggle).toHaveCount(0);
  const tabs = pane.getByRole('tablist').getByRole('tab');
  await expect(tabs.first()).toBeVisible();
  const selected = pane.getByRole('tab', { selected: true });
  await expect(selected).toHaveCount(1);
  const panelId = await selected.getAttribute('aria-controls');
  await expect(pane.locator(`[role=tabpanel][id="${panelId}"]`)).toBeVisible();

  await paneHide.click();
  await expect(pane).toHaveCount(0);
  await expect(paneToggle).toBeVisible();
});

test('the header crumb goes back to the home screen', async () => {
  const { homeCrumb, home, sidebar } = ui(page);
  await homeCrumb.click();
  await expect(home).toBeVisible();
  await expect(page).not.toHaveURL(/[?&]c=agent/);
  // The conversation is one click away in the sidebar.
  await sidebar.getByRole('link', { name: /hello agent/ }).click();
  await expect(page).toHaveURL(/[?&]c=agent/);
  await expect(agentMessage(ui(page).timeline, 'after resume')).toBeVisible();
});
