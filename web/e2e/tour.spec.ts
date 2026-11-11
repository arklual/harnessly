// The hands-on tour on the demo projects: a lesson started by its link counts the person's real
// actions, step by step, and puts the agent's request into the design's field. Runs with TOUR_E2E set.
import { existsSync, mkdirSync, readFileSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';

import { expect, type Locator, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

const PASSWORD = 'correct horse';
const SHOTS = resolve(process.env.TOUR_SHOTS ?? 'test-results/tour');

test.skip(!process.env.TOUR_E2E, 'walks the hands-on tour only when TOUR_E2E is set');
// A machine under load needs longer: TOUR_SLOW multiplies the time the whole test and the runtime's start may take.
const SLOW = Math.max(1, Number(process.env.TOUR_SLOW ?? 1));
test.describe.configure({ mode: 'default', timeout: 180_000 * SLOW });

let rt: Runtime;

test.beforeAll(async () => {
  test.setTimeout(120_000 * SLOW);
  mkdirSync(SHOTS, { recursive: true });
  rt = await startRuntime({ env: { RUNTIME_LOCALE: 'ru', RUNTIME_WEB_FEATURES: 'chats,design' } });
});
test.afterAll(async () => {
  await rt?.stop();
});

const card = (page: Page) => page.locator('.hx-tour-card');
const title = (page: Page) => card(page).locator('.hx-tour-card__title');
let shot = 0;
const snap = async (page: Page, name: string) => {
  // The ring and the card glide to their place; the picture is of where they stop.
  await page.waitForTimeout(700);
  await page.screenshot({ path: join(SHOTS, `${String(++shot).padStart(2, '0')}-${name}.png`) });
};

let base = '';
let state = '';

/** Signed in: set up once per runtime, then the same cookies. */
const signIn = async (page: Page) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  // A fresh browser shows «Что нового» once, whenever it loads; the person closes it, so does the test.
  await page.addLocatorHandler(page.getByRole('button', { name: /^(Got it|Понятно)$/ }), (b) => b.click());
  if (!state) {
    await page.goto(`${rt.url}/`);
    const setup = page.getByTestId('setup-form');
    await setup.locator('input[name=password]').fill(PASSWORD);
    await setup.locator('input[name=confirm]').fill(PASSWORD);
    await setup.locator('button[type=submit]').click();
    await expect(page.locator('.hx-hello')).toBeVisible();
    base = new URL(page.url()).pathname.replace(/\/$/, '');
    state = join(SHOTS, '.state.json');
    await page.context().storageState({ path: state });
  } else {
    await page.context().addCookies(JSON.parse(readFileSync(state, 'utf8')).cookies);
  }
};

/** Every Sinopia lesson starts signed in on the home page, its link opened, and goes to Sinopia first. */
const begin = async (page: Page, track: string) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/?tour=${track}`);
  await expect(title(page)).toHaveText('Откройте Синопию');
  await page.locator('.hx-nav__group[data-i="2"] .hx-nav__item').click();
};
/** Signed in, the lesson's link opened on the greeting. */
const lessonAt = async (page: Page, track: string) => {
  await signIn(page);
  await page.goto(`${rt.url}${base}/?tour=${track}`);
};
const did = (page: Page) => card(page).getByRole('button', { name: 'Сделал' }).click();

/** The tour's ring stands over the element: its box holds the element's centre. */
const ringOn = async (page: Page, target: Locator) => {
  await expect
    .poll(
      async () => {
        const ring = await page.locator('.hx-tour-ring').boundingBox();
        const box = await target.boundingBox();
        if (!ring || !box) return false;
        const x = box.x + box.width / 2;
        const y = box.y + box.height / 2;
        return x >= ring.x && x <= ring.x + ring.width && y >= ring.y && y <= ring.y + ring.height;
      },
      { timeout: 10_000, message: 'the ring is not on the element' },
    )
    .toBe(true);
};

const demoTile = (page: Page) =>
  page.locator('.hx-dl-folder', { hasText: 'Демо' }).locator('.hx-dl-folder__open');
const demoCard = (page: Page, id: string) => page.locator(`.hx-dl-card[data-id="${id}"]`);

/** A demo project lies in the «Демо» folder: the ring is on the folder, then on the card inside it. */
const openDemo = async (page: Page, id: string) => {
  await expect(card(page).locator('.hx-tour-card__text')).toContainText('«Демо»', { timeout: 15_000 });
  await ringOn(page, demoTile(page));
  await demoTile(page).click();
  await expect(demoCard(page, id)).toBeVisible();
  await ringOn(page, demoCard(page, id));
  await demoCard(page, id).click();
};

test('welcome, the lessons page, a film and the first chat by hand', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addLocatorHandler(page.getByRole('button', { name: /^(Got it|Понятно)$/ }), (b) => b.click());
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  base = new URL(page.url()).pathname.replace(/\/$/, '');
  state = join(SHOTS, '.state.json');
  await page.context().storageState({ path: state });

  // A first launch with no chat: the welcome comes once «Что нового» is gone.
  const hello = page.locator('.hx-learn-hello');
  await expect(hello).toBeVisible({ timeout: 15_000 });
  await snap(page, 'welcome');
  await hello.getByRole('button', { name: 'Все уроки' }).click();
  await expect(page).toHaveURL(/\/learn$/);
  await expect(page.getByRole('heading', { name: 'Обучение', level: 1 })).toBeVisible();
  await expect(page.locator('.hx-learn-card')).toHaveCount(10);
  await snap(page, 'learn');

  // The film: it plays, its chapters move it, Esc puts it away.
  await page.getByRole('button', { name: 'Смотреть: Правая панель' }).click();
  const player = page.locator('.hx-learn-player');
  await expect(player).toBeVisible();
  await expect
    .poll(() => player.locator('video').evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(0.5);
  await player.getByRole('button', { name: /Терминал агента/ }).click();
  await expect(player.locator('.hx-learn-sub')).toHaveText(/Терминал агента/);
  await snap(page, 'learn-film');
  await page.keyboard.press('Escape');
  await expect(player).toHaveCount(0);

  // «Пройти руками» on the first lesson: from the greeting, every step a real action.
  await page.locator('.hx-learn-hero').getByRole('button', { name: 'Пройти на реальных действиях' }).click();
  await expect(title(page)).toHaveText('Напишите задачу', { timeout: 10_000 });
  await snap(page, 'chat-type');
  await page.locator('.hx-composer__input').fill('Расскажи, что в этой папке');
  await expect(title(page)).toHaveText('Откройте «+»', { timeout: 10_000 });
  await page.locator('.hx-plus-btn').click();
  await expect(title(page)).toHaveText('Откройте выбор модели', { timeout: 10_000 });
  await snap(page, 'chat-model');
  await page.keyboard.press('Escape');
  await page.locator('.hx-model').click();
  await expect(title(page)).toHaveText('Можно голосом', { timeout: 10_000 });
  await page.keyboard.press('Escape');
  await did(page);
  await expect(title(page)).toHaveText('Отправьте');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(title(page)).toHaveText('Раскройте ход агента', { timeout: 15_000 });
  await snap(page, 'chat-turn');
  await page.locator('.hx-work__head').first().click({ timeout: 30_000 });
  await expect(title(page)).toHaveText('Урок пройден', { timeout: 10_000 });
  await snap(page, 'chat-passed');
  await card(page).getByRole('button', { name: 'Все уроки' }).click();
  await expect(page).toHaveURL(/\/learn$/);
  await expect(
    page.locator('.hx-learn-card').filter({ hasText: 'Первый чат' }).locator('.hx-learn-card__passed'),
  ).toBeVisible();
});

test('the lessons page and the film fit a phone', async ({ page }) => {
  await signIn(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${rt.url}${base}/learn`);
  await expect(page.locator('.hx-learn-card')).toHaveCount(10);
  const wide = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(await wide()).toBeLessThanOrEqual(0);
  await snap(page, 'learn-phone');
  await page.getByRole('button', { name: 'Смотреть: Первый чат' }).first().click();
  await expect(page.locator('.hx-learn-player')).toBeVisible();
  expect(await wide()).toBeLessThanOrEqual(0);
  await snap(page, 'film-phone');
  await page.getByRole('button', { name: 'Закрыть' }).first().click();
  await expect(page.locator('.hx-learn-player')).toHaveCount(0);
});

