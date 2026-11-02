// A slide drawn into the page, by hand and not by React: the person types right in these elements and the browser owns them
// meanwhile. The slide's own pixels, scaled as a whole; its words go in as text, and its values through the style's own setters,
// which take nothing but the one value.
import type { OfficeImages } from '@/next/features/pane/office-parts';
import type {
  IPptxBox,
  IPptxDrawn,
  IPptxParagraph,
  IPptxText,
  TPptxFill,
  TPptxShape,
} from '@/next/features/pane/pptx/pptx-core';

const SVG = 'http://www.w3.org/2000/svg';

const px = (value: number) => `${Math.round(value * 100) / 100}px`;

interface IDrawScope {
  images: OfficeImages;
  /** The words can be typed into: false on a thumbnail. */
  editable: boolean;
  words: { chart: string; diagram: string; object: string };
}

const placeBox = (element: HTMLElement | SVGElement, box: IPptxBox) => {
  const { style } = element;
  style.setProperty('left', px(box.x));
  style.setProperty('top', px(box.y));
  style.setProperty('width', px(box.w));
  style.setProperty('height', px(box.h));
  const turns = [
    box.rotation ? `rotate(${box.rotation}deg)` : '',
    box.flipH ? 'scaleX(-1)' : '',
    box.flipV ? 'scaleY(-1)' : '',
  ]
    .filter(Boolean)
    .join(' ');
  if (turns) style.setProperty('transform', turns);
};

const paintFill = (element: HTMLElement, fill: TPptxFill | null, images: OfficeImages) => {
  if (!fill) return;
  const { style } = element;
  if (fill.kind === 'solid') style.setProperty('background-color', fill.color);
  else if (fill.kind === 'gradient') {
    const stops = fill.stops.map((stop) => `${stop.color} ${stop.at}%`).join(', ');
    // The file counts from the right clockwise, CSS from the top.
    style.setProperty('background-image', `linear-gradient(${fill.angle + 90}deg, ${stops})`);
  } else {
    const url = images.urlOf(fill.path);
    if (!url) return;
    style.setProperty('background-image', `url("${url}")`);
    style.setProperty('background-size', 'cover');
    style.setProperty('background-position', 'center');
  }
};

const drawParagraph = (paragraph: IPptxParagraph): HTMLElement => {
  const element = document.createElement('div');
  element.className = 'hx-slide__p';
  if (paragraph.index !== null) element.dataset.p = String(paragraph.index);
  if (paragraph.align) element.dataset.align = paragraph.align;
  const { style } = element;
  style.setProperty('font-size', px(paragraph.size));
  style.setProperty('line-height', px(paragraph.lineHeight));
  if (paragraph.before) style.setProperty('margin-top', px(paragraph.before));
  if (paragraph.after) style.setProperty('margin-bottom', px(paragraph.after));
  if (paragraph.left) style.setProperty('padding-left', px(paragraph.left));
  // A hanging line holds its marker in what hangs, and the words start at the margin.
  if (paragraph.firstLine && !paragraph.marker) style.setProperty('text-indent', px(paragraph.firstLine));
  if (paragraph.marker) {
    element.dataset.marker = paragraph.marker;
    style.setProperty('--hx-slide-marker', px(Math.max(0, paragraph.left + paragraph.firstLine)));
  }
  let words = false;
  for (const run of paragraph.runs) {
    if (run.kind === 'break') {
      element.append(document.createElement('br'));
      continue;
    }
    if (run.text === '') continue;
    words = true;
    const piece = document.createElement(run.href ? 'a' : 'span');
    piece.textContent = run.text;
    if (piece instanceof HTMLAnchorElement && run.href) {
      piece.href = run.href;
      piece.title = run.href;
    }
    const look = piece.style;
    look.setProperty('font-size', px(run.size));
    look.setProperty('color', run.color);
    if (run.bold) look.setProperty('font-weight', '700');
    if (run.italic) look.setProperty('font-style', 'italic');
    const lines = [run.underline ? 'underline' : '', run.strike ? 'line-through' : ''].filter(Boolean);
    if (lines.length > 0) look.setProperty('text-decoration', lines.join(' '));
    if (run.font) look.setProperty('font-family', `"${run.font}", var(--hx-slide-font)`);
    element.append(piece);
  }
  // A line for the caret to stand on.
  if (!words) element.append(document.createElement('br'));
  return element;
};

const drawText = (text: IPptxText, scope: IDrawScope, inCell: boolean): HTMLElement => {
  const element = document.createElement('div');
  element.className = 'hx-slide__text';
  element.dataset.anchor = text.anchor;
  if (!text.wrap) element.dataset.nowrap = 'true';
  const { style } = element;
  style.setProperty(
    'padding',
    `${px(text.insets.t)} ${px(text.insets.r)} ${px(text.insets.b)} ${px(text.insets.l)}`,
  );
  if (scope.editable && text.paragraphs.some((paragraph) => paragraph.index !== null)) {
    element.contentEditable = 'plaintext-only';
    element.spellcheck = false;
  }
  if (inCell) element.dataset.cell = 'true';
  element.append(...text.paragraphs.map(drawParagraph));
  return element;
};

