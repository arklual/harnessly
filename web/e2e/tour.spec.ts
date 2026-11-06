// The hands-on tour on the demo projects: a lesson started by its link counts the person's real
// actions, step by step, and puts the agent's request into the design's field. Runs with TOUR_E2E set.
import { mkdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

const PASSWORD = 'correct horse';
const SHOTS = resolve(process.env.TOUR_SHOTS ?? 'test-results/tour');

test.skip(!process.env.TOUR_E2E, 'walks the hands-on tour only when TOUR_E2E is set');
test.describe.configure({ mode: 'default', timeout: 180_000 });

let rt: Runtime;

test.beforeAll(async () => {
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
  await page.locator('.hx-nav__group[data-i="4"] .hx-nav__item').click();
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
  await expect(title(page)).toHaveText('Отправьте задачу', { timeout: 10_000 });
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

  await page.locator('.hx-dl-card[data-id="relay-site"]').click();
  await expect(title(page)).toHaveText('Пощёлкайте переключатели', { timeout: 15_000 });
  await page.waitForTimeout(1500);
  await snap(page, 'click');
  await card(page).getByRole('button', { name: 'Сделал' }).click();

  await expect(title(page)).toHaveText('Включите «Редактирование»');
  await snap(page, 'edit');
  await page.getByRole('button', { name: 'Редактирование' }).first().click();

  await expect(title(page)).toHaveText('Укажите блок', { timeout: 10_000 });
  await snap(page, 'pick');
  // The test's runtime shows the board without the page's bridge, so «Выделить» is off here: the step is done by hand.
  await expect(page.locator('.hx-browser__tool[aria-label="Выделить"]')).toBeVisible();
  await did(page);

  await expect(title(page)).toHaveText('Попросите агента', { timeout: 10_000 });
  await expect(page.getByRole('textbox', { name: /Опишите правку/ })).toHaveValue(/заголовок короче/);
  await snap(page, 'ask');
  await card(page).getByRole('button', { name: 'Пропустить' }).click();

  await expect(title(page)).toHaveText('Откройте страницу «Цены»');
  await snap(page, 'page');
  await page.getByText('Цены', { exact: true }).first().click();
  await expect(title(page)).toHaveText('Переключите «За год»', { timeout: 10_000 });
  await card(page).getByRole('button', { name: 'Сделал' }).click();

  await expect(title(page)).toHaveText('Посмотрите на iPhone 14', { timeout: 10_000 });
  await snap(page, 'device');
  await page.locator('.hx-browser__dev').click();
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
  await page.locator('.hx-dl-card[data-id="relay-deck"]').click();
  await expect(title(page)).toHaveText('Перелистните слайд', { timeout: 15_000 });
  await snap(page, 'deck-next');
  await page.locator('.hx-deck__frame[data-on="1"]').click();
  await page.keyboard.press('ArrowRight');
  await expect(title(page)).toHaveText('Включите «Редактирование»', { timeout: 10_000 });
  await snap(page, 'deck-edit');
  await did(page);
  await expect(title(page)).toHaveText('Оставьте комментарий');
  await snap(page, 'deck-comment');
  await skip(page);
  await expect(title(page)).toHaveText('Покажите во весь экран');
  await snap(page, 'deck-show');
  await page.locator('.hx-deck__bar [data-k="full"]').click();
  await expect(title(page)).toHaveText('Попросите агента', { timeout: 10_000 });
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Редактирование' }).first().click();
  await expect(page.locator('textarea').filter({ visible: true }).first()).toHaveValue(/отзывом клиента/);
  await snap(page, 'deck-ask');
});

test('video lesson', async ({ page }) => {
  await begin(page, 'video');
  await expect(title(page)).toHaveText('Откройте «Relay за 30 секунд»', { timeout: 15_000 });
  await page.locator('.hx-dl-card[data-id="relay-video"]').click();
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
  await page.locator('.hx-dl-card[data-id="relay-ds"]').click();
  await expect(title(page)).toHaveText('Посмотрите, что внутри', { timeout: 15_000 });
  await snap(page, 'ds-look');
  await did(page);
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
