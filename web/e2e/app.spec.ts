import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

// One person's first session on a fresh machine: set the password, open a workspace, use a terminal and an agent, and
// look at the machine. Screenshots go to E2E_SHOTS when it is set.

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

test('a fresh machine: sign in, workspace, terminal, agent, machine', async ({ page }) => {
  const url = process.env.E2E_URL!;
  const project = join(process.env.E2E_HOME!, 'project');

  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto(url);
  await expect(page).toHaveURL(/\/agents\/$/);

  // --- set the machine's password, which signs in ---
  await expect(page.getByRole('heading', { name: 'Set up this machine' })).toBeVisible();
  await shot(page, '01-setup-dark');
  await page.getByLabel('Password', { exact: true }).fill('correct horse');
  await page.getByLabel('Repeat the password').fill('correct horse');
  await page.getByRole('button', { name: 'Set password and continue' }).click();

  // --- the machine's first workspace is made on the first load ---
  const nav = page.getByRole('navigation', { name: 'Workspaces' });
  await expect(nav.getByRole('button', { name: /^Demo/ }).first()).toBeVisible();

  // --- a new workspace for the project folder ---
  await nav.getByRole('button', { name: 'New workspace' }).click();
  const dialog = page.getByRole('dialog', { name: 'New workspace' });
  await dialog.getByLabel('Folder').fill(project);
  await dialog.getByLabel('Name').click();
  await expect(dialog.getByLabel('Name')).toHaveValue('project');
  await dialog.getByLabel('Name').fill('Project');
  await dialog.getByRole('button', { name: 'Create' }).click();
  await expect(nav.getByRole('button', { name: /^Project/ }).first()).toHaveAttribute('aria-current', 'page');

  // --- its terminal answers ---
  const terminal = page.getByTestId('terminal').first();
  await expect(terminal).toHaveAttribute('data-state', 'open');
  await terminal.click();
  await page.keyboard.type('echo hello-from-$((40+2))');
  await page.keyboard.press('Enter');
  await expect(terminal.locator('.xterm-rows')).toContainText('hello-from-42');
  await shot(page, '02-terminal-dark');

  // --- split the pane, then close the new one ---
  await page.getByRole('button', { name: 'Pane actions' }).first().click();
  await page.getByRole('menuitem', { name: 'Split right' }).click();
  await expect(page.locator('section.pane')).toHaveCount(2);
  await expect(page.getByTestId('terminal')).toHaveCount(2);
  await expect(page.getByTestId('terminal').nth(1)).toHaveAttribute('data-state', 'open');
  await shot(page, '03-split-dark');
  await page.getByRole('button', { name: 'Pane actions' }).nth(1).click();
  await page.getByRole('menuitem', { name: 'Close pane' }).click();
  await expect(page.locator('section.pane')).toHaveCount(1);

  // --- an agent tab: the fake Claude answers through its transcript ---
  await page.getByRole('button', { name: 'New tab' }).first().click();
  await page.getByRole('menuitem', { name: 'Claude Code' }).click();
  const agent = page.getByTestId('agent-view');
  await expect(agent).toBeVisible();
  await expect(agent.getByTestId('agent-status')).not.toHaveText('Not running', { timeout: 30_000 });
  const input = agent.getByLabel('Message');
  await input.fill('hello agent');
  await agent.getByTestId('agent-send').click();
  await expect(agent.getByTestId('msg-user').filter({ hasText: 'hello agent' })).toBeVisible();
  await expect(
    agent.getByTestId('msg-agent').filter({ hasText: 'Fake agent reply: hello agent' }),
  ).toBeVisible();
  await expect(agent.getByRole('button', { name: /1 step/ })).toBeVisible();
  await agent.getByRole('button', { name: /1 step/ }).click();
  await expect(agent.getByRole('button', { name: 'Bash $ ls -la', exact: true })).toBeVisible();
  await shot(page, '04-agent-dark');

  // --- a long task: the agent works, and Stop interrupts it ---
  await input.fill('a long task');
  await input.press('Enter');
  await expect(agent.getByTestId('agent-status')).toHaveText('Working');
  await expect(agent.getByTestId('agent-stop')).toBeVisible();
  await shot(page, '04b-agent-working-dark');
  await agent.getByTestId('agent-stop').click();
  await expect(agent.getByTestId('agent-status')).toHaveText('Idle');
  await expect(agent.getByTestId('agent-send')).toBeVisible();

  // --- the agent's own terminal shows it running ---
  await agent.getByRole('button', { name: 'Terminal' }).click();
  await expect(agent.getByTestId('terminal').locator('.xterm-rows')).toContainText('fake claude ready');
  await shot(page, '05-agent-terminal-dark');
  await agent.getByRole('button', { name: 'Terminal' }).click();

  // --- the machine panel ---
  await page.getByTestId('machine-button').click();
  const panel = page.getByTestId('machine-panel');
  await expect(panel.getByText('Memory')).toBeVisible();
  await shot(page, '06-machine-dark');
  await page.keyboard.press('Escape');

  // --- the same screens in the light theme ---
  await setTheme(page, 'light');
  await shot(page, '07-agent-light');
  await page.getByTestId('machine-button').click();
  await expect(panel.getByText('Memory')).toBeVisible();
  await shot(page, '08-machine-light');
  await page.keyboard.press('Escape');
  await page
    .getByRole('tab', { name: /Terminal/ })
    .first()
    .click();
  await expect(page.getByTestId('terminal').first()).toHaveAttribute('data-state', 'open');
  await shot(page, '09-terminal-light');

  // --- signing out goes back to the password ---
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await shot(page, '10-sign-in-light');
});
