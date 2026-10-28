// Screenshots of the hub UI for tools/hub-e2e.sh, with the system Chrome (the same one web/playwright.config.ts drives).
//
//   node tools/hub-e2e/shots.mjs <hub url> <out dir> <cookie name> <cookie value> <space slug> <machine id>
//
// Signed out it takes the sign-in and sign-up pages; signed in (the session cookie of the script's owner) the
// onboarding steps, the space home, the settings and the machine's own page under /m/<id>/. A console error, a failed
// request or an HTTP 5xx on any of them is printed, and the exit code is 1.
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(here, '..', '..', 'web', 'package.json'));
const { chromium } = require('@playwright/test');

const [hub, outDir, cookieName, cookieValue, slug, machineId] = process.argv.slice(2);
if (!hub || !outDir) {
  console.error('usage: shots.mjs <hub url> <out dir> <cookie name> <cookie value> <space slug> <machine id>');
  process.exit(2);
}
mkdirSync(outDir, { recursive: true });

const problems = [];
const watch = (page, label) => {
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    // Signed out, the UI asks who it is and is told no: the browser logs that answer as an error.
    if (/status of 401/.test(m.text()) && m.location().url.endsWith('/api/me')) return;
    problems.push(`${label}: console error: ${m.text()}`);
  });
  page.on('pageerror', (e) => problems.push(`${label}: page error: ${e.message}`));
  page.on('requestfailed', (r) => {
    // Streams are closed by the page itself when it leaves.
    if (!/ERR_ABORTED/.test(r.failure()?.errorText ?? '')) problems.push(`${label}: request failed: ${r.url()}`);
  });
  page.on('response', (r) => {
    if (r.status() >= 500) problems.push(`${label}: HTTP ${r.status()} ${r.url()}`);
  });
};

const shoot = async (browser, { name, url, signedIn, mobile, settle }) => {
  const context = await browser.newContext(
    mobile
      ? { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true }
      : { viewport: { width: 1440, height: 900 } },
  );
  if (signedIn) {
    const { hostname } = new URL(hub);
    await context.addCookies([{ name: cookieName, value: cookieValue, domain: hostname, path: '/', httpOnly: true }]);
  }
  const page = await context.newPage();
  watch(page, name);
  await page.goto(`${hub}${url}`, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {});
  if (settle) await page.waitForTimeout(settle);
  const file = path.join(outDir, `e2e-${name}${mobile ? '-phone' : ''}.png`);
  await page.screenshot({ path: file, fullPage: false });
  console.log(`shot ${file} (${page.url().replace(hub, '')})`);
  await context.close();
};

const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const base = `/s/${slug}`;
  const shots = [
    { name: 'signin', url: '/signin' },
    { name: 'signup', url: '/signup' },
    { name: 'signin', url: '/signin', mobile: true },
    { name: 'setup-machines', url: `${base}/setup/machines`, signedIn: true },
    { name: 'setup-machines', url: `${base}/setup/machines`, signedIn: true, mobile: true },
    { name: 'setup-agents', url: `${base}/setup/agents`, signedIn: true },
    { name: 'setup-access', url: `${base}/setup/access`, signedIn: true },
    { name: 'setup-done', url: `${base}/setup/done`, signedIn: true },
    { name: 'space-home', url: base, signedIn: true },
    { name: 'space-settings', url: `${base}/settings`, signedIn: true },
    { name: 'account', url: '/settings', signedIn: true },
  ];
  if (machineId) shots.push({ name: 'runtime-page', url: `/m/${machineId}/`, signedIn: true, settle: 2500 });
  for (const s of shots) await shoot(browser, s);
} finally {
  await browser.close();
}

if (problems.length) {
  console.error(`${problems.length} problem(s) in the browser:`);
  for (const p of [...new Set(problems)]) console.error(`  ${p}`);
  process.exit(1);
}
console.log('no console errors, failed requests or 5xx');
