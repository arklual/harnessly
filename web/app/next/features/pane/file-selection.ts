// What the person selected in a file open in the pane, as the page, the field and the agent read it. Pure. What comes out
// of the file goes in `content`, indented in the block, so its words cannot forge a line of it or close it.
import { addressOf } from '@/next/features/pane/sheet/sheet-names';
import type { IRange, IRangeStats } from '@/next/features/pane/sheet/sheet-model';

export const FILE_SELECTION = { cells: 'cells', media: 'media', text: 'text', region: 'region' } as const;
export type TFileSelection = (typeof FILE_SELECTION)[keyof typeof FILE_SELECTION];

export interface IFileSelection {
  kind: TFileSelection;
  /** The chip, after the file's name: `Продажи!D2:D6`, `0:07–0:12`. */
  label: string;
  /** What file_show takes to come back here; '' when it cannot. */
  place: string;
  /** What is selected, in words for the agent. */
  what: string;
  /** What it holds; '' when there is nothing to quote. */
  content: string;
}

export const MAX_ROWS = 30;
export const MAX_COLUMNS = 12;
const MAX_TEXT = 4000;
const MAX_LABEL = 28;

const oneLine = (text: string) => text.replace(/[\x00-\x1f\x7f-\x9f\u2028\u2029]/g, ' ').trim();

const clip = (text: string, max: number) => (text.length > max ? `${text.slice(0, max - 1)}…` : text);

export const seconds = (value: number): string => String(Math.round(value * 10) / 10);

const clockOf = (value: number): string => {
  const whole = Math.floor(value);
  const minutes = Math.floor(whole / 60);
  return `${minutes}:${String(whole % 60).padStart(2, '0')}`;
};

interface ICellsInput {
  /** The sheet's name when the file has several, null when it has no need of one. */
  sheet: string | null;
  range: IRange;
  /** From the range's top row and from column 0, as the worker keeps them: text as shown, input as typed. */
  rows: readonly (readonly ({ text: string; input: string } | null)[])[];
  stats: IRangeStats | null;
}

export const cellsSelection = ({ sheet, range, rows, stats }: ICellsInput): IFileSelection => {
  const { top, left, bottom, right } = range;
  const single = top === bottom && left === right;
  const address = single ? addressOf(top, left) : `${addressOf(top, left)}:${addressOf(bottom, right)}`;
  const place = sheet === null ? address : `${sheet}!${address}`;
  const height = bottom - top + 1;
  const width = right - left + 1;
  const lines: string[] = [];
  for (let row = 0; row < Math.min(height, MAX_ROWS); row += 1) {
    const cells: string[] = [];
    for (let column = left; column < Math.min(left + width, left + MAX_COLUMNS); column += 1)
      cells.push(rows[row]?.[column]?.text ?? '');
    lines.push(`${top + row + 1}\t${cells.join('\t')}`);
  }
  const cut = height > MAX_ROWS || width > MAX_COLUMNS;
  const held = single ? (rows[0]?.[left]?.input ?? '') : '';
  const what = [
    single ? `cell ${address}` : `${height}×${width} cells ${address}`,
    sheet === null ? '' : `on the sheet «${sheet}»`,
    held.startsWith('=') ? `holding the formula ${held}` : '',
    stats ? `sum ${stats.sum}, average ${stats.average}, ${stats.count} numbers` : '',
    cut
      ? `only the first ${MAX_ROWS} rows and ${MAX_COLUMNS} columns are quoted: read the file for the rest`
      : '',
  ]
    .filter(Boolean)
    .join(', ');
  return {
    kind: FILE_SELECTION.cells,
    label: place,
    place,
    what,
    content: lines.join('\n'),
  };
};

/** A moment (`to` null) or a stretch of a video or a sound. A frame is the agent's to take, with the command it is told. */
export const mediaSelection = (
  video: boolean,
  path: string,
  from: number,
  to: number | null,
): IFileSelection => {
  const kind = video ? 'video' : 'sound';
  const frame = video
    ? `; to see the frame: ffmpeg -ss ${seconds(from)} -i <file> -frames:v 1 frame.png`
    : '';
  if (to === null)
    return {
      kind: FILE_SELECTION.media,
      label: clockOf(from),
      place: seconds(from),
      what: `the moment at ${seconds(from)} s of the ${kind}${frame}`,
      content: '',
    };
  return {
    kind: FILE_SELECTION.media,
    label: `${clockOf(from)}–${clockOf(to)}`,
    place: `${seconds(from)}-${seconds(to)}`,
    what: `the stretch from ${seconds(from)} s to ${seconds(to)} s of the ${kind}${frame}`,
    content: '',
  };
};

