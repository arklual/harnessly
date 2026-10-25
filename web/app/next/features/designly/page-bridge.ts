// The bridge the runtime puts into every preview slot's page (/__tk/bridge.js) speaks postMessage with the page that
// framed it: `tk:pick {on}` arms one pick, the person's click answers `tk:picked` with the block and `tk:pick {on:false}`
// ends it; a page that loaded says `tk:hello`, a page that is on its way says `tk:progress {state, resources}` (its
// document's state and the files taken so far, throttled), a page that is scrolled says `tk:scroll {y}` (the bridge
// throttles it), and while the site pins notes to blocks (`tk:anchors`) the page tells where they show (`tk:anchored`).
// The library's script in a board speaks for its settings: `tk:lib-ready` when it is there, `tk:tweaks {controls}` with
// what the board offers; the site answers `tk:tweaks-host {on, open}` (it draws the panel) and sends `tk:tweak {name,
// value}` for a change, and the page's own `tk:tweaks-panel {open}` asks the site to show or hide its panel.
// The same script also answers the DevTools' elements tab (`tk:dom-*`, read in dom-bridge.ts), again only to the host that
// framed the board and only from it.
// Only what the slot's own window sends from the slot's origin is read, and only the fields the design uses: they are
// the page's words, data for the agent, not instructions.

/** What a picked block looks like, read by the bridge when it is picked. */
export interface IPickedLook {
  /** `#rrggbb`; null for a clear or unreadable fill. */
  background: string | null;
  /** Corner radius in pixels; null when the page gives it in other units. */
  radius: number | null;
  /** 0 to 1. */
  opacity: number;
}

export interface IPicked {
  /** The number the bridge gave the block: looks are tried on it by that number (`tk:styles`). Null from a bridge that has none. */
  id: number | null;
  /** Null from a bridge that does not read it: the card then has no settings to offer. */
  look: IPickedLook | null;
  selector: string;
  tag: string;
  /** The block's words, clipped by the bridge. */
  text: string;
  /** In the page's viewport, in its CSS pixels. */
  rect: { x: number; y: number; width: number; height: number };
  /** The component the block belongs to, when the page's framework tells it. */
  component: string | null;
  /** The block's markup, clipped by the bridge: the agent finds the block in the board by it. */
  html: string;
}

/** A box in the page's viewport, in its CSS pixels. */
export interface IAnchorBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Where a block a note is pinned to shows on the page, as its bridge tells it. */
export interface IAnchorGeo {
  /** The whole block; null when the page has no such block now. */
  box: IAnchorBox | null;
  /** What is on screen: in the viewport, in its ancestors, from under what the page lays over it. */
  shown: IAnchorBox | null;
  /** The page lays something over all of it. */
  covered: boolean;
}

/** The pinned blocks at one moment, by the site's keys, and the page's viewport. */
export interface IAnchorReport {
  vw: number;
  vh: number;
  items: ReadonlyMap<string, IAnchorGeo>;
}

/** A value a setting holds: a board's settings are plain words, numbers and switches. */
export type TTweakValue = string | number | boolean;

export interface ITweakOption {
  value: TTweakValue;
  text: string;
}

/** A setting a board offers (its `data-props`, or the screen state and the theme of a wrapper board). */
export interface ITweak {
  name: string;
  label: string;
  /** The group it stands under in the panel. */
  section: string;
  /** What the board says it is: `enum`, `boolean`, `number`, `string`… */
  editor: string;
  value: TTweakValue | null;
  options: ITweakOption[];
  min: number | null;
  max: number | null;
  step: number | null;
}

/** Where the page's document is: parsing, parsed (its scripts and files still come), loaded. */
export const DOC_STATE = { loading: 'loading', interactive: 'interactive', complete: 'complete' } as const;
export type TDocState = (typeof DOC_STATE)[keyof typeof DOC_STATE];

