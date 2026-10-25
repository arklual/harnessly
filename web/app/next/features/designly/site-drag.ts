// The site's drag layer (the design's siteDragStart): the field, the pill, the chat's head and corner, the border of the
// chat and the viewport's handles. A gesture writes custom properties on the elements once a frame and commits to the
// state only when the pointer is released; a cancelled one puts everything back.
import type { PointerEvent as ReactPointerEvent } from 'react';
import type { ISize } from '@/next/features/designly/devices';
import { PHONE_QUERY } from '@/next/ui/use-media';
import { t } from '@/next/i18n';

/** The window of «Окно» in the section's pixels. */
export interface IFloat {
  x: number;
  y: number;
  w: number;
  h: number;
}

export type TDock = 'left' | 'right';
/** Where a dragged chat can stand: at an edge, in a window, as the field, or folded into the pill. */
export type TZone = TDock | 'float' | 'island' | 'min';
type TKind = 'island' | 'dock' | 'float' | 'fres' | 'split' | 'mini' | 'vp' | 'dev' | 'tweaks';

export const FLOAT_MIN = { w: 300, h: 280 } as const;
const FLOAT_MAX_W = 680;
const FLOAT_NEW_W = 360;
export const CHAT_W = { min: 160, max: 620, close: 240 } as const;
/** The height of DevTools: pulled lower than `close` they shut. */
export const DEV_H = { min: 40, max: 520, close: 100, start: 220 } as const;
/** The fold of the field: pulled down this far, or flung this fast (px/ms). */
const FOLD = { dy: 56, speed: 0.5 } as const;
const EXPAND = { dy: -24, speed: -0.4 } as const;
const MARGIN = 8;

export interface IPoint {
  x: number;
  y: number;
}

interface IRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface IGesture {
  kind: 'island' | 'dock' | 'float';
  dock: TDock;
  rect: IRect;
  /** Where the press was. */
  from: IPoint;
}

const clamp = (low: number, high: number, value: number) => Math.max(low, Math.min(high, value));

/** The zone under the pointer: the edges dock, a pull up from the field opens a window, a pull down folds it. */
export const zoneAt = (g: IGesture, at: IPoint): TZone | null => {
  const px = at.x - g.rect.left;
  const py = at.y - g.rect.top;
  const dx = at.x - g.from.x;
  const dy = at.y - g.from.y;
  if (Math.abs(dx) + Math.abs(dy) < 32) return null;
  const edge = g.kind === 'float' ? 56 : Math.min(220, g.rect.width * 0.16);
  if (px < edge) return g.kind === 'dock' && g.dock === 'left' ? null : 'left';
  if (px > g.rect.width - edge) return g.kind === 'dock' && g.dock === 'right' ? null : 'right';
  if (g.kind === 'island') {
    if (dy > 40 && Math.abs(dx) < 160) return 'min';
    return dy < -80 ? 'float' : null;
  }
  if (py > g.rect.height - 72) return 'island';
  return g.kind === 'dock' ? 'float' : null;
};

/** The window a chat dropped by the pointer opens as: its head under the pointer, inside the section. */
export const floatDropAt = (rect: IRect, at: IPoint): IFloat => {
  const w = FLOAT_NEW_W;
  const h = Math.max(320, Math.min(560, rect.height - 140));
  return {
    x: Math.round(Math.max(12, Math.min(rect.width - w - 12, at.x - rect.left - w / 2))),
    y: Math.round(Math.max(12, Math.min(rect.height - h - 12, at.y - rect.top - 22))),
    w,
    h,
  };
};

/** The window moved by the pointer: it stays in the section, its head always within reach. */
export const floatMoved = (f: IFloat, section: IRect, dx: number, dy: number): IFloat => ({
  ...f,
  x: Math.round(clamp(0, section.width - f.w, f.x + dx)),
  y: Math.round(clamp(0, section.height - 80, f.y + dy)),
});

