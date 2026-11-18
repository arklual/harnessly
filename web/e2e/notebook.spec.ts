import { expect, test } from '@playwright/test';

// Exercise the real editor and the complete app stylesheet, not mocked CodeMirror or jsdom's zero-sized layout.
// The gallery needs no sign-in, API or kernel; a file selected there stays in browser memory.
const gallery = (theme: string) => {
  const base = process.env.E2E_URL;
  if (!base) throw new Error('The browser test setup must provide E2E_URL');
  return `${base.replace(/\/$/, '')}/?dev=notebook&lang=en&theme=${theme}`;
};

for (const theme of ['light', 'dark']) {
  test(`long notebooks retain visible cells under all app styles (${theme})`, async ({ page }, info) => {
    await page.goto(gallery(theme));
    await expect(page.getByRole('heading', { name: 'Notebook rendering regression' })).toBeVisible();
    const cells = page.locator('.hx-jnb__cell');
    await expect(cells).toHaveCount(74);
    await expect(page.locator('.hx-jnb__warning')).toHaveCount(0);
    const layout = await page.locator('.hx-jnb__body').evaluate((body) => ({
      display: getComputedStyle(body).display,
      scrolls: body.scrollHeight > body.clientHeight,
      heights: [...body.querySelectorAll('.hx-jnb__cell')].map((cell) => cell.getBoundingClientRect().height),
    }));
    expect(layout.display).toBe('block');
    expect(layout.scrolls).toBe(true);
    expect(layout.heights.every((height) => height > 40)).toBe(true);
    const code = page.locator('.hx-jnb__source .cm-content').first();
    await expect(code).toContainText('value = 1');
    expect(
      await code.evaluate((node) => parseFloat(getComputedStyle(node).paddingBottom)),
    ).toBeLessThanOrEqual(12);
    await page.screenshot({ path: info.outputPath(`notebook-${theme}.png`) });
    await cells.last().scrollIntoViewIfNeeded();
    await expect(cells.last()).toContainText('value = 73');
  });
}

test('a selected notebook shows Markdown, editable code and saved rich output without uploading it', async ({
  page,
}) => {
  const kernelCalls: string[] = [];
  page.on('request', (request) => {
    if (/\/conversations\/[^/]+\/notebook(?:\?|$)/.test(request.url())) kernelCalls.push(request.url());
  });
  await page.goto(gallery('light'));
  const fixture = {
    nbformat: 4,
    nbformat_minor: 5,
    metadata: { kernelspec: { name: 'project-venv', language: 'python' } },
    cells: [
      { cell_type: 'markdown', source: '# Uploaded notebook title', metadata: {} },
      {
        cell_type: 'code',
        source: 'print(42)',
        metadata: {},
        execution_count: 1,
        outputs: [
          {
            output_type: 'display_data',
            data: { 'text/html': '<table><tr><th>Result</th></tr><tr><td>42</td></tr></table>' },
            metadata: {},
          },
        ],
      },
    ],
  };
  await page.locator('input[type=file]').setInputFiles({
    name: 'uploaded.ipynb',
    mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify(fixture)),
  });
  await expect(page.getByRole('heading', { name: 'Uploaded notebook title' })).toBeVisible();
  await expect(page.locator('.hx-jnb__cell')).toHaveCount(2);
  await expect(page.locator('.hx-jnb__warning')).toHaveCount(0);
  await expect(
    page.frameLocator('.hx-jnb__outputs iframe').getByRole('cell', { name: '42', exact: true }),
  ).toBeVisible();
  const source = page.getByRole('textbox', { name: 'Cell 2 source' });
  await source.fill('print(43)');
  await expect(page.locator('.hx-fed__state')).toHaveAttribute('data-state', 'dirty');
  await expect(page.getByText('Unsaved edits (memory only)')).toBeVisible();
  expect(kernelCalls).toEqual([]);
});
