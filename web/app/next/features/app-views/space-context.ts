// What a space's view (the boards, the documents) tells the page around it: what is open, what the person
// selected in it and where, and a call of the agent written on the board or in a comment. The view publishes it with
// `hl.publish({ space: … })` (the right context.publish); the page turns it into the chat's context chip and puts the
// chat's window beside the selection. Everything here comes from the view: it is data, cut to size, never markup.

/** A rectangle in the view's own pixels (the frame's viewport). */
export interface ISpaceRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface ISpaceContext {
  /** `board`, `document`; anything else is shown as it is. */
  kind: string;
  /** The board's or the document's name. */
  title: string;
  /** Its address, for the agent to open with its tools. */
  url: string | null;
  /** What is selected, as words (labels of the shapes, the quoted text); empty: nothing is. */
  selection: string;
  /** Where the selection (or the call) is in the view; null: the view did not say. */
  rect: ISpaceRect | null;
  /** The person called the agent on the board or in a comment: the words after `@harnessly`. */
  call: string | null;
}

const TEXT_MAX = 600;
const TITLE_MAX = 120;
const URL_MAX = 500;

/** An http(s) address with nothing in it that could close the context block or start a line of its own; else null. */
const safeUrl = (url: string): string | null => {
  if (/[\s<>"'`]/.test(url)) return null;
  try {
    const u = new URL(url);
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : null;
  } catch {
    return null;
  }
};

const words = (v: unknown, max: number): string =>
  typeof v === 'string'
    ? v
        .replace(/[\x00-\x08\x0b-\x1f\x7f]/g, ' ')
        .trim()
        .slice(0, max)
    : '';

const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);

const rectOf = (v: unknown): ISpaceRect | null => {
  if (typeof v !== 'object' || v === null) return null;
  const r = v as Record<string, unknown>;
  const x = num(r.x);
  const y = num(r.y);
  const w = num(r.w);
  const h = num(r.h);
  if (x === null || y === null || w === null || h === null || w < 0 || h < 0) return null;
  return { x, y, w, h };
};

/** The space's part of what the view published; null when it published none. */
export const spaceContextOf = (data: unknown): ISpaceContext | null => {
  if (typeof data !== 'object' || data === null) return null;
  const space = (data as { space?: unknown }).space;
  if (typeof space !== 'object' || space === null) return null;
  const s = space as Record<string, unknown>;
  const title = words(s.title, TITLE_MAX);
  if (!title) return null;
  const url = words(s.url, URL_MAX);
  const call = words(s.call, TEXT_MAX);
  return {
    kind: words(s.kind, 40) || 'board',
    title,
    url: safeUrl(url),
    selection: words(s.selection, TEXT_MAX),
    rect: rectOf(s.rect),
    call: call || null,
  };
};

/** The view said nothing is open any more (`{ space: null }`): back to its list. */
export const spaceCleared = (data: unknown): boolean =>
  typeof data === 'object' && data !== null && 'space' in data && (data as { space: unknown }).space === null;

/** The selection's place on the page: the view's rectangle moved by the frame's own. */
export const onPage = (rect: ISpaceRect | null, frame: DOMRect | null): ISpaceRect | null =>
  rect && frame ? { x: frame.left + rect.x, y: frame.top + rect.y, w: rect.w, h: rect.h } : null;

const quote = (text: string) => text.replace(/[<>]/g, ' ').replace(/\s+/g, ' ');

/**
 * The chip and the block the next message carries: what is open and what is selected. The block is whole, so the
 * chat numbers it as one of the message's annotations.
 */
export const spaceChatContext = (ctx: ISpaceContext) => {
  const lines = [
    '<annotation n="1" kind="space">',
    `open: ${quote(ctx.kind)} «${quote(ctx.title)}»${ctx.url ? ` ${quote(ctx.url)}` : ''}`,
    ...(ctx.selection ? [`selection: ${quote(ctx.selection)}`] : []),
    '</annotation>',
  ];
  return {
    label: ctx.selection ? `${quote(ctx.title)} · ${quote(ctx.selection).slice(0, 40)}` : quote(ctx.title),
    text: lines.join('\n'),
    selected: ctx.selection !== '',
    block: true,
  };
};
