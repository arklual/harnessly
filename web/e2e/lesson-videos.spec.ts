/// <reference lib="dom" />
// Ролики уроков «Обучения»: настоящая страница отдельного рантайма с фейковыми агентами, записанная
// браузером. Это не тест: запускается только по просьбе,
//
//   pnpm build && LESSON_VIDEOS=1 pnpm exec playwright test lesson-videos
//
// и пишет webm в LESSON_OUT (по умолчанию test-results/lessons). Курсор и клики рисует страница сама: в записи
// headless-браузера системного курсора нет.
import { mkdirSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { hostname } from 'node:os';
import { join, resolve } from 'node:path';

import { type Browser, type BrowserContext, expect, type Locator, type Page, test } from '@playwright/test';

import { startRuntime, type Runtime } from './runtime';

const OUT = resolve(process.env.LESSON_OUT ?? 'test-results/lessons');
const PASSWORD = 'correct horse';
const SIZE = { width: 1280, height: 800 };

test.skip(!process.env.LESSON_VIDEOS, 'records the lesson videos only when LESSON_VIDEOS is set');
test.describe.configure({ mode: 'default', timeout: 240_000 });

/** Курсор, клик и подпись поверх страницы: их видно в записи. */
const OVERLAY = `(() => {
  // Только CSSOM и Web Animations: страница не пускает встроенные <style> (CSP).
  if (window.__lesson) return;
  window.__lesson = true;
  const ns = 'http://www.w3.org/2000/svg';
  const mount = () => {
    const cur = document.createElement('div');
    Object.assign(cur.style, { position: 'fixed', left: '-40px', top: '-40px', zIndex: '2147483647', width: '26px', height: '26px',
      pointerEvents: 'none', transform: 'translate(-3px,-2px)', filter: 'drop-shadow(0 2px 4px rgba(0,0,0,.35))' });
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('viewBox', '0 0 24 24'); svg.setAttribute('width', '26'); svg.setAttribute('height', '26');
    const path = document.createElementNS(ns, 'path');
    path.setAttribute('d', 'M4 2.5 20 11l-7.2 1.9L9.6 20z'); path.setAttribute('fill', '#fff');
    path.setAttribute('stroke', '#111'); path.setAttribute('stroke-width', '1.4'); path.setAttribute('stroke-linejoin', 'round');
    svg.appendChild(path); cur.appendChild(svg);
    document.documentElement.appendChild(cur);
    addEventListener('mousemove', (e) => { cur.style.left = e.clientX + 'px'; cur.style.top = e.clientY + 'px'; }, true);
    addEventListener('mousedown', (e) => {
      const r = document.createElement('div');
      Object.assign(r.style, { position: 'fixed', left: e.clientX - 22 + 'px', top: e.clientY - 22 + 'px', width: '44px', height: '44px',
        borderRadius: '999px', border: '3px solid #7c6cff', pointerEvents: 'none', zIndex: '2147483646', boxSizing: 'border-box' });
      document.documentElement.appendChild(r);
      r.animate([{ transform: 'scale(.2)', opacity: 1 }, { transform: 'scale(1.3)', opacity: 0 }], { duration: 600, easing: 'ease-out', fill: 'forwards' });
      setTimeout(() => r.remove(), 700);
    }, true);
  };
  if (document.documentElement) mount(); else addEventListener('DOMContentLoaded', mount);
  // Служебные слова фейкового агента (long, widget N) в ролике не нужны: убираем их из текста страницы.
  const MARKS = [/,? это long/g, /,? long\\b/g, /,? widget \\d+/g];
  setInterval(() => {
    if (!document.body) return;
    // Предупреждения машины, на которой пишется ролик (память, диск), — не часть урока.
    for (const e of document.querySelectorAll('.hx-health, .hx-toasts, .hx-notice-slot, .hx-notice')) e.style.display = 'none';
    const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const t = n.textContent || '';
      if (!/long|widget/.test(t)) continue;
      let v = t; for (const m of MARKS) v = v.replace(m, '');
      if (v !== t) n.textContent = v;
    }
  }, 120);
})()`;

let rt: Runtime;
let browser: Browser;
let base = '';

async function hideHost(page: Page) {
  await page.evaluate(
    (hosts) => {
      const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
      for (let n = walk.nextNode(); n; n = walk.nextNode())
        for (const host of hosts)
          if (host && n.textContent?.includes(host))
            n.textContent = n.textContent.replaceAll(host, 'my-server');
    },
    [hostname(), hostname().split('.')[0] ?? ''],
  );
}

/** Плавно довести курсор до элемента и кликнуть. */
async function glide(page: Page, target: string | Locator, click = true, area?: string | Locator) {
  const loc = typeof target === 'string' ? page.locator(target) : target;
  // Элемент ниже края экрана: сначала плавно докручиваем к нему, иначе курсор уедет за кадр.
  const off = await loc
    .first()
    .evaluate(
      (el) => {
        const r = el.getBoundingClientRect();
        return r.bottom > innerHeight - 24 || r.top < 0;
      },
      undefined,
      { timeout: 5_000 },
    )
    .catch(() => false);
  if (off) {
    await loc.first().evaluate((el) => el.scrollIntoView({ block: 'center', behavior: 'smooth' }));
    await page.waitForTimeout(900);
  }
  const box = await loc
    .first()
    .boundingBox({ timeout: 5_000 })
    .catch(async () => {
      // Не нашли элемент: в лог — какой, рядом — снимок экрана в виде дерева ролей, чтобы поправить локатор.
      const n = `skip-${Date.now()}`;
      writeFileSync(
        join(OUT, `${n}.yml`),
        `# ${String(target)}\n` +
          (await page
            .locator('body')
            .ariaSnapshot()
            .catch(() => '')),
      );
      throw new Error(`no element ${String(target)} → ${n}.yml`);
    });
  if (!box) throw new Error(`no box for ${String(target)}`);
  // Куда ведёт курсор — точка, на которую карточка урока приблизит ролик.
  const focus = (page as Page & { camFocus?: (b: Box) => void }).camFocus;
  focus?.(box);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 28 });
  await page.waitForTimeout(350);
  // Окна, что уже стояли до клика, — не результат действия: их запоминаем и потом не считаем.
  const shown = () =>
    page.evaluate(() =>
      [...document.querySelectorAll('[role=menu],[role=listbox],[role=dialog]')]
        .filter((e) => {
          const st = getComputedStyle(e);
          return st.visibility !== 'hidden' && st.opacity !== '0';
        })
        .map((e) => e.getBoundingClientRect())
        .filter((b) => b.width > 40 && b.height > 30 && b.bottom > 0 && b.top < innerHeight)
        .map((b) => ({
          x: Math.round(b.x),
          y: Math.round(b.y),
          width: Math.round(b.width),
          height: Math.round(b.height),
        })),
    );
  const before = click ? await shown() : [];
  if (click) {
    await page.mouse.down();
    await page.mouse.up();
    // Открылось меню или окно — главное теперь оно: ролик центрирует кнопку вместе с ним.
    await page.waitForTimeout(450);
    const key = (b: Box) => `${b.x},${b.y},${b.width},${b.height}`;
    const was = new Set(before.map(key));
    const open = (await shown()).filter((b) => !was.has(key(b)) && b.width < 900 && b.height < 650);
    // Клик открыл раздел, а не меню (настройки, вкладка) — главное теперь этот раздел.
    if (area) {
      const ab = await (typeof area === 'string' ? page.locator(area) : area)
        .first()
        .boundingBox({ timeout: 2_000 })
        .catch(() => null);
      if (ab)
        open.push({
          x: Math.round(ab.x),
          y: Math.round(ab.y),
          width: Math.round(ab.width),
          height: Math.round(ab.height),
        });
    }
    if (open.length) {
      const all = [box, ...open];
      const x = Math.min(...all.map((b) => b.x));
      const y = Math.min(...all.map((b) => b.y));
      focus?.({
        x,
        y,
        width: Math.max(...all.map((b) => b.x + b.width)) - x,
        height: Math.max(...all.map((b) => b.y + b.height)) - y,
      });
    }
    await page.waitForTimeout(50);
  } else await page.waitForTimeout(500);
}
type Box = { x: number; y: number; width: number; height: number };

