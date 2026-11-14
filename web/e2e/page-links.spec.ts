// Links between the pages of a design: a board's link to another page switches the site's page — the
// address (`?page=`, so Back and Forward work), the settings panel with the link's props over the target's defaults,
// a phone for a phone's page — and a deck's link turns to its slide; a link to a page that is not there says so.
// Runs with PAGE_LINKS_E2E set (a runtime of its own); TOUR_SHOTS names the folder for the screenshots.
import { execFileSync } from 'node:child_process';
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

const PASSWORD = 'correct horse';

test.skip(!process.env.PAGE_LINKS_E2E, 'needs PAGE_LINKS_E2E');
test.describe.configure({ mode: 'serial', timeout: 240_000 });

let rt: Runtime;
let base = '';
const shots = process.env.TOUR_SHOTS ?? 'test-results';

test.beforeAll(async () => {
  test.setTimeout(180_000);
  rt = await startRuntime({ env: { RUNTIME_LOCALE: 'ru', RUNTIME_WEB_FEATURES: 'chats,design' } });
});
test.afterAll(async () => {
  await rt?.stop();
});

const signIn = async (page: Page) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addLocatorHandler(page.getByRole('button', { name: /^(Got it|Понятно)$/ }), (b) => b.click());
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  base = new URL(page.url()).pathname.replace(/\/$/, '');
};

const board = (props: string, body: string, vals: string) => `<!doctype html>
<html><head>
<meta charset="utf-8">
<script src="./support.js"></script>
</head><body>
<x-dc>
  <helmet><style>body{margin:0;font-family:system-ui;background:#f6f5f2;color:#1d1d1f} a{color:#5b3df5} .card{background:#fff;border-radius:16px;padding:24px}</style></helmet>
${body}
</x-dc>
<script type="text/x-dc" data-dc-script data-props='${props}'>
class Component extends DCLogic {
  renderVals() { return ${vals}; }
}
</script>
</body></html>
`;

const SITE: Record<string, string> = {
  'Главная.dc.html': board(
    '{"theme":{"editor":"enum","options":["Светлая","Тёмная"],"default":"Светлая"},"$preview":{"width":1280,"height":800}}',
    `<main style="padding:48px;display:flex;flex-direction:column;gap:16px;max-width:720px">
    <h1 style="margin:0;font-size:40px">Главная · тема {{ theme }}</h1>
    <div class="card" style="display:flex;flex-direction:column;gap:12px">
      <a id="to-pay" href="./Оплата.dc.html?variant=Mobile&amp;loading=true#pay">Оплатить билет</a>
      <a id="to-profile" href="sinopia:page/Профиль?name=Ирина">Профиль</a>
      <a id="to-missing" href="./Нет-такой.dc.html">Несуществующая страница</a>
    </div>
  </main>`,
    '{ theme: this.props.theme }',
  ),
  'Оплата.dc.html': board(
    '{"variant":{"editor":"enum","options":["Desktop","Mobile"],"default":"Desktop","section":"Экран"},"loading":{"editor":"boolean","default":false,"section":"Состояния"},"$preview":{"width":1200,"height":900}}',
    `<main style="padding:48px;display:flex;flex-direction:column;gap:16px">
    <h1 style="margin:0;font-size:40px">Оплата · {{ variant }}</h1>
    <p id="loading" style="margin:0">Загрузка: {{ loadingText }}</p>
    <div style="height:1400px" class="card">Длинное содержимое</div>
    <div id="pay" class="card" style="background:#e9e3ff">Блок оплаты <a href="./Главная.dc.html">На главную</a></div>
  </main>`,
    '{ variant: this.props.variant, loadingText: this.props.loading ? "да" : "нет" }',
  ),
  'Профиль.dc.html': board(
    '{"name":{"editor":"text","default":"Гость"},"$preview":{"width":390,"height":844}}',
    `<main style="padding:24px;display:flex;flex-direction:column;gap:12px">
    <h1 style="margin:0;font-size:28px">Профиль: {{ name }}</h1>
    <a id="to-home" href="./Главная.dc.html?theme=Тёмная">На главную</a>
  </main>`,
    '{ name: this.props.name }',
  ),
};

/** The library's folder (where the demo projects are) and a project of three pages made in it. */
const makeProjects = () => {
  const found = execFileSync('find', [rt.root, '-path', '*/relay-site/project/support.js'], {
    encoding: 'utf8',
  })
    .split('\n')
    .filter(Boolean);
  expect(found.length, 'the demo site on disk').toBeGreaterThan(0);
  const support = found[0] ?? '';
  const library = dirname(dirname(dirname(support)));
  const site = join(library, 'links-test', 'project');
  mkdirSync(site, { recursive: true });
  copyFileSync(support, join(site, 'support.js'));
  for (const [file, html] of Object.entries(SITE)) writeFileSync(join(site, file), html);
  // The demo deck's cover links to its fifth slide.
  const cover = join(library, 'relay-deck', 'project', '01 Обложка.dc.html');
  const html = readFileSync(cover, 'utf8');
  writeFileSync(
    cover,
    html.replace(
      '<div class="foot" data-block="Подвал">',
      '<a id="to-result" data-block="Ссылка" href="./05 Результат.dc.html" style="position:absolute;left:6cqw;top:42cqw;font-size:1.6cqw">К результату</a>\n<div class="foot" data-block="Подвал">',
    ),
  );
};

