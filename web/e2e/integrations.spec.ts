import { expect, type Page, test } from '@playwright/test';

// Boards and documents beside the conversation, against real boards and artifacts servers (E2E_BOARDS_DIR and
// E2E_ARTIFACTS_DIR name their checkouts; skipped without them). The agent, given the services by the runtime, draws
// a board and publishes a document; their links in its reply are chips that open in the right pane: the board
// signed in through the runtime's hand-off, the document through the page's own reader.

const PASSWORD = 'correct horse';

/** Signs in, or sets the password first when this is the machine's first visit. */
async function signIn(page: Page) {
  const setup = page.getByTestId('setup-form');
  const signInForm = page.getByTestId('signin-form');
  await expect(setup.or(signInForm)).toBeVisible();
  if (await setup.isVisible()) {
    await setup.locator('input[name=password]').fill(PASSWORD);
    await setup.locator('input[name=confirm]').fill(PASSWORD);
    await setup.locator('button[type=submit]').click();
  } else {
    await signInForm.locator('input[name=password]').fill(PASSWORD);
    await signInForm.locator('button[type=submit]').click();
  }
  await expect(page.locator('main')).toBeVisible();
}

test('a board and a document open beside the conversation', async ({ page }) => {
  test.skip(
    !process.env.E2E_BOARDS_URL,
    'needs boards and artifacts checkouts (E2E_BOARDS_DIR, E2E_ARTIFACTS_DIR)',
  );
  const boards = process.env.E2E_BOARDS_URL!;
  const artifacts = process.env.E2E_ARTIFACTS_URL!;
  const main = page.locator('main');

  await page.goto(`${process.env.E2E_URL}/`);
  await signIn(page);

  // An agent that draws: it makes a board and a document through the services the runtime handed it.
  const home = main.getByTestId('home-composer');
  await home.getByRole('textbox').fill('draw the checkout flow and write the plan');
  await main.getByTestId('home-send').click();
  await expect(page).toHaveURL(/[?&]c=agent/);
  const reply = main.getByTestId('timeline').locator('.agent-message').filter({ hasText: 'Drew' });
  await expect(reply).toBeVisible();
  // No pane until something opens in it.
  await expect(page.getByTestId('right-pane')).toHaveCount(0);

  // The board link is a chip with the agent's words; it opens the editor in the pane, signed in by the runtime.
  const boardChip = reply.getByTestId('board-chip');
  await expect(boardChip).toHaveText('the checkout flow');
  await expect(boardChip).toHaveAttribute('href', new RegExp(`^${boards}/b/checkout-flow-[a-z0-9]+\\?k=`));
  await boardChip.click();
  const pane = page.getByTestId('right-pane');
  await expect(pane).toBeVisible();
  const boardFrame = pane.getByTestId('board-frame');
  await expect(boardFrame).toHaveAttribute('src', new RegExp(`^${boards}/b/auth/handoff\\?t=`));
  const editor = page.frameLocator('[data-testid="board-frame"]');
  await expect(editor.locator('.excalidraw').first()).toBeVisible({ timeout: 60_000 });
  // The hand-off landed on the board itself.
  await expect
    .poll(async () => page.frame({ url: /\/b\/checkout-flow-/ })?.url() ?? '', { timeout: 30_000 })
    .toMatch(new RegExp(`^${boards}/b/checkout-flow-[a-z0-9]+\\?k=`));

  // The document link is a chip too; the pane reads it through the page's own reader, not the catalog's login.
  const docChip = reply.getByTestId('document-chip');
  await expect(docChip).toHaveAttribute('href', new RegExp(`^${artifacts}/a/checkout-plan-[a-z0-9]+`));
  await docChip.click();
  const docFrame = pane.getByTestId('document-frame');
  await expect(docFrame).toHaveAttribute('src', /\/docs\/checkout-plan-[a-z0-9]+\/content/);
  // The reader frames the catalog's short-lived read link: the document itself is one frame further in.
  const reader = page.frameLocator('[data-testid="document-frame"]').frameLocator('iframe').first();
  await expect(reader.getByRole('heading', { name: 'Checkout plan' })).toBeVisible({ timeout: 30_000 });
  await expect(reader.getByText('a paid order gets its receipt')).toBeVisible();
  // Both stay open as tabs of the pane, named by the agent's words and the document's title.
  await expect(pane.getByRole('tab', { name: 'the checkout flow' })).toBeVisible();
  await expect(pane.getByRole('tab', { name: 'Checkout plan' })).toHaveAttribute('aria-selected', 'true');
});