test('safe lesson walks the settings', async ({ page }) => {
  await lessonAt(page, 'safe');
  await expect(title(page)).toHaveText('Откройте настройки');
  await page.locator('.hx-user__open').click();
  await page
    .getByRole('menuitem', { name: /Настройки/ })
    .first()
    .click({ timeout: 5_000 })
    .catch(() => undefined);
  await expect(title(page)).toHaveText('Откройте «Сейф»', { timeout: 10_000 });
  await page.locator('.hx-set__nav-item[data-id="safe"]').click();
  await expect(title(page)).toHaveText('Откройте «Подключения»', { timeout: 10_000 });
  await snap(page, 'safe-conn');
  await page.locator('.hx-set__nav-item[data-id="connections"]').click();
  await expect(title(page)).toHaveText('Урок пройден', { timeout: 10_000 });
});

const skip = (page: Page) => card(page).getByRole('button', { name: 'Пропустить' }).click();

test('pane and small comforts lessons', async ({ page }) => {
  await lessonAt(page, 'pane');
  await expect(title(page)).toHaveText('Откройте чат');
  await page.locator('.hx-sidebar__list').getByText('Расскажи, что в этой папке').first().click();
  await expect(title(page)).toHaveText('Откройте терминал', { timeout: 10_000 });
  await snap(page, 'pane-term');
  await page.locator('.hx-header__term').click();
  await expect(title(page)).toHaveText('Откройте панель', { timeout: 10_000 });
  await page.locator('.hx-header__tools').click();
  await expect(title(page)).toHaveText('Инструменты — вкладками', { timeout: 10_000 });
  await snap(page, 'pane-tools');
  await card(page).getByRole('button', { name: 'Дальше' }).click();
  await expect(title(page)).toHaveText('Урок пройден');

  await card(page).getByRole('button', { name: 'Выйти из урока' }).click();
  await page.goto(`${rt.url}${base}/?tour=extra`);
  await expect(title(page)).toHaveText('Откройте поиск');
  await page.locator('.hx-nav__group[data-i="1"] .hx-nav__item').click();
  await expect(title(page)).toHaveText('Откройте уведомления', { timeout: 10_000 });
  await page.keyboard.press('Escape');
  await page.locator('.hx-user__bell').click();
  await expect(title(page)).toHaveText('Боковой чат — в панели', { timeout: 10_000 });
  await snap(page, 'extra-side');
});