/** The window grown by its corner: not under the minimum, not wider than the cap, not out of the section. */
export const floatGrown = (f: IFloat, section: IRect, dx: number, dy: number): IFloat => ({
  ...f,
  w: Math.round(Math.max(FLOAT_MIN.w, Math.min(Math.min(FLOAT_MAX_W, section.width - f.x), f.w + dx))),
  h: Math.round(Math.max(FLOAT_MIN.h, Math.min(section.height - f.y - 12, f.h + dy))),
});

/** The chat's width after the border moved by `dx`: a chat on the right grows to the left. */
export const chatWidthAt = (w: number, dock: TDock, dx: number) =>
  clamp(CHAT_W.min, CHAT_W.max, w + (dock === 'right' ? -dx : dx));

/** DevTools' height after its top edge moved by `dy`: pulled up it grows. */
export const devHeightAt = (h: number, dy: number) => clamp(DEV_H.min, DEV_H.max, h - dy);

/** The panel of settings, as the browser's box and the panel's own box stand when it is taken. */
export interface ITweaksBox {
  /** The panel's corner in the browser's pixels, and its size. */
  x: number;
  y: number;
  w: number;
  h: number;
  /** The browser's size. */
  bw: number;
  bh: number;
}

/** The panel follows the pointer, but 8 px inside the browser stay free of it. */
export const tweaksMovedBy = (box: ITweaksBox, dx: number, dy: number): IPoint => ({
  x: Math.round(clamp(MARGIN, Math.max(MARGIN, box.bw - box.w - MARGIN), box.x + dx)),
  y: Math.round(clamp(MARGIN, Math.max(MARGIN, box.bh - box.h - MARGIN), box.y + dy)),
});

/** The field follows the pointer, but 8 px inside the section stay free of it. */
export const boxShift = (box: IRect, section: IRect, dx: number, dy: number): IPoint => ({
  x: Math.max(MARGIN - box.left, Math.min(section.width - MARGIN - box.left - box.width, dx)),
  y: Math.max(MARGIN - box.top, Math.min(section.height - MARGIN - box.top - box.height, dy)),
});

/** The pill resists a pull up and gives way to a pull down less and less. */
export const pillShift = (dy: number) => (dy < 0 ? dy / 2 : dy / (1.5 + dy / 20));

/** A viewport of «Резина» dragged by its handle: the box is centred, so a side moves by half of what the handle did. */
export const viewportDragged = (
  edge: 'e' | 's' | 'se',
  start: ISize,
  fit: number,
  dx: number,
  dy: number,
): ISize => {
  const side = (v: number, d: number) => Math.round(clamp(240, 3840, v + (2 * d) / fit));
  return {
    w: edge === 's' ? start.w : side(start.w, dx),
    h: edge === 'e' ? start.h : side(start.h, dy),
  };
};

const sectionOf = (el: Element) => el.closest<HTMLElement>('.hx-site');
const onPhone = () => window.matchMedia(PHONE_QUERY).matches;

interface IPull {
  dx: number;
  dy: number;
  at: IPoint;
  /** Went further than a tap. */
  moved: boolean;
  /** The vertical speed of the whole gesture, px/ms. */
  speed: number;
}

/** One gesture of the pointer: `step` once a frame, `end` once on release (`cancelled` when the system took the pointer). */
const track = (
  event: ReactPointerEvent<HTMLElement>,
  section: HTMLElement,
  kind: TKind,
  step: (pull: IPull) => void,
  end: (pull: IPull, cancelled: boolean) => void,
) => {
  const target = event.currentTarget;
  const from = { x: event.clientX, y: event.clientY };
  const t0 = performance.now();
  let moved = false;
  let frame = 0;
  let last = from;
  const pullAt = (at: IPoint): IPull => ({
    dx: at.x - from.x,
    dy: at.y - from.y,
    at,
    moved,
    speed: (at.y - from.y) / Math.max(1, performance.now() - t0),
  });
  const onMove = (e: PointerEvent) => {
    last = { x: e.clientX, y: e.clientY };
    if (Math.abs(last.x - from.x) + Math.abs(last.y - from.y) > 4) moved = true;
    if (frame === 0)
      frame = requestAnimationFrame(() => {
        frame = 0;
        step(pullAt(last));
      });
  };
  const onEnd = (e: PointerEvent) => {
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onEnd);
    window.removeEventListener('pointercancel', onEnd);
    cancelAnimationFrame(frame);
    const cancelled = e.type === 'pointercancel';
    const pull = pullAt(cancelled ? last : { x: e.clientX, y: e.clientY });
    if (!cancelled) step(pull);
    delete section.dataset.resizing;
    end(pull, cancelled);
  };
  section.dataset.resizing = kind;
  // The pointer stays with the handle over the page's frame too.
  target.setPointerCapture(event.pointerId);
  window.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onEnd);
  window.addEventListener('pointercancel', onEnd);
};