/** Действие по месту на экране, а не по элементу (клик в макете, взгляд на список): тоже точка для камеры. */
async function spot(page: Page, b: Box, click = false) {
  (page as Page & { camFocus?: (b: Box) => void }).camFocus?.(b);
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 26 });
  await page.waitForTimeout(350);
  if (click) {
    await page.mouse.down();
    await page.mouse.up();
  }
  await page.waitForTimeout(400);
}

/** Записать урок: свой контекст с видео, а файл — под имя урока. */
/** Флаги фейкового агента файлом (долгий ход, виджет): в тексте сообщения служебных слов нет. */
const flag = (n: 'long' | 'widget', v = '1') => writeFileSync(join(rt.home, `.fake-${n}`), v);
const unflag = () => {
  for (const n of ['long', 'widget']) rmSync(join(rt.home, `.fake-${n}`), { force: true });
};

async function record(name: string, run: (page: Page) => Promise<void>) {
  unflag();
  const dir = join(OUT, '.raw', name);
  mkdirSync(dir, { recursive: true });
  const ctx: BrowserContext = await browser.newContext({
    viewport: SIZE,
    colorScheme: 'light',
    recordVideo: { dir, size: SIZE },
    storageState: join(OUT, '.state.json'),
  });
  await ctx.addInitScript(OVERLAY);
  const page = await ctx.newPage();
  const marks: { t: number; cap: string }[] = [];
  const t0 = Date.now();
  (page as Page & { cap?: (c: string) => void }).cap = (cap: string) =>
    marks.push({ t: (Date.now() - t0) / 1000, cap });
  const focus: { t: number; x: number; y: number; w: number; h: number }[] = [];
  (page as Page & { camFocus?: (b: Box) => void }).camFocus = (b: Box) =>
    focus.push({
      t: (Date.now() - t0) / 1000,
      x: Math.round(b.x + b.width / 2),
      y: Math.round(b.y + b.height / 2),
      w: Math.round(b.width),
      h: Math.round(b.height),
    });
  await run(page);
  unflag();
  await page.waitForTimeout(800);
  const video = page.video();
  await ctx.close();
  if (video) renameSync(await video.path(), join(OUT, `${name}.webm`));
  writeFileSync(join(OUT, `${name}.json`), JSON.stringify(marks, null, 2));
  writeFileSync(join(OUT, `${name}.focus.json`), JSON.stringify(focus));
}
const cap = (page: Page, c: string) => (page as Page & { cap: (c: string) => void }).cap(c);

