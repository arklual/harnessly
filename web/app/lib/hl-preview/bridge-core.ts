/**
 * The in-page bridge of a preview slot. The served script (bridge-script.ts) is assembled from these
 * functions' source, so each one must stand alone: no imports, no module-level names, only its arguments and
 * browser globals. Functions that need others take them as `core`.
 */

/* eslint-disable @typescript-eslint/no-explicit-any -- these read foreign DOM nodes and framework internals */

export type TFramework = 'angular' | 'react' | 'unknown';

export interface IPickedComponent {
  name: string;
  hostTag?: string;
  source?: { file: string; line: number };
}

export interface IFoundComponent {
  framework: TFramework;
  component: IPickedComponent | null;
}

export type TParentMessage =
  | { type: 'tk:pick'; on: boolean }
  | { type: 'tk:go'; action: 'back' | 'forward' | 'reload' }
  /** The parent saw the frame load and asks whether a bridge lives in this document: the answer is `tk:hello`. */
  | { type: 'tk:ping' }
  /** A chip in the composer was clicked: scroll to the element and outline it for a moment. */
  | { type: 'tk:highlight'; selector: string }
  /**
   * The safe asks to fill the login form of this document. The message carries only a one-time token; the bridge
   * trades it at its own slot origin for the value, which the parent never holds. The answer is `tk:filled`.
   */
  | { type: 'tk:fill'; token: string }
  /** Is there a password field, and is the form fit to fill? Asked before a token is minted and after a login: `tk:login-probe`. */
  | { type: 'tk:probe-login' }
  /**
   * The whole set of looks the person is trying on the blocks they picked, by the number `tk:picked` gave each. A block
   * that is no longer in the set gets back what it had; the answer is `tk:styled`.
   */
  | { type: 'tk:styles'; items: IStyleItem[] }
  /**
   * The page stands on a canvas (a design): Space held over it (`tk:space`) and a drag with the middle button (`tk:pan`)
   * are the canvas's, and are told to the parent. The wheel stays the page's: at the end of its scroll nothing moves.
   */
  | { type: 'tk:canvas'; on: boolean }
  /**
   * The page is one of several screens scrolled together («Все экраны»): while on, a scroll of the document is told at
   * once, a frame at most (`tk:doc-scroll {y}`), and `tk:scroll-to` puts the document at a place.
   */
  | { type: 'tk:scroll-sync'; on: boolean }
  | { type: 'tk:scroll-to'; y: number }
  /** The page is a phone's or a tablet's, touched (installTouch): no hover, the media of a touch screen, a fingertip. */
  | { type: 'tk:touch'; on: boolean }
  /**
   * The blocks the parent pins its notes to: the bridge keeps telling where each one shows (`tk:anchored`) while the page
   * scrolls, moves or covers it. An empty set stops it.
   */
  | { type: 'tk:anchors'; items: IAnchorItem[] }
  /** The coding agent acts on the page (the pane relays it after the person let it onto this site): the answer is `tk:acted`. */
  | IActMessage
  | TDomMessageIn;

/**
 * The DevTools «Elements» protocol (the same one a design board's own script speaks). The bridge answers it only after
 * `tk:dom-on`: a board runs its script and this bridge in one frame, and only one of them answers.
 */
export type TDomMessageIn =
  | { type: 'tk:dom-on' }
  | { type: 'tk:dom-tree'; req: number; path: number[]; depth: number }
  | { type: 'tk:dom-node'; req: number; path: number[] }
  | { type: 'tk:dom-mark'; path: number[] | null; reveal: boolean }
  | { type: 'tk:dom-inspect'; on: boolean }
  | { type: 'tk:dom-watch'; on: boolean };

export type TActOp =
  | 'snapshot'
  | 'locate'
  | 'click'
  | 'type'
  | 'press'
  | 'hover'
  | 'select'
  | 'scroll'
  | 'wait'
  | 'tools'
  | 'call';

/** One action; an unused field is absent (the parent may send it empty or zero, which is read as absent). */
export interface IActMessage {
  type: 'tk:act';
  id: string;
  op: TActOp;
  ref?: string;
  text?: string;
  key?: string;
  value?: string;
  dy?: number;
  ms?: number;
  submit?: boolean;
  /** `call`: the name of one of the page's own (WebMCP) tools. */
  tool?: string;
  /** `call`: the tool's input as JSON text. */
  input?: string;
}

/** What an action came to: `rect` is the target's box in the viewport (CSS px) once it was scrolled into view. */
export interface IActResult {
  ok: boolean;
  text: string;
  rect?: { x: number; y: number; width: number; height: number };
}

/** A block a note is pinned to: by the number `tk:picked` gave it, else by its selector (a page loaded again). */
export interface IAnchorItem {
  /** The parent's name for it, given back in `tk:anchored`. */
  key: string;
  pick: number | null;
  selector: string;
}

/** A box in the page's viewport, in its CSS pixels. */
export interface IAnchorBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Where a pinned block shows. */
export interface IAnchorGeometry {
  /** The whole block. */
  box: IAnchorBox;
  /** The part on screen: inside the viewport, the ancestors and from under what the page lays over it; null for none. */
  shown: IAnchorBox | null;
  /** Something of the page lies over all of what is left of it. */
  covered: boolean;
}

/** What is tried on one picked block; null leaves that property as the page has it. */
export interface IStyleItem {
  id: number;
  /** `#rrggbb`. */
  background: string | null;
  /** Pixels. */
  radius: number | null;
  /** 0 to 1. */
  opacity: number | null;
}

/** What a picked block looks like now, read when it is picked. */
export interface IBlockLook {
  /** `#rrggbb`; null for a transparent or unreadable fill. */
  background: string | null;
  /** Pixels; null when the page gives it in other units. */
  radius: number | null;
  opacity: number;
}

export type TFillReason =
  'no-form' | 'not-login-form' | 'foreign-action' | 'service-worker' | 'expired' | 'moved' | 'failed';

export interface IFillResult {
  ok: boolean;
  submitted: boolean;
  reason?: TFillReason;
}

/**
 * The proxy keeps a target's cookies as `__t_<tag>_<name>`, the tag naming the target: one slot serves many targets in
 * turn, and a cookie of one must never reach another. The page sees only its target's cookies, under their real names.
 */
export const targetCookies = (header: string, tag: string): string => {
  const prefix = `__t_${tag}_`;
  return header
    .split(';')
    .map((part) => part.trim())
    .filter((part) => part.startsWith(prefix))
    .map((part) => part.slice(prefix.length))
    .join('; ');
};

/**
 * A cookie the target sets, pinned to the slot host: the same rewrite for a Set-Cookie header and for `document.cookie =`.
 * `plain` is a slot served over http (a standalone runtime on a local address), where a Secure cookie is never kept.
 */
export const prefixSetCookie = (value: string, tag: string, plain = false): string => {
  const [pair, ...attributes] = value.split(';').map((part) => part.trim());
  const kept = attributes.filter((attr) => !/^(domain|secure|samesite|partitioned)\b/i.test(attr));
  const scope = plain ? ['SameSite=Lax'] : ['Secure', 'SameSite=None', 'Partitioned'];
  return [`__t_${tag}_${pair}`, ...kept, ...scope].join('; ');
};

const MAX_STYLE_ITEMS = 64;
const MAX_RADIUS = 1000;