const next = (page: Page) => card(page).getByRole('button', { name: 'Дальше' }).click();

test('introduction points at the greeting, step by step', async ({ page }) => {
  await lessonAt(page, 'welcome');
  for (const step of [
    'Пишите задачу словами',
    'Файлы, скриншоты, макеты',
    'Агент и модель',
    'Все чаты и их статусы',
    'Правая панель',
    'Синопия',
    'Уроки — в «Ещё»',
  ]) {
    await expect(title(page)).toHaveText(step);
    // Every look has its element ringed: a ring on nothing is a target the redesign took away.
    await expect(page.locator('.hx-tour-ring')).toBeVisible({ timeout: 10_000 });
    await next(page);
  }
  await expect(title(page)).toHaveText('Урок пройден');
  // The introduction leads into the first lesson.
  await card(page).getByRole('button', { name: 'Следующий урок' }).click();
  await expect(card(page).locator('.hx-tour-card__track')).toHaveText('Первый чат');
});

test('while the agent works: the turn and the queue', async ({ page }) => {
  await lessonAt(page, 'work');
  await expect(title(page)).toHaveText('Дайте агенту задачу');
  // «long» makes the fake agent hold its tool for a while: the turn stays live.
  await page.locator('.hx-composer__input').fill('Пройдись по проекту, long');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(title(page)).toHaveText('Статус — в списке', { timeout: 15_000 });
  await next(page);
  await expect(title(page)).toHaveText('Раскройте ход');
  await page.locator('.hx-work__head').first().click();
  await expect(title(page)).toHaveText('Напишите, не дожидаясь', { timeout: 10_000 });
  await page.locator('.hx-composer__input').fill('И проверь тесты');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(title(page)).toHaveText('Агент возьмёт его сам', { timeout: 10_000 });
  await snap(page, 'work-queue');
  await next(page);
  await expect(title(page)).toHaveText('Урок пройден');
});

