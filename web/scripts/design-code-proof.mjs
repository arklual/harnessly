#!/usr/bin/env node
// The proof of `harnessly design code` on the sample of internal/design/testdata/code: the exported
// application type-checks against the real package, and draws the same pixels as the mockup it came from.
//
//   DESIGN_CODE_PROOF=/tmp/p DESIGN_CODE_NODE_MODULES=$PWD/web/node_modules \
//     go test ./internal/design -run TestDesignCodeProof          # the mockup, the export and the build of the target
//   node web/scripts/design-code-proof.mjs /tmp/p [--modules web/node_modules] [--chrome /usr/bin/google-chrome]
//
// It prints one line per check and exits 1 when one fails. The screenshots stay in the folder (mockup.png, app.png).
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';

const args = process.argv.slice(2);
const opt = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : fallback;
};
const dir = path.resolve(args.find((a, i) => !a.startsWith('--') && !args[i - 1]?.startsWith('--')) || '');
const modules = path.resolve(
  opt('--modules', path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'node_modules')),
);
const chrome = opt('--chrome', process.env.CHROME_BIN || '/usr/bin/google-chrome');
if (
  !fs.existsSync(path.join(dir, 'app', 'dist', 'main.js')) ||
  !fs.existsSync(path.join(dir, 'mockup', 'index.html'))
) {
  console.error(
    'usage: node web/scripts/design-code-proof.mjs <folder TestDesignCodeProof left> [--modules node_modules] [--chrome bin]',
  );
  process.exit(2);
}
let failed = 0;
const report = (ok, what, detail = '') => {
  if (!ok) failed++;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${what}${detail ? `: ${detail}` : ''}`);
};

// 1. Types on the real package.
const tsc = spawnSync(
  process.execPath,
  [path.join(modules, 'typescript', 'bin', 'tsc'), '-p', path.join(dir, 'app', 'tsconfig.json')],
  { encoding: 'utf8' },
);
report(
  tsc.status === 0,
  'types (tsc) on @acme/ui',
  (tsc.stdout + tsc.stderr).trim().split('\n').slice(0, 10).join(' | '),
);

// 2. The same pixels: the mockup drawn by the board engine and the exported page drawn by the application.
const types = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
};
const server = http.createServer((req, res) => {
  const file = path.join(dir, decodeURIComponent(new URL(req.url, 'http://x').pathname));
  if (!file.startsWith(dir + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
    res.writeHead(404).end();
    return;
  }
  res.writeHead(200, { 'content-type': types[path.extname(file)] || 'application/octet-stream' });
  fs.createReadStream(file).pipe(res);
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;
const require = createRequire(path.join(modules, 'x.js'));
const { chromium } = require('@playwright/test');
const browser = await chromium.launch({ executablePath: chrome, args: ['--font-render-hinting=none'] });
try {
  const shot = async (url, file) => {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1 });
    const errors = [];
    page.on('pageerror', (e) => errors.push(String(e)));
    await page.goto(url, { waitUntil: 'networkidle' });
    await page.getByText('Create event').waitFor({ timeout: 20000 });
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(300);
    const png = await page.screenshot({ path: path.join(dir, file) });
    await page.close();
    return { png, errors };
  };
  const mock = await shot(`${base}/mockup/index.html`, 'mockup.png');
  const app = await shot(`${base}/app/index.html`, 'app.png');
  report(
    mock.errors.length === 0 && app.errors.length === 0,
    'no script errors',
    [...mock.errors, ...app.errors].join(' | '),
  );
  let differ = 0;
  if (!mock.png.equals(app.png)) {
    const page = await browser.newPage();
    differ = await page.evaluate(
      async ([a, b]) => {
        const load = async (src) => {
          const img = new Image();
          img.src = 'data:image/png;base64,' + src;
          await img.decode();
          const c = document.createElement('canvas');
          c.width = img.width;
          c.height = img.height;
          const ctx = c.getContext('2d');
          ctx.drawImage(img, 0, 0);
          return ctx.getImageData(0, 0, c.width, c.height).data;
        };
        const [x, y] = [await load(a), await load(b)];
        if (x.length !== y.length) return -1;
        let n = 0;
        for (let i = 0; i < x.length; i += 4)
          if (x[i] !== y[i] || x[i + 1] !== y[i + 1] || x[i + 2] !== y[i + 2] || x[i + 3] !== y[i + 3]) n++;
        return n;
      },
      [mock.png.toString('base64'), app.png.toString('base64')],
    );
    await page.close();
  }
  report(
    differ === 0,
    'mockup and exported page match pixel for pixel (1280x800)',
    differ ? `${differ} pixels differ (mockup.png, app.png)` : '',
  );
} finally {
  await browser.close();
  server.close();
}
process.exit(failed ? 1 : 0);