/** The set of looks as the parent sent it: every item is checked, one that is not right drops the whole set. */
export const parseStyleItems = (value: unknown): IStyleItem[] | null => {
  if (!Array.isArray(value) || value.length > MAX_STYLE_ITEMS) return null;
  const items: IStyleItem[] = [];
  for (const raw of value) {
    if (!raw || typeof raw !== 'object') return null;
    const item = raw as Record<string, unknown>;
    const { id, background, radius, opacity } = item;
    if (typeof id !== 'number' || !Number.isInteger(id) || id < 1) return null;
    if (background !== null && !(typeof background === 'string' && /^#[0-9a-f]{6}$/i.test(background)))
      return null;
    if (radius !== null && !(typeof radius === 'number' && radius >= 0 && radius <= MAX_RADIUS)) return null;
    if (opacity !== null && !(typeof opacity === 'number' && opacity >= 0 && opacity <= 1)) return null;
    items.push({ id, background, radius, opacity });
  }
  return items;
};

/** The farthest place a parent may put the document at: a page longer than this is not a design's. */
const MAX_SCROLL_TO = 1_000_000;

const DOM_PATH_MAX = 64;
const DOM_INDEX_MAX = 100000;
const DOM_DEPTH_MAX = 6;
const DOM_NODES_MAX = 400;
const DOM_KIDS_MAX = 120;

/** A path of child indices from the document element: at most 64 non-negative integers. */
export const parseDomPath = (value: unknown): number[] | null => {
  if (!Array.isArray(value) || value.length > DOM_PATH_MAX) return null;
  for (const item of value)
    if (typeof item !== 'number' || !Number.isInteger(item) || item < 0 || item > DOM_INDEX_MAX) return null;
  return value.slice();
};

export const parseDomMessage = (message: Record<string, unknown>): TDomMessageIn | null => {
  if (message.type === 'tk:dom-on') return { type: 'tk:dom-on' };
  if (message.type === 'tk:dom-inspect' || message.type === 'tk:dom-watch') {
    return typeof message.on === 'boolean' ? { type: message.type, on: message.on } : null;
  }
  if (message.type === 'tk:dom-mark') {
    const path = message.path === null ? null : parseDomPath(message.path);
    if (message.path !== null && path === null) return null;
    if (message.reveal !== undefined && typeof message.reveal !== 'boolean') return null;
    return { type: 'tk:dom-mark', path, reveal: message.reveal === true };
  }
  if (message.type === 'tk:dom-tree' || message.type === 'tk:dom-node') {
    const path = parseDomPath(message.path);
    if (!path || typeof message.req !== 'number' || !Number.isFinite(message.req)) return null;
    if (message.type === 'tk:dom-node') return { type: 'tk:dom-node', req: message.req, path };
    if (message.depth !== undefined && (typeof message.depth !== 'number' || !Number.isFinite(message.depth)))
      return null;
    const depth = message.depth === undefined ? 1 : Math.floor(message.depth);
    return {
      type: 'tk:dom-tree',
      req: message.req,
      path,
      depth: Math.max(0, Math.min(DOM_DEPTH_MAX, depth)),
    };
  }
  return null;
};

export const isDomMessage = (message: { type: string }): message is TDomMessageIn =>
  message.type.startsWith('tk:dom-');

export const parseParentMessage = (data: unknown): TParentMessage | null => {
  if (!data || typeof data !== 'object') return null;
  const message = data as Record<string, unknown>;
  if (message.type === 'tk:pick' && typeof message.on === 'boolean')
    return { type: 'tk:pick', on: message.on };
  if (message.type === 'tk:ping') return { type: 'tk:ping' };
  if (
    message.type === 'tk:highlight' &&
    typeof message.selector === 'string' &&
    message.selector.length > 0 &&
    message.selector.length <= 512
  ) {
    return { type: 'tk:highlight', selector: message.selector };
  }
  if (
    message.type === 'tk:go' &&
    (message.action === 'back' || message.action === 'forward' || message.action === 'reload')
  ) {
    return { type: 'tk:go', action: message.action };
  }
  if (
    message.type === 'tk:fill' &&
    typeof message.token === 'string' &&
    /^[A-Za-z0-9_-]{43}$/.test(message.token)
  ) {
    return { type: 'tk:fill', token: message.token };
  }
  if (message.type === 'tk:probe-login') return { type: 'tk:probe-login' };
  if (message.type === 'tk:canvas' && typeof message.on === 'boolean')
    return { type: 'tk:canvas', on: message.on };
  if (message.type === 'tk:scroll-sync' && typeof message.on === 'boolean')
    return { type: 'tk:scroll-sync', on: message.on };
  if (message.type === 'tk:touch' && typeof message.on === 'boolean')
    return { type: 'tk:touch', on: message.on };
  if (
    message.type === 'tk:scroll-to' &&
    typeof message.y === 'number' &&
    Number.isFinite(message.y) &&
    message.y >= 0 &&
    message.y <= MAX_SCROLL_TO
  )
    return { type: 'tk:scroll-to', y: message.y };
  if (message.type === 'tk:styles') {
    const items = parseStyleItems(message.items);
    return items ? { type: 'tk:styles', items } : null;
  }
  if (message.type === 'tk:anchors') {
    const items = parseAnchorItems(message.items);
    return items ? { type: 'tk:anchors', items } : null;
  }
  if (message.type === 'tk:act') return parseActMessage(message);
  if (typeof message.type === 'string' && message.type.startsWith('tk:dom-')) return parseDomMessage(message);
  return null;
};

const ACT_OPS = [
  'snapshot',
  'locate',
  'click',
  'type',
  'press',
  'hover',
  'select',
  'scroll',
  'wait',
  'tools',
  'call',
];
const TOOL_NAME = /^[A-Za-z0-9_.:-]{1,128}$/;

/** An action as the parent sent it: every field that is there must be right, else the whole message is dropped. */
export const parseActMessage = (message: Record<string, unknown>): IActMessage | null => {
  const { id, op, ref, text, key, value, dy, ms, submit, tool, input } = message;
  if (typeof id !== 'string' || id.length < 1 || id.length > 64) return null;
  if (typeof op !== 'string' || !ACT_OPS.includes(op)) return null;
  const act: IActMessage = { type: 'tk:act', id, op: op as TActOp };
  if (ref !== undefined && ref !== null && ref !== '') {
    if (typeof ref !== 'string' || !/^e\d{1,6}$/.test(ref)) return null;
    act.ref = ref;
  }
  if (text !== undefined && text !== null) {
    if (typeof text !== 'string' || text.length > 4000) return null;
    if (text !== '' || op === 'type') act.text = text;
  }
  if (key !== undefined && key !== null && key !== '') {
    if (typeof key !== 'string' || key.length > 32) return null;
    act.key = key;
  }
  if (value !== undefined && value !== null && value !== '') {
    if (typeof value !== 'string' || value.length > 500) return null;
    act.value = value;
  }
  if (dy !== undefined && dy !== null) {
    if (typeof dy !== 'number' || !Number.isFinite(dy) || Math.abs(dy) > 20_000) return null;
    if (dy !== 0) act.dy = dy;
  }
  if (ms !== undefined && ms !== null) {
    if (typeof ms !== 'number' || !Number.isFinite(ms) || ms < 0 || ms > 10_000) return null;
    if (ms !== 0) act.ms = ms;
  }
  if (submit !== undefined && submit !== null) {
    if (typeof submit !== 'boolean') return null;
    if (submit) act.submit = true;
  }
  if (tool !== undefined && tool !== null && tool !== '') {
    if (typeof tool !== 'string' || !TOOL_NAME.test(tool)) return null;
    act.tool = tool;
  }
  if (input !== undefined && input !== null && input !== '') {
    if (typeof input !== 'string' || input.length > 8000) return null;
    act.input = input;
  }
  return act;
};

/** A tool the page offers to agents through `navigator.modelContext` (WebMCP). */
export interface ISiteTool {
  name: string;
  description: string;
  inputSchema: unknown;
  readOnly: boolean;
  execute: (input: unknown, client: unknown) => unknown;
}

export const makeSiteTools = (): Map<string, ISiteTool> => new Map();

/** The tool as the page gave it; null when it is not one (nothing throws). */
export const readSiteTool = (raw: any): ISiteTool | null => {
  try {
    if (!raw || typeof raw !== 'object') return null;
    const { name, description, inputSchema, annotations, execute } = raw;
    if (typeof name !== 'string' || !TOOL_NAME.test(name) || typeof execute !== 'function') return null;
    return {
      name,
      description: typeof description === 'string' ? description.slice(0, 1000) : '',
      inputSchema: inputSchema && typeof inputSchema === 'object' ? inputSchema : undefined,
      readOnly: annotations?.readOnlyHint === true,
      execute: (input, client) => execute.call(raw, input, client),
    };
  } catch {
    return null;
  }
};

/**
 * `navigator.modelContext` for the page: made when the browser has none, else its methods are wrapped to still run
 * and also record into `registry`. Never throws into the page.
 */
export const installModelContext = (win: any, registry: Map<string, ISiteTool>): void => {
  const add = (raw: any) => {
    const tool = readSiteTool(raw);
    if (tool) registry.set(tool.name, tool);
    return tool;
  };
  const provide = (options: any) => {
    try {
      const list = options && Array.isArray(options.tools) ? options.tools : [];
      registry.clear();
      for (const raw of list) add(raw);
    } catch {
      // A broken list: the registry stays as it is.
    }
  };
  const register = (raw: any) => {
    const tool = add(raw);
    return {
      unregister: () => {
        try {
          if (tool && registry.get(tool.name) === tool) registry.delete(tool.name);
        } catch {
          // Nothing to remove.
        }
      },
    };
  };
  const unregister = (name: unknown) => {
    try {
      if (typeof name === 'string') registry.delete(name);
    } catch {
      // Nothing to remove.
    }
  };
  const nav = win.navigator;
  const native = nav.modelContext;
  if (!native) {
    const context = {
      provideContext: (options: any) => provide(options),
      clearContext: () => registry.clear(),
      registerTool: register,
      unregisterTool: unregister,
    };
    try {
      Object.defineProperty(nav, 'modelContext', {
        value: context,
        configurable: true,
        enumerable: true,
        writable: true,
      });
    } catch {
      // The navigator does not take it: the page has no WebMCP here.
    }
    return;
  }
  const wrap = (method: string, record: (...args: any[]) => any, merge: boolean) => {
    const original = native[method];
    if (typeof original !== 'function') return;
    try {
      native[method] = function (this: unknown, ...args: any[]) {
        const result = original.apply(this ?? native, args);
        const recorded = record(...args);
        if (merge && result && typeof result.unregister === 'function') {
          const first = result.unregister;
          result.unregister = (...rest: any[]) => {
            recorded.unregister();
            return first.apply(result, rest);
          };
        }
        return result;
      };
    } catch {
      // A frozen object: the tools of this page stay unseen.
    }
  };
  wrap('provideContext', provide, false);
  wrap('clearContext', () => registry.clear(), false);
  wrap('registerTool', register, true);
  wrap('unregisterTool', unregister, false);
};

const MAX_ANCHORS = 64;

/** The pinned blocks as the parent sent them: one item that is not right drops the whole set. */
export const parseAnchorItems = (value: unknown): IAnchorItem[] | null => {
  if (!Array.isArray(value) || value.length > MAX_ANCHORS) return null;
  const items: IAnchorItem[] = [];
  for (const raw of value) {
    if (!raw || typeof raw !== 'object') return null;
    const { key, pick, selector } = raw as Record<string, unknown>;
    if (typeof key !== 'string' || !/^[\w.:~-]{1,64}$/.test(key)) return null;
    if (pick !== null && !(typeof pick === 'number' && Number.isInteger(pick) && pick >= 1)) return null;
    if (typeof selector !== 'string' || selector.length > 512) return null;
    items.push({ key, pick, selector });
  }
  return items;
};

/** The part of `a` inside `b`; null when nothing of it is. */
export const meetBoxes = (a: IAnchorBox, b: IAnchorBox): IAnchorBox | null => {
  const x = Math.max(a.x, b.x);
  const y = Math.max(a.y, b.y);
  const right = Math.min(a.x + a.w, b.x + b.w);
  const bottom = Math.min(a.y + a.h, b.y + b.h);
  return right > x && bottom > y ? { x, y, w: right - x, h: bottom - y } : null;
};

/** What `b` leaves of `a` when it lies over a whole side of it (a sticky bar, a sheet, a sidebar); `a` itself otherwise. */
export const trimBox = (a: IAnchorBox, b: IAnchorBox): IAnchorBox | null => {
  const edge = 0.5;
  let { x, y } = a;
  let right = a.x + a.w;
  let bottom = a.y + a.h;
  if (b.x <= x + edge && b.x + b.w >= right - edge) {
    if (b.y <= y + edge && b.y + b.h > y) y = b.y + b.h;
    else if (b.y + b.h >= bottom - edge && b.y < bottom) bottom = b.y;
  } else if (b.y <= y + edge && b.y + b.h >= bottom - edge) {
    if (b.x <= x + edge && b.x + b.w > x) x = b.x + b.w;
    else if (b.x + b.w >= right - edge && b.x < right) right = b.x;
  }
  return right > x && bottom > y ? { x, y, w: right - x, h: bottom - y } : null;
};

/** The element a box hangs from in the page's layout: a slot it is shown in, its parent, a shadow root's host. */
export const layoutParent = (node: any): any =>
  node.assignedSlot || node.parentElement || (node.parentNode && node.parentNode.host) || null;

/**
 * The part of a box its ancestors leave: every ancestor that clips its overflow (hidden, scroll, auto, clip; each axis on
 * its own) cuts it to its padding box. A fixed box escapes them up to an ancestor that holds fixed boxes (a transform, a
 * filter, a containment), an absolute one up to a positioned ancestor. The root and the body are the viewport's.
 */
export const clipByAncestors = (
  el: any,
  box: IAnchorBox,
  win: any,
  doc: any,
  core: any,
): IAnchorBox | null => {
  const far = 1e7;
  const holdsFixed = (style: any) =>
    (style.transform && style.transform !== 'none') ||
    (style.filter && style.filter !== 'none') ||
    (style.perspective && style.perspective !== 'none') ||
    /paint|layout|strict|content/.test(String(style.contain || ''));
  let left: IAnchorBox | null = box;
  let escape = String(win.getComputedStyle(el).position);
  for (let node = core.layoutParent(el); node && left; node = core.layoutParent(node)) {
    if (node === doc.documentElement || node === doc.body || node.nodeType !== 1) continue;
    const style = win.getComputedStyle(node);
    if (escape === 'fixed' && !holdsFixed(style)) continue;
    if (escape === 'absolute' && style.position === 'static' && !holdsFixed(style)) continue;
    escape = String(style.position);
    const cutX = style.overflowX !== 'visible';
    const cutY = style.overflowY !== 'visible';
    if (!cutX && !cutY) continue;
    const rect = node.getBoundingClientRect();
    // A scaled ancestor: its client sizes are its own pixels, its rect the viewport's.
    const kx = node.offsetWidth > 0 ? rect.width / node.offsetWidth : 1;
    const ky = node.offsetHeight > 0 ? rect.height / node.offsetHeight : 1;
    left = core.meetBoxes(left, {
      x: cutX ? rect.left + node.clientLeft * kx : -far,
      y: cutY ? rect.top + node.clientTop * ky : -far,
      w: cutX ? node.clientWidth * kx : 2 * far,
      h: cutY ? node.clientHeight * ky : 2 * far,
    });
  }
  return left;
};

/** What answers at a point, down through the open shadow roots (the document names the host of a component only). */
export const hitAt = (doc: any, x: number, y: number): any => {
  let hit = doc.elementFromPoint(x, y);
  for (
    let depth = 0;
    hit && hit.shadowRoot && typeof hit.shadowRoot.elementFromPoint === 'function' && depth < 16;
    depth += 1
  ) {
    const inner = hit.shadowRoot.elementFromPoint(x, y);
    if (!inner || inner === hit) break;
    hit = inner;
  }
  return hit;
};

/**
 * What the page lays over a block: the points of a grid on its visible part are hit-tested. What answers there and is
 * neither the block, inside it nor under it (an ancestor) lies over it; taken up to the widest of its ancestors that is
 * not the block's, it cuts the part off when it spans a whole side. Covered when every point is another's and nothing
 * can be cut off. A block that lets the pointer through (`pointer-events: none`) is not tested: what is under it answers.
 */
export const uncover = (el: any, shown: IAnchorBox, win: any, doc: any, core: any) => {
  const holds = (outer: any, inner: any) => {
    for (let node = inner; node; node = core.layoutParent(node)) if (node === outer) return true;
    return false;
  };
  const own = (hit: any) => !hit || holds(el, hit) || holds(hit, el);
  if (win.getComputedStyle(el).pointerEvents === 'none' || typeof doc.elementFromPoint !== 'function')
    return { shown, covered: false };
  const layer = (hit: any) => {
    let top = hit;
    for (let node = core.layoutParent(hit); node; node = core.layoutParent(node)) {
      if (node === doc.body || node === doc.documentElement || holds(node, el)) break;
      if (win.getComputedStyle(node).pointerEvents === 'none') break;
      top = node;
    }
    return top;
  };
  let left: IAnchorBox = shown;
  for (let round = 0; round < 4; round += 1) {
    const xs = left.w < 3 ? [left.x + left.w / 2] : [left.x + 1, left.x + left.w / 2, left.x + left.w - 1];
    const ys = left.h < 3 ? [left.y + left.h / 2] : [left.y + 1, left.y + left.h / 2, left.y + left.h - 1];
    const hits: any[] = [];
    for (const y of ys) for (const x of xs) hits.push(core.hitAt(doc, x, y));
    const others = [...new Set(hits.filter((hit) => !own(hit)))];
    if (others.length === 0) return { shown: left, covered: false };
    let cut = false;
    for (const other of others) {
      const rect = layer(other).getBoundingClientRect();
      const next = core.trimBox(left, { x: rect.left, y: rect.top, w: rect.width, h: rect.height });
      if (!next) return { shown: null, covered: true };
      if (next.x !== left.x || next.y !== left.y || next.w !== left.w || next.h !== left.h) cut = true;
      left = next;
    }
    if (!cut) {
      const all = hits.every((hit) => !own(hit));
      return all ? { shown: null, covered: true } : { shown: left, covered: false };
    }
  }
  return { shown: left, covered: false };
};

/** Where a pinned block shows now: its box, what is on screen and whether it is covered. */
export const anchorGeometry = (el: any, win: any, doc: any, core: any): IAnchorGeometry => {
  const rect = el.getBoundingClientRect();
  const box = { x: rect.left, y: rect.top, w: rect.width, h: rect.height };
  const root = doc.documentElement;
  const view = {
    x: 0,
    y: 0,
    w: (root && root.clientWidth) || win.innerWidth,
    h: (root && root.clientHeight) || win.innerHeight,
  };
  const reach = core.clipByAncestors(el, box, win, doc, core);
  const inView = reach ? core.meetBoxes(reach, view) : null;
  if (!inView) return { box, shown: null, covered: false };
  const { shown, covered } = core.uncover(el, inView, win, doc, core);
  return { box, shown, covered };
};

/**
 * The pinned blocks followed: whatever may move one (a scroll of the page or of any scroller in it, a resize, a change of
 * the DOM, a size a block takes, a transition or an animation running) asks for one look in the next frame, and the
 * parent hears `tk:anchored` when something changed. `find` gives the element of an item or null.
 */
export const installAnchors = (
  win: any,
  doc: any,
  post: (message: Record<string, unknown>) => void,
  find: (item: IAnchorItem) => any,
  core: any,
) => {
  let items: IAnchorItem[] = [];
  const found = new Map<string, any>();
  let frame = 0;
  let last = '';
  let running = 0;
  let busyUntil = 0;
  const round = (v: number) => Math.round(v * 100) / 100;
  const pack = (b: IAnchorBox | null) => (b ? [round(b.x), round(b.y), round(b.w), round(b.h)] : null);
  const Resize = win.ResizeObserver;
  const Mutation = win.MutationObserver;
  const sizes = Resize ? new Resize(() => queue()) : null;
  const changes = Mutation ? new Mutation(() => queue()) : null;
  const look = () => {
    frame = 0;
    if (items.length === 0) return;
    const root = doc.documentElement;
    const out: Record<string, unknown>[] = [];
    for (const item of items) {
      let el = found.get(item.key);
      if (!el || !el.isConnected) {
        el = find(item);
        if (el) {
          found.set(item.key, el);
          sizes?.observe(el);
        } else found.delete(item.key);
      }
      if (!el) {
        out.push({ key: item.key, box: null, shown: null, covered: false });
        continue;
      }
      let g: IAnchorGeometry | null = null;
      try {
        g = core.anchorGeometry(el, win, doc, core);
      } catch {
        g = null;
      }
      out.push(
        g
          ? { key: item.key, box: pack(g.box), shown: pack(g.shown), covered: g.covered }
          : { key: item.key, box: null, shown: null, covered: false },
      );
    }
    const message = {
      type: 'tk:anchored',
      vw: (root && root.clientWidth) || win.innerWidth,
      vh: (root && root.clientHeight) || win.innerHeight,
      items: out,
    };
    const text = JSON.stringify(message);
    if (text !== last) {
      last = text;
      post(message);
    }
    // While something animates, every frame is looked at; a lost end event stops it after a while.
    if (running > 0 && Date.now() < busyUntil) queue();
    else running = 0;
  };
  const queue = () => {
    if (!frame && items.length > 0) frame = win.requestAnimationFrame(look);
  };
  const start = () => {
    running += 1;
    busyUntil = Date.now() + 3000;
    queue();
  };
  const stop = () => {
    running = Math.max(0, running - 1);
    queue();
  };
  win.addEventListener('scroll', queue, { capture: true, passive: true });
  win.addEventListener('resize', queue);
  for (const type of ['transitionrun', 'animationstart']) win.addEventListener(type, start, true);
  for (const type of ['transitionend', 'transitioncancel', 'animationend', 'animationcancel'])
    win.addEventListener(type, stop, true);
  // What moves without a word (a change of the page's own timers' making that no event or observer sees) is caught by a
  // slow look while there is anything to follow.
  let slow = 0;
  return (next: IAnchorItem[]) => {
    const had = items.length > 0;
    items = next;
    found.clear();
    sizes?.disconnect();
    changes?.disconnect();
    win.clearInterval(slow);
    slow = 0;
    last = '';
    if (items.length === 0) {
      if (had) post({ type: 'tk:anchored', vw: 0, vh: 0, items: [] });
      return;
    }
    slow = win.setInterval(queue, 500);
    if (doc.documentElement) {
      sizes?.observe(doc.documentElement);
      changes?.observe(doc.documentElement, {
        subtree: true,
        childList: true,
        attributes: true,
        characterData: true,
      });
    }
    queue();
  };
};

/** Where Space is the page's own: a field being typed in. */
export const isTypingTarget = (target: any): boolean =>
  !!target &&
  typeof target.closest === 'function' &&
  target.closest(
    'input:not([type="button"]):not([type="checkbox"]):not([type="radio"]):not([type="submit"]), textarea, select, [contenteditable]:not([contenteditable="false"])',
  ) !== null;

/**
 * Whether the wheel's turn (dx, dy) scrolls something under `target`: an element of it with room left that way (its
 * overflow lets it scroll), or the document itself when neither the root nor the body hides its overflow.
 */
export const scrollsBy = (win: any, doc: any, target: any, dx: number, dy: number): boolean => {
  const room = (el: any, ok: (axis: 'x' | 'y') => boolean) =>
    (dy > 0 && ok('y') && el.scrollTop + el.clientHeight < el.scrollHeight - 1) ||
    (dy < 0 && ok('y') && el.scrollTop > 0) ||
    (dx > 0 && ok('x') && el.scrollLeft + el.clientWidth < el.scrollWidth - 1) ||
    (dx < 0 && ok('x') && el.scrollLeft > 0);
  const root = doc.scrollingElement ?? doc.documentElement;
  for (let el = target && target.nodeType === 1 ? target : target?.parentElement; el; el = el.parentElement) {
    if (el === root || el === doc.body) continue;
    const style = win.getComputedStyle(el);
    const scrolls = (axis: 'x' | 'y') =>
      /auto|scroll|overlay/.test(axis === 'y' ? style.overflowY : style.overflowX);
    if (room(el, scrolls)) return true;
  }
  if (!root) return false;
  const html = win.getComputedStyle(doc.documentElement);
  const body = doc.body ? win.getComputedStyle(doc.body) : null;
  const open = (axis: 'x' | 'y') => {
    const of = (style: any) => (axis === 'y' ? style.overflowY : style.overflowX);
    return !/hidden|clip/.test(of(html)) && !(body && /hidden|clip/.test(of(body)));
  };
  return room(root, open);
};

export const clip = (text: string, max: number): string =>
  text.length > max ? `${text.slice(0, max - 1)}…` : text;

/** `#id` when unique; otherwise up to four levels of `tag.class.class:nth-of-type(n)`, skipping long or hashed classes. */
export const cssSelector = (el: any, doc: any): string => {
  const escape = (value: string) => value.replace(/[^\w-]/g, (c) => `\\${c}`);
  const uniqueId = (node: any) => {
    const id = node.id;
    if (typeof id !== 'string' || !id) return null;
    return doc.querySelectorAll(`[id="${id.replace(/["\\]/g, '\\$&')}"]`).length === 1
      ? `#${escape(id)}`
      : null;
  };
  const hashed = (name: string) =>
    name.length > 20 ||
    /^\d/.test(name) ||
    name.split(/[-_]/).some((part) => part.length >= 5 && /\d/.test(part) && /[a-z]/i.test(part));
  const step = (node: any) => {
    const tag = String(node.tagName).toLowerCase();
    const classes = Array.from(node.classList ?? ([] as string[]))
      .filter((name: any) => typeof name === 'string' && !hashed(name))
      .slice(0, 2);
    const parent = node.parentElement;
    const sameTag = parent
      ? Array.from(parent.children as ArrayLike<any>).filter((sibling) => sibling.tagName === node.tagName)
      : [];
    const nth = sameTag.length > 1 ? `:nth-of-type(${sameTag.indexOf(node) + 1})` : '';
    return `${tag}${classes.map((name) => `.${escape(name as string)}`).join('')}${nth}`;
  };
  const own = uniqueId(el);
  if (own) return own;
  const steps: string[] = [];
  for (let node = el; node && steps.length < 4; node = node.parentElement) {
    const tag = String(node.tagName).toLowerCase();
    if (tag === 'body' || tag === 'html') break;
    const id = steps.length ? uniqueId(node) : null;
    if (id) {
      steps.unshift(id);
      break;
    }
    steps.unshift(step(node));
  }
  return steps.join(' > ');
};

/**
 * Angular dev mode: the nearest host for which `ng.getComponent` answers. React: the fiber on the node or an ancestor,
 * up `return` to a function or class (through memo/forwardRef). `_debugSource` (React ≤ 18) of the clicked element's
 * fiber is the JSX line inside its component's file.
 */
export const findComponent = (el: any, win: any): IFoundComponent | null => {
  const ng = win.ng;
  if (ng && typeof ng.getComponent === 'function') {
    for (let node = el; node; node = node.parentElement) {
      let instance = null;
      try {
        instance = ng.getComponent(node);
      } catch {
        instance = null;
      }
      if (instance)
        return {
          framework: 'angular',
          component: {
            name: instance.constructor?.name || 'Component',
            hostTag: String(node.tagName).toLowerCase(),
          },
        };
    }
    return { framework: 'angular', component: null };
  }
  for (let node = el; node; node = node.parentElement) {
    const key = Object.keys(node).find((name) => name.startsWith('__reactFiber$'));
    if (!key) continue;
    const start = node[key];
    for (let fiber = start; fiber; fiber = fiber.return) {
      let type = fiber.type;
      if (type && typeof type === 'object') type = type.render ?? type.type;
      const name = typeof type === 'function' ? type.displayName || type.name : '';
      if (!name) continue;
      const source = start._debugSource ?? fiber._debugSource;
      const component: IPickedComponent = { name };
      if (source?.fileName) component.source = { file: source.fileName, line: source.lineNumber };
      return { framework: 'react', component };
    }
    return { framework: 'react', component: null };
  }
  return null;
};

export const detectFramework = (win: any, doc: any): { framework: TFramework; ngVersion?: string } => {
  const ngRoot = doc.querySelector('[ng-version]');
  if (ngRoot) return { framework: 'angular', ngVersion: ngRoot.getAttribute('ng-version') };
  if (win.ng && typeof win.ng.getComponent === 'function') return { framework: 'angular' };
  const nodes = Array.from(doc.querySelectorAll('body, body *') as ArrayLike<any>).slice(0, 500);
  if (
    nodes.some((node) =>
      Object.keys(node).some(
        (name) => name.startsWith('__reactContainer$') || name.startsWith('__reactFiber$'),
      ),
    )
  )
    return { framework: 'react' };
  return { framework: 'unknown' };
};

/** The `data-hl-*` bindings of an element (the apps' fields of a Sinopia board): name without the prefix, up to 12, clipped. */
export const hlBindings = (el: any): Record<string, string> | null => {
  const out: Record<string, string> = {};
  let n = 0;
  for (const attribute of Array.from(el.attributes ?? []) as { name: string; value: string }[]) {
    if (n >= 12) break;
    if (!attribute.name.startsWith('data-hl-')) continue;
    out[attribute.name.slice(8)] = String(attribute.value).slice(0, 200);
    n += 1;
  }
  return n > 0 ? out : null;
};

export const pickedMessage = (el: any, win: any, doc: any, core: any) => {
  const box = el.getBoundingClientRect();
  const found = core.findComponent(el, win);
  return {
    type: 'tk:picked',
    hl: hlBindings(el),
    selector: core.cssSelector(el, doc),
    tag: String(el.tagName).toLowerCase(),
    text: core.clip(
      String(el.innerText ?? el.textContent ?? '')
        .replace(/\s+/g, ' ')
        .trim(),
      120,
    ),
    rect: {
      x: Math.round(box.left),
      y: Math.round(box.top),
      width: Math.round(box.width),
      height: Math.round(box.height),
    },
    outerHTML: core.clip(String(el.outerHTML ?? ''), 2048),
    component: found ? found.component : null,
    framework: found ? found.framework : core.detectFramework(win, doc).framework,
    url: win.location.href,
    viewport: { w: win.innerWidth, h: win.innerHeight },
  };
};

/** `#rrggbb` for a computed color the canvas can read (it also turns `oklch()` and the like into sRGB); null when it is clear or unreadable. */
export const colorHex = (value: string, doc: any): string | null => {
  const context = doc.createElement('canvas').getContext('2d');
  if (!context) return null;
  context.fillStyle = '#000000';
  context.fillStyle = value;
  const read = String(context.fillStyle);
  if (/^#[0-9a-f]{6}$/i.test(read)) return read.toLowerCase();
  const rgba = /^rgba\((\d+), (\d+), (\d+), ([\d.]+)\)$/.exec(read);
  if (!rgba || Number(rgba[4]) === 0) return null;
  return `#${[rgba[1], rgba[2], rgba[3]].map((part) => Number(part).toString(16).padStart(2, '0')).join('')}`;
};

/** What the block looks like now: its fill, the corner radius and the opacity. */
export const readLook = (el: any, win: any, doc: any): IBlockLook => {
  const style = win.getComputedStyle(el);
  const radius = /^(\d+(?:\.\d+)?)px$/.exec(String(style.borderTopLeftRadius));
  const opacity = Number.parseFloat(String(style.opacity));
  return {
    background: colorHex(String(style.backgroundColor), doc),
    radius: radius ? Number(radius[1]) : null,
    opacity: Number.isFinite(opacity) ? opacity : 1,
  };
};

/** The properties each look is written to; `!important` so a rule of the page cannot hide what is tried on. */
const LOOK_PROPERTIES = {
  background: ['background-color', 'background-image'],
  radius: ['border-radius'],
  opacity: ['opacity'],
} as const;

export interface IKept {
  [property: string]: { value: string; priority: string };
}

/** Puts `item` on the block, keeping in `kept` what the block had before the first time a property is touched. */
export const applyLook = (el: any, item: IStyleItem, kept: IKept): void => {
  const inline = el.style;
  const values = {
    background: item.background === null ? null : [item.background, 'none'],
    radius: item.radius === null ? null : [`${item.radius}px`],
    opacity: item.opacity === null ? null : [String(item.opacity)],
  };
  for (const group of ['background', 'radius', 'opacity'] as const) {
    const wanted = values[group];
    LOOK_PROPERTIES[group].forEach((property, index) => {
      if (wanted === null) {
        if (!(property in kept)) return;
        const before = kept[property];
        if (before.value === '') inline.removeProperty(property);
        else inline.setProperty(property, before.value, before.priority);
        delete kept[property];
        return;
      }
      if (!(property in kept))
        kept[property] = {
          value: inline.getPropertyValue(property),
          priority: inline.getPropertyPriority(property),
        };
      inline.setProperty(property, wanted[index], 'important');
    });
  }
};

/** The block as it was before anything was tried on it. */
export const restoreLook = (el: any, kept: IKept): void =>
  applyLook(el, { id: 0, background: null, radius: null, opacity: null }, kept);

/**
 * The DOM accessors the safe relies on, taken from the prototypes before any script of the page runs. A page
 * controls the names and ids of its elements: `<input name="action">` shadows `form.action`, `name="querySelectorAll"`
 * shadows the method, `id`s turn into `document.x`. Everything the bridge reads of a form goes through these instead.
 * A missing one (a very old engine) leaves `ready` false, and nothing is filled.
 */
export const captureNatives = (win: any) => {
  const get = (proto: any, name: string) =>
    proto ? Object.getOwnPropertyDescriptor(proto, name)?.get : undefined;
  const form = win.HTMLFormElement?.prototype;
  const input = win.HTMLInputElement?.prototype;
  const natives = {
    HTMLFormElement: win.HTMLFormElement,
    HTMLInputElement: win.HTMLInputElement,
    formProto: form,
    selectAll: win.Element?.prototype?.querySelectorAll,
    selectAllInDocument: win.Document?.prototype?.querySelectorAll,
    getAttribute: win.Element?.prototype?.getAttribute,
    compare: win.Node?.prototype?.compareDocumentPosition,
    baseURI: get(win.Node?.prototype, 'baseURI'),
    title: get(win.Document?.prototype, 'title'),
    box: win.Element?.prototype?.getBoundingClientRect,
    style: win.getComputedStyle,
    focus: win.HTMLElement?.prototype?.focus,
    dispatch: win.EventTarget?.prototype?.dispatchEvent,
    formAction: get(form, 'action'),
    formTarget: get(form, 'target'),
    requestSubmit: form?.requestSubmit,
    inputForm: get(input, 'form'),
    inputType: get(input, 'type'),
    disabled: get(input, 'disabled'),
    readOnly: get(input, 'readOnly'),
    setValue: input ? Object.getOwnPropertyDescriptor(input, 'value')?.set : undefined,
    Event: win.Event,
    setTimeout: win.setTimeout,
    stringify: win.JSON?.stringify,
    parse: win.JSON?.parse,
    XMLHttpRequest: win.XMLHttpRequest,
    xhrOpen: win.XMLHttpRequest?.prototype?.open,
    xhrHeader: win.XMLHttpRequest?.prototype?.setRequestHeader,
    xhrSend: win.XMLHttpRequest?.prototype?.send,
    xhrStatus: get(win.XMLHttpRequest?.prototype, 'status'),
    xhrText: get(win.XMLHttpRequest?.prototype, 'responseText'),
    messageSource: get(win.MessageEvent?.prototype, 'source'),
    messageOrigin: get(win.MessageEvent?.prototype, 'origin'),
    messageData: get(win.MessageEvent?.prototype, 'data'),
    stopImmediately: win.Event?.prototype?.stopImmediatePropagation,
  };
  // Outside a secure context there is no ServiceWorkerContainer, and then no service worker can control the document.
  // A sandboxed document without allow-same-origin (an app's view) throws on reading it: none controls it either.
  let workers: any = null;
  try {
    workers = win.navigator?.serviceWorker ?? null;
  } catch {
    workers = null;
  }
  const controller = get(win.ServiceWorkerContainer?.prototype, 'controller');
  return {
    ...natives,
    controlled: () => Boolean(workers && controller && controller.call(workers)),
    // What an action of the agent needs. None of it gates `ready`: a missing one fails that action alone.
    setTextarea: win.HTMLTextAreaElement
      ? Object.getOwnPropertyDescriptor(win.HTMLTextAreaElement.prototype, 'value')?.set
      : undefined,
    setSelectedIndex: win.HTMLSelectElement
      ? Object.getOwnPropertyDescriptor(win.HTMLSelectElement.prototype, 'selectedIndex')?.set
      : undefined,
    HTMLTextAreaElement: win.HTMLTextAreaElement,
    HTMLSelectElement: win.HTMLSelectElement,
    MouseEvent: win.MouseEvent,
    PointerEvent: win.PointerEvent,
    KeyboardEvent: win.KeyboardEvent,
    InputEvent: win.InputEvent,
    closest: win.Element?.prototype?.closest,
    matches: win.Element?.prototype?.matches,
    scrollIntoView: win.Element?.prototype?.scrollIntoView,
    tagName: get(win.Element?.prototype, 'tagName'),
    isConnected: get(win.Node?.prototype, 'isConnected'),
    textContent: get(win.Node?.prototype, 'textContent'),
    innerText: get(win.HTMLElement?.prototype, 'innerText'),
    activeElement: get(win.Document?.prototype, 'activeElement'),
    execCommand: win.Document?.prototype?.execCommand,
    scrollBy: win.scrollBy,
    createRange: win.Document?.prototype?.createRange,
    getSelection: win.getSelection,
    WeakRef: win.WeakRef,
    MutationObserver: win.MutationObserver,
    requestAnimationFrame: win.requestAnimationFrame,
    ready:
      Object.values(natives).every((value) => value !== undefined && value !== null) &&
      (!workers || Boolean(controller)),
  };
};

export const shownInput = (win: any, el: any, bound: any): boolean => {
  if (bound.disabled.call(el) || bound.readOnly.call(el)) return false;
  const box = bound.box.call(el);
  const style = bound.style.call(win, el);
  return box.width > 0 && box.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
};

/** Real visible `type=password` inputs under `root` (a document or a form) that take an existing password. */
export const loginFields = (win: any, root: any, bound: any, core: any): any[] => {
  const select = root instanceof bound.HTMLFormElement ? bound.selectAll : bound.selectAllInDocument;
  return Array.from(select.call(root, 'input') as ArrayLike<any>).filter(
    (el) =>
      el instanceof bound.HTMLInputElement &&
      bound.inputType.call(el) === 'password' &&
      core.shownInput(win, el, bound) &&
      String(bound.getAttribute.call(el, 'autocomplete') ?? '').toLowerCase() !== 'new-password',
  );
};

/**
 * The login form of this document, or why there is none fit to fill. The password field must be a real
 * `type=password` input taking an existing password, alone in its form, with a login field before it in the same form;
 * the document must not be controlled by a service worker; the form must submit to this very origin in this very frame, by its own `action` and by every submit button's
 * `formaction`/`formtarget`, with no `<base>` pointing away; and no control of the form may carry a name or id that
 * shadows a property of a form (`action`, `elements`, `target`, `submit`, `querySelectorAll`…). A sign-up or
 * change-of-password form has a second password field and is refused too.
 */
export const findLoginForm = (win: any, doc: any, bound: any, core: any) => {
  if (!bound.ready) return { ok: false, reason: 'failed' };
  const refuse = (reason: string) => ({ ok: false, reason });
  // A service worker of the page would see the exchange (same origin) and could have served this very bridge.
  if (bound.controlled()) return refuse('service-worker');
  const allPasswords = (root: any, select: any) =>
    Array.from(select.call(root, 'input') as ArrayLike<any>).filter(
      (el) =>
        el instanceof bound.HTMLInputElement &&
        bound.inputType.call(el) === 'password' &&
        core.shownInput(win, el, bound),
    );
  const fields = core.loginFields(win, doc, bound, core);
  if (!fields.length)
    return refuse(allPasswords(doc, bound.selectAllInDocument).length ? 'not-login-form' : 'no-form');
  const field = fields[0];
  const form = bound.inputForm.call(field);
  if (!(form instanceof bound.HTMLFormElement)) return refuse('no-form');
  const controls = Array.from(bound.selectAll.call(form, '[name], [id]') as ArrayLike<any>);
  const shadows = (name: unknown) => typeof name === 'string' && name !== '' && name in bound.formProto;
  if (
    controls.some(
      (el) => shadows(bound.getAttribute.call(el, 'name')) || shadows(bound.getAttribute.call(el, 'id')),
    )
  )
    return refuse('not-login-form');
  if (allPasswords(form, bound.selectAll).length > 1) return refuse('not-login-form');
  const here = win.location.origin;
  const sameOrigin = (value: string, base: string) => {
    try {
      return new URL(value, base).origin === here;
    } catch {
      return false;
    }
  };
  const base = bound.baseURI.call(doc);
  if (!sameOrigin(base, here)) return refuse('foreign-action');
  const actionAttribute = bound.getAttribute.call(form, 'action');
  if (
    !sameOrigin(bound.formAction.call(form), base) ||
    (actionAttribute && !sameOrigin(actionAttribute, base))
  )
    return refuse('foreign-action');
  const target = String(bound.formTarget.call(form) ?? '').toLowerCase();
  if (target && target !== '_self') return refuse('foreign-action');
  const submitters = Array.from(
    bound.selectAll.call(form, 'button, input[type="submit"], input[type="image"]') as ArrayLike<any>,
  );
  for (const button of submitters) {
    const formAction = bound.getAttribute.call(button, 'formaction');
    const formTarget = String(bound.getAttribute.call(button, 'formtarget') ?? '').toLowerCase();
    if ((formAction && !sameOrigin(formAction, base)) || (formTarget && formTarget !== '_self'))
      return refuse('foreign-action');
  }
  const before = Array.from(bound.selectAll.call(form, 'input') as ArrayLike<any>).filter((el) => {
    if (!(el instanceof bound.HTMLInputElement)) return false;
    const kind = String(bound.inputType.call(el)).toLowerCase();
    return (
      (kind === 'text' || kind === 'email' || kind === 'tel') &&
      bound.inputForm.call(el) === form &&
      core.shownInput(win, el, bound) &&
      (bound.compare.call(el, field) & 4) !== 0
    );
  });
  const user =
    before.find(
      (el) => String(bound.getAttribute.call(el, 'autocomplete') ?? '').toLowerCase() === 'username',
    ) ?? before[before.length - 1];
  if (!user) return refuse('not-login-form');
  return { ok: true, form, field, user };
};

/**
 * One synchronous step after the value arrived: this is still the document and the frame the token was for (same
 * origin over https, same parent, still framed), the form still passes `findLoginForm`, then the fields are set as typing
 * would set them (the native setter, plain `input` and `change` events that carry no data), the form is submitted, and the
 * password field is emptied whatever happens, again on the next tick and a moment later (a page re-rendering it from its
 * own state). Nothing of the value is kept, returned, posted or logged.
 */
export const applyLogin = (win: any, doc: any, answer: any, bound: any, core: any): IFillResult => {
  const here =
    win.location.protocol === 'https:' &&
    win.location.origin === bound.origin &&
    win.parent === bound.parent &&
    win !== win.top;
  if (!here) return { ok: false, submitted: false, reason: 'moved' };
  if (!answer || typeof answer.login !== 'string' || typeof answer.password !== 'string' || !answer.password)
    return { ok: false, submitted: false, reason: 'failed' };
  const found = core.findLoginForm(win, doc, bound, core);
  if (!found.ok) return { ok: false, submitted: false, reason: found.reason };
  const type = (el: any, value: string) => {
    bound.focus.call(el);
    bound.setValue.call(el, value);
    bound.dispatch.call(el, new bound.Event('input', { bubbles: true }));
    bound.dispatch.call(el, new bound.Event('change', { bubbles: true }));
  };
  const field = found.field;
  const empty = () => bound.setValue.call(field, '');
  try {
    type(found.user, answer.login);
    type(field, answer.password);
    bound.requestSubmit.call(found.form);
  } finally {
    empty();
    bound.setTimeout.call(win, empty, 0);
    bound.setTimeout.call(win, empty, 1500);
  }
  return { ok: true, submitted: true };
};

/**
 * Trades the token for the value and fills, all in one synchronous call: a synchronous request through the
 * `XMLHttpRequest` taken before the page ran, so no promise, `then`, `fetch`, `Response` or `JSON` the page may have
 * replaced ever sees the token, the nonce or the value. The absolute URL of the slot itself, never the page's `<base>`.
 */
export const redeemAndFill = (
  win: any,
  doc: any,
  token: string,
  nonce: string,
  bound: any,
  core: any,
): IFillResult => {
  const fit = core.findLoginForm(win, doc, bound, core);
  if (!fit.ok) return { ok: false, submitted: false, reason: fit.reason };
  let answer: any = null;
  try {
    const request = new bound.XMLHttpRequest();
    bound.xhrOpen.call(request, 'POST', `${bound.origin}/__tk/vault-fill`, false);
    bound.xhrHeader.call(request, 'content-type', 'application/json');
    bound.xhrHeader.call(request, 'x-hl-vault', '1');
    bound.xhrSend.call(request, bound.stringify({ token, nonce }));
    const status = bound.xhrStatus.call(request);
    if (status !== 200)
      return { ok: false, submitted: false, reason: status === 409 || status === 410 ? 'expired' : 'failed' };
    answer = bound.parse(bound.xhrText.call(request));
    return core.applyLogin(win, doc, answer, bound, core);
  } catch {
    return { ok: false, submitted: false, reason: 'failed' };
  } finally {
    answer = null;
  }
};

/** `document.cookie` of the page reads and writes the target's own cookies; the slot cookie and foreign ones stay hidden. */
export const installCookieShim = (doc: any, core: any, tag: string): void => {
  let proto = Object.getPrototypeOf(doc);
  while (proto && !Object.getOwnPropertyDescriptor(proto, 'cookie')) proto = Object.getPrototypeOf(proto);
  const native = proto && Object.getOwnPropertyDescriptor(proto, 'cookie');
  if (!native?.get || !native.set) return;
  Object.defineProperty(doc, 'cookie', {
    configurable: true,
    get: () => core.targetCookies(String(native.get.call(doc)), tag),
    set: (value: unknown) => native.set.call(doc, core.prefixSetCookie(String(value), tag)),
  });
};

/** The event's composed path goes through the design runtime's own UI (its `[data-tk-library-bar]` host): its card, bar, panels. */
export const isRuntimeUi = (path: readonly unknown[]): boolean =>
  path.some((node) => {
    const el = node as { nodeType?: number; hasAttribute?: (name: string) => boolean } | null;
    return (
      el?.nodeType === 1 && typeof el.hasAttribute === 'function' && el.hasAttribute('data-tk-library-bar')
    );
  });

/**
 * Messaging with the parent page: only to `win.parent` with its origin as the target, only from it with that
 * origin. Nothing from a message is executed: the bridge reads the DOM, scrolls and draws one highlight box.
 */
/** A selector that never matches while hovered: each `:hover` of it asks for an element that is none (`:not(*)`). */
export const untouchSelector = (selector: string): string =>
  selector.replace(/:hover(?![\w-])/g, ':hover:not(*)');

/** A media query as a touch screen reads it, on a computer with a mouse: `hover` and `pointer` turned the other way. */
export const touchMedia = (text: string): string =>
  text
    .replace(
      /\b(any-)?hover\s*:\s*(hover|none)\b/gi,
      (_all: string, any: string | undefined, value: string) =>
        `${any ?? ''}hover: ${value.toLowerCase() === 'hover' ? 'none' : 'hover'}`,
    )
    .replace(
      /\b(any-)?pointer\s*:\s*(fine|coarse)\b/gi,
      (_all: string, any: string | undefined, value: string) =>
        `${any ?? ''}pointer: ${value.toLowerCase() === 'fine' ? 'coarse' : 'fine'}`,
    );

/**
 * A phone's or a tablet's page on a design's canvas, touched (`tk:touch`): no rule of the page answers hover, the media of
 * `hover` and `pointer` read as a touch screen's (only where this computer is one with a mouse), and the pointer is a
 * fingertip. The sheets the page adds later are taken as they come; off, every rule is given back what it had.
 */
export const installTouch = (win: any, doc: any, core: any): ((on: boolean) => void) => {
  let changed: { target: any; key: 'selectorText' | 'mediaText'; was: string }[] = [];
  let seen = new WeakSet<object>();
  let counts = new WeakMap<object, number>();
  let on = false;
  let cursor: any = null;
  let observer: any = null;
  let timer = 0;
  const mouse = (() => {
    try {
      return win.matchMedia('(hover: hover)').matches === true;
    } catch {
      return false;
    }
  })();
  const rulesOf = (owner: any): any[] => {
    try {
      return Array.from(owner.cssRules ?? []);
    } catch {
      // A sheet of another origin keeps its rules to itself.
      return [];
    }
  };
  const change = (target: any, key: 'selectorText' | 'mediaText', next: string) => {
    const was = String(target[key]);
    if (next === was) return;
    try {
      target[key] = next;
      changed.push({ target, key, was });
    } catch {
      // A rule the browser does not let be changed stays as it is.
    }
  };
  const visit = (rules: any[]) => {
    for (const rule of rules) {
      if (!seen.has(rule)) {
        seen.add(rule);
        if (typeof rule.selectorText === 'string' && rule.selectorText.includes(':hover'))
          change(rule, 'selectorText', core.untouchSelector(rule.selectorText));
        if (mouse && rule.media && typeof rule.media.mediaText === 'string' && rule.cssRules)
          change(rule.media, 'mediaText', core.touchMedia(rule.media.mediaText));
      }
      if (rule.cssRules) visit(rulesOf(rule));
    }
  };
  const sweep = () => {
    timer = 0;
    for (const sheet of Array.from(doc.styleSheets ?? []) as any[]) {
      const rules = rulesOf(sheet);
      // A sheet read already, with as many rules as then, has nothing new.
      if (counts.get(sheet) === rules.length) continue;
      counts.set(sheet, rules.length);
      visit(rules);
    }
  };
  const later = () => {
    if (!timer) timer = win.setTimeout(sweep, 120);
  };
  const styles = (nodes: any) =>
    Array.from(nodes ?? []).some((node: any) => node.nodeName === 'STYLE' || node.nodeName === 'LINK');
  return (next: boolean) => {
    if (next === on) return;
    on = next;
    if (on) {
      cursor = doc.createElement('style');
      cursor.setAttribute('data-tk-touch', '');
      cursor.textContent =
        'html,html *{cursor:url("data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 width=%2728%27 height=%2728%27%3E%3Ccircle cx=%2714%27 cy=%2714%27 r=%2711%27 fill=%27rgba(120,120,130,.35)%27 stroke=%27white%27 stroke-width=%272%27/%3E%3C/svg%3E") 14 14,pointer!important}';
      (doc.head ?? doc.documentElement).appendChild(cursor);
      sweep();
      // A sheet added, a <link> loaded or a <style> rewritten later is read in a moment.
      observer = new win.MutationObserver((records: any[]) => {
        if (
          records.some(
            (r) =>
              styles(r.addedNodes) ||
              (r.type === 'characterData' && r.target.parentNode?.nodeName === 'STYLE') ||
              r.target?.nodeName === 'STYLE',
          )
        )
          later();
      });
      observer.observe(doc.documentElement, { childList: true, subtree: true, characterData: true });
      doc.addEventListener('load', later, true);
      return;
    }
    observer?.disconnect();
    observer = null;
    doc.removeEventListener('load', later, true);
    if (timer) win.clearTimeout(timer);
    timer = 0;
    cursor?.remove();
    cursor = null;
    for (const { target, key, was } of changed.reverse()) {
      try {
        target[key] = was;
      } catch {
        // Gone with its sheet.
      }
    }
    changed = [];
    seen = new WeakSet();
    counts = new WeakMap();
  };
};

export const installBridge = (win: any, doc: any, parentOrigin: string, core: any): void => {
  // Taken before any script of the page runs (the bridge is the first one in <head>): the page cannot swap them later.
  const parent = win.parent;
  const nonce = Array.from(win.crypto.getRandomValues(new win.Uint8Array(16)) as ArrayLike<number>, (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');
  const siteTools = core.makeSiteTools();
  const bound = { ...core.captureNatives(win), origin: win.location.origin, parent, siteTools };
  core.installModelContext(win, siteTools);
  const post = (message: Record<string, unknown>) => parent.postMessage(message, parentOrigin);
  const actRefs = core.makeActRefs();
  const setTouch = core.installTouch(win, doc, core);
  const domState = core.makeDomState(actRefs);
  let lastUrl = win.location.href;
  let picking = false;
  let box: any = null;
  let label: any = null;

  const hide = () => {
    if (box) {
      box.remove();
      box = null;
      label = null;
    }
  };
  const setPicking = (on: boolean, report: boolean) => {
    if (picking === on) return;
    picking = on;
    if (!on) hide();
    if (report && !on) post({ type: 'tk:pick', on: false });
  };
  const highlight = (el: any) => {
    if (!box) {
      box = doc.createElement('div');
      box.setAttribute('data-tk-bridge-box', '');
      box.style.cssText =
        'position:absolute;pointer-events:none;z-index:2147483647;box-sizing:border-box;border:2px dashed #8b5cf6;background:rgba(139,92,246,.12);border-radius:2px';
      label = doc.createElement('div');
      label.style.cssText =
        'position:absolute;left:-2px;bottom:100%;margin-bottom:2px;padding:1px 6px;font:12px/18px system-ui,sans-serif;color:#fff;background:#8b5cf6;border-radius:3px;white-space:nowrap';
      box.appendChild(label);
      doc.documentElement.appendChild(box);
    }
    const rect = el.getBoundingClientRect();
    box.style.left = `${rect.left + win.scrollX}px`;
    box.style.top = `${rect.top + win.scrollY}px`;
    box.style.width = `${rect.width}px`;
    box.style.height = `${rect.height}px`;
    const found = core.findComponent(el, win);
    const tag = String(el.tagName).toLowerCase();
    label.textContent = found?.component ? `${found.component.name} · ${tag}` : tag;
  };
  const isPage = (target: any) =>
    target && typeof target.getBoundingClientRect === 'function' && target !== box && target !== label;

  let flashTimer = 0;
  const flash = (selector: string) => {
    let el: any = null;
    try {
      el = doc.querySelector(selector);
    } catch {
      el = null;
    }
    if (!isPage(el)) return;
    el.scrollIntoView({ block: 'center', inline: 'nearest' });
    highlight(el);
    win.clearTimeout(flashTimer);
    flashTimer = win.setTimeout(() => {
      if (!picking) hide();
    }, 1500);
  };

  // The blocks the person picked, by number, and what the bridge put on them: the parent tries looks on them (`tk:styles`).
  const MAX_PICKED = 64;
  const pickedBlocks = new Map<number, any>();
  const kept = new Map<any, any>();
  let pickSeq = 0;
  const remember = (el: any) => {
    pickSeq += 1;
    pickedBlocks.set(pickSeq, el);
    if (pickedBlocks.size > MAX_PICKED) {
      const [oldest] = pickedBlocks.keys();
      const gone = pickedBlocks.get(oldest);
      if (kept.has(gone)) core.restoreLook(gone, kept.get(gone));
      kept.delete(gone);
      pickedBlocks.delete(oldest);
    }
    return pickSeq;
  };
  const readLookSafely = (el: any) => {
    try {
      return core.readLook(el, win, doc);
    } catch {
      return null;
    }
  };
  const setStyles = (items: any[]) => {
    const wanted = new Set<any>();
    const missing: number[] = [];
    for (const item of items) {
      const el = pickedBlocks.get(item.id);
      if (!el || !el.isConnected) {
        missing.push(item.id);
        continue;
      }
      wanted.add(el);
      if (!kept.has(el)) kept.set(el, {});
      core.applyLook(el, item, kept.get(el));
    }
    for (const [el, was] of [...kept]) {
      if (wanted.has(el)) continue;
      core.restoreLook(el, was);
      kept.delete(el);
    }
    post({ type: 'tk:styled', gone: missing });
  };

  // The blocks the parent's notes are pinned to: by number while it is the block its selector names (a number from a page
  // loaded before may name another block now), else the first that the selector finds.
  const query = (selector: string) => {
    try {
      return selector ? doc.querySelector(selector) : null;
    } catch {
      return null;
    }
  };
  const findAnchor = (item: any) => {
    const el = item.pick === null ? null : pickedBlocks.get(item.pick);
    if (!el || !el.isConnected) return query(item.selector);
    let fits = true;
    try {
      fits = !item.selector || el.matches(item.selector);
    } catch {
      fits = true;
    }
    return fits ? el : (query(item.selector) ?? el);
  };
  const setAnchors = core.installAnchors(win, doc, post, findAnchor, core);

  const nav = () => {
    if (win.location.href === lastUrl) return;
    lastUrl = win.location.href;
    setPicking(false, true);
    win.setTimeout(() => post({ type: 'tk:nav', url: win.location.href, title: doc.title }), 0);
  };
  for (const method of ['pushState', 'replaceState']) {
    const original = win.history[method];
    win.history[method] = (...args: unknown[]) => {
      const result = original.apply(win.history, args);
      nav();
      return result;
    };
  }
  win.addEventListener('popstate', nav);
  win.addEventListener('hashchange', nav);

  // The design runtime's own card, bar and panels are pressed as they are: a pick never takes them, or the board's error
  // card could not be clicked while picking.
  const ownUi = (event: any) =>
    typeof event.composedPath === 'function' && core.isRuntimeUi(event.composedPath());
  const swallow = (event: any) => {
    if (!picking || ownUi(event)) return;
    event.preventDefault();
    event.stopPropagation();
    event.stopImmediatePropagation?.();
  };
  for (const type of [
    'pointerdown',
    'pointerup',
    'mousedown',
    'mouseup',
    'dblclick',
    'auxclick',
    'contextmenu',
  ])
    win.addEventListener(type, swallow, true);
  // A web component keeps its content in a shadow root, and the event's target is retargeted to its host: the first
  // node of the composed path is the element under the pointer (open roots; a closed one still yields its host).
  const deepTarget = (event: any) => {
    const first = typeof event.composedPath === 'function' ? event.composedPath()[0] : null;
    return first && first.nodeType === 1 ? first : event.target;
  };
  win.addEventListener(
    'pointermove',
    (event: any) => {
      if (!picking || ownUi(event)) return;
      const target = deepTarget(event);
      if (isPage(target)) highlight(target);
    },
    true,
  );
  win.addEventListener(
    'click',
    (event: any) => {
      if (!picking || ownUi(event)) return;
      swallow(event);
      const target = deepTarget(event);
      if (!isPage(target)) return;
      post({
        ...core.pickedMessage(target, win, doc, core),
        pick: remember(target),
        look: readLookSafely(target),
      });
      setPicking(false, true);
    },
    true,
  );
  win.addEventListener(
    'keydown',
    (event: any) => {
      if (!picking || event.key !== 'Escape') return;
      swallow(event);
      setPicking(false, true);
    },
    true,
  );

  // The page is being scrolled (the document or any scroller in it): the parent narrows its field while it goes and folds a
  // phone's bars by the direction. One message per 100 ms at most, the last position of a burst follows it.
  let scrollAt = 0;
  let scrollTimer = 0;
  let scrollY = 0;
  const postScroll = () => {
    scrollTimer = 0;
    scrollAt = Date.now();
    post({ type: 'tk:scroll', y: scrollY });
  };
  win.addEventListener(
    'scroll',
    (event: any) => {
      const target = event.target;
      scrollY = Number(target && target.nodeType === 1 ? target.scrollTop : win.scrollY) || 0;
      if (scrollTimer) return;
      const wait = 100 - (Date.now() - scrollAt);
      if (wait <= 0) postScroll();
      else scrollTimer = win.setTimeout(postScroll, wait);
    },
    { capture: true, passive: true },
  );

  // Screens scrolled together: the document's own scroll is told once a frame while the parent asks for it.
  let syncScroll = false;
  let syncFrame = 0;
  win.addEventListener(
    'scroll',
    (event: any) => {
      if (!syncScroll || syncFrame || (event.target && event.target.nodeType === 1)) return;
      syncFrame = win.requestAnimationFrame(() => {
        syncFrame = 0;
        post({ type: 'tk:doc-scroll', y: Number(win.scrollY) || 0 });
      });
    },
    { capture: true, passive: true },
  );

  // The canvas's zoom lives in the parent: a pinch (a wheel with ctrl on a trackpad), ⌘/Ctrl + wheel and Safari's gesture
  // over this page would zoom the browser's page or nothing. They are taken here and told to the parent as a wheel's delta
  // at the cursor (`tk:wheel`); the plain wheel stays the page's own scroll.
  const postWheel = (x: number, y: number, dy: number) => post({ type: 'tk:wheel', x, y, dy });
  win.addEventListener(
    'wheel',
    (event: any) => {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      postWheel(Number(event.clientX) || 0, Number(event.clientY) || 0, Number(event.deltaY) || 0);
    },
    { capture: true, passive: false },
  );
  let gestureScale = 1;
  win.addEventListener(
    'gesturestart',
    (event: any) => {
      event.preventDefault();
      gestureScale = 1;
    },
    { capture: true, passive: false },
  );
  win.addEventListener(
    'gesturechange',
    (event: any) => {
      event.preventDefault();
      const scale = Number(event.scale) || 1;
      postWheel(
        Number(event.clientX) || 0,
        Number(event.clientY) || 0,
        -Math.log(scale / gestureScale) / 0.01,
      );
      gestureScale = scale;
    },
    { capture: true, passive: false },
  );

  // On a canvas (`tk:canvas` from the parent), what moves the canvas is told to the parent instead of being the page's:
  // Space held (the canvas's hand, which lays a shield over this frame there) and a drag with the middle button. The
  // wheel stays the page's: at the end of its scroll nothing moves.
  let canvas = false;
  let spaceDown = false;
  let middle: { x: number; y: number } | null = null;
  const postSpace = (down: boolean) => {
    if (spaceDown === down) return;
    spaceDown = down;
    post({ type: 'tk:space', down });
  };
  win.addEventListener(
    'keydown',
    (event: any) => {
      if (!canvas || event.code !== 'Space' || event.ctrlKey || event.metaKey || event.altKey) return;
      if (core.isTypingTarget(event.target)) return;
      event.preventDefault();
      if (!event.repeat) postSpace(true);
    },
    true,
  );
  win.addEventListener(
    'keyup',
    (event: any) => {
      if (event.code !== 'Space' || !spaceDown) return;
      event.preventDefault();
      postSpace(false);
    },
    true,
  );
  win.addEventListener('blur', () => postSpace(false));
  const onMiddleDown = (event: any) => {
    if (!canvas || event.button !== 1) return;
    // No autoscroll and no paste of the selection: the press is the canvas's.
    event.preventDefault();
    if (event.type === 'pointerdown')
      middle = { x: Number(event.screenX) || 0, y: Number(event.screenY) || 0 };
  };
  win.addEventListener('pointerdown', onMiddleDown, true);
  win.addEventListener('mousedown', onMiddleDown, true);
  win.addEventListener(
    'pointermove',
    (event: any) => {
      if (!middle) return;
      // On the screen: the page moves under the pointer with the canvas, its own coordinates would not.
      const x = Number(event.screenX) || 0;
      const y = Number(event.screenY) || 0;
      post({ type: 'tk:pan', dx: x - middle.x, dy: y - middle.y, by: 'drag' });
      middle = { x, y };
    },
    true,
  );
  const endMiddle = (event: any) => {
    if (middle && (event.type !== 'pointerup' || event.button === 1)) middle = null;
  };
  win.addEventListener('pointerup', endMiddle, true);
  win.addEventListener('pointercancel', endMiddle, true);

  // A page can dispatch its own MessageEvent naming any source and origin, or patch the getters: only a trusted event,
  // read through the accessors taken now, comes from the parent.
  const isTrusted = (event: any) =>
    Object.getOwnPropertyDescriptor(event, 'isTrusted')?.get?.call(event) === true;
  win.addEventListener(
    'message',
    (event: any) => {
      if (!isTrusted(event)) return;
      const source = bound.messageSource ? bound.messageSource.call(event) : event.source;
      const origin = bound.messageOrigin ? bound.messageOrigin.call(event) : event.origin;
      if (source !== parent || origin !== parentOrigin) return;
      const message = core.parseParentMessage(bound.messageData ? bound.messageData.call(event) : event.data);
      if (!message) return;
      if (message.type === 'tk:ping') hello();
      else if (message.type === 'tk:highlight') flash(message.selector);
      else if (message.type === 'tk:pick') setPicking(message.on, false);
      else if (message.type === 'tk:styles') setStyles(message.items);
      else if (message.type === 'tk:anchors') setAnchors(message.items);
      else if (isDomMessage(message)) core.handleDomMessage(win, doc, bound, core, domState, message, post);
      else if (message.type === 'tk:act') {
        const answer = (result: { ok: boolean; text: string; rect?: unknown }) => {
          try {
            post({
              type: 'tk:acted',
              id: message.id,
              ok: result.ok,
              text: result.text,
              ...(result.rect ? { rect: result.rect } : {}),
            });
          } catch {
            // The parent is gone: nobody waits for the answer.
          }
        };
        try {
          core.runAct(win, doc, bound, core, actRefs, message).then(answer, (error: any) =>
            answer({
              ok: false,
              text: `The page got in the way of the action: ${String(error?.message ?? error).slice(0, 200)}`,
            }),
          );
        } catch (error: any) {
          answer({
            ok: false,
            text: `The page got in the way of the action: ${String(error?.message ?? error).slice(0, 200)}`,
          });
        }
      } else if (message.type === 'tk:scroll-sync') syncScroll = message.on;
      else if (message.type === 'tk:touch') setTouch(message.on);
      else if (message.type === 'tk:scroll-to') win.scrollTo({ top: message.y, behavior: 'instant' });
      else if (message.type === 'tk:canvas') {
        canvas = message.on;
        if (!canvas) postSpace(false);
      } else if (message.type === 'tk:fill') {
        // No listener of the page sees the token: this one runs first (capture, registered before any script of the page).
        bound.stopImmediately.call(event);
        const result = core.redeemAndFill(win, doc, message.token, nonce, bound, core);
        post({
          type: 'tk:filled',
          ok: result.ok,
          submitted: result.submitted,
          ...(result.reason ? { reason: result.reason } : {}),
        });
      } else if (message.type === 'tk:probe-login') {
        const fit = core.findLoginForm(win, doc, bound, core);
        post({
          type: 'tk:login-probe',
          passwordField: bound.ready && core.loginFields(win, doc, bound, core).length > 0,
          fit: fit.ok ? 'ok' : fit.reason,
        });
      } else if (message.action === 'back') win.history.back();
      else if (message.action === 'forward') win.history.forward();
      else win.location.reload();
    },
    true,
  );

  // The page on its way: what state its document is in and how many files it has taken so far go to the parent, which
  // tells the person what is loading while the page is not there yet (a design's runtime pulls several megabytes
  // after its document). Said at once, then at most five times a second, and no more once the page has loaded.
  let taken = 0;
  let progressTimer = 0;
  const postProgress = () => {
    progressTimer = 0;
    let state = 'loading';
    try {
      state = String(doc.readyState);
    } catch {
      state = 'loading';
    }
    post({ type: 'tk:progress', state, resources: taken });
  };
  const queueProgress = () => {
    if (progressTimer || doc.readyState === 'complete') return;
    progressTimer = win.setTimeout(postProgress, 200);
  };
  try {
    const Observer = win.PerformanceObserver;
    new Observer((list: any) => {
      taken += list.getEntries().length;
      queueProgress();
    }).observe({ type: 'resource', buffered: true });
    doc.addEventListener('readystatechange', queueProgress);
  } catch {
    // A page without the observer is simply not reported on: the parent keeps its first words.
  }
  postProgress();

  // `document` names override its built-ins (`<img name="title">`, `name="querySelectorAll"`): hello must survive a page that does that.
  const hello = () => {
    let found: { framework: string; ngVersion?: string } = { framework: 'unknown' };
    try {
      found = core.detectFramework(win, doc);
    } catch {
      found = { framework: 'unknown' };
    }
    let title = '';
    try {
      title = String(bound.title ? bound.title.call(doc) : doc.title);
    } catch {
      title = '';
    }
    post({
      type: 'tk:hello',
      url: win.location.href,
      title,
      framework: found.framework,
      nonce,
      ...(found.ngVersion ? { ngVersion: found.ngVersion } : {}),
    });
  };
  if (doc.readyState === 'complete') hello();
  else win.addEventListener('load', hello);
};

/** What the bridge applies to the page's requests, from the proxy (`data-devtools`, `/__tk/devtools/config`). */
export interface IDevtoolsConfig {
  blocked: string[];
  offline: boolean;
  latencyMs: number;
  /** Any throttling at all: entries say they ran under it. */
  throttled: boolean;
  /** What the slot's own addresses stand for: `http://localhost:4200`, `https://example.com`. */
  target: string | null;
}

export const parseDevtoolsConfig = (value: any): IDevtoolsConfig | null => {
  if (!value || typeof value !== 'object' || !Array.isArray(value.blocked)) return null;
  const blocked = value.blocked
    .filter((item: unknown) => typeof item === 'string' && item.length > 0 && item.length <= 300)
    .slice(0, 50);
  const throttle = value.throttle && typeof value.throttle === 'object' ? value.throttle : {};
  const latency = Number(throttle.latencyMs);
  const latencyMs = Number.isFinite(latency) && latency > 0 ? Math.min(Math.round(latency), 60_000) : 0;
  const offline = throttle.preset === 'offline';
  const target =
    typeof value.target === 'string' && /^https?:\/\/[^/?#\s]+$/.test(value.target) ? value.target : null;
  return {
    blocked,
    offline,
    latencyMs,
    throttled: offline || latencyMs > 0 || Number(throttle.downKbps) > 0 || Number(throttle.upKbps) > 0,
    target,
  };
};

/**
 * The store's `blockedBy`, in the page: a pattern found anywhere in the full URL, case-insensitively, `*` standing for
 * any run of characters. Piece by piece with indexOf, never a RegExp: many stars against a long URL would backtrack.
 */
export const blockedPattern = (url: string, patterns: string[]): string | null => {
  const lower = url.toLowerCase();
  for (const pattern of patterns) {
    let from = 0;
    const found = pattern
      .toLowerCase()
      .split('*')
      .filter(Boolean)
      .every((piece) => {
        const at = lower.indexOf(piece, from);
        if (at < 0) return false;
        from = at + piece.length;
        return true;
      });
    if (found) return pattern;
  }
  return null;
};

/** A slot address as the target's, as the proxy routes it: `/__tk/p/5173/x` is the neighbour `http://localhost:5173/x`. */
export const slotToTarget = (href: string, slotOrigin: string, target: string | null): string => {
  if (!target || !href.startsWith(slotOrigin)) return href;
  const rest = href.slice(slotOrigin.length);
  if (rest && !/^[/?#]/.test(rest)) return href;
  const neighbour = /^\/__tk\/p\/([1-9]\d{0,4})(?=[/?#]|$)\/?/.exec(rest);
  if (neighbour && /^http:\/\/localhost:\d+$/.test(target))
    return `http://localhost:${neighbour[1]}/${rest.slice(neighbour[0].length)}`;
  return `${target}${rest || '/'}`;
};

/** A console argument in words, never throwing: getters, proxies and cycles of the page's objects included. */
export const describeValue = (value: any, max: number): string => {
  const seen: any[] = [];
  const walk = (item: any, depth: number): string => {
    try {
      if (typeof item === 'string') return depth ? `'${item}'` : item;
      if (item === null || item === undefined || typeof item === 'number' || typeof item === 'boolean')
        return String(item);
      if (typeof item === 'bigint') return `${item}n`;
      if (typeof item === 'symbol') return item.toString();
      if (typeof item === 'function') return `ƒ ${item.name || 'anonymous'}()`;
      if (typeof item.message === 'string' && typeof item.stack === 'string')
        return `${item.name || 'Error'}: ${item.message}`;
      if (typeof item.nodeType === 'number' && typeof item.nodeName === 'string') {
        return `<${item.nodeName.toLowerCase()}${typeof item.id === 'string' && item.id ? `#${item.id}` : ''}>`;
      }
      if (seen.includes(item)) return '[Circular]';
      const name =
        item.constructor && item.constructor !== Object && typeof item.constructor.name === 'string'
          ? item.constructor.name
          : '';
      if (typeof item.size === 'number' && typeof item.forEach === 'function') return `${name}(${item.size})`;
      if (depth >= 2) return Array.isArray(item) ? `Array(${item.length})` : `${name}{…}`;
      seen.push(item);
      if (Array.isArray(item)) {
        const parts = item.slice(0, 20).map((part) => walk(part, depth + 1));
        if (item.length > 20) parts.push(`…${item.length - 20}`);
        return `[${parts.join(', ')}]`;
      }
      const keys = Object.keys(item);
      const field = (key: string) => {
        try {
          return walk(item[key], depth + 1);
        } catch {
          return '[?]';
        }
      };
      const parts = keys.slice(0, 20).map((key) => `${key}: ${field(key)}`);
      if (keys.length > 20) parts.push('…');
      return `${name ? `${name} ` : ''}{${parts.join(', ')}}`;
    } catch {
      return '[?]';
    }
  };
  const text = walk(value, 0);
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
};

/** console.* arguments as Chrome prints them: `%s`/`%d`/`%o` substituted, `%c` styles dropped; the stack of the first error. */
export const consoleText = (args: any[], core: any): { text: string; stack: string | null } => {
  const rest = Array.from(args);
  const parts: string[] = [];
  if (typeof rest[0] === 'string' && rest[0].includes('%')) {
    parts.push(
      String(rest.shift()).replace(/%[sdifoOc%]/g, (token) => {
        if (token === '%%') return '%';
        if (!rest.length) return token;
        const next = rest.shift();
        if (token === '%c') return '';
        if (token === '%d' || token === '%i') return String(parseInt(String(next), 10));
        if (token === '%f') return String(parseFloat(String(next)));
        return core.describeValue(next, 1000);
      }),
    );
  }
  for (const item of rest) parts.push(core.describeValue(item, 2000));
  const error = args.find((item) => item && typeof item === 'object' && typeof item.stack === 'string');
  return {
    text: core.clip(parts.join(' '), 4000),
    stack: error ? core.clip(String(error.stack), 4000) : null,
  };
};

/** A Resource Timing initiator, or the tag of an element that failed to load, as a DevTools type. */
export const resourceTypeOfInitiator = (initiator: string, url: string): string => {
  const path = url.split(/[?#]/)[0];
  if (initiator === 'script') return 'script';
  if (initiator === 'img' || initiator === 'image' || initiator === 'input') return 'image';
  if (initiator === 'audio' || initiator === 'video' || initiator === 'source' || initiator === 'track')
    return 'media';
  if (initiator === 'iframe' || initiator === 'frame') return 'document';
  if (/\.(woff2?|ttf|otf|eot)$/i.test(path)) return 'font';
  if (/\.css$/i.test(path)) return 'style';
  if (/\.(png|jpe?g|gif|webp|avif|svg|ico)$/i.test(path)) return 'image';
  return initiator === 'link' ? 'style' : 'other';
};

/**
 * DevTools of the right panel's browser, inside the page: console errors and warnings, uncaught errors and
 * rejections, failed loads, and requests to other sites (the proxy sees only the target's), sent in batches to the
 * slot's `/__tk/devtools/report`. Blocking, offline and latency apply here to fetch, XHR and sendBeacon, and blocking
 * and offline to scripts and images given a `src` by code. The slot's own requests stay with the proxy, which logs and
 * throttles them; only a fetch, XHR or beacon to the target that DevTools refuse fails here, as in Chrome. Everything
 * the bridge uses is taken before the page's scripts run: its own requests are never logged, blocked or delayed, and
 * nothing it reports can report itself again.
 */
export const installDevtools = (win: any, doc: any, core: any, initial: string | null): void => {
  const origin = win.location.origin;
  if (typeof origin !== 'string' || !/^https?:\/\//.test(origin)) return;
  // An opaque-origin document (an app's view in its sandbox) may not reach the slot: no reports, no polling.
  if (win.origin === 'null') return;
  const Url = win.URL;
  const parse = win.JSON.parse;
  const stringify = win.JSON.stringify;
  const later = win.setTimeout;
  const cancel = win.clearTimeout;
  const every = win.setInterval;
  const dateNow = win.Date.now;
  const clock = win.performance;
  const clockNow = clock && clock.now;
  const nativeFetch = typeof win.fetch === 'function' ? win.fetch : null;
  const nav = win.navigator;
  const beacon = nav && typeof nav.sendBeacon === 'function' ? nav.sendBeacon : null;
  const NativeBlob = win.Blob;
  const eventTarget = win.EventTarget && win.EventTarget.prototype;
  const listen = eventTarget && eventTarget.addEventListener;
  const dispatch = eventTarget && eventTarget.dispatchEvent;
  const now = (): number => dateNow.call(win.Date);
  const mark = (): number => (clockNow ? clockNow.call(clock) : now());
  const read = (text: string | null) => {
    try {
      return text ? parse(text) : null;
    } catch {
      return null;
    }
  };
  const reportUrl = `${origin}/__tk/devtools/report`;
  const configUrl = `${origin}/__tk/devtools/config`;
  let config: IDevtoolsConfig = core.parseDevtoolsConfig(read(initial)) || {
    blocked: [],
    offline: false,
    latencyMs: 0,
    throttled: false,
    target: null,
  };

  const queue: any[] = [];
  const resources: any[] = [];
  /** Failed loads of other sites, matched with their Resource Timing entries when the batch goes. */
  const broken: Array<{ url: string; type: string; at: number }> = [];
  /** Other sites' addresses already logged (refused or failed): their Resource Timing entries are not logged again. */
  const logged = new win.Map();
  /** Elements whose `src` was refused: the error event they get carries no address, and their `src` is not it. */
  const refusedOn = new win.WeakMap();
  let timer: any = 0;
  let busy = false;

  const send = (events: any[], unloading: boolean) => {
    const body = stringify({ events });
    // text/plain: a beacon with any other type is refused by some engines.
    if (unloading && beacon && NativeBlob) {
      beacon.call(nav, reportUrl, new NativeBlob([body], { type: 'text/plain' }));
      return;
    }
    if (!nativeFetch) return;
    nativeFetch
      .call(win, reportUrl, {
        method: 'POST',
        body,
        headers: { 'content-type': 'text/plain' },
        credentials: 'same-origin',
        cache: 'no-store',
        keepalive: body.length < 60_000,
      })
      .then(undefined, () => {});
  };
  const network = (entry: any) =>
    enqueue({
      kind: 'network',
      at: now(),
      method: 'GET',
      status: null,
      mime: null,
      size: null,
      durationMs: null,
      thirdParty: true,
      blocked: false,
      throttled: config.throttled,
      error: null,
      ...entry,
    });
  const drain = () => {
    const cutoff = now() - 30_000;
    logged.forEach((at: number, url: string) => {
      if (at < cutoff) logged.delete(url);
    });
    const timeOrigin = clock && typeof clock.timeOrigin === 'number' ? clock.timeOrigin : now();
    for (const entry of resources.splice(0)) {
      if (
        entry.initiatorType === 'fetch' ||
        entry.initiatorType === 'xmlhttprequest' ||
        entry.initiatorType === 'beacon'
      )
        continue;
      const info = classifySafely(entry.name);
      if (!info || info.same || logged.has(info.url)) continue;
      logged.set(info.url, now());
      const failedAt = broken.findIndex((item) => item.url === info.url);
      if (failedAt >= 0) broken.splice(failedAt, 1);
      const status = entry.responseStatus || null;
      network({
        at: Math.round(timeOrigin + entry.startTime),
        url: info.url,
        type: core.resourceTypeOfInitiator(String(entry.initiatorType), info.url),
        status,
        size: entry.transferSize || null,
        durationMs: Math.round(entry.duration),
        error: failedAt >= 0 && !status ? 'failed' : null,
      });
    }
    for (const item of broken.splice(0)) {
      if (logged.has(item.url)) continue;
      logged.set(item.url, now());
      network({ at: item.at, url: item.url, type: item.type, error: 'failed' });
    }
  };
  const flush = (unloading: boolean) => {
    if (timer) {
      cancel.call(win, timer);
      timer = 0;
    }
    drain();
    while (queue.length) send(queue.splice(0, 100), unloading);
  };
  const schedule = () => {
    if (!timer)
      timer = later.call(
        win,
        () => {
          timer = 0;
          flush(false);
        },
        1000,
      );
  };
  const enqueue = (event: any) => {
    if (queue.length >= 1000) return;
    queue.push(event);
    if (queue.length >= 50) flush(false);
    else schedule();
  };
  const log = (level: string, source: string, text: string, stack: string | null) =>
    enqueue({
      kind: 'console',
      at: now(),
      level,
      source,
      text: core.clip(text, 4000),
      stack,
      url: win.location.href,
    });

  /** null for what is not a request DevTools show: other schemes, and the platform's own `/__tk/` paths. */
  const classify = (raw: unknown): { same: boolean; url: string } | null => {
    let url: any;
    try {
      url = new Url(String(raw), doc.baseURI || win.location.href);
    } catch {
      return null;
    }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
    const same = url.origin === origin;
    if (same && url.pathname.startsWith('/__tk/') && !url.pathname.startsWith('/__tk/p/')) return null;
    return { same, url: same ? core.slotToTarget(url.href, origin, config.target) : url.href };
  };
  const classifySafely = (raw: unknown) => {
    try {
      return classify(raw);
    } catch {
      return null;
    }
  };
  const refusalOf = (info: { url: string }): { error: string; blocked: boolean } | null => {
    if (config.offline) return { error: 'offline', blocked: false };
    const pattern = core.blockedPattern(info.url, config.blocked);
    return pattern ? { error: `blocked: ${pattern}`, blocked: true } : null;
  };
  const delayed = (run: () => void) => {
    if (config.latencyMs > 0) later.call(win, run, config.latencyMs);
    else run();
  };

  const con = win.console;
  for (const level of ['error', 'warn']) {
    const original = con && con[level];
    if (typeof original !== 'function') continue;
    con[level] = (...args: any[]) => {
      if (!busy) {
        busy = true;
        try {
          const { text, stack } = core.consoleText(args, core);
          log(level, 'console', text, stack);
        } catch {
          /* an argument that cannot be read at all */
        } finally {
          busy = false;
        }
      }
      return original.apply(con, args);
    };
  }

  const failedElement = (el: any) => {
    const tag = String(el.tagName).toLowerCase();
    const refusals = refusedOn.get(el);
    if (refusals && refusals.length) {
      log('error', 'network', `Blocked in DevTools ${tag}: ${refusals.shift()}`, null);
      return;
    }
    const info = classifySafely(el.currentSrc || el.src || el.href);
    if (!info) return;
    log('error', 'network', `Failed to load ${tag}: ${info.url}`, null);
    if (!info.same)
      broken.push({ url: info.url, type: core.resourceTypeOfInitiator(tag, info.url), at: now() });
  };
  const exceptionStack = (event: any): string | null => {
    const error = event.error;
    if (error && typeof error.stack === 'string') return error.stack;
    return event.filename ? `at ${event.filename}:${event.lineno}:${event.colno}` : null;
  };
  win.addEventListener(
    'error',
    (event: any) => {
      if (busy || !event) return;
      busy = true;
      try {
        const el = event.target;
        if (el && el !== win && el.nodeType === 1) {
          failedElement(el);
          return;
        }
        log(
          'error',
          'exception',
          String(event.message || core.describeValue(event.error, 1000)),
          exceptionStack(event),
        );
      } catch {
        /* an error that cannot be read */
      } finally {
        busy = false;
      }
    },
    true,
  );
  win.addEventListener('unhandledrejection', (event: any) => {
    if (busy || !event) return;
    busy = true;
    try {
      const reason = event.reason;
      log(
        'error',
        'rejection',
        `Uncaught (in promise) ${core.describeValue(reason, 1000)}`,
        reason && typeof reason.stack === 'string' ? reason.stack : null,
      );
    } catch {
      /* a reason that cannot be read */
    } finally {
      busy = false;
    }
  });

  if (nativeFetch) {
    win.fetch = (input: any, init?: any) => {
      let info: { same: boolean; url: string } | null = null;
      let method = 'GET';
      try {
        const request = win.Request && input instanceof win.Request ? input : null;
        info = classify(request ? request.url : input);
        method = String((init && init.method) || (request ? request.method : 'GET')).toUpperCase();
      } catch {
        info = null;
      }
      if (!info) return nativeFetch.call(win, input, init);
      const target = info;
      const refusal = refusalOf(target);
      if (refusal) {
        network({ method, url: target.url, type: 'fetch', thirdParty: !target.same, ...refusal });
        return win.Promise.reject(new win.TypeError('Failed to fetch'));
      }
      if (target.same) return nativeFetch.call(win, input, init);
      const at = now();
      const started = mark();
      const run = () =>
        nativeFetch.call(win, input, init).then(
          (response: any) => {
            const opaque = response.type === 'opaque';
            const length = opaque ? null : response.headers.get('content-length');
            network({
              at,
              method,
              url: target.url,
              type: 'fetch',
              status: opaque ? null : response.status,
              mime: opaque ? null : response.headers.get('content-type'),
              size: length === null ? null : Number(length),
              durationMs: Math.round(mark() - started),
            });
            return response;
          },
          (error: any) => {
            network({
              at,
              method,
              url: target.url,
              type: 'fetch',
              error: error && error.name === 'AbortError' ? 'aborted' : 'failed',
              durationMs: Math.round(mark() - started),
            });
            throw error;
          },
        );
      if (config.latencyMs <= 0) return run();
      return new win.Promise((resolve: any) => later.call(win, resolve, config.latencyMs)).then(run);
    };
  }

  const xhrProto = win.XMLHttpRequest && win.XMLHttpRequest.prototype;
  if (xhrProto && listen && dispatch && win.WeakMap) {
    const open = xhrProto.open;
    const sendRequest = xhrProto.send;
    const abort = xhrProto.abort;
    const header = xhrProto.getResponseHeader;
    const statusOf = Object.getOwnPropertyDescriptor(xhrProto, 'status')?.get;
    const states = new win.WeakMap();
    const fire = (xhr: any, type: string) => dispatch.call(xhr, new (win.ProgressEvent || win.Event)(type));
    xhrProto.open = function (this: any, ...args: any[]) {
      states.set(this, {
        method: String(args[0]).toUpperCase(),
        info: classifySafely(args[1]),
        sync: args.length > 2 && !args[2],
        cancelled: false,
      });
      return open.apply(this, args);
    };
    xhrProto.abort = function (this: any, ...args: any[]) {
      const state = states.get(this);
      if (state) state.cancelled = true;
      return abort.apply(this, args);
    };
    xhrProto.send = function (this: any, ...args: any[]) {
      const state = states.get(this);
      if (!state || !state.info) return sendRequest.apply(this, args);
      const refusal = refusalOf(state.info);
      if (refusal) {
        network({
          method: state.method,
          url: state.info.url,
          type: 'xhr',
          thirdParty: !state.info.same,
          ...refusal,
        });
        if (state.sync)
          throw new win.DOMException("Failed to execute 'send' on 'XMLHttpRequest'", 'NetworkError');
        later.call(
          win,
          () => {
            fire(this, 'error');
            fire(this, 'loadend');
          },
          0,
        );
        return undefined;
      }
      if (state.info.same) return sendRequest.apply(this, args);
      const at = now();
      const started = mark();
      listen.call(this, 'loadend', () => {
        let status = 0;
        let length: string | null = null;
        let mime: string | null = null;
        try {
          status = statusOf ? statusOf.call(this) : this.status;
          length = header.call(this, 'content-length');
          mime = header.call(this, 'content-type');
        } catch {
          /* a request that never got an answer */
        }
        let error: string | null = null;
        if (!status) error = state.cancelled ? 'aborted' : 'failed';
        network({
          at,
          method: state.method,
          url: state.info.url,
          type: 'xhr',
          status: status || null,
          mime,
          size: length === null ? null : Number(length),
          durationMs: Math.round(mark() - started),
          error,
        });
      });
      if (state.sync || config.latencyMs <= 0) return sendRequest.apply(this, args);
      delayed(() => {
        if (state.cancelled) return;
        try {
          sendRequest.apply(this, args);
        } catch {
          /* aborted or reopened while waiting */
        }
      });
      return undefined;
    };
  }

  if (beacon) {
    nav.sendBeacon = (url: unknown, data?: unknown) => {
      const info = classifySafely(url);
      if (!info) return beacon.call(nav, url, data);
      const refusal = refusalOf(info);
      if (refusal) {
        network({ method: 'POST', url: info.url, type: 'beacon', thirdParty: !info.same, ...refusal });
        return false;
      }
      const queued = beacon.call(nav, url, data);
      if (!info.same)
        network({ method: 'POST', url: info.url, type: 'beacon', error: queued ? null : 'failed' });
      return queued;
    };
  }

  /** A script or image of another site given a `src` by code: refused, it fails to load as a blocked one does. */
  const refuseSource = (el: any, value: unknown, type: string): boolean => {
    const info = classifySafely(value);
    if (!info || info.same) return false;
    const refusal = refusalOf(info);
    if (!refusal) return false;
    logged.set(info.url, now());
    refusedOn.set(el, [...(refusedOn.get(el) || []), info.url]);
    network({ url: info.url, type, ...refusal });
    if (dispatch)
      later.call(
        win,
        () => {
          try {
            dispatch.call(el, new win.Event('error'));
          } catch {
            /* a detached element */
          }
        },
        0,
      );
    return true;
  };
  const sourceTypeOf = (el: any): string | null => {
    if (win.HTMLScriptElement && el instanceof win.HTMLScriptElement) return 'script';
    if (win.HTMLImageElement && el instanceof win.HTMLImageElement) return 'image';
    return null;
  };
  for (const Element of [win.HTMLScriptElement, win.HTMLImageElement]) {
    const proto = Element && Element.prototype;
    const own = proto && Object.getOwnPropertyDescriptor(proto, 'src');
    if (!own || !own.get || !own.set) continue;
    Object.defineProperty(proto, 'src', {
      configurable: true,
      enumerable: own.enumerable,
      get: own.get,
      set(this: any, value: unknown) {
        const type = sourceTypeOf(this);
        if (!type || !refuseSource(this, value, type)) own.set.call(this, value);
      },
    });
  }
  const elementProto = win.Element && win.Element.prototype;
  const setAttribute = elementProto && elementProto.setAttribute;
  if (setAttribute) {
    elementProto.setAttribute = function (this: any, ...args: any[]) {
      const name = args[0];
      if (typeof name === 'string' && name.length === 3 && name.toLowerCase() === 'src') {
        const type = sourceTypeOf(this);
        if (type && refuseSource(this, args[1], type)) return undefined;
      }
      return setAttribute.apply(this, args);
    };
  }

  const Observer = win.PerformanceObserver;
  if (Observer) {
    try {
      new Observer((list: any) => {
        for (const entry of list.getEntries()) if (resources.length < 500) resources.push(entry);
        if (resources.length) schedule();
      }).observe({ type: 'resource', buffered: true });
    } catch {
      /* no Resource Timing in this engine */
    }
  }

  if (nativeFetch && typeof every === 'function') {
    every.call(
      win,
      () => {
        if (doc.visibilityState === 'hidden') return;
        nativeFetch
          .call(win, configUrl, { credentials: 'same-origin', cache: 'no-store' })
          .then((response: any) => (response.ok ? response.text() : null))
          .then(
            (text: string | null) => {
              const next = core.parseDevtoolsConfig(read(text));
              if (next) config = next;
            },
            () => {},
          );
      },
      3000,
    );
  }
  win.addEventListener('pagehide', () => flush(true));
};

// ---------------------------------------------------------------------------------------------------------------------
// The coding agent acting on the page (`tk:act` → `tk:acted`). The page is foreign code: everything is read through the
// accessors taken before its scripts ran, inside try/catch, and nothing is written to its DOM but what an action does.
// ---------------------------------------------------------------------------------------------------------------------

/** The refs of one document: the same element keeps its ref over snapshots; a ref is only given by a snapshot. */
export interface IActRefs {
  byElement: WeakMap<object, string>;
  byRef: Map<string, { deref: () => any }>;
  next: number;
  /** The bridge's own overlay (the DevTools mark): never listed to the agent. */
  skip?: any;
}

export const makeActRefs = (): IActRefs => ({ byElement: new WeakMap(), byRef: new Map(), next: 1 });

const ACT_MAX_ITEMS = 400;
const ACT_MAX_CHARS = 24_000;
const ACT_ROLES = 'button|link|checkbox|radio|tab|menuitem|option|switch|combobox|textbox';
const ACT_ITEM_SELECTOR =
  `a[href],button,input:not([type="hidden"]),textarea,select,summary,[role]:not([role=""]),` +
  `[contenteditable]:not([contenteditable="false"]),h1,h2,h3`;
const ACT_FOCUSABLE_SELECTOR =
  'a[href],button,input:not([type="hidden"]),select,textarea,summary,[tabindex],[contenteditable]:not([contenteditable="false"])';
const ACT_SECRET_NAME = /card|cvv|cvc|ccnum|cc-?num|cardnumber|(^|[^a-z])pan([^a-z]|$)/i;
export const ACT_SECRET_REFUSAL =
  'This field takes a password or card details: the person types it themselves.';

const actClip = (value: string, max: number): string => {
  const flat = value.replace(/\s+/g, ' ').trim();
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
};

const actAttr = (bound: any, el: any, name: string): string | null => {
  try {
    const value = bound.getAttribute.call(el, name);
    return value === null || value === undefined ? null : String(value);
  } catch {
    return null;
  }
};

const actTag = (bound: any, el: any): string => {
  try {
    return String(bound.tagName.call(el)).toLowerCase();
  } catch {
    return '';
  }
};

const actTextOf = (bound: any, node: any): string => {
  try {
    return String(bound.textContent.call(node) ?? '');
  } catch {
    return '';
  }
};

/** What the person reads on the page: `innerText` where the engine has it. */
const actPageText = (bound: any, doc: any): string => {
  try {
    const body = doc.body;
    if (!body) return '';
    const text = bound.innerText ? bound.innerText.call(body) : undefined;
    return String(typeof text === 'string' ? text : (bound.textContent.call(body) ?? ''));
  } catch {
    return '';
  }
};

const actSelectAll = (bound: any, root: any, selector: string): any[] => {
  try {
    const select = root.nodeType === 9 ? bound.selectAllInDocument : bound.selectAll;
    return Array.from(select.call(root, selector) as ArrayLike<any>);
  } catch {
    return [];
  }
};

const actInputType = (bound: any, el: any): string => {
  try {
    return String(bound.inputType.call(el)).toLowerCase();
  } catch {
    return 'text';
  }
};

const actIsInput = (bound: any, el: any): boolean => el instanceof bound.HTMLInputElement;
const actIsTextarea = (bound: any, el: any): boolean =>
  Boolean(bound.HTMLTextAreaElement) && el instanceof bound.HTMLTextAreaElement;
const actIsSelect = (bound: any, el: any): boolean =>
  Boolean(bound.HTMLSelectElement) && el instanceof bound.HTMLSelectElement;
const actIsEditable = (bound: any, el: any): boolean => {
  const value = actAttr(bound, el, 'contenteditable');
  return value !== null && value.toLowerCase() !== 'false';
};

export const actVisible = (win: any, el: any, bound: any): boolean => {
  try {
    const box = bound.box.call(el);
    if (!(box.width > 0 && box.height > 0)) return false;
    const style = bound.style.call(win, el);
    return style.visibility !== 'hidden' && style.display !== 'none';
  } catch {
    return false;
  }
};

/** A field that takes a password or card details: the agent never types into it and its value is never shown. */
export const actIsSecret = (bound: any, el: any): boolean => {
  const tag = actTag(bound, el);
  if (tag !== 'input' && tag !== 'textarea' && tag !== 'select') return false;
  if (actIsInput(bound, el) && actInputType(bound, el) === 'password') return true;
  const autocomplete = (actAttr(bound, el, 'autocomplete') ?? '').toLowerCase();
  if (/(^|\s)(cc-|current-password|new-password|one-time-code)/.test(autocomplete)) return true;
  return [actAttr(bound, el, 'name'), actAttr(bound, el, 'id'), autocomplete].some(
    (hint) => hint !== null && ACT_SECRET_NAME.test(hint),
  );
};

export const actRole = (bound: any, el: any): string => {
  const tag = actTag(bound, el);
  const explicit = (actAttr(bound, el, 'role') ?? '').trim().toLowerCase().split(/\s+/)[0];
  if (explicit && new RegExp(`^(${ACT_ROLES})$`).test(explicit)) return explicit;
  if (tag === 'a') return 'link';
  if (tag === 'button' || tag === 'summary') return 'button';
  if (tag === 'select') return 'combobox';
  if (tag === 'textarea') return 'textbox';
  if (tag === 'input') {
    const type = actInputType(bound, el);
    if (type === 'button' || type === 'submit' || type === 'reset' || type === 'image') return 'button';
    if (type === 'checkbox' || type === 'radio') return type;
    return 'textbox';
  }
  if (actIsEditable(bound, el)) return 'textbox';
  return '';
};

const actByIdText = (bound: any, doc: any, ids: string): string => {
  const parts: string[] = [];
  for (const id of ids.split(/\s+/).filter(Boolean).slice(0, 8)) {
    const found = actSelectAll(bound, doc, `[id="${id.replace(/["\\]/g, '\\$&')}"]`)[0];
    if (found) parts.push(actTextOf(bound, found));
  }
  return actClip(parts.join(' '), 80);
};

/** The name a person would call an element by: aria-label, aria-labelledby, its label, placeholder, alt, title, its text. */
export const actName = (bound: any, doc: any, el: any): string => {
  const tag = actTag(bound, el);
  const label = actAttr(bound, el, 'aria-label');
  if (label && label.trim()) return actClip(label, 80);
  const labelledBy = actAttr(bound, el, 'aria-labelledby');
  if (labelledBy) {
    const named = actByIdText(bound, doc, labelledBy);
    if (named) return named;
  }
  if (tag === 'input' || tag === 'textarea' || tag === 'select') {
    let text = '';
    try {
      for (const item of Array.from((el.labels ?? []) as ArrayLike<any>))
        text += ` ${actTextOf(bound, item)}`;
    } catch {
      text = '';
    }
    if (!text.trim()) {
      try {
        const wrap = bound.closest.call(el, 'label');
        if (wrap) text = actTextOf(bound, wrap);
      } catch {
        text = '';
      }
    }
    if (text.trim()) return actClip(text, 80);
  }
  const placeholder = actAttr(bound, el, 'placeholder');
  if (placeholder && placeholder.trim()) return actClip(placeholder, 80);
  const alt = actAttr(bound, el, 'alt');
  if (alt && alt.trim()) return actClip(alt, 80);
  if (tag === 'input' && ['button', 'submit', 'reset'].includes(actInputType(bound, el))) {
    const value = actAttr(bound, el, 'value');
    if (value && value.trim()) return actClip(value, 80);
  }
  const title = actAttr(bound, el, 'title');
  if (title && title.trim()) return actClip(title, 80);
  if (tag === 'input' || tag === 'select' || tag === 'textarea') return '';
  return actClip(actTextOf(bound, el), 80);
};

const actQuote = (value: string): string => `"${value.replace(/["\\]/g, '\\$&')}"`;

const actHref = (win: any, bound: any, el: any): string | null => {
  const raw = actAttr(bound, el, 'href');
  if (raw === null || raw.trim() === '' || /^\s*javascript:/i.test(raw)) return null;
  try {
    const url = new win.URL(raw, win.location.href);
    return actClip(
      url.origin === win.location.origin ? `${url.pathname}${url.search}${url.hash}` : url.href,
      100,
    );
  } catch {
    return actClip(raw, 100);
  }
};

/** The words after the name: what the element is in (disabled, checked, expanded…) and what it holds. */
const actStates = (win: any, bound: any, el: any, role: string): string[] => {
  const words: string[] = [];
  const tag = actTag(bound, el);
  const flag = (name: string) => actAttr(bound, el, name)?.toLowerCase();
  try {
    if (el.disabled === true || flag('aria-disabled') === 'true') words.push('disabled');
  } catch {
    // A page may put anything under these names.
  }
  try {
    if (role === 'checkbox' || role === 'radio' || role === 'switch') {
      const checked = actIsInput(bound, el) ? el.checked === true : flag('aria-checked') === 'true';
      if (checked) words.push('checked');
    }
  } catch {
    // Same.
  }
  if (flag('aria-expanded') === 'true') words.push('expanded');
  if (flag('aria-expanded') === 'false') words.push('collapsed');
  if (flag('aria-selected') === 'true') words.push('selected');
  if (flag('aria-pressed') === 'true') words.push('pressed');
  if (tag === 'a') {
    const href = actHref(win, bound, el);
    if (href) words.push(`href=${href}`);
  }
  const secret = actIsSecret(bound, el);
  if (secret)
    words.push(actIsInput(bound, el) && actInputType(bound, el) === 'password' ? 'password' : 'sensitive');
  try {
    if (tag === 'input' && role === 'textbox') {
      const type = actInputType(bound, el);
      if (type !== 'text' && type !== 'password') words.push(`type=${type}`);
      const value = String(el.value ?? '');
      if (!secret && value) words.push(`value=${actQuote(actClip(value, 60))}`);
    } else if (tag === 'textarea') {
      const value = String(el.value ?? '');
      if (!secret && value) words.push(`value=${actQuote(actClip(value, 60))}`);
    } else if (actIsSelect(bound, el)) {
      const options = Array.from((el.options ?? []) as ArrayLike<any>);
      const picked = options
        .filter((option) => option.selected)
        .map((option) => actClip(actTextOf(bound, option), 40));
      if (!secret && picked.length) words.push(`value=${actQuote(picked.join(', '))}`);
      const shown = options.slice(0, 20).map((option) => actQuote(actClip(actTextOf(bound, option), 40)));
      words.push(
        `options=[${shown.join(', ')}${options.length > 20 ? `, … ${options.length - 20} more` : ''}]`,
      );
    } else if (actIsEditable(bound, el)) {
      const value = actClip(actTextOf(bound, el), 60);
      if (value) words.push(`value=${actQuote(value)}`);
    }
  } catch {
    // Fields the page broke are listed without their value.
  }
  return words;
};

/** The ref of an element, given now if it has none. */
const actRefOf = (refs: IActRefs, bound: any, el: any): string => {
  const known = refs.byElement.get(el);
  if (known) return known;
  if (refs.next > 999_000) {
    // A document that lived through a million elements: start over, old refs read as unknown.
    refs.byRef.clear();
    refs.next = 1;
  }
  const ref = `e${refs.next}`;
  refs.next += 1;
  refs.byElement.set(el, ref);
  refs.byRef.set(ref, typeof bound.WeakRef === 'function' ? new bound.WeakRef(el) : { deref: () => el });
  return ref;
};

/** The page as text: its address and title, then what can be acted on, with refs, in the document's order. */
export const actSnapshot = (win: any, doc: any, bound: any, refs: IActRefs): string => {
  let title = '';
  try {
    title = String(bound.title.call(doc));
  } catch {
    title = '';
  }
  const lines = [`url: ${String(win.location.href)}`, `title: ${actClip(title, 200)}`];
  let size = lines.join('\n').length;
  let listed = 0;
  let skipped = 0;
  for (const el of actSelectAll(bound, doc, ACT_ITEM_SELECTOR)) {
    if (refs.skip && (el === refs.skip || refs.skip.contains(el))) continue;
    const tag = actTag(bound, el);
    const heading = /^h[1-3]$/.test(tag);
    const role = heading ? 'heading' : actRole(bound, el);
    if (!role || !actVisible(win, el, bound)) continue;
    let line: string;
    if (heading) {
      const text = actClip(actTextOf(bound, el), 80);
      if (!text) continue;
      line = `- heading ${actQuote(text)} level=${tag.slice(1)}`;
    } else {
      if (listed >= ACT_MAX_ITEMS || size > ACT_MAX_CHARS) {
        skipped += 1;
        continue;
      }
      const name = actName(bound, doc, el);
      const parts = [`- ${role}`];
      if (name) parts.push(actQuote(name));
      parts.push(`[${actRefOf(refs, bound, el)}]`, ...actStates(win, bound, el, role));
      line = parts.join(' ');
      listed += 1;
    }
    if (size + line.length > ACT_MAX_CHARS + 400) {
      skipped += 1;
      continue;
    }
    size += line.length + 1;
    lines.push(line);
  }
  if (skipped > 0) lines.push(`… (${skipped} more)`);
  if (listed === 0) lines.push('(nothing to act on is shown on this page yet)');
  return lines.join('\n');
};

const actDescribe = (bound: any, doc: any, el: any): string => {
  const role = actRole(bound, el) || actTag(bound, el) || 'element';
  const name = actName(bound, doc, el);
  return name ? `${role} ${actQuote(name)}` : role;
};

const actFail = (text: string): IActResult => ({ ok: false, text });

/** The element a ref stands for; the failure when the page dropped it or the ref is not one a snapshot gave. */
const actResolve = (
  refs: IActRefs,
  bound: any,
  ref: string | undefined,
): { el: any } | { fail: IActResult } => {
  if (!ref) return { fail: actFail('This action needs a ref from the last snapshot.') };
  const entry = refs.byRef.get(ref);
  if (!entry) return { fail: actFail(`${ref} is not a ref of this page: take a snapshot first.`) };
  const el = entry.deref();
  let connected = false;
  try {
    connected = Boolean(el && bound.isConnected.call(el));
  } catch {
    connected = false;
  }
  if (!connected) return { fail: actFail(`${ref} is gone from the page: take a new snapshot`) };
  return { el };
};

const actBox = (win: any, bound: any, el: any): IActResult['rect'] => {
  const box = bound.box.call(el);
  return { x: box.left, y: box.top, width: box.width, height: box.height };
};

const actBringIntoView = (el: any, bound: any): void => {
  try {
    bound.scrollIntoView.call(el, { block: 'center', inline: 'nearest', behavior: 'instant' });
  } catch {
    // An element that cannot be scrolled to is acted on where it is.
  }
};

const actCenter = (rect: NonNullable<IActResult['rect']>) => ({
  clientX: rect.x + rect.width / 2,
  clientY: rect.y + rect.height / 2,
});

const actFire = (win: any, bound: any, el: any, type: string, init: Record<string, unknown>): boolean => {
  const Pointer =
    type.startsWith('pointer') && typeof bound.PointerEvent === 'function' ? bound.PointerEvent : null;
  const Ctor = Pointer ?? bound.MouseEvent ?? bound.Event;
  const enter = type.endsWith('enter') || type.endsWith('leave');
  const event = new Ctor(type, {
    bubbles: !enter,
    cancelable: !enter,
    composed: true,
    ...init,
    ...(Pointer ? { pointerId: 1, pointerType: 'mouse', isPrimary: true } : {}),
  });
  return bound.dispatch.call(el, event);
};

const actFocus = (bound: any, el: any): void => {
  try {
    bound.focus.call(el);
  } catch {
    // Not focusable: the events still go to it.
  }
};

const actUrlNote = (win: any, before: string): string => {
  const now = String(win.location.href);
  return now === before ? '' : `; the page is now ${now}`;
};

const actPause = (win: any, bound: any, ms: number): Promise<void> =>
  new Promise((resolve) => {
    bound.setTimeout.call(win, resolve, ms);
  });

const TOOLS_LISTED = 50;
const TOOL_WAIT_MS = 10_000;

const actSiteTools = (bound: any): ISiteTool[] =>
  bound.siteTools ? Array.from(bound.siteTools.values()) : [];

const actToolsHint = (bound: any): string => {
  const count = actSiteTools(bound).length;
  return count > 0
    ? `\nThis page offers ${count} tools of its own (WebMCP): list them with browser_site_tools.`
    : '';
};

const actToolsList = (bound: any): IActResult => {
  const tools = actSiteTools(bound);
  if (tools.length === 0) return { ok: true, text: 'This page offers no tools of its own (WebMCP).' };
  const lines = [`This page offers ${tools.length} tools of its own (WebMCP):`];
  for (const tool of tools.slice(0, TOOLS_LISTED)) {
    const about = tool.description.replace(/\s+/g, ' ').trim();
    lines.push(`- ${tool.name}${tool.readOnly ? ' (read-only)' : ''}: ${about}`);
    if (tool.inputSchema) {
      let schema: string;
      try {
        schema = String(bound.stringify(tool.inputSchema)).slice(0, 600);
      } catch {
        schema = '';
      }
      if (schema) lines.push(`  input: ${schema}`);
    }
  }
  if (tools.length > TOOLS_LISTED) lines.push(`…and ${tools.length - TOOLS_LISTED} more`);
  return { ok: true, text: lines.join('\n') };
};

/** What a tool answered as text, and whether it says it failed. */
const actToolText = (bound: any, result: any): { text: string; error: boolean } => {
  if (typeof result === 'string') return { text: result, error: false };
  if (result && typeof result === 'object' && Array.isArray(result.content)) {
    const parts = result.content.map((item: any) =>
      item && item.type === 'text' && typeof item.text === 'string' ? item.text : `[${String(item?.type)}]`,
    );
    return { text: parts.join('\n'), error: result.isError === true };
  }
  try {
    const json = bound.stringify(result);
    return { text: typeof json === 'string' ? json : String(result), error: false };
  } catch {
    return { text: String(result), error: false };
  }
};

// A site tool's field for a password, a one-time code or card details: the agent never fills one, as with typing.
const TOOL_SECRET_WORD =
  /\b(password|passwd|passcode|passphrase|pwd|otp|totp|mfa|2fa|one time (code|password)|pincode|pin code|cvv|cvv2|cvc|cardnumber|card (number|num|no)|ccnum|pan|secret)\b/;

export const isSecretKey = (key: string): boolean =>
  TOOL_SECRET_WORD.test(
    key
      .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
      .replace(/[_\-.]+/g, ' ')
      .toLowerCase(),
  );

/** What a check of keys came to: the first key that names a secret, or that the check could not finish. */
export type TSecretCheck = { kind: 'clean' } | { kind: 'secret'; key: string } | { kind: 'unchecked' };

const CLEAN: TSecretCheck = { kind: 'clean' };
const UNCHECKED: TSecretCheck = { kind: 'unchecked' };
/** Nodes a check reads at most: past it the call is refused, not let through. */
const SECRET_CHECK_NODES = 5000;

/** Every key of the agent's arguments, at any depth (they are JSON the agent wrote, at most 8000 characters). */
export const secretInInput = (input: unknown): TSecretCheck => {
  const stack: unknown[] = [input];
  let seen = 0;
  while (stack.length > 0) {
    const value = stack.pop();
    if (!value || typeof value !== 'object') continue;
    if ((seen += 1) > SECRET_CHECK_NODES) return UNCHECKED;
    if (Array.isArray(value)) stack.push(...value);
    else
      for (const [key, child] of Object.entries(value)) {
        if (isSecretKey(key)) return { kind: 'secret', key };
        stack.push(child);
      }
  }
  return CLEAN;
};

// The parts of a JSON Schema that hold schemas: under the first, the keys are field names; under the rest, not.
const SCHEMA_NAMED = ['properties', 'patternProperties', 'dependentSchemas'];
const SCHEMA_MAPS = ['$defs', 'definitions'];
const SCHEMA_ONE = [
  'items',
  'additionalProperties',
  'unevaluatedProperties',
  'contains',
  'not',
  'if',
  'then',
  'else',
  'propertyNames',
];
const SCHEMA_LISTS = ['prefixItems', 'anyOf', 'oneOf', 'allOf', 'items'];

/**
 * The field names a site tool's input schema declares, walked by the schema's own structure. The schema is the page's
 * object: one that throws on reading, or is too large to read whole, counts as unchecked, and the call is refused.
 */
export const secretInSchema = (schema: unknown): TSecretCheck => {
  const stack: unknown[] = [schema];
  let seen = 0;
  try {
    while (stack.length > 0) {
      const node = stack.pop() as Record<string, unknown> | null | undefined;
      if (!node || typeof node !== 'object' || Array.isArray(node)) continue;
      if ((seen += 1) > SECRET_CHECK_NODES) return UNCHECKED;
      for (const part of SCHEMA_NAMED) {
        const map = node[part];
        if (!map || typeof map !== 'object') continue;
        for (const [key, child] of Object.entries(map)) {
          if (isSecretKey(key)) return { kind: 'secret', key };
          stack.push(child);
        }
      }
      for (const part of SCHEMA_MAPS) {
        const map = node[part];
        if (map && typeof map === 'object') stack.push(...Object.values(map));
      }
      for (const part of SCHEMA_ONE) stack.push(node[part]);
      for (const part of SCHEMA_LISTS) {
        const list = node[part];
        if (Array.isArray(list)) stack.push(...list);
      }
      const required = node.required;
      if (Array.isArray(required))
        for (const key of required)
          if (typeof key === 'string' && isSecretKey(key)) return { kind: 'secret', key };
    }
  } catch {
    return UNCHECKED;
  }
  return CLEAN;
};

const actToolCall = async (win: any, bound: any, act: IActMessage, before: string): Promise<IActResult> => {
  if (!act.tool) return actFail('call needs a tool name.');
  const name = act.tool;
  const tool: ISiteTool | undefined = bound.siteTools?.get(name);
  if (!tool) return actFail(`The page has no tool ${name}; list them with browser_site_tools.`);
  let input: unknown = {};
  if (act.input !== undefined) {
    try {
      input = bound.parse(act.input);
    } catch {
      return actFail('input must be a JSON object.');
    }
    if (!input || typeof input !== 'object' || Array.isArray(input))
      return actFail('input must be a JSON object.');
  }
  const checked = [secretInInput(input), secretInSchema(tool.inputSchema)];
  const secret = checked.find((c) => c.kind === 'secret');
  if (secret?.kind === 'secret')
    return actFail(
      `The tool ${name} takes a password, a one-time code or card details (${secret.key}): the person enters them. Ask them in the chat.`,
    );
  if (checked.some((c) => c.kind === 'unchecked'))
    return actFail(
      `The input of the tool ${name} is too large or too odd to check for passwords: it is not called.`,
    );
  const client = { requestUserInteraction: async (callback: () => unknown) => callback() };
  const limit = bound.toolWaitMs ?? TOOL_WAIT_MS;
  const late = Symbol('late');
  let timer: unknown;
  try {
    const answer = await Promise.race([
      Promise.resolve().then(() => tool.execute(input, client)),
      new Promise<symbol>((resolve) => {
        timer = bound.setTimeout.call(win, () => resolve(late), limit);
      }),
    ]);
    if (answer === late) return actFail(`The tool ${name} did not answer within ${limit / 1000} s.`);
    const { text, error } = actToolText(bound, answer);
    return {
      ok: !error,
      text: `${name} returned:\n${text.slice(0, 16_000)}${actUrlNote(win, before)}`,
    };
  } catch (error: any) {
    let message: string;
    try {
      message = String(error?.message ?? error);
    } catch {
      message = 'error';
    }
    return actFail(`The tool ${name} failed: ${message.slice(0, 500)}`);
  } finally {
    try {
      win.clearTimeout(timer);
    } catch {
      // The timer fires into nothing.
    }
  }
};

const actActive = (bound: any, doc: any): any => {
  try {
    return bound.activeElement.call(doc) || doc.body;
  } catch {
    return doc.body;
  }
};

const actKeyInit = (key: string): Record<string, unknown> => {
  const parts = key.length > 1 && key.includes('+') ? key.split('+') : [key];
  const main = parts[parts.length - 1] || '+';
  const has = (...names: string[]) => parts.slice(0, -1).some((part) => names.includes(part.toLowerCase()));
  const letter = /^[a-z]$/i.test(main)
    ? `Key${main.toUpperCase()}`
    : /^[0-9]$/.test(main)
      ? `Digit${main}`
      : main;
  return {
    key: main,
    code: main === ' ' ? 'Space' : letter,
    ctrlKey: has('control', 'ctrl'),
    shiftKey: has('shift'),
    altKey: has('alt'),
    metaKey: has('meta', 'cmd', 'command'),
  };
};

const actSubmitForm = (bound: any, el: any): boolean => {
  try {
    const form = actIsInput(bound, el) ? bound.inputForm.call(el) : bound.closest.call(el, 'form');
    if (!form) return false;
    bound.requestSubmit.call(form);
    return true;
  } catch {
    return false;
  }
};

const actFireKeys = (win: any, bound: any, el: any, key: string): boolean => {
  const init: Record<string, unknown> = {
    ...actKeyInit(key),
    bubbles: true,
    cancelable: true,
    composed: true,
  };
  const Ctor = bound.KeyboardEvent ?? bound.Event;
  const down = bound.dispatch.call(el, new Ctor('keydown', init));
  if (down && (String(init.key).length === 1 || init.key === 'Enter'))
    bound.dispatch.call(el, new Ctor('keypress', init));
  bound.dispatch.call(el, new Ctor('keyup', init));
  return down;
};

export const actFocusNext = (win: any, doc: any, bound: any, from: any): any => {
  const items = actSelectAll(bound, doc, ACT_FOCUSABLE_SELECTOR).filter((el) => {
    try {
      return !el.disabled && actAttr(bound, el, 'tabindex') !== '-1' && actVisible(win, el, bound);
    } catch {
      return false;
    }
  });
  if (!items.length) return null;
  const at = items.indexOf(from);
  const next = items[(at + 1) % items.length];
  actFocus(bound, next);
  return next;
};

const actType = async (
  win: any,
  doc: any,
  bound: any,
  el: any,
  text: string,
  submit: boolean,
): Promise<IActResult> => {
  if (actIsSecret(bound, el)) return actFail(ACT_SECRET_REFUSAL);
  const what = actDescribe(bound, doc, el);
  const input = actIsInput(bound, el);
  const area = actIsTextarea(bound, el);
  try {
    if (el.disabled === true) return actFail(`The ${what} is disabled.`);
    if ((input || area) && el.readOnly === true) return actFail(`The ${what} is read-only.`);
  } catch {
    // Judged by what the events do.
  }
  if (
    input &&
    !/^(text|search|email|url|tel|number|date|time|datetime-local|month|week|color|range)$/.test(
      actInputType(bound, el),
    )
  )
    return actFail(`The ${what} does not take text: use click for a checkbox, radio or button.`);
  actFocus(bound, el);
  if (input || area) {
    const set = input ? bound.setValue : bound.setTextarea;
    if (!set) return actFail('This page does not let the bridge set a field.');
    // The native setter, not `el.value =`: a framework that tracks the value on the element (React) then sees a change.
    set.call(el, text);
    const Input = bound.InputEvent ?? bound.Event;
    bound.dispatch.call(
      el,
      new Input('input', { bubbles: true, composed: true, inputType: 'insertText', data: text }),
    );
    bound.dispatch.call(el, new bound.Event('change', { bubbles: true }));
  } else if (actIsEditable(bound, el)) {
    let done = false;
    try {
      const range = bound.createRange.call(doc);
      range.selectNodeContents(el);
      const selection = bound.getSelection.call(win);
      selection.removeAllRanges();
      selection.addRange(range);
      done =
        Boolean(bound.execCommand) &&
        bound.execCommand.call(doc, text === '' ? 'delete' : 'insertText', false, text);
    } catch {
      done = false;
    }
    if (!done) {
      el.textContent = text;
      const Input = bound.InputEvent ?? bound.Event;
      bound.dispatch.call(
        el,
        new Input('input', { bubbles: true, composed: true, inputType: 'insertText', data: text }),
      );
    }
  } else {
    return actFail(`The ${what} is not a text field.`);
  }
  if (submit) {
    const prevented = !actFireKeys(win, bound, el, 'Enter');
    if (!prevented) actSubmitForm(bound, el);
  }
  return { ok: true, text: `typed into ${what}${submit ? ' and pressed Enter' : ''}` };
};

const actSelect = (doc: any, bound: any, el: any, value: string | undefined): IActResult => {
  const what = actDescribe(bound, doc, el);
  if (!actIsSelect(bound, el)) return actFail(`The ${what} is not a select: use click or type.`);
  if (value === undefined) return actFail("select needs the option's value or text.");
  const options = Array.from(el.options as ArrayLike<any>);
  const textOf = (option: any) => actTextOf(bound, option).trim();
  const found =
    options.find((option) => option.value === value) ??
    options.find((option) => textOf(option) === value.trim()) ??
    options.find((option) => textOf(option).toLowerCase() === value.trim().toLowerCase());
  if (!found) {
    const shown = options.slice(0, 10).map((option) => actQuote(textOf(option)));
    return actFail(
      `No option "${value}" in the ${what}; it has ${shown.join(', ')}${options.length > 10 ? ', …' : ''}.`,
    );
  }
  if (found.disabled) return actFail(`The option "${textOf(found)}" is disabled.`);
  actFocus(bound, el);
  const index = options.indexOf(found);
  if (bound.setSelectedIndex) bound.setSelectedIndex.call(el, index);
  else found.selected = true;
  bound.dispatch.call(el, new bound.Event('input', { bubbles: true, composed: true }));
  bound.dispatch.call(el, new bound.Event('change', { bubbles: true }));
  return { ok: true, text: `selected "${textOf(found)}" in ${what}` };
};

export const actWait = async (win: any, doc: any, bound: any, act: IActMessage): Promise<IActResult> => {
  if (act.text === undefined && act.ms === undefined) return actFail('wait needs text or ms.');
  if (act.text === undefined) {
    await actPause(win, bound, act.ms as number);
    return { ok: true, text: `waited ${act.ms} ms` };
  }
  const wanted = act.text.replace(/\s+/g, ' ').trim();
  const limit = Math.min(act.ms ?? 5000, 10_000);
  const read = () => actPageText(bound, doc).replace(/\s+/g, ' ');
  let waited = 0;
  for (;;) {
    if (read().includes(wanted))
      return { ok: true, text: `the text ${actQuote(actClip(wanted, 80))} is on the page` };
    if (waited >= limit)
      return actFail(`The text ${actQuote(actClip(wanted, 80))} did not show up within ${limit} ms.`);
    const step = Math.min(100, limit - waited);
    await actPause(win, bound, step);
    waited += step;
  }
};

/**
 * One action of the agent. Answers with what it came to and never throws: a page that breaks the action gets a plain
 * failure. `refs` is the document's `makeActRefs()`. A step that may move the page is given a moment before its address
 * is read; a full navigation takes this document with it, and the next one's `tk:hello` tells the parent.
 */
export const runAct = async (
  win: any,
  doc: any,
  bound: any,
  core: any,
  refs: IActRefs,
  act: IActMessage,
): Promise<IActResult> => {
  try {
    if (!bound.ready) return actFail('The page bridge cannot act on this page.');
    const before = String(win.location.href);
    if (act.op === 'snapshot') {
      return { ok: true, text: core.actSnapshot(win, doc, bound, refs) + actToolsHint(bound) };
    }
    if (act.op === 'tools') return actToolsList(bound);
    if (act.op === 'call') return await actToolCall(win, bound, act, before);
    if (act.op === 'wait') return await core.actWait(win, doc, bound, act);
    if (act.op === 'press') {
      if (!act.key) return actFail('press needs a key.');
      const target = actActive(bound, doc);
      const prevented = !actFireKeys(win, bound, target, act.key);
      const main = actKeyInit(act.key).key as string;
      const tag = actTag(bound, target);
      const chord = actKeyInit(act.key);
      if (!prevented && !chord.ctrlKey && !chord.metaKey) {
        if (main === 'Enter') {
          if (tag === 'input') actSubmitForm(bound, target);
          else if (
            tag === 'button' ||
            tag === 'summary' ||
            (tag === 'a' && actAttr(bound, target, 'href') !== null)
          ) {
            const rect = actBox(win, bound, target);
            actFire(win, bound, target, 'click', { ...actCenter(rect as any), button: 0 });
          }
        } else if (main === 'Tab') core.actFocusNext(win, doc, bound, target);
      }
      await actPause(win, bound, 60);
      const rect = target && target !== doc.body ? actBox(win, bound, target) : undefined;
      return { ok: true, text: `pressed ${act.key}${actUrlNote(win, before)}`, ...(rect ? { rect } : {}) };
    }
    if (act.op === 'scroll' && !act.ref) {
      if (act.dy === undefined) return actFail('scroll needs a ref or dy.');
      bound.scrollBy.call(win, { left: 0, top: act.dy, behavior: 'instant' });
      return { ok: true, text: actScrolled(win, doc) };
    }
    const found = actResolve(refs, bound, act.ref);
    if ('fail' in found) return found.fail;
    const el = found.el;
    const what = actDescribe(bound, doc, el);
    if (act.op !== 'scroll' && !actVisible(win, el, bound))
      return actFail(`The ${what} [${act.ref}] is not shown on the page now.`);
    actBringIntoView(el, bound);
    const rect = actBox(win, bound, el);
    const center = actCenter(rect as any);
    if (act.op === 'scroll') return { ok: true, text: actScrolled(win, doc), rect };
    if (act.op === 'locate') return { ok: true, text: `found ${what}`, rect };
    if (act.op === 'hover') {
      for (const type of [
        'pointerover',
        'pointerenter',
        'mouseover',
        'mouseenter',
        'pointermove',
        'mousemove',
      ])
        actFire(win, bound, el, type, center);
      return { ok: true, text: `hovered ${what}`, rect };
    }
    if (act.op === 'click') {
      try {
        if (el.disabled === true) return actFail(`The ${what} is disabled.`);
      } catch {
        // Judged by what the click does.
      }
      for (const type of ['pointerover', 'pointerenter', 'mouseover', 'pointerdown', 'mousedown']) {
        actFire(win, bound, el, type, { ...center, button: 0, buttons: type.endsWith('down') ? 1 : 0 });
      }
      actFocus(bound, el);
      for (const type of ['pointerup', 'mouseup', 'click'])
        actFire(win, bound, el, type, { ...center, button: 0 });
      await actPause(win, bound, 80);
      return { ok: true, text: `clicked ${what}${actUrlNote(win, before)}`, rect };
    }
    if (act.op === 'select') {
      const result = actSelect(doc, bound, el, act.value);
      return result.ok ? { ...result, rect } : result;
    }
    if (act.op === 'type') {
      const result = await actType(win, doc, bound, el, act.text ?? '', act.submit === true);
      if (!result.ok) return result;
      if (act.submit) await actPause(win, bound, 80);
      return { ...result, text: `${result.text}${actUrlNote(win, before)}`, rect };
    }
    return actFail('Unknown action.');
  } catch (error) {
    return actFail(
      `The page got in the way of the action: ${String((error as any)?.message ?? error).slice(0, 200)}`,
    );
  }
};

const actScrolled = (win: any, doc: any): string => {
  const root = doc.scrollingElement || doc.documentElement;
  return `scrolled to y=${Math.round(Number(win.scrollY) || 0)} of ${Math.round(Number(root?.scrollHeight) || 0)}`;
};

// ---------------------------------------------------------------------------------------------------------------------
// DevTools «Elements»: the page's DOM read for the pane, as a design board's own script does (internal/design/texts/
// client.js). Silent until the parent says `tk:dom-on`. What goes back is tags, ids, classes, a few words of a node's own
// text and numbers: never attribute values, scripts or styles' text.
// ---------------------------------------------------------------------------------------------------------------------

const DOM_SKIP_TAGS = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE']);
const DOM_STYLES = [
  'display',
  'position',
  'width',
  'height',
  'margin',
  'padding',
  'color',
  'background-color',
  'font-family',
  'font-size',
  'font-weight',
  'line-height',
  'border-radius',
  'opacity',
  'z-index',
  'overflow',
];
const DOM_OWN_SELECTOR = '[data-tk-library-bar],[data-tk-library-mark],[data-tk-bridge-box]';

export interface IDomState {
  on: boolean;
  refs: IActRefs | null;
  markPath: number[] | null;
  mark: any;
  frame: number;
  inspecting: boolean;
  stopInspect: (() => void) | null;
  observer: any;
  stopWatch: (() => void) | null;
  changeTimer: number;
}

export const makeDomState = (refs: IActRefs | null = null): IDomState => ({
  on: false,
  refs,
  markPath: null,
  mark: null,
  frame: 0,
  inspecting: false,
  stopInspect: null,
  observer: null,
  stopWatch: null,
  changeTimer: 0,
});

const domRect = (bound: any, el: any) => (bound.box ? bound.box.call(el) : el.getBoundingClientRect());

/** The bridge's own elements (the mark, the picking frame) and the design runtime's: not the page's, never in a path. */
export const domOurs = (bound: any, state: IDomState, node: any): boolean => {
  try {
    if (!node || node.nodeType !== 1) return false;
    if (node === state.mark) return true;
    const closest = bound.closest ?? node.closest;
    return Boolean(closest && closest.call(node, DOM_OWN_SELECTOR)) || node.hasAttribute('data-artifact');
  } catch {
    return false;
  }
};

export const domKids = (bound: any, state: IDomState, el: any): any[] =>
  Array.prototype.filter.call(el.children, (child: any) => !domOurs(bound, state, child));

export const domPathOf = (doc: any, bound: any, state: IDomState, el: any): number[] | null => {
  const path: number[] = [];
  let node = el;
  while (node && node !== doc.documentElement) {
    const parent = node.parentElement;
    if (!parent) return null;
    const index = domKids(bound, state, parent).indexOf(node);
    if (index < 0) return null;
    path.unshift(index);
    node = parent;
  }
  return node && path.length <= DOM_PATH_MAX ? path : null;
};

export const domAt = (doc: any, bound: any, state: IDomState, path: readonly number[]): any => {
  let el = doc.documentElement;
  for (const index of path) {
    el = el ? domKids(bound, state, el)[index] : null;
    if (!el) return null;
  }
  return el;
};

export const domWords = (el: any): string => {
  if (DOM_SKIP_TAGS.has(String(el.tagName).toUpperCase())) return '';
  const own = Array.prototype.filter
    .call(el.childNodes, (child: any) => child.nodeType === 3)
    .map((child: any) => child.nodeValue)
    .join(' ')
    .replace(/\s+/g, ' ')
    .trim();
  return own.slice(0, 60);
};

const domClasses = (el: any, max: number): string =>
  typeof el.className === 'string' ? el.className.trim().replace(/\s+/g, ' ').slice(0, max) : '';

export const domNode = (
  bound: any,
  state: IDomState,
  el: any,
  depth: number,
  budget: { left: number },
): any => {
  const kids = domKids(bound, state, el);
  const node: Record<string, unknown> = { tag: String(el.localName).slice(0, 32), n: kids.length };
  if (el.id) node.id = String(el.id).slice(0, 64);
  const cls = domClasses(el, 120);
  if (cls) node.cls = cls;
  const words = domWords(el);
  if (words) node.text = words;
  budget.left -= 1;
  if (depth > 0 && kids.length) {
    const list: any[] = [];
    for (let i = 0; i < kids.length && i < DOM_KIDS_MAX && budget.left > 0; i += 1)
      list.push(domNode(bound, state, kids[i], depth - 1, budget));
    node.kids = list;
    if (list.length < kids.length) node.more = kids.length - list.length;
  }
  return node;
};

const domBoxValue = (style: any, name: string): string => {
  const sides = ['top', 'right', 'bottom', 'left'].map((side) => style.getPropertyValue(`${name}-${side}`));
  return sides[0] === sides[1] && sides[1] === sides[2] && sides[2] === sides[3] ? sides[0] : sides.join(' ');
};

export const domStyles = (win: any, bound: any, el: any): Record<string, string> => {
  const style = bound.style.call(win, el);
  const out: Record<string, string> = {};
  for (const name of DOM_STYLES) {
    const value =
      name === 'margin' || name === 'padding' ? domBoxValue(style, name) : style.getPropertyValue(name);
    out[name] = String(value).slice(0, 120);
  }
  return out;
};

export const domPlaceMark = (win: any, doc: any, bound: any, state: IDomState): void => {
  const el = state.markPath && state.mark ? domAt(doc, bound, state, state.markPath) : null;
  const mark = state.mark;
  if (!el || !mark) {
    mark?.style.setProperty('display', 'none');
    return;
  }
  const rect = domRect(bound, el);
  const set = (name: string, value: string) => mark.style.setProperty(name, value);
  set('display', 'block');
  set('left', `${rect.left}px`);
  set('top', `${rect.top}px`);
  set('width', `${rect.width}px`);
  set('height', `${rect.height}px`);
  const label = mark.firstChild;
  const cls = domClasses(el, 120);
  const classes = cls ? `.${cls.split(' ').slice(0, 3).join('.')}` : '';
  label.firstChild.textContent = `${String(el.localName)}${el.id ? `#${el.id}` : ''}${classes}`;
  label.lastChild.textContent = `${Math.round(rect.width)} × ${Math.round(rect.height)}`;
  // The label stands above the box; a box at the top edge takes it inside.
  const top = rect.top < 28;
  label.style.setProperty('top', top ? '6px' : 'auto');
  label.style.setProperty('bottom', top ? 'auto' : 'calc(100% + 6px)');
  label.style.setProperty('left', top ? '6px' : '0');
};

const domEnsureMark = (doc: any, state: IDomState): void => {
  if (state.mark) return;
  const mark = doc.createElement('div');
  mark.setAttribute('data-tk-library-mark', '');
  const label = doc.createElement('span');
  label.appendChild(doc.createElement('span'));
  label.appendChild(doc.createElement('span'));
  mark.appendChild(label);
  const boxStyle: Record<string, string> = {
    position: 'fixed',
    display: 'none',
    'z-index': '2147483645',
    'pointer-events': 'none',
    'box-sizing': 'border-box',
    background: 'rgba(111, 168, 220, .36)',
    outline: '1px solid rgba(111, 168, 220, .95)',
  };
  for (const name of Object.keys(boxStyle)) mark.style.setProperty(name, boxStyle[name]);
  const tagStyle: Record<string, string> = {
    position: 'absolute',
    display: 'flex',
    gap: '8px',
    padding: '4px 8px',
    'border-radius': '4px',
    background: '#1f2430',
    color: '#7dd3fc',
    font: '12px/16px ui-monospace, SFMono-Regular, Menlo, monospace',
    'white-space': 'nowrap',
  };
  for (const name of Object.keys(tagStyle)) label.style.setProperty(name, tagStyle[name]);
  label.lastChild.style.setProperty('color', 'rgba(255, 255, 255, .7)');
  state.mark = mark;
  // Watched before it is placed in the document, so that placing it is already not the page's change.
  if (state.refs) state.refs.skip = mark;
  doc.documentElement.appendChild(mark);
};

export const domMarkNode = (
  win: any,
  doc: any,
  bound: any,
  state: IDomState,
  path: number[] | null,
  reveal: boolean,
): void => {
  state.markPath = path;
  if (path) {
    domEnsureMark(doc, state);
    const el = domAt(doc, bound, state, path);
    if (el && reveal) {
      try {
        (bound.scrollIntoView ?? el.scrollIntoView).call(el, { block: 'nearest', inline: 'nearest' });
      } catch {
        // A page that cannot scroll there is left as it is.
      }
    }
  }
  domPlaceMark(win, doc, bound, state);
};

const domLater = (win: any, bound: any, fn: () => void): number => {
  const raf = bound.requestAnimationFrame;
  return raf ? raf.call(win, fn) : bound.setTimeout.call(win, fn, 16);
};

export const domWatch = (
  win: any,
  doc: any,
  bound: any,
  state: IDomState,
  tell: (m: Record<string, unknown>) => void,
  on: boolean,
): void => {
  if (!on) {
    state.stopWatch?.();
    state.stopWatch = null;
    state.observer?.disconnect();
    state.observer = null;
    return;
  }
  if (state.observer || !bound.MutationObserver) return;
  const changed = () => {
    win.clearTimeout(state.changeTimer);
    state.changeTimer = bound.setTimeout.call(win, () => tell({ type: 'tk:dom-changed' }), 400);
  };
  win.addEventListener('resize', changed);
  state.stopWatch = () => {
    win.removeEventListener('resize', changed);
    win.clearTimeout(state.changeTimer);
  };
  state.observer = new bound.MutationObserver((records: any[]) => {
    try {
      const real = records.some((record) => {
        if (domOurs(bound, state, record.target)) return false;
        if (record.type !== 'childList') return true;
        const moved = [
          ...Array.from(record.addedNodes as ArrayLike<any>),
          ...Array.from(record.removedNodes as ArrayLike<any>),
        ];
        return !moved.every((node) => domOurs(bound, state, node));
      });
      if (real) changed();
    } catch {
      // A page that breaks the observer's records is not reported on.
    }
  });
  state.observer.observe(doc.documentElement, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ['class', 'id'],
  });
};

export const domInspect = (
  win: any,
  doc: any,
  bound: any,
  state: IDomState,
  tell: (m: Record<string, unknown>) => void,
  on: boolean,
): void => {
  if (state.inspecting === on) return;
  state.inspecting = on;
  if (!on) {
    state.stopInspect?.();
    state.stopInspect = null;
    domMarkNode(win, doc, bound, state, null, false);
    return;
  }
  const target = (event: any) => {
    const el = event.target;
    return el && el.nodeType === 1 && !domOurs(bound, state, el) ? el : null;
  };
  const over = (event: any) => {
    const el = target(event);
    if (el) domMarkNode(win, doc, bound, state, domPathOf(doc, bound, state, el), false);
  };
  const swallow = (event: any) => {
    const el = target(event);
    if (!el) return;
    event.preventDefault();
    event.stopPropagation();
    event.stopImmediatePropagation?.();
    if (event.type !== 'click') return;
    const path = domPathOf(doc, bound, state, el);
    domInspect(win, doc, bound, state, tell, false);
    if (path) tell({ type: 'tk:dom-picked', path });
    else tell({ type: 'tk:dom-inspect', on: false });
  };
  const escape = (event: any) => {
    if (event.key !== 'Escape') return;
    domInspect(win, doc, bound, state, tell, false);
    tell({ type: 'tk:dom-inspect', on: false });
  };
  const swallowed = ['click', 'mousedown', 'mouseup', 'pointerdown', 'pointerup', 'touchstart', 'touchend'];
  win.addEventListener('mousemove', over, true);
  for (const name of swallowed) win.addEventListener(name, swallow, true);
  win.addEventListener('keydown', escape, true);
  state.stopInspect = () => {
    win.removeEventListener('mousemove', over, true);
    for (const name of swallowed) win.removeEventListener(name, swallow, true);
    win.removeEventListener('keydown', escape, true);
  };
};

/** `tk:dom-on`: the tools are on for this document, and the parent is told (the board script's own `tk:lib-ready`). */
export const domEnable = (
  win: any,
  doc: any,
  bound: any,
  state: IDomState,
  tell: (m: Record<string, unknown>) => void,
): void => {
  if (!state.on) {
    state.on = true;
    const replace = () => {
      if (!state.markPath || state.frame) return;
      state.frame = domLater(win, bound, () => {
        state.frame = 0;
        try {
          domPlaceMark(win, doc, bound, state);
        } catch {
          // The mark is only a hint.
        }
      });
    };
    win.addEventListener('scroll', replace, true);
    win.addEventListener('resize', replace);
  }
  tell({ type: 'tk:lib-ready' });
};

/** Answers one parsed `tk:dom-*` message of the parent. Nothing is answered before `tk:dom-on`; nothing throws out. */
export const handleDomMessage = (
  win: any,
  doc: any,
  bound: any,
  core: any,
  state: IDomState,
  message: TDomMessageIn,
  post: (m: Record<string, unknown>) => void,
): void => {
  const tell = (m: Record<string, unknown>) => {
    try {
      post(m);
    } catch {
      // The parent is gone.
    }
  };
  try {
    if (message.type === 'tk:dom-on') return core.domEnable(win, doc, bound, state, tell);
    if (!state.on) return;
    if (message.type === 'tk:dom-tree') {
      const el = core.domAt(doc, bound, state, message.path);
      if (!el) return tell({ type: 'tk:dom-tree', req: message.req, path: message.path, gone: true });
      const node = core.domNode(bound, state, el, message.depth, { left: DOM_NODES_MAX });
      return tell({ type: 'tk:dom-tree', req: message.req, path: message.path, node });
    }
    if (message.type === 'tk:dom-node') {
      const el = core.domAt(doc, bound, state, message.path);
      if (!el) return tell({ type: 'tk:dom-node', req: message.req, path: message.path, gone: true });
      const rect = domRect(bound, el);
      return tell({
        type: 'tk:dom-node',
        req: message.req,
        path: message.path,
        tag: String(el.localName).slice(0, 32),
        id: el.id ? String(el.id).slice(0, 64) : '',
        cls: domClasses(el, 120),
        text: core.domWords(el),
        rect: {
          x: Math.round(rect.left + win.scrollX),
          y: Math.round(rect.top + win.scrollY),
          w: Math.round(rect.width),
          h: Math.round(rect.height),
        },
        viewport: { w: win.innerWidth, h: win.innerHeight },
        styles: core.domStyles(win, bound, el),
      });
    }
    if (message.type === 'tk:dom-mark')
      return core.domMarkNode(win, doc, bound, state, message.path, message.reveal);
    if (message.type === 'tk:dom-inspect') return core.domInspect(win, doc, bound, state, tell, message.on);
    return core.domWatch(win, doc, bound, state, tell, message.on);
  } catch {
    // The page got in the way: the pane's request times out and says the page is silent.
  }
};