test.beforeAll(async ({ browser: b }) => {
  browser = b;
  mkdirSync(OUT, { recursive: true });
  rt = await startRuntime({ env: { RUNTIME_LOCALE: 'ru', RUNTIME_WEB_FEATURES: 'chats,design' } });
  writeFileSync(
    join(rt.home, '.fake-reply.md'),
    '## Нашёл, почему падает загрузка аватарок\n\n' +
      'Файлы больше **5 МБ** упираются в лимит прокси, а клиент молча ждёт ответа. Прогнал три размера:\n\n' +
      '| Размер | До правки | После |\n|---|---|---|\n| 800 КБ | ✅ 0,4 с | ✅ 0,3 с |\n| 4,9 МБ | ✅ 2,1 с | ✅ 0,9 с |\n| 12 МБ | ❌ 413 | ✅ 1,6 с |\n\n' +
      'Что поменял:\n\n1. Сжимаю картинку на клиенте до 2048 px.\n2. Поднял лимит в `nginx.conf` до 20 МБ.\n3. Показываю понятную ошибку вместо вечного спиннера.\n\n' +
      '```ts\nconst blob = await resize(file, { maxSide: 2048, quality: 0.85 });\nawait upload(`/api/avatar`, blob);\n```\n\n' +
      'Правка в `src/avatar/upload.ts`, тест на большой файл — рядом.',
  );
  // Вход один раз: установка, пароль, «Что нового» закрыт. Уроки начинаются с готового экрана.
  const ctx = await browser.newContext({ viewport: SIZE });
  const page = await ctx.newPage();
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill(PASSWORD);
  await setup.locator('input[name=confirm]').fill(PASSWORD);
  await setup.locator('button[type=submit]').click();
  await expect(page.locator('.hx-hello')).toBeVisible();
  base = new URL(page.url()).pathname.replace(/\/$/, '');
  const gotIt = page.getByRole('button', { name: /^(Got it|Понятно)$/ });
  await gotIt.click({ timeout: 5_000 }).catch(() => undefined);
  await ctx.storageState({ path: join(OUT, '.state.json') });
  await ctx.close();
});
test.afterAll(async () => {
  await rt?.stop();
});