test('several agents: a new chat and the subagents in the pane', async ({ page }) => {
  await lessonAt(page, 'agents');
  await expect(title(page)).toHaveText('Отправьте задачу', { timeout: 40_000 });
  await page.locator('.hx-composer__input').fill('Посчитай файлы в проекте');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(title(page)).toHaveText('Статус — значком', { timeout: 15_000 });
  await next(page);
  await expect(title(page)).toHaveText('Откройте «Сабагенты»');
  await page.locator('.hx-header__tools').click();
  await page.locator('.hx-pane').getByText('Сабагенты', { exact: true }).first().click();
  await expect(title(page)).toHaveText('Урок пройден', { timeout: 10_000 });
  await snap(page, 'agents-passed');
});

test('site lesson counts the real actions', async ({ page }) => {
  await begin(page, 'site');
  expect(new URL(page.url()).searchParams.has('tour')).toBe(false);
  await expect(title(page)).toHaveText('Откройте «Relay — лендинг»', { timeout: 15_000 });
  await snap(page, 'open');

  await openDemo(page, 'relay-site');
  await expect(title(page)).toHaveText('Пощёлкайте переключатели', { timeout: 15_000 });
  // The ring stands on the flags inside the page: the page's bridge tells where they are.
  const anchor = page.locator('.hx-tour-anchor[data-place="shown"]');
  await expect(anchor).toBeVisible({ timeout: 20_000 });
  await snap(page, 'click');
  const site = page.frameLocator('.hx-site__iframe');
  await site.locator('.flag').first().click();
  await did(page);

  await expect(title(page)).toHaveText('Включите «Редактирование»');
  await snap(page, 'edit');
  await page.getByRole('button', { name: 'Редактирование' }).first().click();
  // The lesson docks the design's chat beside the page.
  await expect(page.locator('.hx-site__chat[aria-hidden="false"]')).toBeVisible({ timeout: 10_000 });

  await expect(title(page)).toHaveText('Нажмите «Выделить»', { timeout: 10_000 });
  await snap(page, 'pick');
  const pick = page.locator('.hx-browser__tool[aria-label="Выделить"]');
  await expect(pick).not.toHaveAttribute('aria-disabled', 'true', { timeout: 15_000 });
  await pick.click();

  await expect(title(page)).toHaveText('Нажмите на заголовок', { timeout: 10_000 });
  await expect(anchor).toBeVisible({ timeout: 10_000 });
  await snap(page, 'heading');
  await site.locator('h1').click();

  await expect(title(page)).toHaveText('Напишите, что поменять', { timeout: 10_000 });
  await expect(page.locator('.hx-ann .hx-ann__input')).toHaveValue('Сделай заголовок короче и ярче');
  await snap(page, 'note');
  await page.locator('.hx-ann .hx-ann__ok').click();

  await expect(title(page)).toHaveText('Отправьте агенту', { timeout: 10_000 });
  await expect(page.locator('.hx-npin')).toHaveCount(1);
  await snap(page, 'ask');
  await card(page).getByRole('button', { name: 'Пропустить' }).click();

  await expect(title(page)).toHaveText('Откройте страницу «Цены»');
  // Away from the design the lesson waits in a corner and leads back.
  await page.locator('.hx-nav__group[data-i="0"] .hx-nav__item').click();
  await expect(card(page).getByRole('button', { name: 'Вернуться' })).toBeVisible({ timeout: 5_000 });
  await snap(page, 'away');
  await card(page).getByRole('button', { name: 'Вернуться' }).click();
  await expect(page).toHaveURL(/\/designly\/relay-site/);
  await expect(title(page)).toHaveText('Откройте страницу «Цены»', { timeout: 10_000 });
  await snap(page, 'page');
  await page.getByText('Цены', { exact: true }).first().click();
  await expect(title(page)).toHaveText('Переключите «За год»', { timeout: 10_000 });
  await expect(anchor).toBeVisible({ timeout: 20_000 });
  await site.locator('label[for="yr"]').click();
  await did(page);

  await expect(title(page)).toHaveText('Посмотрите на iPhone 14', { timeout: 10_000 });
  await page.locator('.hx-browser__dev').click();
  // The card keeps off the button and the menu it opened.
  await page.waitForTimeout(700);
  const btn = await page.locator('.hx-browser__dev').boundingBox();
  const box = await card(page).boundingBox();
  expect(btn && box).toBeTruthy();
  if (btn && box) {
    const apart =
      box.x >= btn.x + btn.width ||
      box.x + box.width <= btn.x ||
      box.y >= btn.y + btn.height ||
      box.y + box.height <= btn.y;
    expect(apart).toBe(true);
  }
  await snap(page, 'device');
  await page
    .getByRole('menuitemradio', { name: /^iPhone 14/ })
    .first()
    .click();
  await expect(title(page)).toHaveText('Урок пройден', { timeout: 10_000 });
  await snap(page, 'passed');

  // Leaving keeps nothing on screen; the lesson can be started again from the library.
  await card(page).getByRole('button', { name: 'Выйти из урока' }).click();
  await expect(card(page)).toHaveCount(0);
});

