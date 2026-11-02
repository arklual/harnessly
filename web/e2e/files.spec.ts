import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, type Page, test } from '@playwright/test';
import { strFromU8, strToU8, unzipSync, zipSync } from 'fflate';

import { startRuntime, type Runtime } from './runtime';

// A file beside the chat, on the real runtime: it opens in its own view in the pane, what the person edits reaches the disk,
// what they select stands above the field and goes to the agent with the message, and the agent can point at a place.
let rt: Runtime;
let page: Page;

const WORKBOOK = (rows: string) =>
  zipSync({
    '[Content_Types].xml': strToU8('<Types/>'),
    'xl/workbook.xml': strToU8(
      '<workbook><sheets><sheet name="Sales" sheetId="1" r:id="rId1"/><sheet name="Total" sheetId="2" r:id="rId2"/></sheets></workbook>',
    ),
    'xl/_rels/workbook.xml.rels': strToU8(
      '<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>',
    ),
    'xl/worksheets/sheet1.xml': strToU8(`<worksheet><sheetData>${rows}</sheetData></worksheet>`),
    'xl/worksheets/sheet2.xml': strToU8(
      '<worksheet><sheetData><row r="1"><c r="A1"><f>SUM(Sales!A1:A3)</f></c></row></sheetData></worksheet>',
    ),
  });