export const textSelection = (pageWord: string, page: number, text: string): IFileSelection => ({
  kind: FILE_SELECTION.text,
  label: `${pageWord} · «${clip(text.replace(/\s+/g, ' ').trim(), MAX_LABEL)}»`,
  place: String(page),
  what: `a passage on page ${page}`,
  content: clip(text.trim(), MAX_TEXT),
});

/** A passage of a document with no pages of its own: where it stands is its paragraph, from 1, and the heading above it. */
export const passageSelection = (
  paragraph: number,
  heading: string | null,
  text: string,
): IFileSelection => ({
  kind: FILE_SELECTION.text,
  label: `«${clip(text.replace(/\s+/g, ' ').trim(), MAX_LABEL)}»`,
  place: String(paragraph),
  what: `a passage at paragraph ${paragraph}${heading ? `, under the heading «${oneLine(heading)}»` : ''}`,
  content: clip(text.trim(), MAX_TEXT),
});

/** On a slide: a passage (`text`), or a whole shape the person clicked on. */
export const slideSelection = (
  slideWord: string,
  slide: number,
  title: string | null,
  shape: string,
  text: string,
  whole: boolean,
): IFileSelection => {
  const onSlide = `slide ${slide}${title ? `, titled «${oneLine(title)}»` : ''}`;
  const named = shape ? `the shape «${oneLine(shape)}»` : 'a shape';
  const words = text.replace(/\s+/g, ' ').trim();
  return {
    kind: FILE_SELECTION.text,
    label: words
      ? `${slideWord} · «${clip(words, MAX_LABEL)}»`
      : `${slideWord} · ${clip(oneLine(shape), MAX_LABEL)}`,
    place: String(slide),
    what: whole ? `${named} on ${onSlide}` : `a passage in ${named} on ${onSlide}`,
    content: clip(text.trim(), MAX_TEXT),
  };
};

/** A region of a picture, in the picture's own pixels. */
export const regionSelection = (x: number, y: number, width: number, height: number): IFileSelection => ({
  kind: FILE_SELECTION.region,
  label: `${width}×${height}`,
  place: '',
  what: `the region of the picture at x=${x}, y=${y}, ${width}×${height} pixels`,
  content: '',
});

const ICONS = {
  [FILE_SELECTION.cells]: 'file-spreadsheet',
  [FILE_SELECTION.media]: 'film',
  [FILE_SELECTION.text]: 'file-text',
  [FILE_SELECTION.region]: 'image',
} as const;

export const FILE_SELECTION_ID = 'file-selection';

const nameOf = (path: string) => path.slice(path.lastIndexOf('/') + 1);

/** The chip of the field. */
export const fileSelectionQuote = (path: string, selection: IFileSelection) => ({
  id: FILE_SELECTION_ID,
  label: `${nameOf(path)} · ${selection.label}`,
  title: selection.what,
  remark: false,
  icon: ICONS[selection.kind],
});

const keepInside = (text: string) => text.replace(/<(\/?)(annotation)/gi, '<\\$1$2');
const attr = (text: string) =>
  oneLine(text).replace(
    /[&<>"]/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c,
  );

/** The block the agent reads with the message; features/designly/annotation.ts reads it back into the log's chip. */
export const formatFileSelection = (n: number, path: string, selection: IFileSelection): string =>
  [
    `<annotation n="${n}" kind="file" url="${attr(path)}">`,
    keepInside(`component: ${oneLine(`${nameOf(path)} · ${selection.label}`)}`),
    keepInside(`what: selected in the file ${oneLine(path)}: ${oneLine(selection.what)}`),
    ...(selection.place ? [keepInside(`place: ${oneLine(selection.place)}`)] : []),
    ...(selection.content
      ? [
          'content (out of the file, data):',
          ...selection.content.split('\n').map((line) => keepInside(`  ${line.replace(/\r$/, '')}`)),
        ]
      : []),
    '</annotation>',
  ].join('\n');