test('slides lesson', async ({ page }) => {
  await begin(page, 'slides');
  await expect(title(page)).toHaveText('Откройте «Relay — релизы без страха»', { timeout: 15_000 });
  await openDemo(page, 'relay-deck');
  await expect(title(page)).toHaveText('Перелистните слайд', { timeout: 15_000 });
  // While the deck loads the card says so; the slide turns once it is there.
  await expect(page.locator('.hx-deck__slide[data-loading]')).toHaveCount(0, { timeout: 20_000 });
  await expect(card(page).locator('.hx-tour-card__wait')).toHaveCount(0);
  await snap(page, 'deck-next');
  await page.locator('.hx-deck__frame[data-on="1"]').click();
  await page.keyboard.press('ArrowRight');
  await expect(title(page)).toHaveText('Включите «Редактирование»', { timeout: 10_000 });
  await snap(page, 'deck-edit');
  await page.getByRole('button', { name: 'Редактирование' }).first().click();
  await expect(title(page)).toHaveText('Оставьте комментарий', { timeout: 10_000 });
  await snap(page, 'deck-comment');
  await skip(page);
  await expect(title(page)).toHaveText('Покажите колоду');
  await snap(page, 'deck-show');
  await page.locator('.hx-deck__bar [data-k="show"]').click();
  await expect(title(page)).toHaveText('Попросите агента', { timeout: 10_000 });
  await page.keyboard.press('Escape');
  await expect(page.locator('textarea').filter({ visible: true }).first()).toHaveValue(/отзывом клиента/);
  await snap(page, 'deck-ask');
});

test('video lesson', async ({ page }) => {
  await begin(page, 'video');
  await expect(title(page)).toHaveText('Откройте «Relay за 30 секунд»', { timeout: 15_000 });
  await openDemo(page, 'relay-video');
  await expect(title(page)).toHaveText('Запустите ролик', { timeout: 15_000 });
  await snap(page, 'video-play');
  await page.mouse.move(800, 400);
  await page.keyboard.press('Space');
  await expect(title(page)).toHaveText('Перейдите к следующей сцене', { timeout: 10_000 });
  await snap(page, 'video-scene');
  await page.getByRole('button', { name: 'Следующая сцена' }).first().click();
  await expect(title(page)).toHaveText('Подвигайте таймлайн', { timeout: 10_000 });
  await snap(page, 'video-timeline');
  await did(page);
  await expect(title(page)).toHaveText('Попросите агента');
  await snap(page, 'video-ask');
  await skip(page);
  await expect(title(page)).toHaveText('Скачайте ролик');
  await page.locator('[data-k="download"]').first().click();
  await expect(title(page)).toHaveText('Урок пройден', { timeout: 10_000 });
  await snap(page, 'video-passed');
});

