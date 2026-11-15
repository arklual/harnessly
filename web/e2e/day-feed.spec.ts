// The page of the day the host draws itself from a stage view's feed, on a real runtime with
// HARNESSLY_UI_SLOTS=on. The feed is the example one (examples/ext-day-feed), read the way a view with a feedSecret
// is: through the runtime's own path, which the browser's router answers here with the example's files. The clock stands
// on the day the example's dates are written for. Screenshots go to SLOT_SHOTS when it is set.
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, test } from '@playwright/test';

import { startRuntime } from './runtime';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');
const PASSWORD = 'correct horse';
const SHOTS = process.env.SLOT_SHOTS;
const FEED = 'https://example.com/day';
// What the runtime names in the list of apps for a view with a feedSecret (cmd/runtime/routes_apps_feed.go).
const FEED_PATH = '/apps/feed/example-corp.day/day';
const NOW = new Date('2026-03-12T10:40:00Z');
const UNBROKEN = 'release_2026_03_12_export_reports_to_a_file_with_the_background_job_and_its_tests_final';
const feedFile = (name: string) => readFileSync(join(repo, 'examples', 'ext-day-feed', 'feed', name), 'utf8');

test('the page of the day is drawn by the host from the feed a stage view names', async ({ browser }) => {
  const root = mkdtempSync(join(tmpdir(), 'hs-day-feed-'));
  const rt = await startRuntime({ root, env: { HARNESSLY_UI_SLOTS: 'on' } });
  try {
    let cli = process.env.HARNESSLY_BIN;
    if (!cli) {
      cli = join(root, 'harnessly');
      execFileSync('go', ['build', '-o', cli, './cmd/harnessly'], { cwd: repo, stdio: 'inherit' });
    }
    execFileSync(cli, ['app', 'install', join(repo, 'examples', 'ext-home-stage')], {
      env: { ...process.env, HARNESSLY_DATA_DIR: join(rt.home, '.harnessly') },
      stdio: 'inherit',
    });
    const context = await browser.newContext({ locale: 'en-US', timezoneId: 'UTC' });
    const page = await context.newPage();
    await page.clock.setFixedTime(NOW);

    // The stage view of the example names a feed the machine signs in (`feedSecret`): the list of the installed apps
    // gets the feed and its path through the runtime on its way to the page.
    await page.route(/\/installed-apps(\?|$)/, async (route) => {
      const res = await route.fetch();
      const list = (await res.json()) as {
        apps: { views?: { height?: string; feed?: string; feedPath?: string }[] }[];
      };
      for (const app of list.apps)
        for (const view of app.views ?? [])
          if (view.height === 'stage') {
            view.feed = FEED;
            view.feedPath = FEED_PATH;
          }
      await route.fulfill({ response: res, json: list });
    });
    // The runtime's road to the feed (<platform api>/apps/feed/<app>/<view>), answered here with the example's files:
    // the day, a thing, and what the person decided about it. Until the token is put, it answers as a machine without
    // one does.
    let tokenPut = false;
    const asked: string[] = [];
    const written: { sub: string; machine: boolean; marked: string | undefined; body: string }[] = [];
    await page.route(/\/apps\/feed\/example-corp\.day\/day(\/|\?|$)/, async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const sub = url.pathname.slice(url.pathname.indexOf(FEED_PATH) + FEED_PATH.length);
      asked.push(`${req.method()} ${sub}`);
      if (!tokenPut)
        return route.fulfill({ status: 409, json: { error: 'secret-missing', secret: 'token' } });
      if (req.method() === 'POST') {
        written.push({
          sub,
          machine: url.searchParams.has('machine'),
          marked: req.headers()['x-requested-with'],
          body: req.postData() ?? '',
        });
        return route.fulfill({ status: 200, json: {} });
      }
      const item = /^\/items\/(\d+)$/.exec(sub);
      if (item)
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: feedFile(`items/${item[1]}.json`),
        });
      if (sub === '') {
        // A real feed carries words that do not break (a branch, an address): one stands in a card here, so the check
        // of the phone's width below means something.
        const today = JSON.parse(feedFile('day.json')) as { priorities: { points?: string[] }[] };
        const third = today.priorities[2];
        if (third) third.points = [UNBROKEN];
        return route.fulfill({ status: 200, json: today });
      }
      return route.fulfill({ status: 404, json: { error: 'not-found' } });
    });

    await page.goto(`${rt.url}/`);
    const setup = page.getByTestId('setup-form');
    await setup.locator('input[name=password]').fill(PASSWORD);
    await setup.locator('input[name=confirm]').fill(PASSWORD);
    await setup.locator('button[type=submit]').click();
    await expect(page.locator('.hx-app')).toBeVisible();
    // «Что нового» and the welcome of the lessons come up once on a fresh browser; the person puts them away first.
    for (const close of ['.hx-news__ok', '.hx-learn-hello__x']) {
      const el = page.locator(close);
      await el.waitFor({ timeout: 10_000 }).then(
        () => el.click(),
        () => undefined,
      );
      await expect(el).toHaveCount(0);
    }
    // The machine's setup card of a fresh runtime stands over the greeting; the person hides it.
    const quiet = page.locator('.hx-setup__quiet');
    await quiet.waitFor({ timeout: 10_000 }).then(
      () => quiet.click(),
      () => undefined,
    );
    await expect(page.locator('.hx-setup')).toHaveCount(0);

    // The host draws the day itself: no frame on the stage, the greeting of the day over the field.
    await expect(page.locator('.hx-stage[data-home=stage]')).toBeVisible({ timeout: 30_000 });
    const day = page.locator('.hx-home-stage .hx-day');
    // The machine has no token for the feed yet: the page says where it is put, and shows no sign-in of the feed.
    await expect(day.getByText('Example Corp day needs its token')).toBeVisible({ timeout: 30_000 });
    await expect(day.getByRole('button', { name: "Open the app's card" })).toBeVisible();
    await expect(day.getByRole('button', { name: 'Sign in' })).toHaveCount(0);
    tokenPut = true;
    await day.getByRole('button', { name: 'Retry' }).click();
    await expect(day.getByText('Review the export merge request')).toBeVisible({ timeout: 30_000 });
    await expect(page.locator('.hx-home-stage iframe')).toHaveCount(0);
    await expect(page.locator('.hx-hello__title')).toHaveText('Good morning, Alex');
    await expect(page.locator('.hx-hello__sub')).toContainText('Start with the export review');
    await expect(day.locator('.hx-day-k__now')).toHaveAttribute('data-t', '10:40');
    await expect(day.locator('.hx-day-e[data-stack]')).toHaveCount(1);
    // The field stays over the page of the day.
    const fieldBox = await page.locator('.hx-dock').boundingBox();
    const stageBox = await page.locator('.hx-home-stage').boundingBox();
    expect(fieldBox && stageBox && fieldBox.y + fieldBox.height <= stageBox.y + 4).toBe(true);

    // What is scrolled or could be scrolled sideways on the page, and what stands outside the screen's width. The
    // strips that scroll inside themselves (the calendar, the slides of a thing) are theirs to scroll.
    const sideways = () =>
      page.evaluate(() => {
        const width = document.documentElement.clientWidth;
        const name = (el: Element) => `${el.tagName.toLowerCase()}.${[...el.classList].join('.')}`;
        const inner = '.hx-day-k, .hx-day-sl, .hx-day-ds';
        const scrolls = [document.documentElement, document.body, ...document.querySelectorAll('.hx-app *')]
          .filter((el) => !el.matches(inner) && !el.closest(inner))
          .filter(
            (el) =>
              el.scrollLeft !== 0 ||
              (el.matches('.hx-stage, .hx-home-stage, .hx-day, .hx-day__scroll, .hx-day-item') &&
                el.scrollWidth > el.clientWidth + 1),
          )
          .map(name);
        const sticksOut = [...document.querySelectorAll('.hx-home-stage *, .hx-dock *')]
          .filter((el) => !el.closest(inner))
          .filter((el) => {
            const box = el.getBoundingClientRect();
            return box.width > 0 && box.height > 0 && (box.right > width + 1 || box.left < -1);
          })
          .map(name);
        return { scrolls, sticksOut: [...new Set(sticksOut)].slice(0, 12) };
      });
    const shoot = async (name: string) => {
      if (!SHOTS) return;
      mkdirSync(SHOTS, { recursive: true });
      for (const theme of ['light', 'dark'] as const) {
        await page.emulateMedia({ colorScheme: theme });
        for (const [w, h] of [
          [1440, 900],
          [390, 844],
        ] as const) {
          await page.setViewportSize({ width: w, height: h });
          await page.waitForTimeout(500);
          await page.screenshot({ path: join(SHOTS, `day-${name}-${theme}-${w}.png`) });
        }
      }
      await page.emulateMedia({ colorScheme: 'light' });
      await page.setViewportSize({ width: 1440, height: 900 });
    };
    await shoot('page');

    // On a phone the whole greeting stays in sight over the field, with its second line.
    await page.setViewportSize({ width: 390, height: 844 });
    const stageTop = (await page.locator('.hx-stage[data-home=stage]').boundingBox())?.y ?? 0;
    await expect
      .poll(async () => (await page.locator('.hx-hello__title').boundingBox())?.y ?? -1)
      .toBeGreaterThanOrEqual(stageTop);
    const helloBox = await page.locator('.hx-hello').boundingBox();
    const dockBox = await page.locator('.hx-dock .hx-composer__input').boundingBox();
    expect(helloBox && dockBox && helloBox.y + helloBox.height <= dockBox.y).toBe(true);
    // And nothing goes off the screen sideways: the calendar strip scrolls inside itself, the page does not.
    await expect.poll(sideways).toEqual({ scrolls: [], sticksOut: [] });
    // A link of the greeting to a meeting moves the strip to it, and nothing else: the screen stays in its place.
    const strip = day.locator('.hx-day-k');
    await strip.evaluate((el) => el.scrollTo({ left: 0 }));
    await page.locator('.hx-hello__sub .hx-day-link', { hasText: 'Planning' }).click();
    await expect.poll(() => strip.evaluate((el) => el.scrollLeft)).toBeGreaterThan(100);
    await page.waitForTimeout(600);
    expect(await sideways()).toEqual({ scrolls: [], sticksOut: [] });
    await page.setViewportSize({ width: 1440, height: 900 });

    // What the agent gathered for a meeting comes up over the strip.
    await day.locator('.hx-day-e', { hasText: 'Venue call' }).hover();
    await expect(page.locator('.hx-day-pop')).toContainText('Dana asks whether Friday still stands.');
    await page.mouse.move(5, 5);
    await expect(page.locator('.hx-day-pop')).toHaveCount(0);

    // The agent's offer stays on the strip when the person takes it; the feed has nothing to write it with yet.
    const offer = day.locator('.hx-day-e[data-offer]');
    await offer.hover();
    await page.locator('.hx-day-pop').getByRole('button', { name: 'Put it' }).click();
    await expect(day.locator('.hx-day-e[data-offer]')).toHaveCount(0);
    await expect(day.locator('.hx-day-e', { hasText: 'Numbers with Lee' })).toBeVisible();

    // A link of the greeting opens the thing into its chat; the field gives way to it.
    await page.locator('.hx-hello__sub .hx-day-link', { hasText: 'the export review' }).click();
    const item = page.locator('.hx-day-item');
    await expect(item.getByText('What the agent would change')).toBeVisible();
    await expect(page.locator('.hx-dock')).toBeHidden();
    await item.getByRole('button', { name: 'Show the other files' }).click();
    await expect(item.getByText('package.json')).toBeVisible();
    await shoot('item');
    await page.setViewportSize({ width: 390, height: 844 });
    await expect.poll(sideways).toEqual({ scrolls: [], sticksOut: [] });
    await page.setViewportSize({ width: 1440, height: 900 });
    await item.getByRole('button', { name: 'Send the comments to Robin' }).click();
    await expect.poll(() => written.length).toBe(1);
    expect(written[0]).toEqual({ sub: '/items/101/take', machine: true, marked: 'harnessly', body: '{}' });
    expect(asked).toEqual(expect.arrayContaining(['GET ', 'GET /items/101', 'POST /items/101/take']));
    await item.getByRole('button', { name: 'Back' }).click();
    await expect(item).toHaveCount(0);
    await expect(page.locator('.hx-dock .hx-composer__input')).toBeVisible();
    await context.close();
  } finally {
    await rt.stop();
  }
});