const FRAME = '.hx-site__iframe';
const pageParam = (page: Page) => new URL(page.url()).searchParams.get('page');

test('a link of a board switches the site to its page, with its props, Back and Forward, a phone page', async ({
  page,
}) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/designly`);
  await expect(page.locator('.hx-dl-card[data-id="relay-site"], .hx-dl-folder').first()).toBeVisible({
    timeout: 60_000,
  });
  makeProjects();
  await page.goto(`${rt.url}${base}/designly/links-test?edit=1`);
  const frame = page.frameLocator(FRAME);
  await expect(frame.getByRole('heading', { name: /Главная/ })).toBeVisible({ timeout: 60_000 });
  await expect.poll(() => pageParam(page), { timeout: 20_000 }).toBe('Главная.dc.html');
  await page.screenshot({ path: join(shots, 'links-1-home.png') });

  // The link with props and a block: the address, the board, its settings.
  await frame.locator('#to-pay').click();
  await expect.poll(() => pageParam(page), { timeout: 20_000 }).toBe('Оплата.dc.html');
  await expect(frame.getByRole('heading', { name: 'Оплата · Mobile' })).toBeVisible({ timeout: 30_000 });
  await expect(frame.locator('#loading')).toHaveText('Загрузка: да');
  await page.getByRole('button', { name: 'Живые настройки' }).click();
  await expect(page.locator('.hx-tweaks')).toBeVisible();
  await expect(page.locator('.hx-tweaks')).toContainText('Загрузка');
  await page.screenshot({ path: join(shots, 'links-2-pay-props.png') });

  // Back and Forward of the browser.
  await page.goBack();
  await expect.poll(() => pageParam(page), { timeout: 20_000 }).toBe('Главная.dc.html');
  await expect(frame.getByRole('heading', { name: /Главная/ })).toBeVisible({ timeout: 30_000 });
  await page.goForward();
  await expect.poll(() => pageParam(page), { timeout: 20_000 }).toBe('Оплата.dc.html');
  await expect(frame.getByRole('heading', { name: /Оплата/ })).toBeVisible({ timeout: 30_000 });
  await page.screenshot({ path: join(shots, 'links-3-forward.png') });

  // A named link to a phone's page: it is shown in a phone.
  await page.goBack();
  await expect(frame.getByRole('heading', { name: /Главная/ })).toBeVisible({ timeout: 30_000 });
  await frame.locator('#to-profile').click();
  await expect.poll(() => pageParam(page), { timeout: 20_000 }).toBe('Профиль.dc.html');
  await expect(frame.getByRole('heading', { name: 'Профиль: Ирина' })).toBeVisible({ timeout: 30_000 });
  await expect(page.locator('.hx-vparea')).not.toHaveAttribute('data-dev', 'fill', { timeout: 15_000 });
  await page.screenshot({ path: join(shots, 'links-4-phone.png') });

  // Back from the phone page with its own prop, then the link to a page that is not there.
  await frame.locator('#to-home').click();
  await expect(frame.getByRole('heading', { name: 'Главная · тема Тёмная' })).toBeVisible({
    timeout: 30_000,
  });
  await frame.locator('#to-missing').click();
  await expect(page.getByText('В макете нет страницы «Нет-такой.dc.html»')).toBeVisible({ timeout: 10_000 });
  expect(pageParam(page)).toBe('Главная.dc.html');
  await page.screenshot({ path: join(shots, 'links-5-missing.png') });

  // A deep link opens its page.
  await page.goto(`${rt.url}${base}/designly/links-test?page=${encodeURIComponent('Оплата.dc.html')}`);
  await expect(frame.getByRole('heading', { name: /Оплата/ })).toBeVisible({ timeout: 60_000 });
  await page.screenshot({ path: join(shots, 'links-6-deep.png') });

  // A slide's link turns the deck to its slide (Alt + click: a slide being edited takes a plain click).
  await page.goto(`${rt.url}${base}/designly/relay-deck`);
  const slide = page.frameLocator('.hx-deck__frame[data-on="1"]');
  await expect(slide.locator('#to-result')).toBeVisible({ timeout: 60_000 });
  await slide.locator('#to-result').click({ modifiers: ['Alt'] });
  await expect(slide.locator('#to-result')).toHaveCount(0, { timeout: 30_000 });
  await expect(page.locator('.hx-deck__frame[data-on="1"]')).toHaveAttribute('src', /05/, {
    timeout: 30_000,
  });
  await page.screenshot({ path: join(shots, 'links-7-deck.png') });
});
