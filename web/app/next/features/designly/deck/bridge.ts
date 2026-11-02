// The deck's words with the board's own script (internal/design/texts/client.js, its deck section): the host turns the
// deck mode on, steps, hit-tests and previews styles and moves; the board tells what is under the pointer, what was
// selected, edited or dragged. The board is foreign code: every field is checked and clipped here, as in dom-bridge.ts.
// Where a message may come from (the frame's window and origin) is the caller's check. No React here.

/** The board's pixels: x, y, width, height. */
export type TDeckRect = [number, number, number, number];

export const DECK_MODE = { edit: 'edit', show: 'show', rec: 'rec', guest: 'guest' } as const;
export type TDeckMode = (typeof DECK_MODE)[keyof typeof DECK_MODE];

/** Host → board. */
export type TDeckHostMessage =
  | { type: 'tk:deck'; on: boolean; mode: TDeckMode }
  | { type: 'tk:deck-step'; n: number }
  /** Which block is at a point (board px); answered by `tk:deck-hit` with the same `req`. */
  | { type: 'tk:deck-hit'; req: number; x: number; y: number }
  /** A live preview of a style; null removes the property. */
  | { type: 'tk:deck-style'; tpl: number; prop: string; value: string | null }
  /** A live move from the host's grip, board px. */
  | { type: 'tk:deck-move'; tpl: number; x: number; y: number }
  | { type: 'tk:deck-pointer'; on: boolean }
  /** A veil over the element while a picture is drawn into it (works on a canvas board too). */
  | { type: 'tk:gen'; tpl: number; on: boolean }
  /** Whether the page draws its buttons on pictures («Изменить», «Сгенерировать»): on while the person may change them. */
  | { type: 'tk:images'; on: boolean; scale: number }
  /** Ends the edit in place (the phone sheet's «Готово», the host's Esc): the board says the text and deselects. */
  | { type: 'tk:deck-done' }
  /** The host draws its own bars: the library's boards bar and tweaks inside the board stay hidden. */
  | { type: 'tk:tweaks-host'; on: true; open: false };

export const DECK_PRESS = { ring: 'ring', cancel: 'cancel' } as const;
export type TDeckPress = (typeof DECK_PRESS)[keyof typeof DECK_PRESS];

export const DECK_SELECT_KIND = { title: 'title', text: 'text', image: 'image', block: 'block' } as const;
export type TDeckSelectKind = (typeof DECK_SELECT_KIND)[keyof typeof DECK_SELECT_KIND];

export interface IDeckGuide {
  /** 1: a vertical line at x = `at`; 0: a horizontal one at y = `at`. */
  v: 0 | 1;
  at: number;
}

/** What an element drawn by a picture job remembers: its brief and the name of the job's files. */
export interface IDeckGen {
  prompt: string;
  job: string;
}

/** Board → host, as read. */
export type TDeckBoardMessage =
  | { k: 'ready'; w: number; h: number; steps: number }
  /**
   * `was` is the element's fingerprint for a style, move or reset patch; `px`: a move is written in px. `tx`, `ty`: the
   * block's hand move so far (its `translate`, board px), 0 when it was not moved.
   */
  | {
      k: 'hover';
      tpl: number | null;
      name: string;
      rect: TDeckRect | null;
      moved: boolean;
      movable: boolean;
      was: string;
      px: boolean;
      tx: number;
      ty: number;
    }
  | {
      k: 'select';
      tpl: number;
      label: string;
      kind: TDeckSelectKind;
      size: number;
      rect: TDeckRect;
      was: string;
      /** A picture or an empty block: its width over its height (0 when unknown) and what it was drawn from. */
      ratio: number;
      gen: IDeckGen | null;
      /** The block's words (clipped), for an app's selection item; '' for a picture. */
      text: string;
      /** The element's tag name. */
      tag: string;
    }
  | { k: 'deselect' }
  /** `html` is already limited by the board to b strong i em u s br and text; it is limited again here. */
  | { k: 'text'; tpl: number; was: string; html: string }
  /** The person tried to edit interpolated text. */
  | { k: 'dynamic'; tpl: number }
  | {
      k: 'drag';
      tpl: number;
      x: number;
      y: number;
      guides: IDeckGuide[];
      end: boolean;
      was: string;
      px: boolean;
    }
  | { k: 'hit'; req: number; label: string; tpl: number | null }
  | { k: 'pointer'; x: number; y: number; down: boolean }
  /** A tap on text on a phone: the edit sheet opens. */
  | { k: 'tap'; x: number; y: number; text: string }
  /** A touch held on the slide (board px): the ring shows at 250 ms; a move or a lift cancels it. */
  | { k: 'press'; x: number; y: number; phase: TDeckPress }
  /** A pick with its `point` (`tk:picked`, board px): a held finger or a click while picking; a comment opens there. */
  | { k: 'held'; x: number; y: number };