test('chat: первый чат', async () => {
  await record('chat', async (page) => {
    await page.goto(`${rt.url}${base}/`);
    await expect(page.locator('.hx-hello')).toBeVisible();
    await page
      .getByRole('button', { name: /^(Got it|Понятно)$/ })
      .click({ timeout: 2_000 })
      .catch(() => undefined);
    await hideHost(page);
    await page.mouse.move(900, 300);
    await page.waitForTimeout(900);

    cap(page, 'Напишите задачу обычными словами');
    await glide(page, page.getByRole('textbox', { name: 'Сообщение агенту' }));
    await page.keyboard.type('Разберись, почему падает загрузка аватарок', { delay: 55 });
    await page.waitForTimeout(900);

    cap(page, '«+» — файл, скриншот, макет или задача');
    await glide(page, page.getByRole('button', { name: 'Добавить', exact: true }));
    await page.waitForTimeout(1800);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(500);

    cap(page, 'Агент, модель и глубина мышления — на каждый чат свои');
    await glide(page, page.getByRole('button', { name: /^Агент, модель/ }));
    await page.waitForTimeout(2000);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(500);

    cap(page, 'Голосом — тоже можно: текст появится прямо в поле');
    await glide(page, page.getByRole('button', { name: 'Голосовой ввод' }), false);
    await page.waitForTimeout(1600);

    cap(page, 'Enter — и агент берётся за работу');
    await glide(page, page.getByRole('button', { name: 'Отправить' }));
    await expect(page).toHaveURL(/\/c\/[^/]+$/);
    await expect(page.locator('.hx-feed').getByText('src/avatar/upload.ts')).toBeVisible({ timeout: 60_000 });
    await hideHost(page);
    await page.waitForTimeout(1500);

    cap(page, 'Ход агента свёрнут в строку — раскройте, если интересно');
    // Свёрнутый ход работы — кнопка с командой агента.
    await soft(() => glide(page, page.getByRole('button', { name: /\$ ls -la/ }).first()));
    await page.waitForTimeout(2200);
    await page.mouse.move(1300, 450, { steps: 20 });
    await page.waitForTimeout(1200);
    writeFileSync(join(OUT, 'chat.aria.yml'), await page.locator('body').ariaSnapshot());
  });
});

test('pane: правая панель', async () => {
  await record('pane', async (page) => {
    await page.goto(`${rt.url}${base}/`);
    await page
      .getByRole('button', { name: /^(Got it|Понятно)$/ })
      .click({ timeout: 2_000 })
      .catch(() => undefined);
    reply(
      'Поднял dev-сервер на **localhost:5173** и прошёлся по главной:\n\n- открывается за **1,2 с**\n- ошибок в консоли нет\n- одна картинка без `alt` — поправил в `Hero.tsx`',
    );
    await send(page, 'Подними dev-сервер и проверь главную');
    await expect(page.locator('.hx-feed').getByText('localhost:5173').first()).toBeVisible({
      timeout: 60_000,
    });
    await hideHost(page);
    await page.mouse.move(700, 400);
    await page.waitForTimeout(800);

    cap(page, 'Кнопка справа открывает панель инструментов');
    await glide(page, '.hx-header__tools');
    await expect(page.locator('.hx-pane .hx-tl__tile').first()).toBeVisible();
    await page.waitForTimeout(1600);

    cap(page, 'Терминал агента — те же команды, что видит он');
    await glide(page, '.hx-header__term');
    await page.waitForTimeout(2600);
    await glide(page, '.hx-header__term');
    await page.waitForTimeout(600);

    cap(page, 'Браузер, файлы, изменения — всё рядом с чатом');
    for (const name of ['Браузер', 'Файлы', 'Изменения']) {
      const tile = page.getByRole('option', { name: new RegExp(`^${name}`) }).first();
      if (await tile.count()) {
        await soft(() => glide(page, tile, false));
        await page.waitForTimeout(500);
      }
    }
    await page.waitForTimeout(1000);
  });
});