const PDF = () => {
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>',
    '<< /Length 57 >>\nstream\nBT /F1 24 Tf 72 700 Td (A clause of the contract) Tj ET\nendstream',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
  ];
  let body = '%PDF-1.4\n';
  const at: number[] = [];
  objects.forEach((object, index) => {
    at.push(body.length);
    body += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const table = body.length;
  body += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  for (const offset of at) body += `${String(offset).padStart(10, '0')} 00000 n \n`;
  return `${body}trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${table}\n%%EOF\n`;
};

const WORD = (body: string) =>
  zipSync({
    '[Content_Types].xml': strToU8('<Types/>'),
    'word/document.xml': strToU8(
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>${body}<w:sectPr/></w:body></w:document>`,
    ),
    'word/styles.xml': strToU8(
      '<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:type="paragraph" w:styleId="H1"><w:name w:val="heading 1"/><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style></w:styles>',
    ),
    'word/media/kept.bin': new Uint8Array([7, 8, 9]),
  });

const PARAGRAPHS =
  '<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>Terms</w:t></w:r></w:p>' +
  '<w:p><w:r><w:t xml:space="preserve">Plain </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>bold</w:t></w:r><w:r><w:t xml:space="preserve"> end</w:t></w:r></w:p>' +
  '<w:p><w:r><w:t>Second clause</w:t></w:r></w:p>';

const DECK_NS =
  'xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"';
const DECK_REL = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';

const SLIDE = (shape: string, words: string) =>
  strToU8(
    `<p:sld ${DECK_NS}><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2" name="${shape}"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="952500" y="952500"/><a:ext cx="4762500" cy="1905000"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:rPr sz="3200" b="1"/><a:t>${words}</a:t></a:r><a:endParaRPr sz="3200"/></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
  );

const DECK = () =>
  zipSync({
    '[Content_Types].xml': strToU8('<Types/>'),
    'ppt/presentation.xml': strToU8(
      `<p:presentation ${DECK_NS}><p:sldIdLst><p:sldId id="256" r:id="rId1"/><p:sldId id="257" r:id="rId2"/></p:sldIdLst><p:sldSz cx="9525000" cy="5715000"/></p:presentation>`,
    ),
    'ppt/_rels/presentation.xml.rels': strToU8(
      `<Relationships><Relationship Id="rId1" Type="${DECK_REL}/slide" Target="slides/slide1.xml"/><Relationship Id="rId2" Type="${DECK_REL}/slide" Target="slides/slide2.xml"/></Relationships>`,
    ),
    'ppt/slides/slide1.xml': SLIDE('Opening', 'First slide'),
    'ppt/slides/slide2.xml': SLIDE('Numbers', 'Second slide'),
    'ppt/media/kept.bin': new Uint8Array([4, 5, 6]),
  });

const project = () => join(rt.home, 'project');
const composer = () => page.locator('.hx-stage .hx-composer__input');
const chip = () => page.locator('.hx-attach[data-kind="quote"]');
const pane = () => page.locator('.hx-pane');

const show = async (file: string, place?: string) => {
  const conv = new URL(page.url()).pathname.split('/').pop() ?? '';
  const legacy = await page.request.get(`${rt.url}/api/v2/conversations/${conv}/legacy`);
  expect(legacy.ok()).toBe(true);
  const { sessionName } = (await legacy.json()) as { sessionName: string };
  const token = readFileSync(join(rt.home, '.harnessly', 'cli-token'), 'utf8').trim();
  // The pane takes a dozen commands a minute, and this file's tests send them faster.
  await expect(async () => {
    const asked = await page.request.post(`${rt.url}/api/cli/panel`, {
      headers: { 'x-cli-token': token },
      data: {
        session: sessionName,
        command: { kind: 'open', tab: 'files', target: join(project(), file), ...(place ? { place } : {}) },
      },
    });
    expect(asked.ok()).toBe(true);
  }).toPass({ timeout: 75_000, intervals: [5_000] });
  // An open pane takes a new file in behind what the person is on.
  const tab = pane().locator('.hx-tab').filter({ hasText: file });
  await expect(tab).toBeVisible({ timeout: 30_000 });
  await tab.click();
};

const wordXml = () => strFromU8(unzipSync(readFileSync(join(project(), 'terms.docx')))['word/document.xml']);
const paragraph = (index: number) => pane().locator(`.hx-docx__p[data-p="${index}"]`);

/** The caret into a paragraph, at this many characters from its start. */
const caretAt = (index: number, offset: number) =>
  paragraph(index).evaluate((element, at) => {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    let left = at;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const size = node.textContent?.length ?? 0;
      if (left <= size) {
        (element.closest('[contenteditable]') as HTMLElement).focus();
        document.getSelection()?.collapse(node, left);
        return;
      }
      left -= size;
    }
  }, offset);

const deckPart = (name: string) => unzipSync(readFileSync(join(project(), 'deck.pptx')))[name];
const slideWord = (index: number) => pane().locator(`.hx-slides__holder [data-p="${index}"]`);

const cell = (row: number, column: number) =>
  pane().locator(`.hx-cells__cell[data-row="${row}"][data-column="${column}"]`);

/** What the agent was told, from the transcript the stand-in writes. */
const toldAgent = () => {
  const root = join(rt.home, '.claude', 'projects');
  return readdirSync(root, { recursive: true })
    .map(String)
    .filter((name) => name.endsWith('.jsonl'))
    .map((name) => readFileSync(join(root, name), 'utf8'))
    .join('\n');
};

test.beforeAll(async ({ browser }) => {
  rt = await startRuntime({ env: { RUNTIME_WEB_FEATURES: 'chats,design' } });
  const rows = [1, 2, 3].map(
    (n) => `<row r="${n}"><c r="A${n}"><v>${n * 10}</v></c><c r="B${n}"><f>A${n}*2</f></c></row>`,
  );
  writeFileSync(join(project(), 'sales.xlsx'), WORKBOOK(rows.join('')));
  writeFileSync(join(project(), 'list.csv'), 'name;sum\r\nann;1\r\n');
  writeFileSync(join(project(), 'contract.pdf'), PDF());
  writeFileSync(join(project(), 'terms.docx'), WORD(PARAGRAPHS));
  writeFileSync(join(project(), 'deck.pptx'), DECK());
  page = await browser.newPage();
  await page.goto(`${rt.url}/`);
  const setup = page.getByTestId('setup-form');
  await setup.locator('input[name=password]').fill('correct horse');
  await setup.locator('input[name=confirm]').fill('correct horse');
  await setup.locator('button[type=submit]').click();
  await page.locator('.hx-composer__input').fill('hello agent');
  await page.locator('.hx-composer__input').press('Enter');
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await expect(page.locator('.hx-feed').getByText('hello agent', { exact: true })).toBeVisible();
});

test.afterAll(async () => {
  await page?.close();
  await rt?.stop();
});

test('a spreadsheet opens in the pane, counts its formulas, and an edit reaches the disk', async () => {
  await show('sales.xlsx');
  await expect(cell(0, 0)).toHaveText('10', { timeout: 30_000 });
  // The file came with no values for its formulas.
  await expect(cell(2, 1)).toHaveText('60');

  await cell(0, 0).click();
  await page.keyboard.type('15');
  await page.keyboard.press('Enter');
  await expect(cell(0, 1)).toHaveText('30');
  await expect(pane().locator('.hx-filesave')).toHaveText('Saved', { timeout: 15_000 });

  const saved = unzipSync(readFileSync(join(project(), 'sales.xlsx')));
  const sheet = strFromU8(saved['xl/worksheets/sheet1.xml']);
  expect(sheet).toMatch(/<c r="A1"[^>]*><v>15<\/v>/);
  expect(sheet).toMatch(/<c r="B2"[^>]*><f>A2\*2<\/f>/);
});

test('the agent writes the file while it stands open: it shows, and the person’s edit goes on top of it', async () => {
  const written = () =>
    strFromU8(unzipSync(readFileSync(join(project(), 'sales.xlsx')))['xl/worksheets/sheet1.xml']);
  const agentWrites = (a2: number) => {
    const zip = unzipSync(readFileSync(join(project(), 'sales.xlsx')));
    const sheet = strFromU8(zip['xl/worksheets/sheet1.xml']).replace(
      /(<c r="A2"[^>]*>)<v>[^<]*<\/v>/,
      `$1<v>${a2}</v>`,
    );
    writeFileSync(
      join(project(), 'sales.xlsx'),
      zipSync({ ...zip, 'xl/worksheets/sheet1.xml': strToU8(sheet) }),
    );
  };
  agentWrites(222);
  await expect(cell(1, 0)).toHaveText('222', { timeout: 15_000 });

  // Written in the moment before the look: the save finds out, and takes the edit onto what is there now.
  agentWrites(333);
  await cell(2, 0).click();
  await page.keyboard.type('44');
  await page.keyboard.press('Enter');
  await expect(pane().locator('.hx-filesave')).toHaveText('Saved', { timeout: 15_000 });
  await expect(pane().locator('.hx-filesave__conflict')).toHaveCount(0);
  await expect.poll(written, { timeout: 15_000 }).toMatch(/<c r="A3"[^>]*><v>44<\/v>/);
  expect(written()).toMatch(/<c r="A2"[^>]*><v>333<\/v>/);
  await expect(cell(1, 0)).toHaveText('333');
});

test('an earlier state of the file comes back from its history', async () => {
  await pane().locator('.hx-history__open').click();
  const rows = page.locator('.hx-history__row');
  await expect(rows.first()).toBeVisible();
  // The oldest is the file as it first opened.
  await rows.last().click();
  await expect(cell(0, 0)).toHaveText('10', { timeout: 15_000 });
  await expect(cell(2, 1)).toHaveText('60');
});

test('a CSV keeps how it was written', async () => {
  await show('list.csv');
  await expect(cell(1, 0)).toHaveText('ann', { timeout: 30_000 });
  await cell(1, 1).click();
  await page.keyboard.type('2');
  await page.keyboard.press('Enter');
  await expect(pane().locator('.hx-filesave')).toHaveText('Saved', { timeout: 15_000 });
  expect(readFileSync(join(project(), 'list.csv'), 'utf8')).toBe('name;sum\r\nann;2\r\n');
});

test('what is selected stands above the field, and goes to the agent with the message once', async () => {
  await show('sales.xlsx');
  await expect(cell(0, 0)).toBeVisible({ timeout: 30_000 });
  await cell(0, 0).click();
  await page.keyboard.down('Shift');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.up('Shift');
  await expect(chip()).toContainText('sales.xlsx · Sales!A1:A2');

  await composer().fill('double these');
  await composer().press('Enter');
  await expect(chip()).toHaveCount(0);
  await expect(page.locator('.hx-feed').getByText('double these', { exact: true })).toBeVisible();
  await expect.poll(toldAgent, { timeout: 60_000 }).toContain('kind=\\"file\\"');
  const told = toldAgent();
  expect(told).toContain('Sales!A1:A2');
  expect(told).toContain('sales.xlsx');
  expect(told).toContain('sum ');

  // Dismissed, it does not go.
  await cell(1, 1).click();
  await expect(chip()).toContainText('Sales!B2');
  await chip().locator('.hx-attach__x').click();
  await expect(chip()).toHaveCount(0);
  await composer().fill('nothing selected');
  await composer().press('Enter');
  await expect(page.locator('.hx-feed').getByText('nothing selected', { exact: true })).toBeVisible();
  await page.waitForTimeout(500);
  expect(toldAgent()).not.toContain('Sales!B2');
});

test('the agent points at a place, and that is no selection of the person’s', async () => {
  await show('sales.xlsx', 'Total!A1');
  const focused = pane().locator('.hx-cells__cell[data-focus="true"]');
  await expect(focused).toHaveAttribute('data-row', '0', { timeout: 30_000 });
  await expect(pane().locator('.hx-cells__tab[aria-selected="true"]')).toHaveText('Total');
  await expect(pane().locator('.hx-cells__address')).toHaveText('A1');
  await expect(chip()).toHaveCount(0);
});

test('a passage of a PDF goes with its page, and stays when the person goes to the field', async () => {
  await show('contract.pdf');
  const text = pane().locator('.hx-pdf__text span').first();
  await expect(text).toBeVisible({ timeout: 30_000 });
  await text.evaluate((node) => {
    const range = document.createRange();
    range.selectNodeContents(node);
    const selection = document.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
  });
  await expect(chip()).toContainText('contract.pdf · p. 1');
  await composer().click();
  await page.waitForTimeout(500);
  await expect(chip()).toContainText('contract.pdf · p. 1');
  await composer().fill('explain the clause');
  await composer().press('Enter');
  await expect.poll(toldAgent, { timeout: 60_000 }).toContain('A clause of the contract');
});

test('a Word file opens as paper, and what is typed reaches the disk with the rest of the file as it was', async () => {
  await show('terms.docx');
  await expect(paragraph(0)).toHaveText('Terms');
  await expect(paragraph(0)).toHaveAttribute('role', 'heading');
  await expect(paragraph(1).locator('[data-look]').nth(1)).toHaveCSS('font-weight', '700');

  await caretAt(1, 8);
  await page.keyboard.type('XY');
  await expect(paragraph(1)).toHaveText('Plain boXYld end');
  await expect.poll(wordXml, { timeout: 15_000 }).toContain('boXYld');
  const saved = unzipSync(readFileSync(join(project(), 'terms.docx')));
  expect([...saved['word/media/kept.bin']]).toEqual([7, 8, 9]);
  expect(wordXml()).toMatch(/<w:b\/><\/w:rPr><w:t[^>]*>boXYld/);

  await page.keyboard.press('Enter');
  await expect(paragraph(2)).toHaveText('ld end');
  await page.keyboard.type('Z');
  await expect(paragraph(2)).toHaveText('Zld end');
  await expect.poll(wordXml, { timeout: 15_000 }).toContain('Zld');

  await caretAt(2, 0);
  await page.keyboard.press('Backspace');
  await expect(paragraph(1)).toHaveText('Plain boXYZld end');
  await expect(paragraph(2)).toHaveText('Second clause');

  await page.keyboard.press('Control+z');
  await expect(paragraph(2)).toHaveText('Zld end');
  await page.keyboard.press('Control+y');
  await expect(paragraph(1)).toHaveText('Plain boXYZld end');
  await expect
    .poll(
      () =>
        wordXml()
          .replace(/<\/w:p>/g, '\n')
          .replace(/<[^>]+>/g, ''),
      { timeout: 15_000 },
    )
    .toContain('Plain boXYZld end\n');
});

test('the agent writes the Word file while it stands open, and an edit goes on top of it', async () => {
  await show('terms.docx');
  await expect(paragraph(2)).toHaveText('Second clause');
  // Written with no edit waiting: it shows by itself.
  writeFileSync(
    join(project(), 'terms.docx'),
    WORD(
      wordXml()
        .match(/<w:body>(.*)<w:sectPr/s)?.[1]
        ?.replace('Second clause', 'Agent clause') ?? '',
    ),
  );
  await expect(paragraph(2)).toHaveText('Agent clause', { timeout: 15_000 });

  // Written under an edit that has not reached the disk yet: both stay.
  await caretAt(0, 5);
  await page.keyboard.type('!');
  writeFileSync(
    join(project(), 'terms.docx'),
    WORD(
      wordXml()
        .match(/<w:body>(.*)<w:sectPr/s)?.[1]
        ?.replace('Agent clause', 'Agent again') ?? '',
    ),
  );
  await expect.poll(wordXml, { timeout: 15_000 }).toContain('Terms!');
  expect(wordXml()).toContain('Agent again');
  await expect(paragraph(2)).toHaveText('Agent again');
});

test('a passage of a Word file goes with its paragraph, and the agent can point at one', async () => {
  await show('terms.docx');
  await expect(paragraph(2)).toBeVisible();
  await paragraph(2).evaluate((element) => {
    const text = element.querySelector('[data-look]')?.firstChild;
    if (text) document.getSelection()?.setBaseAndExtent(text, 0, text, 5);
  });
  await expect(chip()).toContainText('terms.docx · «');
  await composer().click();
  await expect(chip()).toBeVisible();
  await composer().fill('this');
  await composer().press('Enter');
  await expect.poll(toldAgent, { timeout: 30_000 }).toContain('paragraph 3');

  await show('terms.docx', '1');
  await expect(paragraph(0)).toBeInViewport();
});

test('a PowerPoint file shows its slides, and words typed on a slide reach the disk', async () => {
  await show('deck.pptx');
  await expect(slideWord(0)).toHaveText('First slide');
  await expect(pane().locator('.hx-slides__count')).toHaveText('1 / 2');
  await expect(pane().locator('.hx-slide__thumb')).toHaveCount(2);

  await pane().locator('.hx-slide__thumb').nth(1).click();
  await expect(slideWord(0)).toHaveText('Second slide');
  await slideWord(0).click();
  await page.keyboard.press('End');
  await page.keyboard.type(' edited');
  await expect(slideWord(0)).toHaveText('Second slide edited');
  await expect
    .poll(() => strFromU8(deckPart('ppt/slides/slide2.xml')), { timeout: 15_000 })
    .toContain('Second slide edited');
  expect(strFromU8(deckPart('ppt/slides/slide1.xml'))).toContain('First slide');
  expect([...deckPart('ppt/media/kept.bin')]).toEqual([4, 5, 6]);

  await page.keyboard.press('Enter');
  await page.keyboard.type('More');
  await expect(slideWord(1)).toHaveText('More');
  await expect
    .poll(() => strFromU8(deckPart('ppt/slides/slide2.xml')).match(/<a:p>/g)?.length, { timeout: 15_000 })
    .toBe(2);
});

test('the agent writes the PowerPoint file while it stands open, and it shows', async () => {
  await show('deck.pptx');
  const zip = unzipSync(readFileSync(join(project(), 'deck.pptx')));
  writeFileSync(
    join(project(), 'deck.pptx'),
    zipSync({ ...zip, 'ppt/slides/slide2.xml': SLIDE('Numbers', 'Written by the agent') }),
  );
  await expect(slideWord(0)).toHaveText('Written by the agent', { timeout: 15_000 });
});

test('a shape or a passage on a slide goes with its slide, and the agent can point at a slide', async () => {
  await show('deck.pptx');
  await pane().locator('.hx-slides__holder [data-shape="Numbers"]').click();
  await expect(chip()).toContainText('deck.pptx · Slide 2');
  await composer().click();
  await composer().fill('this shape');
  await composer().press('Enter');
  await expect.poll(toldAgent, { timeout: 30_000 }).toContain('the shape «Numbers» on slide 2');

  await show('deck.pptx', '1');
  await expect(pane().locator('.hx-slides__count')).toHaveText('1 / 2');
  await expect(slideWord(0)).toHaveText('First slide');
});