const showZone = (section: HTMLElement, zone: TZone | null, drop: IFloat | null) => {
  const ghost = section.querySelector<HTMLElement>('.hx-site__ghost');
  if (!ghost) return;
  if (drop) {
    ghost.style.setProperty('--gx', `${drop.x}px`);
    ghost.style.setProperty('--gy', `${drop.y}px`);
    ghost.style.setProperty('--gh', `${drop.h}px`);
  }
  if (!zone) {
    delete ghost.dataset.zone;
    return;
  }
  ghost.dataset.zone = zone;
  const label = ghost.querySelector('.hx-site__ghost-label');
  if (label) label.textContent = t(`next.site.zone.${zone}`);
};

interface IAim {
  zone: TZone | null;
  drop: IFloat | null;
}

const aim = (section: HTMLElement, g: IGesture, at: IPoint): IAim => {
  const zone = zoneAt(g, at);
  const drop = zone === 'float' ? floatDropAt(g.rect, at) : null;
  showZone(section, zone, drop);
  return { zone, drop };
};

export type TDropZone = (zone: TZone, float: IFloat | null) => void;

/** The chat took its place: the field there is ready for words. */
const land = (section: HTMLElement, aimed: IAim, onZone: TDropZone) => {
  if (!aimed.zone) return;
  onZone(aimed.zone, aimed.drop);
  if (aimed.zone === 'float' || aimed.zone === 'island')
    requestAnimationFrame(() =>
      section.querySelector<HTMLTextAreaElement>('.hx-island__input')?.focus({ preventScroll: true }),
    );
};

const setFloatVars = (section: HTMLElement, f: IFloat) => {
  section.style.setProperty('--fx', `${f.x}px`);
  section.style.setProperty('--fy', `${f.y}px`);
  section.style.setProperty('--fw', `${f.w}px`);
  section.style.setProperty('--fh', `${f.h}px`);
};

/** The field by its grab: it follows the pointer, the ghost shows where it would stand, a pull down folds it. */
export const dragIsland = (event: ReactPointerEvent<HTMLElement>, onZone: TDropZone) => {
  const section = sectionOf(event.currentTarget);
  const box = section?.querySelector<HTMLElement>('.hx-island__box');
  if (!section || !box || event.button !== 0) return;
  event.preventDefault();
  const rect = section.getBoundingClientRect();
  box.style.translate = '';
  const b = box.getBoundingClientRect();
  const at = { left: b.left - rect.left, top: b.top - rect.top, width: b.width, height: b.height };
  const g: IGesture = { kind: 'island', dock: 'left', rect, from: { x: event.clientX, y: event.clientY } };
  let aimed: IAim = { zone: null, drop: null };
  track(
    event,
    section,
    'island',
    (pull) => {
      const shift = boxShift(at, rect, pull.dx, pull.dy);
      box.style.translate = `${shift.x.toFixed(1)}px ${shift.y.toFixed(1)}px`;
      aimed = aim(section, g, pull.at);
    },
    (pull, cancelled) => {
      box.style.translate = '';
      showZone(section, null, null);
      if (cancelled) return;
      if (aimed.zone) land(section, aimed, onZone);
      else if (pull.dy > FOLD.dy || pull.speed > FOLD.speed) onZone('min', null);
    },
  );
};

