import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Page } from '@playwright/test';
import { RELEASES } from '../app/next/features/whats-new/releases';

export interface SafeFixture {
  asks: Record<string, unknown>[];
  secret: {
    name: string;
    createdAt: number;
    updatedAt: number;
    lastUsedAt: number;
    usedBy: { conv: string; title: string; at: number; origin: string }[];
    origins: string[];
    projects: string[];
  };
  calls: { method: string; path: string; body: string | null }[];
}

/** Only public metadata and fake asks: the browser never reads or writes a runtime safe. */
export async function mockSafe(page: Page): Promise<SafeFixture> {
  const now = Date.now();
  const state: SafeFixture = {
    asks: [],
    secret: {
      name: 'release-token',
      createdAt: now - 3600_000,
      updatedAt: now - 3600_000,
      lastUsedAt: now - 60_000,
      usedBy: [
        { conv: 'c-fixture', title: 'Release notes', at: now - 60_000, origin: 'https://example.org' },
      ],
      origins: ['https://example.org'],
      projects: [],
    },
    calls: [],
  };
  await page.route(/\/api\/platform\/(?:secrets|secret-asks)(?:[/?]|$)/, async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace(/^.*(?=\/api\/platform\/)/, '');
    const method = request.method();
    state.calls.push({ method, path, body: request.postData() });
    if (method === 'GET') {
      await route.fulfill({
        json: path.endsWith('/secret-asks')
          ? { asks: state.asks }
          : { secrets: [state.secret], asks: state.asks },
      });
    } else if (method === 'PATCH') {
      const metadata = request.postDataJSON() as { origins: string[]; projects: string[] };
      state.secret = { ...state.secret, ...metadata };
      await route.fulfill({ json: { secret: state.secret } });
    } else if (method === 'POST' && /\/(put|grant|decline)$/.test(path)) {
      const id = path.split('/').at(-2)!;
      state.asks = state.asks.filter((ask) => ask.id !== id);
      await route.fulfill({ json: path.endsWith('/decline') ? { declined: id } : { ready: id } });
    } else {
      await route.fulfill({ status: 400, json: { error: 'unexpected-safe-mutation' } });
    }
  });
  return state;
}

export const ask = (over: Record<string, unknown> = {}) => ({
  id: 'sa_fixture',
  name: 'release-token',
  why: 'Publish the release',
  conv: '',
  title: 'Release notes',
  at: Date.now(),
  replace: false,
  origin: 'https://example.org',
  ...over,
});

export async function safeReady(page: Page) {
  await page.addInitScript((id) => localStorage.setItem('hx:whats-new', id), RELEASES[0]!.id);
  await page.goto(process.env.E2E_URL!);
  await page
    .locator('.hx-composer__input, [data-testid="setup-form"], [data-testid="signin-form"]')
    .first()
    .waitFor();
  const setup = page.getByTestId('setup-form');
  if (await setup.isVisible()) {
    await setup.locator('input[name=password]').fill('safe test password');
    await setup.locator('input[name=confirm]').fill('safe test password');
    await setup.locator('button[type=submit]').click();
  }
  const login = page.getByTestId('signin-form');
  if (await login.isVisible()) {
    await login.locator('input[name=password]').fill('safe test password');
    await login.locator('button[type=submit]').click();
  }
  await expect(page.locator('.hx-composer__input')).toBeVisible();
}

export async function safeProject(page: Page, name: string): Promise<string> {
  await page.locator('.hx-sidebar').getByRole('button', { name: 'New project', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'New project', exact: true });
  await dialog.getByLabel('Name', { exact: true }).fill(name);
  await dialog.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const row = page.locator('.hx-sidebar [data-drop="project"]').filter({ hasText: name });
  await expect(row).toHaveCount(1);
  return (await row.getAttribute('data-drop-id'))!;
}

export async function safeShot(page: Page, name: string) {
  const dir = process.env.E2E_SCREENSHOTS;
  if (!dir) return;
  mkdirSync(dir, { recursive: true });
  await page.screenshot({ path: join(dir, `${name}.png`), animations: 'disabled' });
}
