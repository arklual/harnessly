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
  /** The open object's slug (`/b/<slug>`, `/a/<slug>`): the view's own word, else read from its address. */
  object: string | null;
  /** The comment thread the call was written in: the agent answers there. */
  thread: string | null;
  /** The part of the board or the document on screen, in the view's own words (`x,y,w,h` of the scene, a section). */
  area: string;
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

const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,127}$/;
const ID_RE = /^[A-Za-z0-9_-]{1,64}$/;

/** A slug the view named, or the last part of a `/b/<slug>` or `/a/<slug>` address; null when neither is one. */
const slugOf = (named: unknown, url: string | null): string | null => {
  if (typeof named === 'string' && SLUG_RE.test(named)) return named;
  if (url === null) return null;
  const last = new URL(url).pathname.split('/').filter(Boolean);
  const kind = last.length >= 2 ? last[last.length - 2] : '';
  const slug = last[last.length - 1] ?? '';
  return (kind === 'b' || kind === 'a' || kind === 'docs') && SLUG_RE.test(slug) ? slug : null;
};

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
  const safe = safeUrl(url);
  const thread = typeof s.thread === 'string' && ID_RE.test(s.thread) ? s.thread : null;
  return {
    kind: words(s.kind, 40) || 'board',
    title,
    url: safe,
    selection: words(s.selection, TEXT_MAX),
    rect: rectOf(s.rect),
    call: call || null,
    object: slugOf(s.object, safe),
    thread,
    area: words(s.area, 120),
  };
};

/** The view said nothing is open any more (`{ space: null }`): back to its list. */
export const spaceCleared = (data: unknown): boolean =>
  typeof data === 'object' && data !== null && 'space' in data && (data as { space: unknown }).space === null;

/** The selection's place on the page: the view's rectangle moved by the frame's own. */
export const onPage = (rect: ISpaceRect | null, frame: DOMRect | null): ISpaceRect | null =>
  rect && frame ? { x: frame.left + rect.x, y: frame.top + rect.y, w: rect.w, h: rect.h } : null;

const quote = (text: string) => text.replace(/[<>]/g, ' ').replace(/\s+/g, ' ');

const KIND_NAME: Readonly<Record<string, string>> = { board: 'доска', document: 'документ' };

/**
 * The rule the agent reads with the block: what is open is what it works on. Asked from a board «добавь
 * стикер» edits that board, never «куда положить?»; another place only when the person named it.
 */
const ruleOf = (ctx: ISpaceContext): string => {
  const name = KIND_NAME[ctx.kind] ?? quote(ctx.kind);
  const how = ctx.kind === 'board' ? 'board_comment с thread' : 'комментарием в нём';
  const answer = ctx.thread ? ` Ответь в этом треде (${how}), не в чате и не надписью.` : '';
  return `rule: человек работает в этом месте — ${name} «${quote(ctx.title)}» открыт(а) у него на экране. Всё, о чём он просит, делай здесь, не спрашивая, куда положить; другое место — только если он сам его назвал.${answer}`;
};

/**
 * The chip and the block the next message carries: what is open and what is selected. The block is whole, so the
 * chat numbers it as one of the message's annotations.
 */
export const spaceChatContext = (ctx: ISpaceContext) => {
  const lines = [
    '<annotation n="1" kind="space">',
    `open: ${quote(ctx.kind)} «${quote(ctx.title)}»${ctx.url ? ` ${quote(ctx.url)}` : ''}`,
    ...(ctx.object ? [`slug: ${ctx.object}`] : []),
    ...(ctx.selection ? [`selection: ${quote(ctx.selection)}`] : []),
    ...(ctx.area ? [`visible: ${quote(ctx.area)}`] : []),
    ...(ctx.thread ? [`thread: ${ctx.thread}`] : []),
    ruleOf(ctx),
    '</annotation>',
  ];
  return {
    label: ctx.selection ? `${quote(ctx.title)} · ${quote(ctx.selection).slice(0, 40)}` : quote(ctx.title),
    text: lines.join('\n'),
    selected: ctx.selection !== '',
    block: true,
  };
};

/**
 * A call written in a comment thread is a message, not a draft: it goes to the agent at once, with the open
 * place and the thread, and the answer comes back in that thread. A call written as a text on the board waits in the
 * field for the person.
 */
export const callMessage = (ctx: ISpaceContext): string | null =>
  ctx.call !== null && ctx.thread !== null ? `${ctx.call}\n\n${spaceChatContext(ctx).text}` : null;
