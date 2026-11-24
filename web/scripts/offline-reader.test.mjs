// Run after the production web build: node --test scripts/offline-reader.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { chromium } from '@playwright/test';

test('cold offline navigation restores a document and its local draft under a mounted base', async (t) => {
  let disconnected = false;
  const server = createServer(async (req, res) => {
    if (disconnected) {
      req.socket.destroy();
      return;
    }
    const path = new URL(req.url, 'http://localhost').pathname.slice('/agents/'.length);
    if (path === 'setup') {
      res.setHeader('Content-Type', 'text/html');
      res.end('<title>Setup</title>');
      return;
    }
    try {
      const bytes = await readFile(new URL('../dist/' + path, import.meta.url));
      res.setHeader(
        'Content-Type',
        path.endsWith('.js') ? 'text/javascript' : path.endsWith('.css') ? 'text/css' : 'text/html',
      );
      res.end(bytes);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => server.close());
  const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH });
  t.after(() => browser.close());
  const page = await browser.newPage({ locale: 'en-US' });
  const base = 'http://127.0.0.1:' + server.address().port + '/agents/';
  await page.goto(base + 'setup');
  await page.evaluate(async () => {
    localStorage.setItem('hx-offline-person:/agents/', 'alice');
    await navigator.serviceWorker.register('./sw.js');
    await navigator.serviceWorker.ready;
    if (!navigator.serviceWorker.controller)
      await new Promise((resolve) =>
        navigator.serviceWorker.addEventListener('controllerchange', resolve, { once: true }),
      );
  });
  await page.goto(base + 'offline.html');
  await page.getByText('Choose a saved item.', { exact: false }).waitFor();
  await page.evaluate(async () => {
    const db = await new Promise((resolve) => {
      const r = indexedDB.open('hx-offline:' + JSON.stringify(['/agents/', 'alice']));
      r.onsuccess = () => resolve(r.result);
    });
    const chat = await new Promise((resolve) => {
      const r = indexedDB.open('hx-next:alice', 3);
      r.onupgradeneeded = () => {
        r.result.createObjectStore('feeds');
        r.result.createObjectStore('drafts');
      };
      r.onsuccess = () => resolve(r.result);
    });
    const history = chat.transaction('feeds', 'readwrite');
    history.objectStore('feeds').put(
      {
        convId: 'example',
        entries: [
          { t: 'user', text: 'Discuss design' },
          { t: 'say', md: 'Ready to review' },
        ],
      },
      'example',
    );
    await new Promise((resolve) => {
      history.oncomplete = resolve;
    });
    chat.close();
    const tx = db.transaction('copies', 'readwrite');
    tx.objectStore('copies').put({
      id: 'doc',
      title: 'Notes',
      kind: 'text',
      body: new Blob(['original']),
      at: 1,
    });
    await new Promise((resolve) => {
      tx.oncomplete = resolve;
    });
    db.close();
  });
  disconnected = true;
  await page.goto(base + 'c/example');
  await page.getByRole('button', { name: 'Notes' }).click();
  await page.getByRole('textbox').fill('edited offline');
  await page.getByRole('button', { name: 'Save local draft' }).click();
  await page.getByText('Draft saved on this device').waitFor();
  await page.reload();
  await page.getByRole('button', { name: 'Notes' }).click();
  assert.equal(await page.getByRole('textbox').inputValue(), 'edited offline');
  await page.getByRole('button', { name: 'Discuss design' }).click();
  await page.getByText('Ready to review').waitFor();
  await page.getByRole('textbox').fill('Review on Monday');
  await page.getByRole('button', { name: 'Save message draft' }).click();
  await page.getByText('Draft saved', { exact: true }).waitFor();
  await page.reload();
  await page.getByRole('button', { name: 'Discuss design' }).click();
  assert.equal(await page.getByRole('textbox').inputValue(), 'Review on Monday');
  await page.evaluate(() => localStorage.removeItem('hx-offline-person:/agents/'));
  await page.reload();
  await page.getByText('Open your workspace online first.').waitFor();
  assert.equal(await page.getByRole('button', { name: 'Notes' }).count(), 0);
});