test('safe: сейф и подключения', async () => {
  await record('safe', async (page) => {
    await page.goto(`${rt.url}${base}/settings/general`);
    await expect(page.locator('.hx-set')).toBeVisible();
    await hideHost(page);
    await page.mouse.move(700, 300);
    await page.waitForTimeout(900);
    cap(page, 'Ключи и токены живут в Сейфе, не в чате');
    const safe = page.locator('.hx-set a, .hx-set button', { hasText: /^Сейф$/ }).first();
    if (await safe.count()) {
      await soft(() => glide(page, safe, true, '.hx-set__page'));
      await page.waitForTimeout(2600);
    }
    cap(page, 'Подключения: трекер, вики, GitLab — агент читает их сам');
    const conn = page.locator('.hx-set a, .hx-set button', { hasText: /^Подключения$/ }).first();
    if (await conn.count()) {
      await soft(() => glide(page, conn, true, '.hx-set__page'));
      await page.waitForTimeout(2600);
    }
    await page.waitForTimeout(800);
  });
});

/** Сайт, презентация, ролик и дизайн-система Relay — демо-проекты, которые рантайм раздаёт каждой библиотеке сам. */
/** Шаг, без которого ролик всё равно получится: не нашёлся элемент — едем дальше. */
async function soft(step: () => Promise<unknown>) {
  try {
    await step();
  } catch (e) {
    // Пропущенный шаг виден в ролике, а не валит запись; в логе — чтобы знать, где камере не хватило точки.
    console.log('[lesson skip]', String(e).split('\n')[0].slice(0, 160));
  }
}
const reply = (text: string) => writeFileSync(join(rt.home, '.fake-reply.md'), text);
const home = async (page: Page) => {
  await page.goto(`${rt.url}${base}/`);
  await page
    .getByRole('button', { name: /^(Got it|Понятно)$/ })
    .click({ timeout: 1_500 })
    .catch(() => undefined);
  await hideHost(page);
};
const openSinopia = async (page: Page) => {
  await home(page);
  await page.getByRole('button', { name: 'Sinopia Design' }).first().click();
  await expect(page.getByText('Что нарисуем?')).toBeVisible();
  await page.waitForTimeout(800);
};
const send = async (page: Page, text: string) => {
  await page.getByRole('textbox', { name: 'Сообщение агенту' }).fill(text);
  await page.getByRole('textbox', { name: 'Сообщение агенту' }).press('Enter');
};

test('work: пока агент работает', async () => {
  reply(
    '**Зависимости обновлены**, все 248 тестов зелёные.\n\n| Пакет | Было | Стало |\n|---|---|---|\n| react | 18.3 | 19.1 |\n| vite | 5.4 | 6.2 |\n| vitest | 1.6 | 3.0 |\n\nПо линтеру: осталось **2 предупреждения** про неиспользуемые импорты — убрал.',
  );
  await record('work', async (page) => {
    await home(page);
    await page.mouse.move(800, 300);
    cap(page, 'Агент работает — весь ход виден одной строкой');
    await glide(page, page.getByRole('textbox', { name: 'Сообщение агенту' }));
    flag('long');
    await page.keyboard.type('Обнови зависимости и прогони тесты', { delay: 45 });
    await page.keyboard.press('Enter');
    await page.waitForTimeout(3500);
    cap(page, 'Раскройте ход: команды, файлы, правки');
    await soft(() => glide(page, page.getByRole('button', { name: /\$ ls -la/ }).first(), false));
    await page.waitForTimeout(2500);
    cap(page, 'Пока он думает, пишите дальше — сообщение встанет в очередь');
    await glide(page, page.getByRole('textbox').last());
    await page.keyboard.type('И потом посмотри предупреждения линтера', { delay: 45 });
    await page.keyboard.press('Enter');
    await page.waitForTimeout(4000);
    cap(page, 'Агент закончит — и сам возьмёт следующее');
    await expect(page.locator('.hx-feed').getByText('Зависимости обновлены').first()).toBeVisible({
      timeout: 60_000,
    });
    await hideHost(page);
    await page.waitForTimeout(2500);
  });
});