/** The pill: a pull up (or a flick, or a tap) opens the field. */
export const dragPill = (event: ReactPointerEvent<HTMLElement>, onExpand: () => void) => {
  const section = sectionOf(event.currentTarget);
  const pill = event.currentTarget;
  if (!section || event.button !== 0) return;
  track(
    event,
    section,
    'mini',
    (pull) => {
      pill.style.translate = `0 ${pillShift(pull.dy).toFixed(1)}px`;
    },
    (pull, cancelled) => {
      pill.style.translate = '';
      if (cancelled) return;
      if (!pull.moved || pull.dy < EXPAND.dy || pull.speed < EXPAND.speed) onExpand();
    },
  );
};

interface IChatHead {
  dock: TDock;
  float: IFloat | null;
  mode: 'chat' | 'float';
  onFloat: (float: IFloat) => void;
  onZone: TDropZone;
}

/** The head of the chat: a docked chat shows where it could go, a window follows the pointer and docks at an edge. */
export const dragChatHead = (event: ReactPointerEvent<HTMLElement>, head: IChatHead) => {
  const section = sectionOf(event.currentTarget);
  if (!section || event.button !== 0 || onPhone()) return;
  if ((event.target as Element).closest('button')) return;
  const f = head.float;
  if (head.mode === 'float' && !f) return;
  event.preventDefault();
  const rect = section.getBoundingClientRect();
  const g: IGesture = {
    kind: head.mode === 'chat' ? 'dock' : 'float',
    dock: head.dock,
    rect,
    from: { x: event.clientX, y: event.clientY },
  };
  let aimed: IAim = { zone: null, drop: null };
  let cur: IFloat | null = null;
  track(
    event,
    section,
    g.kind,
    (pull) => {
      if (f && g.kind === 'float') {
        cur = floatMoved(f, rect, pull.dx, pull.dy);
        setFloatVars(section, cur);
      }
      aimed = aim(section, g, pull.at);
    },
    (_, cancelled) => {
      showZone(section, null, null);
      if (f && (cancelled || aimed.zone)) setFloatVars(section, f);
      if (cancelled) return;
      if (aimed.zone) land(section, aimed, head.onZone);
      else if (cur) head.onFloat(cur);
    },
  );
};

/** The corner of the window. */
export const dragFloatCorner = (
  event: ReactPointerEvent<HTMLElement>,
  f: IFloat,
  onFloat: (float: IFloat) => void,
) => {
  const section = sectionOf(event.currentTarget);
  if (!section || event.button !== 0 || onPhone()) return;
  event.preventDefault();
  const rect = section.getBoundingClientRect();
  let cur: IFloat | null = null;
  track(
    event,
    section,
    'fres',
    (pull) => {
      cur = floatGrown(f, rect, pull.dx, pull.dy);
      section.style.setProperty('--fw', `${cur.w}px`);
      section.style.setProperty('--fh', `${cur.h}px`);
    },
    (_, cancelled) => {
      if (cancelled || !cur) setFloatVars(section, f);
      else onFloat(cur);
    },
  );
};

/** The border of the chat: its width; let go close to the edge, the chat closes. */
export const dragSplit = (
  event: ReactPointerEvent<HTMLElement>,
  chat: { w: number; dock: TDock; onWidth: (w: number) => void; onClose: () => void },
) => {
  const section = sectionOf(event.currentTarget);
  if (!section || event.button !== 0) return;
  event.preventDefault();
  let cur: number | null = null;
  track(
    event,
    section,
    'split',
    (pull) => {
      cur = chatWidthAt(chat.w, chat.dock, pull.dx);
      section.style.setProperty('--site-chat', `${cur}px`);
    },
    (_, cancelled) => {
      if (cur === null) return;
      const closes = !cancelled && cur < CHAT_W.close;
      if (cancelled || closes) section.style.setProperty('--site-chat', `${chat.w}px`);
      if (closes) chat.onClose();
      else if (!cancelled) chat.onWidth(cur);
    },
  );
};

