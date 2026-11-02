// A Word file's blocks drawn into the page, by hand and not by React: the person types right in these elements and the browser
// owns them meanwhile. The file's words go in as text, its colours and sizes come out of a closed list of values.
import type { OfficeImages } from '@/next/features/pane/office-parts';
import type {
  IDocxBook,
  IDocxCell,
  IDocxLook,
  IDocxParagraph,
  TDocxBlock,
  TDocxRun,
} from '@/next/features/pane/docx/docx-core';

/** The rules of the file's looks. The values are read through docx-core: hex colours, numbers, a font name of plain letters. */
export const looksToCss = (looks: readonly IDocxLook[]): string =>
  looks
    .map((look, at) => {
      const rules = [
        look.bold ? 'font-weight:700' : 'font-weight:400',
        look.italic && 'font-style:italic',
        look.underline && look.strike && 'text-decoration:underline line-through',
        look.underline && !look.strike && 'text-decoration:underline',
        look.strike && !look.underline && 'text-decoration:line-through',
        look.color && `color:#${look.color}`,
        look.fill && `background:#${look.fill}`,
        look.size && `font-size:${look.size}pt`,
        look.font && `font-family:"${look.font}",var(--hx-docx-font)`,
      ].filter(Boolean);
      return `.hx-docx [data-look="${at}"]{${rules.join(';')}}`;
    })
    .join('\n');

const px = (value: number) => `${Math.round(value * 100) / 100}px`;

const isText = (run: TDocxRun): run is Extract<TDocxRun, { kind: 'text' }> => run.kind === 'text';

const drawRun = (run: TDocxRun, images: OfficeImages): Node | null => {
  if (run.kind === 'text') {
    if (run.text === '') return null;
    const piece = document.createElement(run.href ? 'a' : 'span');
    piece.dataset.look = String(run.look);
    piece.textContent = run.text;
    if (piece instanceof HTMLAnchorElement && run.href) {
      piece.href = run.href;
      piece.title = run.href;
    }
    return piece;
  }
  if (run.kind === 'break') return document.createElement('br');
  if (run.kind === 'tab') {
    const tab = document.createElement('span');
    tab.className = 'hx-docx__tab';
    tab.contentEditable = 'false';
    return tab;
  }
  const url = images.urlOf(run.path);
  if (!url) {
    const missing = document.createElement('span');
    missing.className = 'hx-docx__no-image';
    missing.contentEditable = 'false';
    return missing;
  }
  const image = document.createElement('img');
  image.className = 'hx-docx__image';
  image.src = url;
  image.alt = '';
  image.loading = 'lazy';
  image.decoding = 'async';
  if (run.width > 0) image.style.setProperty('--hx-docx-w', px(run.width));
  if (run.width > 0 && run.height > 0)
    image.style.setProperty('--hx-docx-ratio', String(run.width / run.height));
  return image;
};

const drawParagraph = (p: IDocxParagraph, images: OfficeImages): HTMLElement => {
  const element = document.createElement('div');
  element.className = 'hx-docx__p';
  element.dataset.p = String(p.index);
  if (p.heading !== 0) {
    element.setAttribute('role', 'heading');
    element.setAttribute('aria-level', String(Math.max(1, p.heading)));
  }
  if (p.align) element.dataset.align = p.align;
  if (p.marker) element.dataset.marker = p.marker;
  if (p.frozen) element.contentEditable = 'false';
  if (p.indent > 0) element.style.setProperty('--hx-docx-indent', px(p.indent));
  if (p.before > 0) element.style.setProperty('--hx-docx-before', px(p.before));
  if (p.after > 0) element.style.setProperty('--hx-docx-after', px(p.after));
  const words = p.runs.filter(isText);
  // A paragraph with no words yet takes the look of the words to come, and a line for the caret to stand on.
  if (words.every((run) => run.text === '')) {
    if (words[0]) element.dataset.look = String(words[0].look);
    element.append(document.createElement('br'));
  }
  for (const run of p.runs) {
    const node = drawRun(run, images);
    if (node) element.append(node);
  }
  return element;
};

const drawCell = (cell: IDocxCell, images: OfficeImages): HTMLElement => {
  const element = document.createElement('td');
  if (cell.span > 1) element.colSpan = cell.span;
  if (cell.fill) element.style.setProperty('--hx-docx-fill', `#${cell.fill}`);
  element.append(...cell.blocks.map((block) => drawBlock(block, images)));
  return element;
};

const drawBlock = (block: TDocxBlock, images: OfficeImages): HTMLElement => {
  if (block.type === 'p') return drawParagraph(block, images);
  const table = document.createElement('table');
  table.className = 'hx-docx__table';
  const body = document.createElement('tbody');
  for (const row of block.rows) {
    const line = document.createElement('tr');
    line.append(...row.map((cell) => drawCell(cell, images)));
    body.append(line);
  }
  table.append(body);
  return table;
};

export const drawDocx = (page: HTMLElement, book: IDocxBook, images: OfficeImages): void => {
  page.replaceChildren(...book.blocks.map((block) => drawBlock(block, images)));
};

/** The last heading at or before this paragraph: says to the agent where a passage stands. */
export const headingBefore = (book: IDocxBook, index: number): string | null => {
  let found: string | null = null;
  const look = (blocks: readonly TDocxBlock[]) => {
    for (const block of blocks) {
      if (block.type === 'table') for (const row of block.rows) for (const cell of row) look(cell.blocks);
      else if (block.heading !== 0 && block.index <= index)
        found = block.runs
          .filter(isText)
          .map((run) => run.text)
          .join('');
    }
  };
  look(book.blocks);
  return found;
};