export type TBridgeMessage =
  | { k: 'hello' }
  /** The page is on its way: it has taken `resources` files so far. */
  | { k: 'progress'; state: TDocState; resources: number }
  | { k: 'lib-ready' }
  | { k: 'tweaks'; controls: ITweak[] }
  | { k: 'tweaks-panel'; open: boolean }
  | { k: 'picked'; picked: IPicked }
  | { k: 'pick-off' }
  /** The page applied the looks sent to it; `gone` are the numbers of blocks it no longer has. */
  | { k: 'styled'; gone: number[] }
  /** The page was scrolled to `y` CSS pixels. */
  | { k: 'scroll'; y: number }
  /** A pinch or ⌘/Ctrl + wheel over the page, in the page's pixels: the canvas zooms by it. */
  | { k: 'wheel'; x: number; y: number; dy: number }
  /** Space held (or let go) over the page: the canvas's hand. */
  | { k: 'space'; down: boolean }
  /** A pan of the canvas from the page, in the screen's pixels: a wheel it cannot scroll by, a drag with the middle button. */
  | { k: 'pan'; dx: number; dy: number; drag: boolean }
  /** Where the pinned blocks show now. */
  | { k: 'anchored'; report: IAnchorReport };

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const clip = (v: unknown, max: number) => (typeof v === 'string' ? v.slice(0, max) : '');

const MAX_TWEAKS = 40;
const MAX_OPTIONS = 60;
const MAX_TEXT = 2000;

const tweakValue = (v: unknown): TTweakValue | null => {
  if (typeof v === 'string') return v.slice(0, MAX_TEXT);
  if (isNum(v) || typeof v === 'boolean') return v;
  return null;
};
const orNull = (v: unknown) => (isNum(v) ? v : null);

const MAX_BLOCK_ID = 1_000_000;
const HEX_COLOR = /^#[0-9a-f]{6}$/i;

const readLook = (raw: unknown): IPickedLook | null => {
  if (!raw || typeof raw !== 'object') return null;
  const l = raw as Record<string, unknown>;
  if (!isNum(l.opacity)) return null;
  const background = typeof l.background === 'string' && HEX_COLOR.test(l.background) ? l.background : null;
  const radius = isNum(l.radius) && l.radius >= 0 ? l.radius : null;
  return {
    background: background?.toLowerCase() ?? null,
    radius,
    opacity: Math.min(1, Math.max(0, l.opacity)),
  };
};

const readTweak = (raw: unknown): ITweak | null => {
  if (!raw || typeof raw !== 'object') return null;
  const c = raw as Record<string, unknown>;
  if (typeof c.name !== 'string' || c.name === '') return null;
  const options: ITweakOption[] = [];
  for (const item of Array.isArray(c.options) ? c.options.slice(0, MAX_OPTIONS) : []) {
    const o = (item ?? {}) as Record<string, unknown>;
    const value = tweakValue(o.value);
    if (value !== null) options.push({ value, text: clip(o.text, 120) });
  }
  return {
    name: clip(c.name, 80),
    label: clip(c.label, 120) || clip(c.name, 80),
    section: clip(c.section, 80),
    editor: clip(c.editor, 24),
    value: tweakValue(c.value),
    options,
    min: orNull(c.min),
    max: orNull(c.max),
    step: orNull(c.step),
  };
};

const MAX_ANCHORS = 64;
/** A page's pixels are clipped to a sane range: the page's word is data. */
const MAX_COORD = 1e6;

const readBox = (raw: unknown): IAnchorBox | null => {
  if (!Array.isArray(raw) || raw.length !== 4 || !raw.every(isNum)) return null;
  const [x, y, w, h] = raw.map((v: number) => Math.max(-MAX_COORD, Math.min(MAX_COORD, v)));
  return w >= 0 && h >= 0 ? { x, y, w, h } : null;
};

const readAnchored = (m: Record<string, unknown>): IAnchorReport | null => {
  if (!isNum(m.vw) || !isNum(m.vh) || !Array.isArray(m.items)) return null;
  const items = new Map<string, IAnchorGeo>();
  for (const raw of m.items.slice(0, MAX_ANCHORS)) {
    const a = (raw ?? {}) as Record<string, unknown>;
    if (typeof a.key !== 'string' || a.key.length > 64) continue;
    items.set(a.key, {
      box: readBox(a.box),
      shown: readBox(a.shown),
      covered: a.covered === true,
    });
  }
  return { vw: Math.max(0, m.vw), vh: Math.max(0, m.vh), items };
};