interface IViewportGrip {
  edge: 'e' | 's' | 'se';
  /** The viewport now, and the scale the box is drawn at. */
  size: ISize;
  fit: number;
  onSize: (size: ISize) => void;
  /** Puts the viewport's own custom properties and label back. */
  restore: () => void;
}

/** A handle of «Резина»'s box: the size of the viewport follows the pointer, the label says it. */
export const dragViewport = (
  event: ReactPointerEvent<HTMLElement>,
  area: HTMLElement,
  grip: IViewportGrip,
) => {
  const section = sectionOf(event.currentTarget);
  if (!section || event.button !== 0) return;
  event.preventDefault();
  const label = section.querySelector('.hx-browser__dev-size')?.firstChild ?? null;
  let cur: ISize | null = null;
  track(
    event,
    section,
    'vp',
    (pull) => {
      cur = viewportDragged(grip.edge, grip.size, grip.fit, pull.dx, pull.dy);
      area.dataset.dev = 'size';
      for (const name of ['--bw', '--vw']) area.style.setProperty(name, `${cur.w}px`);
      for (const name of ['--bh', '--vh']) area.style.setProperty(name, `${cur.h}px`);
      area.style.setProperty('--k', '1');
      if (label) label.nodeValue = `${cur.w} × ${cur.h}`;
    },
    (_, cancelled) => {
      if (cancelled || !cur) grip.restore();
      else grip.onSize(cur);
    },
  );
};

/** The top edge of DevTools: its height follows the pointer; let go too low, it closes. */
export const dragDevtools = (
  event: ReactPointerEvent<HTMLElement>,
  dev: { h: number; onHeight: (h: number) => void; onClose: () => void },
) => {
  const section = sectionOf(event.currentTarget);
  if (!section || event.button !== 0) return;
  event.preventDefault();
  let cur: number | null = null;
  track(
    event,
    section,
    'dev',
    (pull) => {
      cur = devHeightAt(dev.h, pull.dy);
      section.style.setProperty('--dev-h', `${cur}px`);
    },
    (_, cancelled) => {
      if (cur === null) return;
      const closes = !cancelled && cur < DEV_H.close;
      if (cancelled || closes) section.style.setProperty('--dev-h', `${dev.h}px`);
      if (closes) dev.onClose();
      else if (!cancelled) dev.onHeight(cur);
    },
  );
};

/** The head of the settings panel: the panel moves with the pointer inside the browser. */
export const dragTweaks = (
  event: ReactPointerEvent<HTMLElement>,
  /** Where the panel was put before, null while it stands in its own place. */
  was: IPoint | null,
  onMoved: (at: IPoint) => void,
) => {
  const section = sectionOf(event.currentTarget);
  const panel = event.currentTarget.closest<HTMLElement>('.hx-tweaks');
  const browser = event.currentTarget.closest<HTMLElement>('.hx-browser');
  if (!section || !panel || !browser || event.button !== 0) return;
  if ((event.target as Element).closest('button')) return;
  event.preventDefault();
  const b = browser.getBoundingClientRect();
  const p = panel.getBoundingClientRect();
  const box: ITweaksBox = {
    x: p.left - b.left,
    y: p.top - b.top,
    w: p.width,
    h: p.height,
    bw: b.width,
    bh: b.height,
  };
  let cur: IPoint | null = null;
  track(
    event,
    section,
    'tweaks',
    (pull) => {
      cur = tweaksMovedBy(box, pull.dx, pull.dy);
      panel.dataset.moved = 'true';
      panel.style.setProperty('--tx', `${cur.x}px`);
      panel.style.setProperty('--ty', `${cur.y}px`);
    },
    (_, cancelled) => {
      if (cancelled || !cur) {
        panel.dataset.moved = String(was !== null);
        if (was) {
          panel.style.setProperty('--tx', `${was.x}px`);
          panel.style.setProperty('--ty', `${was.y}px`);
        }
        return;
      }
      onMoved(cur);
    },
  );
};