const HTML_MAX = 20_000;
const LABEL_MAX = 120;
const TEXT_MAX = 400;
const FINGERPRINT_MAX = 64;
const TPL_MAX = 1_000_000;
const COORD_MAX = 100_000;
const STEPS_MAX = 200;

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const coord = (v: unknown): number | null => (isNum(v) && Math.abs(v) <= COORD_MAX ? v : null);
const clip = (v: unknown, max: number) => (typeof v === 'string' ? v.slice(0, max) : '');
const tplOf = (v: unknown): number | null =>
  Number.isInteger(v) && (v as number) >= 0 && (v as number) <= TPL_MAX ? (v as number) : null;

const RATIO_MAX = 1000;
const PROMPT_MAX = 1000;
const JOB_RE = /^gen-[a-z0-9]{4,32}$/;

const readKind = (v: unknown): TDeckSelectKind => {
  if (v === 'title' || v === 'image' || v === 'block') return v;
  return 'text';
};

/** What the board says an element was drawn from; null unless both the brief and a well-formed job name are there. */
export const readGen = (v: unknown): IDeckGen | null => {
  if (!v || typeof v !== 'object') return null;
  const g = v as Record<string, unknown>;
  if (typeof g.prompt !== 'string' || g.prompt === '' || typeof g.job !== 'string' || !JOB_RE.test(g.job)) {
    return null;
  }
  return { prompt: g.prompt.slice(0, PROMPT_MAX), job: g.job };
};

const readRect = (v: unknown): TDeckRect | null => {
  if (!Array.isArray(v) || v.length !== 4) return null;
  const [x, y, w, h] = v.map(coord);
  if (x === null || y === null || w === null || h === null || w < 0 || h < 0) return null;
  return [x, y, w, h];
};

const ALLOWED_TAGS = new Set(['b', 'strong', 'i', 'em', 'u', 's', 'br']);

/** The edited text's markup with every tag but b strong i em u s br dropped (their words kept) and no attributes. */
export const cleanDeckHtml = (html: string): string =>
  html.slice(0, HTML_MAX).replace(/<\/?([a-zA-Z][a-zA-Z0-9-]*)\b[^>]*>/g, (tag, name: string) => {
    const lower = name.toLowerCase();
    if (!ALLOWED_TAGS.has(lower)) return '';
    if (lower === 'br') return '<br>';
    return tag.startsWith('</') ? `</${lower}>` : `<${lower}>`;
  });

const readGuides = (v: unknown): IDeckGuide[] =>
  Array.isArray(v)
    ? v.slice(0, 8).flatMap((g) => {
        if (!g || typeof g !== 'object') return [];
        const r = g as Record<string, unknown>;
        const at = coord(r.at);
        return (r.v === 0 || r.v === 1) && at !== null ? [{ v: r.v, at }] : [];
      })
    : [];