test('design system lesson', async ({ page }) => {
  await begin(page, 'ds');
  await expect(title(page)).toHaveText('Откройте «Дизайн-система Relay»', { timeout: 15_000 });
  await openDemo(page, 'relay-ds');
  await expect(title(page)).toHaveText('Посмотрите, что внутри', { timeout: 15_000 });
  await snap(page, 'ds-look');
  await card(page).getByRole('button', { name: 'Дальше' }).click();
  await expect(title(page)).toHaveText('Вернитесь в библиотеку');
  await page.locator('.hx-nav__group[data-i="2"] .hx-nav__item').click();
  await expect(title(page)).toHaveText('Выберите систему Relay', { timeout: 10_000 });
  await snap(page, 'ds-back');
  await page.getByRole('radio', { name: /^Макет сайта/ }).click();
  await snap(page, 'ds-site');
  await page.locator('.hx-deck-chip').filter({ visible: true }).first().click({ timeout: 10_000 });
  await snap(page, 'ds-pick');
  await page.getByRole('radio', { name: /Дизайн-система Relay/ }).click();
  await expect(title(page)).toHaveText('Соберите страницу', { timeout: 10_000 });
  await page.keyboard.press('Escape');
  await expect(page.getByRole('textbox').first()).toHaveValue(/О компании/);
  await snap(page, 'ds-make');
});

/** Starts the site lesson on the library: the greeting, then Sinopia, the open step on screen. */
const atOpen = async (page: Page) => {
  await begin(page, 'site');
  await expect(title(page)).toHaveText('Откройте «Relay — лендинг»', { timeout: 15_000 });
};

test('the way to the project: a filter, a search, another folder, the trash, a deleted project', async ({
  page,
}) => {
  await atOpen(page);
  const text = card(page).locator('.hx-tour-card__text');
  await expect(text).toContainText('«Демо»', { timeout: 15_000 });
  await ringOn(page, demoTile(page));
  await snap(page, 'way-folder');

  // A chip that hides the project: the lesson says so and clears it.
  await page.getByRole('button', { name: 'Фильтры библиотеки' }).click();
  await page.locator('.hx-cmenu__item', { hasText: 'Видео' }).click();
  await page.keyboard.press('Escape');
  await expect(text).toContainText('спрятан', { timeout: 10_000 });
  await ringOn(page, page.locator('.hx-dl__ibtn[data-on]:has(.hx-icon[data-i="sliders-horizontal"])'));
  await snap(page, 'way-filter');
  await card(page).getByRole('button', { name: 'Сбросить' }).click();
  await expect(page.locator('.hx-dl__ibtn[data-on]:has(.hx-icon[data-i="sliders-horizontal"])')).toHaveCount(
    0,
  );
  await expect(text).toContainText('«Демо»', { timeout: 10_000 });

  // A search that hides it, and a search that finds it (the card stands in the list, the ring on the card).
  const search = page.locator('.hx-dl__search input');
  await search.fill('qwerty');
  await expect(text).toContainText('спрятан', { timeout: 10_000 });
  await card(page).getByRole('button', { name: 'Сбросить' }).click();
  await expect(search).toHaveValue('');
  await search.fill('лендинг');
  await expect(demoCard(page, 'relay-site')).toBeVisible();
  await ringOn(page, demoCard(page, 'relay-site'));
  await snap(page, 'way-search');
  await search.fill('');

  // Another tab of the library, and the trash.
  await page.getByRole('tab', { name: 'Общие' }).click();
  await expect(text).toContainText('спрятан', { timeout: 10_000 });
  await card(page).getByRole('button', { name: 'Сбросить' }).click();
  await page.locator('.hx-dl__ibtn[aria-label="Ещё"]').click();
  await page.locator('.hx-cmenu__item', { hasText: 'Корзина' }).click();
  await expect(text).toContainText('спрятан', { timeout: 10_000 });
  await ringOn(page, page.locator('.hx-dl__trash button.hx-dl__crumb'));
  await card(page).getByRole('button', { name: 'Сбросить' }).click();
  await expect(demoTile(page)).toBeVisible();

  // The project lies in another folder than the one that is open: the ring leads up first.
  await demoTile(page).click();
  await demoCard(page, 'relay-site')
    .getByRole('button', { name: /Действия с/ })
    .click();
  await page.getByRole('menuitem', { name: 'Переместить в…' }).click();
  await page.getByRole('option', { name: /Библиотека \(без папки\)/ }).click();
  await expect(demoCard(page, 'relay-site')).toHaveCount(0);
  await expect(text).toContainText('другой папке', { timeout: 10_000 });
  await ringOn(page, page.locator('.hx-dl__crumb[data-drop-folder=""]'));
  await snap(page, 'way-up');
  await page.locator('.hx-dl__crumb[data-drop-folder=""]').click();
  await expect(demoCard(page, 'relay-site')).toBeVisible();
  await ringOn(page, demoCard(page, 'relay-site'));

  // Gone from the library (the menu of a card cannot delete a project nobody owns, so its folder goes from the disk):
  // the lesson brings the demo back, into the same one «Демо» folder.
  const dir = join(rt.home, 'project', 'design-artifacts', 'relay-site');
  expect(existsSync(dir)).toBe(true);
  rmSync(dir, { recursive: true, force: true });
  await page.reload();
  // The library has loaded when something of it stands there (a count of 0 right after a reload proves nothing).
  await expect(page.locator('.hx-dl-folder, .hx-dl-card').first()).toBeVisible({ timeout: 30_000 });
  await expect(demoCard(page, 'relay-site')).toHaveCount(0);
  await expect(text).toContainText('нет', { timeout: 30_000 });
  await snap(page, 'way-gone');
  await card(page).getByRole('button', { name: 'Вернуть демо' }).click();
  await expect(page.locator('.hx-dl-folder', { hasText: 'Демо' })).toHaveCount(1, { timeout: 20_000 });
  await expect(text).toContainText('«Демо»', { timeout: 20_000 });
  await demoTile(page).click();
  await expect(demoCard(page, 'relay-site')).toBeVisible();
  await expect(demoCard(page, 'relay-deck')).toBeVisible();
  await demoCard(page, 'relay-site').click();
  await expect(title(page)).toHaveText('Пощёлкайте переключатели', { timeout: 15_000 });
});

