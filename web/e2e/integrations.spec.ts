import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

// Boards and artifacts beside the conversation: the agent, given the services by the runtime, draws a board and
// publishes a document; their links become chips that open in the side pane (the board signed in through the
// runtime's hand-off, the document after signing in to the catalog), and the pane lists the conversation's documents.
// Runs against real boards and artifacts servers (E2E_BOARDS_DIR, E2E_ARTIFACTS_DIR); skipped without them.

const shots = process.env.E2E_SHOTS;
async function shot(page: Page, name: string) {
  if (!shots) return;
  mkdirSync(shots, { recursive: true });
  await page.screenshot({ path: join(shots, `${name}.png`) });
}

async function setTheme(page: Page, theme: 'light' | 'dark') {
  await page.getByRole('button', { name: 'Theme' }).click();
  await page.getByRole('menuitem', { name: theme === 'light' ? 'Light' : 'Dark' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
}

async function signIn(page: Page) {
  const setup = page.getByRole('heading', { name: 'Set up this machine' });
  const signInHeading = page.getByRole('heading', { name: 'Sign in' });
  await expect(setup.or(signInHeading)).toBeVisible();
  if (await setup.isVisible()) {
    await page.getByLabel('Password', { exact: true }).fill('correct horse');
    await page.getByLabel('Repeat the password').fill('correct horse');
    await page.getByRole('button', { name: 'Set password and continue' }).click();
  } else {
    await page.getByLabel('Password', { exact: true }).fill('correct horse');
    await page.getByRole('button', { name: 'Sign in' }).click();
  }
}

test('a board and a document open beside the conversation', async ({ page }) => {
  test.skip(
    !process.env.E2E_BOARDS_URL,
    'needs boards and artifacts checkouts (E2E_BOARDS_DIR, E2E_ARTIFACTS_DIR)',
  );
  const boards = process.env.E2E_BOARDS_URL!;
  const artifacts = process.env.E2E_ARTIFACTS_URL!;

  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto(process.env.E2E_URL!);
  await signIn(page);
  // Whichever workspace is open: a fresh machine's first one, or the last one used.
  await expect(page.getByRole('button', { name: 'New tab' }).first()).toBeVisible();
  await setTheme(page, 'dark');

  // --- the pane is there, empty, until something opens in it ---
  await page.getByTestId('side-pane-toggle').click();
  const pane = page.getByTestId('side-pane');
  await expect(pane.getByText('No board open')).toBeVisible();
  await page.getByTestId('side-pane-toggle').click();
  await expect(pane).toBeHidden();

  // --- an agent that draws: a board and a document through the services the runtime handed it ---
  await page.getByRole('button', { name: 'New tab' }).first().click();
  await page.getByRole('menuitem', { name: 'Claude Code' }).click();
  const agent = page.getByTestId('agent-view');
  await expect(agent.getByTestId('agent-status')).not.toHaveText('Not running', { timeout: 30_000 });
  await agent.getByLabel('Message').fill('draw the checkout flow and write the plan');
  await agent.getByTestId('agent-send').click();
  const reply = agent.getByTestId('msg-agent').filter({ hasText: 'Drew' });
  await expect(reply).toBeVisible();

  // --- the board link is a chip; it opens the editor in the pane, signed in by the runtime's hand-off ---
  const boardChip = reply.getByTestId('board-chip');
  await expect(boardChip).toHaveText('the checkout flow');
  await expect(boardChip).toHaveAttribute('title', new RegExp(`^${boards}/b/checkout-flow-[a-z0-9]+\\?k=`));
  await boardChip.click();
  await expect(pane).toBeVisible();
  await expect(pane.getByRole('tab', { name: 'Board' })).toHaveAttribute('aria-selected', 'true');
  const boardFrame = pane.getByTestId('board-frame');
  await expect(boardFrame).toHaveAttribute('src', new RegExp(`^${boards}/b/auth/handoff\\?t=`));
  const editor = page.frameLocator('[data-testid="board-frame"]');
  await expect(editor.locator('.excalidraw').first()).toBeVisible({ timeout: 60_000 });
  await expect
    .poll(async () => (await page.frame({ url: /\/b\/checkout-flow-/ }))?.url() ?? '', { timeout: 30_000 })
    .toMatch(new RegExp(`^${boards}/b/checkout-flow-[a-z0-9]+\\?k=`));
  await page.waitForTimeout(1500);
  await shot(page, 'board-pane-dark');

  // --- the document link is a chip too; the pane lists the conversation's documents ---
  const docChip = reply.getByTestId('document-chip');
  await expect(docChip).toHaveText('Checkout plan');
  await docChip.click();
  await expect(pane.getByRole('tab', { name: 'Document' })).toHaveAttribute('aria-selected', 'true');
  const list = pane.getByTestId('conversation-documents');
  await expect(list.getByTestId('document-item')).toHaveCount(1);
  await expect(list.getByTestId('document-item')).toContainText('Checkout plan');
  await expect(list.getByTestId('document-item')).toHaveAttribute('data-active', 'true');
  await expect(pane.getByTestId('document-frame')).toHaveAttribute(
    'src',
    new RegExp(`^${artifacts}/embed/checkout-plan-[a-z0-9]+\\?theme=dark$`),
  );

  // --- the catalog wants its own sign-in: it opens in a window, and the pane reloads after ---
  const banner = pane.getByRole('alert').filter({ hasText: 'Sign in to the catalog' });
  await expect(banner).toBeVisible();
  const popupPromise = page.waitForEvent('popup');
  await banner.getByRole('button', { name: 'Sign in' }).click();
  const popup = await popupPromise;
  await popup.waitForLoadState();
  expect(popup.url().startsWith(`${artifacts}/login`)).toBe(true);
  await popup.locator('input[name="token"]').fill(process.env.E2E_ARTIFACTS_READ_TOKEN!);
  await popup.locator('input[name="token"]').press('Enter');
  await popup.waitForURL((u) => !u.pathname.startsWith('/login'));
  await popup.close();
  await expect(banner).toBeHidden();
  const reader = page.frameLocator('[data-testid="document-frame"]').frameLocator('iframe').first();
  await expect(reader.getByRole('heading', { name: 'Checkout plan' })).toBeVisible({ timeout: 30_000 });
  await expect(reader.getByText('a paid order gets its receipt')).toBeVisible();
  await shot(page, 'document-pane-dark');

  // --- the same in the light theme: the document follows without a reload; boards follows the system's ---
  await page.emulateMedia({ colorScheme: 'light' });
  await setTheme(page, 'light');
  await expect(page.frameLocator('[data-testid="document-frame"]').locator('html')).toHaveAttribute(
    'data-theme',
    'light',
  );
  await page.waitForTimeout(500);
  await shot(page, 'document-pane-light');
  await pane.getByRole('tab', { name: 'Board' }).click();
  await pane.getByRole('button', { name: 'Reload' }).click();
  await expect(boardFrame).toHaveAttribute('src', new RegExp(`^${boards}/b/auth/handoff\\?t=`));
  await expect(editor.locator('.excalidraw').first()).toBeVisible();
  await page.waitForTimeout(1500);
  await shot(page, 'board-pane-light');
});