/** A board's message as the deck reads it; null for anything that is not one or is malformed. */
export const readDeckMessage = (data: unknown): TDeckBoardMessage | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (typeof m.type !== 'string' || !(m.type.startsWith('tk:deck') || m.type === 'tk:picked')) return null;
  const tpl = tplOf(m.tpl);
  switch (m.type) {
    case 'tk:deck-ready': {
      const w = coord(m.w);
      const h = coord(m.h);
      if (w === null || h === null || w <= 0 || h <= 0) return null;
      const steps = Number.isInteger(m.steps) ? Math.min(STEPS_MAX, Math.max(0, m.steps as number)) : 0;
      return { k: 'ready', w, h, steps };
    }
    case 'tk:deck-hover':
      return {
        k: 'hover',
        tpl,
        name: clip(m.name, LABEL_MAX),
        rect: tpl === null ? null : readRect(m.rect),
        moved: m.moved === true,
        movable: m.movable === true,
        was: clip(m.was, FINGERPRINT_MAX),
        px: m.px === true,
        tx: coord(m.tx) ?? 0,
        ty: coord(m.ty) ?? 0,
      };
    case 'tk:deck-select': {
      const rect = readRect(m.rect);
      if (tpl === null || rect === null) return null;
      const size = coord(m.size);
      return {
        k: 'select',
        tpl,
        label: clip(m.label, LABEL_MAX),
        kind: readKind(m.kind),
        size: size !== null && size > 0 ? size : 0,
        rect,
        was: clip(m.was, FINGERPRINT_MAX),
        ratio: isNum(m.ratio) && m.ratio > 0 && m.ratio <= RATIO_MAX ? m.ratio : 0,
        gen: readGen(m.gen),
        text: clip(m.text, TEXT_MAX),
        tag: clip(m.tag, 32)
          .toLowerCase()
          .replace(/[^a-z0-9-]/g, ''),
      };
    }
    case 'tk:deck-deselect':
      return { k: 'deselect' };
    case 'tk:deck-text':
      if (tpl === null || typeof m.was !== 'string' || typeof m.html !== 'string') return null;
      return { k: 'text', tpl, was: m.was.slice(0, HTML_MAX), html: cleanDeckHtml(m.html) };
    case 'tk:deck-dynamic':
      return tpl === null ? null : { k: 'dynamic', tpl };
    case 'tk:deck-drag': {
      const x = coord(m.x);
      const y = coord(m.y);
      if (tpl === null || x === null || y === null) return null;
      return {
        k: 'drag',
        tpl,
        x,
        y,
        guides: readGuides(m.guides),
        end: m.end === true,
        was: clip(m.was, FINGERPRINT_MAX),
        px: m.px === true,
      };
    }
    case 'tk:deck-hit':
      if (!Number.isInteger(m.req)) return null;
      return { k: 'hit', req: m.req as number, label: clip(m.label, LABEL_MAX), tpl };
    case 'tk:deck-pointer': {
      const x = coord(m.x);
      const y = coord(m.y);
      return x === null || y === null ? null : { k: 'pointer', x, y, down: m.down === true };
    }
    case 'tk:deck-tap': {
      const x = coord(m.x);
      const y = coord(m.y);
      return x === null || y === null ? null : { k: 'tap', x, y, text: clip(m.text, 400) };
    }
    case 'tk:deck-press': {
      const x = coord(m.x);
      const y = coord(m.y);
      const phase = m.phase === DECK_PRESS.ring || m.phase === DECK_PRESS.cancel ? m.phase : null;
      return x === null || y === null || phase === null ? null : { k: 'press', x, y, phase };
    }
    case 'tk:picked': {
      // Every pick carries its point now (a click while picking and a held finger alike); an older board sends none.
      const point = readPickedPoint(m);
      return point === null ? null : { k: 'held', x: point[0], y: point[1] };
    }
  }
  return null;
};

/** The point a picked block was held or clicked at (`tk:picked`'s `point`, board px); null for an older board. */
export const readPickedPoint = (data: unknown): [number, number] | null => {
  if (!data || typeof data !== 'object') return null;
  const p = (data as Record<string, unknown>).point;
  if (!Array.isArray(p) || p.length !== 2) return null;
  const x = coord(p[0]);
  const y = coord(p[1]);
  return x === null || y === null ? null : [x, y];
};

/** Sends a deck message to the board in a frame, to the frame's own origin only. */
export const postDeck = (frame: HTMLIFrameElement | null, src: string | null, message: TDeckHostMessage) => {
  if (!frame?.contentWindow || !src) return;
  frame.contentWindow.postMessage(message, new URL(src, window.location.href).origin);
};