test('«Назад» does not bounce a step that is done already', async ({ page }) => {
  await atOpen(page);
  await expect(card(page).locator('.hx-tour-card__text')).toContainText('«Демо»', { timeout: 15_000 });
  await card(page).getByRole('button', { name: 'Назад' }).click();
  // The library is open: the step «Откройте Синопию» is done, and stays with «Дальше» instead of leaving at once.
  await expect(title(page)).toHaveText('Откройте Синопию');
  await page.waitForTimeout(2_000);
  await expect(title(page)).toHaveText('Откройте Синопию');
  await snap(page, 'back-held');
  await card(page).getByRole('button', { name: 'Дальше' }).click();
  await expect(title(page)).toHaveText('Откройте «Relay — лендинг»');
});

test('the lesson finds the project on a phone', async ({ page }) => {
  await signIn(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${rt.url}${base}/designly?tour=site`);
  await expect(title(page)).toHaveText('Откройте «Relay — лендинг»', { timeout: 20_000 });
  const inView = async (target: Locator) => {
    const box = await target.boundingBox();
    expect(box).toBeTruthy();
    if (box)
      expect(box.y >= 0 && box.y + box.height <= 844 && box.x >= 0 && box.x + box.width <= 390).toBe(true);
  };
  await expect(demoTile(page)).toBeVisible();
  await inView(demoTile(page));
  await ringOn(page, demoTile(page));
  await snap(page, 'phone-folder');
  await demoTile(page).click();
  await expect(demoCard(page, 'relay-site')).toBeVisible();
  await ringOn(page, demoCard(page, 'relay-site'));
  await snap(page, 'phone-card');
  const wide = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(wide).toBeLessThanOrEqual(0);
  await demoCard(page, 'relay-site').click();
  await expect(title(page)).toHaveText('Пощёлкайте переключатели', { timeout: 20_000 });
  await snap(page, 'phone-click');
});