test('site: синопия · сайт', async () => {
  await record('site', async (page) => {
    await openSinopia(page);
    const frame = page.frameLocator('.hx-site__iframe');
    cap(page, 'Демо-проект Relay — ваш, его можно ломать');
    await soft(() => glide(page, page.getByRole('button', { name: /^Relay — лендинг/ })));
    await page.waitForTimeout(3500);
    cap(page, 'Макет живой: переключатели работают, как в продукте');
    await soft(() => glide(page, frame.locator('.flag').nth(1)));
    await page.waitForTimeout(700);
    await soft(() => glide(page, frame.locator('.flag').nth(2)));
    await page.waitForTimeout(1500);
    cap(page, 'Редактирование — правки делает агент');
    await soft(() => glide(page, page.getByRole('button', { name: 'Редактирование' }).first()));
    await page.waitForTimeout(1500);
    await soft(() => glide(page, page.locator('.hx-island__mini')));
    await page.waitForTimeout(800);
    await page.keyboard.type('Сделай главный заголовок короче и ярче', { delay: 40 });
    await page.waitForTimeout(1500);
    await page.keyboard.press('Escape');
    cap(page, 'Страницы проекта — вкладками сверху');
    await soft(() => glide(page, page.getByText('Цены', { exact: true }).first()));
    await page.waitForTimeout(2500);
    cap(page, 'Цены пересчитываются — логика макета настоящая');
    await soft(() => glide(page, frame.locator('label[for="yr"]')));
    await page.waitForTimeout(2200);
    cap(page, 'Как это выглядит на телефоне — в один клик');
    await soft(() => glide(page, page.getByRole('button', { name: 'Устройство' }).first()));
    await page.waitForTimeout(1200);
    await soft(() => glide(page, page.getByRole('menuitemradio', { name: /^iPhone 14/ }).first()));
    await page.waitForTimeout(3000);
  });
});

test('slides: синопия · презентация', async () => {
  await record('slides', async (page) => {
    await openSinopia(page);
    cap(page, 'Слайды: тема, длина и тон — в настройках');
    await soft(() => glide(page, page.getByRole('radio', { name: /^Слайды/ })));
    await soft(() => glide(page, page.locator('.hx-deck-chip').first()));
    await page.waitForTimeout(2200);
    await page.keyboard.press('Escape');
    cap(page, 'Готовая презентация — живые слайды, не картинки');
    await soft(() => glide(page, page.getByRole('button', { name: /^Relay — релизы без страха/ })));
    await page.waitForTimeout(3500);
    for (let i = 0; i < 3; i++) {
      await soft(() =>
        glide(
          page,
          page.locator('[aria-label*="лед"], [aria-label*="Next"], [aria-label*="Вперёд"]').last(),
          true,
          'iframe',
        ),
      );
      await page.keyboard.press('ArrowRight');
      await page.waitForTimeout(1600);
    }
    cap(page, 'Редактирование: колоду правит агент, заметки — под слайдом');
    await soft(() => glide(page, page.getByRole('button', { name: 'Редактирование' }).first()));
    await spot(page, { x: 600, y: 260, width: 520, height: 300 });
    await page.waitForTimeout(1600);
    cap(page, 'Комментарии команды — метками на слайде');
    await soft(() => glide(page, page.getByRole('button', { name: /Комментировать/ }).first(), false));
    await page.waitForTimeout(1500);
    cap(page, 'Показ — во весь экран, одной кнопкой');
    await soft(() => glide(page, page.getByRole('button', { name: /^Показать/ }).first()));
    await page.waitForTimeout(3000);
    await page.keyboard.press('ArrowRight');
    await page.waitForTimeout(2000);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(1000);
  });
});

test('video: синопия · видео', async () => {
  await record('video', async (page) => {
    await openSinopia(page);
    cap(page, 'Видео — это сцены, как слайды, только во времени');
    await soft(() => glide(page, page.getByRole('radio', { name: /^Видео/ }).first()));
    await page.waitForTimeout(1200);
    await soft(() => glide(page, page.getByRole('button', { name: /^Relay за 30 секунд/ })));
    await page.waitForTimeout(4000);
    cap(page, 'Каждая сцена — живая анимация');
    await page.mouse.move(700, 400, { steps: 25 });
    await page.keyboard.press('Space');
    await page.waitForTimeout(5000);
    cap(page, 'Листайте сцены и двигайте таймлайн');
    await soft(() =>
      glide(page, page.getByRole('button', { name: 'Следующая сцена' }).first(), true, 'iframe'),
    );
    await page.waitForTimeout(3500);
    await soft(() => glide(page, page.getByRole('slider', { name: 'Позиция в ролике' }).first()));
    await page.waitForTimeout(2000);
    cap(page, 'Скачать — в mp4, рендер прямо на машине');
    await soft(() => glide(page, page.getByRole('button', { name: /Скачать/ }).first()));
    await page.waitForTimeout(2500);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(800);
  });
});