/** A forwarded wheel is clipped: the page's word is data, not a command to jump. */
const MAX_WHEEL = 2000;

export const readBridgeMessage = (data: unknown): TBridgeMessage | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (m.type === 'tk:hello') return { k: 'hello' };
  if (m.type === 'tk:lib-ready') return { k: 'lib-ready' };
  if (m.type === 'tk:progress') {
    const state = Object.values(DOC_STATE).find((known) => known === m.state);
    return state && isNum(m.resources)
      ? { k: 'progress', state, resources: Math.max(0, Math.floor(m.resources)) }
      : null;
  }
  if (m.type === 'tk:tweaks-panel')
    return typeof m.open === 'boolean' ? { k: 'tweaks-panel', open: m.open } : null;
  if (m.type === 'tk:tweaks') {
    if (!Array.isArray(m.controls)) return null;
    const controls = m.controls.slice(0, MAX_TWEAKS).map(readTweak);
    return { k: 'tweaks', controls: controls.filter((c) => c !== null) };
  }
  if (m.type === 'tk:pick' && m.on === false) return { k: 'pick-off' };
  if (m.type === 'tk:styled') {
    if (!Array.isArray(m.gone)) return null;
    const gone = m.gone.filter((id): id is number => Number.isInteger(id) && id > 0 && id <= MAX_BLOCK_ID);
    return { k: 'styled', gone: gone.slice(0, MAX_TWEAKS) };
  }
  if (m.type === 'tk:scroll') return isNum(m.y) ? { k: 'scroll', y: Math.max(0, m.y) } : null;
  if (m.type === 'tk:wheel')
    return isNum(m.x) && isNum(m.y) && isNum(m.dy)
      ? { k: 'wheel', x: m.x, y: m.y, dy: Math.max(-MAX_WHEEL, Math.min(MAX_WHEEL, m.dy)) }
      : null;
  if (m.type === 'tk:space') return typeof m.down === 'boolean' ? { k: 'space', down: m.down } : null;
  if (m.type === 'tk:pan')
    return isNum(m.dx) && isNum(m.dy)
      ? {
          k: 'pan',
          dx: Math.max(-MAX_WHEEL, Math.min(MAX_WHEEL, m.dx)),
          dy: Math.max(-MAX_WHEEL, Math.min(MAX_WHEEL, m.dy)),
          drag: m.by === 'drag',
        }
      : null;
  if (m.type === 'tk:anchored') {
    const report = readAnchored(m);
    return report ? { k: 'anchored', report } : null;
  }
  if (m.type !== 'tk:picked' || typeof m.selector !== 'string' || typeof m.tag !== 'string') return null;
  const r = (m.rect ?? {}) as Record<string, unknown>;
  if (!isNum(r.x) || !isNum(r.y) || !isNum(r.width) || !isNum(r.height)) return null;
  const component = (m.component ?? null) as Record<string, unknown> | null;
  return {
    k: 'picked',
    picked: {
      id:
        Number.isInteger(m.pick) && (m.pick as number) > 0 && (m.pick as number) <= MAX_BLOCK_ID
          ? (m.pick as number)
          : null,
      look: readLook(m.look),
      selector: clip(m.selector, 512),
      tag: clip(m.tag, 32),
      text: clip(m.text, 120),
      rect: { x: r.x, y: r.y, width: r.width, height: r.height },
      component: component && typeof component.name === 'string' ? clip(component.name, 80) : null,
      html: clip(m.outerHTML, 2048),
    },
  };
};

/** A minified build names its components with a letter or two: such a name tells the person nothing. */
const MEANINGFUL_NAME = 3;

/** What a person calls a picked block: its words, else its component, else its tag. */
export const pickedLabel = (p: IPicked) => {
  if (p.text) return p.text.slice(0, 40);
  if (p.component && p.component.length >= MEANINGFUL_NAME) return p.component;
  return `<${p.tag}>`;
};
