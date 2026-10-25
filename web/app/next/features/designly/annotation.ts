// A note on a design's page as the agent reads it: the `<annotation>` block its system prompt describes (the same
// block the Legacy pane writes), and the same block read back into a chip for the log. Whatever the page supplied is
// one line each and cannot close the block, so the page's words cannot forge a line or an instruction.
import { styleLine, type IStyleEdit } from '@/next/features/designly/element-style';
import type { IPicked } from '@/next/features/designly/page-bridge';

const MAX_HTML = 1000;
const MAX_TEXT = 120;
const MAX_SELECTOR = 512;
const MAX_NAME = 120;
const MAX_LABEL_TEXT = 40;

const stripControls = (text: string) =>
  text
    .replace(/\x1b/g, '␛')
    .replace(/[\u2028\u2029\u0085]/g, '\n')
    .replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, '');
const oneLine = (text: string) => stripControls(text).replace(/\s+/g, ' ').trim();
const clip = (text: string, max: number) => (text.length > max ? `${text.slice(0, max - 1)}…` : text);
const keepInside = (text: string) => text.replace(/<(\/?)(annotation)/gi, '<\\$1$2');
const escapeMarkup = (text: string) =>
  text.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c);

export interface IAnnotationInput {
  n: number;
  picked: IPicked;
  comment: string;
  /** The look the person tried on the block; it is already on the page, the agent puts it into the source. */
  style: IStyleEdit;
  /** The page's path on the library server. */
  url: string;
  /** The library server's port on the machine. */
  port: number;
}

export const formatAnnotation = ({ n, picked, comment, style, url, port }: IAnnotationInput): string => {
  const attributes = [
    ['n', String(n)],
    ['url', oneLine(url)],
    ['target', `port:${port}`],
  ]
    .map(([name, value]) => `${name}="${escapeMarkup(value)}"`)
    .join(' ');
  const selector = clip(oneLine(picked.selector), MAX_SELECTOR);
  const text = clip(oneLine(picked.text), MAX_TEXT);
  const name = picked.component ? clip(oneLine(picked.component), MAX_NAME) : null;
  const html = picked.html ? escapeMarkup(clip(oneLine(picked.html), MAX_HTML)) : '';
  const typed = stripControls(comment).trim();
  const look = styleLine(style, picked.look);
  return [
    `<annotation ${attributes}>`,
    keepInside(text ? `element: ${selector} — «${text}»` : `element: ${selector}`),
    ...(name ? [keepInside(`component: ${name}`)] : []),
    ...(html ? [`html: ${html}`] : []),
    ...(look ? [`style (already tried on the page, change the source to match): ${look}`] : []),
    ...(typed ? [`comment: ${keepInside(typed)}`] : []),
    '</annotation>',
  ].join('\n');
};

/** The typed words, then every note in order. */
export const withAnnotations = (text: string, blocks: readonly string[]) =>
  [text.trim(), ...blocks].filter(Boolean).join('\n\n');

const BLOCK_RE = /<annotation\b[^>]*>\n[\s\S]*?\n<\/annotation>/g;

export interface ISentNote {
  n: number;
  /** As the chip shows it: `#1 Hero · «Куда сходить»`. */
  label: string;
  comment: string;
  /** The block the note is on, for a quiet hint: `Hero · «Куда сходить»`. */
  element: string;
  /** The page as a person names it: the board's file name, decoded, without its extension. */
  page: string;
  /** The design the page belongs to and its board (a path inside it), null for an address that is not a design's. */
  design: string | null;
  board: string | null;
  /** The block as it was sent: what goes to the agent again when the message comes back into the field. */
  raw: string;
}

const unescapeMarkup = (text: string) =>
  text.replace(
    /&(amp|lt|gt|quot);/g,
    (_, name: string) => ({ amp: '&', lt: '<', gt: '>', quot: '"' })[name] ?? '',
  );

const decode = (text: string) => {
  try {
    return decodeURIComponent(text);
  } catch {
    return text;
  }
};

/** The design and board of a library address (boardHref's `/a/<design>/p/<board>`), or nulls. */
export const placeOf = (url: string): { design: string | null; board: string | null; page: string } => {
  const found = url.match(/^\/a\/([^/?#]+)\/p\/([^?#]*)([?#].*)?$/);
  const path = found ? found[2].split('/').map(decode).join('/') : decode(url.split(/[?#]/)[0]);
  const file = path.split('/').filter(Boolean).at(-1) ?? '';
  const page = file.replace(/\.html?$/i, '') || path || url;
  if (!found) return { design: null, board: null, page };
  return { design: decode(found[1]), board: `${path}${found[3] ?? ''}`, page };
};

/** The same block under another number: the field numbers the notes it sends 1, 2, 3 again. */
export const renumberBlock = (block: string, n: number) =>
  block.replace(/^(<annotation\b[^>]*?\bn=")\d+"/, `$1${n}"`);

const selectorTag = (selector: string) =>
  selector
    .split(' > ')
    .at(-1)
    ?.match(/^[a-z][\w-]*/i)?.[0] ?? null;

const readBlock = (block: string): ISentNote | null => {
  const head = block.match(/^<annotation\b([^>]*)>\n([\s\S]*)\n<\/annotation>$/);
  const n = Number(head?.[1].match(/\bn="(\d+)"/)?.[1]);
  if (!head || !Number.isInteger(n) || /\bkind="text"/.test(head[1])) return null;
  const lines = head[2].split('\n');
  const commentAt = lines.findIndex((line) => line.startsWith('comment: '));
  const fields = commentAt < 0 ? lines : lines.slice(0, commentAt);
  const value = (prefix: string) =>
    fields.find((line) => line.startsWith(prefix))?.slice(prefix.length) ?? null;
  const element = value('element: ')?.match(/^(.*?)(?: — «(.*)»)?$/);
  const component = value('component: ');
  const markupTag = value('html: ')?.match(/^&lt;([a-z][\w-]*)/i)?.[1] ?? null;
  const title = component ?? selectorTag(element?.[1] ?? '') ?? markupTag ?? 'element';
  const short = clip(element?.[2] ?? '', MAX_LABEL_TEXT);
  const comment = commentAt < 0 ? '' : [lines[commentAt].slice(9), ...lines.slice(commentAt + 1)].join('\n');
  const blockName = short ? `${title} · «${short}»` : title;
  const url = unescapeMarkup(head[1].match(/\burl="([^"]*)"/)?.[1] ?? '');
  return { n, label: `#${n} ${blockName}`, comment, element: blockName, ...placeOf(url), raw: block };
};

/** A sent message split back into the person's words and its notes. */
export const readAnnotated = (message: string): { text: string; notes: ISentNote[] } => {
  const notes: ISentNote[] = [];
  const text = message
    .replace(BLOCK_RE, (block) => {
      const note = readBlock(block);
      if (!note) return block;
      notes.push(note);
      return '';
    })
    .trim();
  return { text, notes };
};
