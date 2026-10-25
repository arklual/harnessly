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
   * The blocks the parent pins its notes to: the bridge keeps telling where each one shows (`tk:anchored`) while the page
   * scrolls, moves or covers it. An empty set stops it.
   */
  | { type: 'tk:anchors'; items: IAnchorItem[] };

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
  if (message.type === 'tk:styles') {
    const items = parseStyleItems(message.items);
    return items ? { type: 'tk:styles', items } : null;
  }
  if (message.type === 'tk:anchors') {
    const items = parseAnchorItems(message.items);
    return items ? { type: 'tk:anchors', items } : null;
  }
  return null;
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
    for (const y of ys) for (const x of xs) hits.push(doc.elementFromPoint(x, y));
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

export const pickedMessage = (el: any, win: any, doc: any, core: any) => {
  const box = el.getBoundingClientRect();
  const found = core.findComponent(el, win);
  return {
    type: 'tk:picked',
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
  const workers = win.navigator?.serviceWorker ?? null;
  const controller = get(win.ServiceWorkerContainer?.prototype, 'controller');
  return {
    ...natives,
    controlled: () => Boolean(workers && controller && controller.call(workers)),
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

/**
 * Messaging with the parent page: only to `win.parent` with its origin as the target, only from it with that
 * origin. Nothing from a message is executed: the bridge reads the DOM, scrolls and draws one highlight box.
 */
export const installBridge = (win: any, doc: any, parentOrigin: string, core: any): void => {
  // Taken before any script of the page runs (the bridge is the first one in <head>): the page cannot swap them later.
  const parent = win.parent;
  const nonce = Array.from(win.crypto.getRandomValues(new win.Uint8Array(16)) as ArrayLike<number>, (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');
  const bound = { ...core.captureNatives(win), origin: win.location.origin, parent };
  const post = (message: Record<string, unknown>) => parent.postMessage(message, parentOrigin);
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

  const swallow = (event: any) => {
    if (!picking) return;
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
      if (!picking) return;
      const target = deepTarget(event);
      if (isPage(target)) highlight(target);
    },
    true,
  );
  win.addEventListener(
    'click',
    (event: any) => {
      if (!picking) return;
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