const drawLine = (shape: Extract<TPptxShape, { kind: 'line' }>): SVGElement => {
  const svg = document.createElementNS(SVG, 'svg');
  svg.setAttribute('class', 'hx-slide__shape hx-slide__line');
  svg.dataset.shape = shape.name;
  placeBox(svg, shape.box);
  const line = document.createElementNS(SVG, 'line');
  line.setAttribute('x1', '0');
  line.setAttribute('y1', '0');
  line.setAttribute('x2', String(shape.box.w));
  line.setAttribute('y2', String(shape.box.h));
  line.style.setProperty('stroke', shape.line.color);
  line.style.setProperty('stroke-width', px(shape.line.width));
  if (shape.line.dashed)
    line.style.setProperty('stroke-dasharray', `${shape.line.width * 4} ${shape.line.width * 3}`);
  svg.append(line);
  return svg;
};

const drawShape = (shape: TPptxShape, scope: IDrawScope): HTMLElement | SVGElement => {
  if (shape.kind === 'line') return drawLine(shape);
  const element = document.createElement('div');
  element.className = 'hx-slide__shape';
  element.dataset.shape = shape.name;
  placeBox(element, shape.box);
  if (shape.kind === 'picture') {
    const url = scope.images.urlOf(shape.path);
    element.dataset.picture = 'true';
    if (!url) {
      element.dataset.missing = 'true';
      return element;
    }
    const image = document.createElement('img');
    image.src = url;
    image.alt = shape.name;
    image.decoding = 'async';
    const { l, t, r, b } = shape.crop;
    const shownW = 1 - l - r;
    const shownH = 1 - t - b;
    if (shownW > 0 && shownH > 0) {
      image.style.setProperty('width', `${100 / shownW}%`);
      image.style.setProperty('height', `${100 / shownH}%`);
      image.style.setProperty('left', `${(-l / shownW) * 100}%`);
      image.style.setProperty('top', `${(-t / shownH) * 100}%`);
    }
    element.append(image);
    return element;
  }
  if (shape.kind === 'other') {
    element.dataset.other = 'true';
    element.textContent = scope.words[shape.what];
    return element;
  }
  if (shape.kind === 'table') {
    const table = document.createElement('table');
    table.className = 'hx-slide__table';
    const columns = document.createElement('colgroup');
    for (const width of shape.columns) {
      const column = document.createElement('col');
      column.style.setProperty('width', px(width));
      columns.append(column);
    }
    table.append(columns);
    for (const row of shape.rows) {
      const line = document.createElement('tr');
      line.style.setProperty('height', px(row.height));
      for (const cell of row.cells) {
        if (cell.hidden) continue;
        const box = document.createElement('td');
        if (cell.span > 1) box.colSpan = cell.span;
        if (cell.rowSpan > 1) box.rowSpan = cell.rowSpan;
        paintFill(box, cell.fill, scope.images);
        box.append(drawText(cell.text, scope, true));
        line.append(box);
      }
      table.append(line);
    }
    element.append(table);
    return element;
  }
  element.dataset.geometry = shape.geometry;
  if (shape.path) {
    const svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('class', 'hx-slide__path');
    svg.setAttribute('viewBox', `0 0 ${shape.path.w} ${shape.path.h}`);
    svg.setAttribute('preserveAspectRatio', 'none');
    const path = document.createElementNS(SVG, 'path');
    path.setAttribute('d', shape.path.d);
    path.setAttribute('vector-effect', 'non-scaling-stroke');
    const color =
      shape.fill?.kind === 'solid'
        ? shape.fill.color
        : shape.fill?.kind === 'gradient'
          ? shape.fill.stops[0]?.color
          : null;
    path.style.setProperty('fill', color ?? 'none');
    if (shape.line) {
      path.style.setProperty('stroke', shape.line.color);
      path.style.setProperty('stroke-width', px(shape.line.width));
    }
    svg.append(path);
    element.append(svg);
  } else {
    paintFill(element, shape.fill, scope.images);
    if (shape.line) {
      element.style.setProperty('border-color', shape.line.color);
      element.style.setProperty('border-width', px(shape.line.width));
      element.style.setProperty('border-style', shape.line.dashed ? 'dashed' : 'solid');
    }
    if (shape.geometry === 'roundRect') element.style.setProperty('border-radius', px(shape.radius));
  }
  if (shape.text) element.append(drawText(shape.text, scope, false));
  return element;
};

/** The slide, at its own size, into `stage`. */
export const drawSlide = (
  stage: HTMLElement,
  drawn: IPptxDrawn,
  width: number,
  height: number,
  scope: IDrawScope,
): void => {
  stage.style.setProperty('width', px(width));
  stage.style.setProperty('height', px(height));
  stage.style.removeProperty('background-color');
  stage.style.removeProperty('background-image');
  paintFill(stage, drawn.background ?? { kind: 'solid', color: '#ffffff' }, scope.images);
  stage.replaceChildren(...drawn.shapes.map((shape) => drawShape(shape, scope)));
};