test('ds: синопия · дизайн-система', async () => {
  await record('ds', async (page) => {
    await openSinopia(page);
    cap(page, 'Дизайн-система — цвета, шрифты и компоненты для всех макетов');
    await soft(() => glide(page, page.getByRole('radio', { name: /^Дизайн-система/ }).first()));
    await page.waitForTimeout(1200);
    cap(page, 'Соберите её с сайта, из кода или по описанию');
    await soft(() => glide(page, page.getByRole('textbox').first()));
    await page.keyboard.type('Собери дизайн-систему с нашего сайта relay.dev', { delay: 40 });
    await page.waitForTimeout(1500);
    cap(page, 'Выберите систему — новые макеты пойдут в этом стиле');
    await soft(() => glide(page, page.getByRole('radio', { name: /^Макет сайта/ })));
    await soft(() => glide(page, page.locator('.hx-deck-chip').first()));
    await page.waitForTimeout(3000);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(800);
  });
});

test('agents: несколько агентов', async () => {
  reply(
    'Разбил аудит на три части и раздал сабагентам:\n\n- **доступность** — 4 замечания\n- **скорость загрузки** — бандл можно ужать на 30%\n- **мёртвый код** — 12 неиспользуемых файлов\n\nПлан и отчёты — в правой панели.',
  );
  await record('agents', async (page) => {
    await home(page);
    cap(page, 'Чаты работают параллельно');
    flag('long');
    await send(page, 'Почини флаки-тесты в CI');
    await page.waitForTimeout(2000);
    await glide(page, page.getByRole('button', { name: 'Новый диалог' }).first());
    await page.waitForTimeout(800);
    await send(page, 'Разбей аудит фронта на части');
    await page.waitForTimeout(2500);
    cap(page, 'Статус каждого — значком в списке чатов');
    await spot(page, { x: 8, y: 360, width: 224, height: 160 });
    await page.waitForTimeout(2100);
    cap(page, 'Сабагенты, план, файлы — в правой панели');
    await soft(() => glide(page, page.getByRole('button', { name: /^Инструменты/ }).first()));
    await page.waitForTimeout(1200);
    await soft(() => glide(page, page.getByRole('option', { name: /^Сабагенты/ }).first()));
    await page.waitForTimeout(3500);
    await hideHost(page);
  });
});

test('extra: приятные мелочи', async () => {
  reply('Собрал время сборки по дням в виджет: в среднем **3 мин 40 с**, медленнее всего — шаг `typecheck`.');
  await record('extra', async (page) => {
    await home(page);
    cap(page, 'Виджеты и приложения — прямо в ленте чата');
    await glide(page, page.getByRole('textbox', { name: 'Сообщение агенту' }));
    flag('widget', '7');
    await page.keyboard.type('Покажи время сборки по дням виджетом', { delay: 40 });
    await page.keyboard.press('Enter');
    await page.waitForTimeout(6000);
    await hideHost(page);
    cap(page, 'Боковой чат: спросите, не отвлекая агента');
    await soft(() => glide(page, page.getByRole('button', { name: /^Инструменты/ }).first()));
    await soft(() => glide(page, page.getByRole('option', { name: /^Боковой чат/ }).first()));
    await page.waitForTimeout(2500);
    await page.keyboard.press('Escape');
    cap(page, 'Поиск по всем чатам и файлам');
    await soft(() => glide(page, page.getByRole('button', { name: 'Поиск' }).first()));
    await page.waitForTimeout(700);
    await page.keyboard.type('сборк', { delay: 70 });
    await page.waitForTimeout(2500);
    await page.keyboard.press('Escape');
    cap(page, 'Уведомления — и в Telegram, когда агент закончил');
    await soft(() => glide(page, page.getByRole('button', { name: 'Уведомления' }).first()));
    await page.waitForTimeout(2500);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(600);
  });
});
