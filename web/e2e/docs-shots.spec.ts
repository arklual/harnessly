/// <reference lib="dom" />
// The screenshots of the documentation (docs/start/img, docs/ru/start/img): the real page of a standalone runtime with
// the fake agents of the other browser tests, shot in English and in Russian. It is not a test: it runs only when asked,
//
//   pnpm build && DOCS_SHOTS=1 pnpm exec playwright test docs-shots
//
// and writes the pictures into the repository, to be looked at and committed with the pages that show them. The
// runtime carries the pictures it was built with, so the shot of the documentation itself shows the new ones from the
// second run on.
import { writeFileSync } from 'node:fs';
import { hostname } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const docs = resolve(here, '..', '..', 'docs');
const PASSWORD = 'correct horse';

const LANGS = [
  {
    lang: 'en',
    out: join(docs, 'start', 'img'),
    ask: 'How do I show my dev server to the person?',
    reply:
      'Start it in the background and name its address in the answer: the right pane opens it by itself.\n\n' +
      '```bash\nnohup pnpm dev --port 4400 > dev.log 2>&1 &\nharnessly open 4400\n```\n\n' +
      'Once it answers, **http://localhost:4400** opens in the browser tab of the pane.',
  },
  {
    lang: 'ru',
    out: join(docs, 'ru', 'start', 'img'),
    ask: 'Как показать человеку мой dev-сервер?',
    reply:
      'Запустите его в фоне и назовите адрес в ответе: правая панель откроет его сама.\n\n' +
      '```bash\nnohup pnpm dev --port 4400 > dev.log 2>&1 &\nharnessly open 4400\n```\n\n' +
      'Как только сервер ответит, **http://localhost:4400** откроется во вкладке браузера панели.',
  },
] as const;

test.skip(!process.env.DOCS_SHOTS, 'shoots the documentation only when DOCS_SHOTS is set');
test.describe.configure({ mode: 'serial' });

for (const l of LANGS) {
  test.describe(l.lang, () => {
    let rt: Runtime;
    let page: Page;
    const shot = async (name: string) => {
      // The pictures settle: no caret, no running animation, no tooltip under the pointer; the machine is named as in
      // the docs, not after the host that shot them.
      await page.mouse.move(1435, 450);
      await page.waitForTimeout(600);
      await page.evaluate(
        (hosts) => {
          const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
          for (let n = walk.nextNode(); n; n = walk.nextNode())
            for (const host of hosts)
              if (n.textContent?.includes(host)) n.textContent = n.textContent.replaceAll(host, 'my-server');
        },
        [hostname(), hostname().split('.')[0] ?? ''],
      );
      await page.screenshot({ path: join(l.out, `${name}.png`), animations: 'disabled', caret: 'hide' });
    };

    test.beforeAll(async ({ browser }) => {
      rt = await startRuntime({ env: { RUNTIME_LOCALE: l.lang, RUNTIME_WEB_FEATURES: 'chats,design' } });
      writeFileSync(join(rt.home, '.fake-reply.md'), l.reply);
      page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark' });
    });
    test.afterAll(async () => {
      await page?.close();
      await rt?.stop();
    });

    test('home, chat, pane, settings, documentation', async () => {
      await page.goto(`${rt.url}/`);
      const setup = page.getByTestId('setup-form');
      await setup.locator('input[name=password]').fill(PASSWORD);
      await setup.locator('input[name=confirm]').fill(PASSWORD);
      await setup.locator('button[type=submit]').click();
      await expect(page.locator('.hx-hello')).toBeVisible();
      // The page lives under the installation's base (the runtime does not serve a deep link outside it).
      const base = new URL(page.url()).pathname.replace(/\/$/, '');
      const at = (path: string) => `${rt.url}${base}${path}`;
      // «What's new» greets a fresh install: it is not what the pictures show.
      const gotIt = page.getByRole('button', { name: /^(Got it|Понятно)$/ });
      await gotIt.click({ timeout: 5_000 }).catch(() => undefined);
      await expect(gotIt).toHaveCount(0);
      await shot('home');

      await page.locator('.hx-composer__input').fill(l.ask);
      await page.locator('.hx-composer__input').press('Enter');
      await expect(page).toHaveURL(/\/c\/[^/]+$/);
      await expect(page.locator('.hx-feed').getByText('harnessly open 4400')).toBeVisible({
        timeout: 60_000,
      });
      await expect(page.locator('.hx-stage .hx-composer [aria-label]').first()).toBeVisible();
      await shot('chat');

      await page.locator('.hx-header__tools').click();
      await expect(page.locator('.hx-pane .hx-tl__tile').first()).toBeVisible();
      await shot('pane');
      await page.keyboard.press('Escape');

      await page.goto(at('/settings/general'));
      await expect(page.locator('.hx-set')).toBeVisible();
      await shot('settings');

      await page.goto(at('/help/start/overview'));
      await expect(page.locator('.hx-help__article h1')).toBeVisible();
      await shot('help');
    });
  });
}
