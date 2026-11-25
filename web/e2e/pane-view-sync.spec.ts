import { createServer } from 'node:http';
import { expect, test } from '@playwright/test';
import { SEEN_ALL } from '../app/next/features/whats-new/seen';
import { freePort, startRuntime } from './runtime';

type Chat = Record<string, unknown> & { id: string };
type Frame = {
  f: string;
  ch?: string;
  seq: number;
  data?: { conversations: Chat[] };
  e?: { type: string; conv?: Chat };
};

// A reconnect can deliver many saved views at once. Use the runtime's own records and add the other chats only
// to its WebSocket snapshot, leaving the file and pane controls on the real runtime.
test('a manually opened pane stays open when other chats sync (HRN-1741)', async ({ page }) => {
  const rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats' } });
  const site = createServer((_, res) => {
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end('<!doctype html><title>Preview</title><h1>Local preview</h1>');
  });
  try {
    let snapshot: Frame | undefined;
    let current: Chat | undefined;
    let seq = 0;
    let send: ((frame: Frame) => void) | undefined;
    await page.routeWebSocket(/\/socket(?:\?|$)/, (ws) => {
      const server = ws.connectToServer();
      send = (frame) => ws.send(JSON.stringify(frame));
      server.onMessage((raw) => {
        const frame = JSON.parse(String(raw)) as Frame;
        if (frame.f === 'snapshot' && frame.ch === 'convs') {
          snapshot = frame;
          current = frame.data?.conversations[0] ?? current;
          seq = frame.seq;
        }
        if (frame.f === 'ev' && frame.ch === 'convs') {
          seq = frame.seq;
          if (frame.e?.type === 'conversation_upsert') current = frame.e.conv;
        }
        ws.send(raw);
      });
    });
    await page.addInitScript((seen) => localStorage.setItem('hx:whats-new', seen), SEEN_ALL);
    await page.goto(rt.url);
    const setup = page.getByTestId('setup-form');
    await setup.locator('input[name=password]').fill('correct horse');
    await setup.locator('input[name=confirm]').fill('correct horse');
    await setup.locator('button[type=submit]').click();
    const input = page.locator('.hx-composer__input');
    await input.fill('Open the project');
    await input.press('Enter');
    await expect.poll(() => Boolean(snapshot && current && send)).toBe(true);

    await page.locator('.hx-header__tools').click();
    const pane = page.locator('.hx-pane');
    await pane
      .locator('.hx-tl__tile')
      .filter({ has: page.locator('[data-i="files"]') })
      .click();
    await pane.locator('.hx-tv__row', { hasText: 'README.md' }).click();
    const editor = pane.locator('.hx-fed .cm-content');
    await expect(editor).toContainText('# demo project');

    const syncOtherChats = async (batch: number) => {
      if (!snapshot || !current || !send) throw new Error('Conversation channel did not open');
      const chat = current;
      const at = Date.now();
      // Enough saved views to fill the 50-entry pane cache.
      const conversations = Array.from({ length: 50 }, (_, i) => ({
        ...chat,
        id: `tab-background-${i}`,
        title: `Background chat ${i}`,
        view: { at, pane: { open: false } },
      }));
      send({ ...snapshot, seq, data: { ...snapshot.data, conversations: [chat, ...conversations] } });
      const last = conversations.at(-1)!.id;
      await expect
        .poll(
          () => page.evaluate((id) => JSON.parse(localStorage.getItem('hx-chat-view-at') ?? '{}')[id], last),
          {
            message: `background snapshot ${batch} was imported`,
          },
        )
        .toBe(at);
    };

    await syncOtherChats(1);
    await expect(editor).toContainText('# demo project', { timeout: 3000 });
    await page.screenshot({ path: test.info().outputPath('pane-after-sync.png') });

    await pane.getByRole('button', { name: 'Close pane', exact: true }).click();
    await syncOtherChats(2);
    await expect(pane).toHaveCount(0);

    const port = await freePort();
    await new Promise<void>((resolve) => site.listen(port, '127.0.0.1', resolve));
    await page.locator('.hx-header__tools').click();
    await pane.locator('.hx-tab[data-home]').click();
    await pane
      .locator('.hx-tl__tile')
      .filter({ has: page.locator('[data-i="globe"]') })
      .click();
    const address = page.getByRole('combobox', { name: 'Address' });
    await address.fill(String(port));
    await address.press('Enter');
    const heading = page.frameLocator('.hx-tbrowser__frame').getByRole('heading', { name: 'Local preview' });
    await expect(heading).toBeVisible();
    await syncOtherChats(3);
    await expect(heading).toBeVisible({ timeout: 3000 });
    await page.screenshot({ path: test.info().outputPath('preview-after-sync.png') });
  } finally {
    await rt.stop();
    await new Promise<void>((resolve) => site.close(() => resolve()));
  }
});
