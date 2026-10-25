// What the board's own script (the library's client, in the page) tells the site about the page's DOM, read as data:
// a branch of the tree, a node's box and styles, the node a click picked. The page is foreign code: every field is
// checked and clipped here again, and the styles are only the names the panel shows. Where the messages may come from
// (the frame's window and origin) is the caller's check.
import type { IDomNode, TDomPath } from '@/next/features/designly/dom-tree';

/** The computed styles the inspector lists, in the order it lists them. */
export const DOM_STYLES = [
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
] as const;

export interface IDomDetail {
  path: TDomPath;
  tag: string;
  id: string;
  cls: string;
  text: string;
  /** The border box in the page's own pixels, from the top left of the document. */
  rect: { x: number; y: number; w: number; h: number };
  viewport: { w: number; h: number };
  styles: { name: string; value: string }[];
}

export type TDomMessage =
  /** `node` is null for a path that leads nowhere now. */
  | { k: 'tree'; req: number; path: TDomPath; node: IDomNode | null }
  | { k: 'node'; req: number; path: TDomPath; detail: IDomDetail | null }
  /** A click in the page picked a node while it was asked to inspect. */
  | { k: 'picked'; path: TDomPath }
  /** The page left the inspecting itself: Esc in it, or a pick. */
  | { k: 'inspect-off' }
  /** The page's markup or classes changed. */
  | { k: 'changed' }
  /** The page's script is there (a page that loaded anew says it again). */
  | { k: 'ready' };

const MAX_PATH = 64;
const MAX_DEPTH = 8;
const MAX_NODES = 500;
const MAX_KIDS = 120;

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const clip = (v: unknown, max: number) => (typeof v === 'string' ? v.slice(0, max) : '');
const count = (v: unknown) => (isNum(v) && v >= 0 ? Math.floor(v) : 0);

const readPath = (v: unknown): TDomPath | null => {
  if (!Array.isArray(v) || v.length > MAX_PATH) return null;
  return v.every((i) => Number.isInteger(i) && i >= 0 && i <= 100000) ? (v as number[]) : null;
};

const readNode = (raw: unknown, depth: number, budget: { left: number }): IDomNode | null => {
  if (!raw || typeof raw !== 'object' || depth > MAX_DEPTH || budget.left <= 0) return null;
  const r = raw as Record<string, unknown>;
  const tag = clip(r.tag, 32);
  if (tag === '') return null;
  budget.left -= 1;
  let kids: IDomNode[] | null = null;
  if (Array.isArray(r.kids)) {
    kids = [];
    for (const item of r.kids.slice(0, MAX_KIDS)) {
      const kid = readNode(item, depth + 1, budget);
      if (kid) kids.push(kid);
    }
  }
  return {
    tag,
    id: clip(r.id, 64),
    cls: clip(r.cls, 120),
    text: clip(r.text, 60),
    n: Math.max(count(r.n), kids?.length ?? 0),
    kids,
    more: count(r.more),
  };
};

const readDetail = (r: Record<string, unknown>, path: TDomPath): IDomDetail | null => {
  const rect = (r.rect ?? {}) as Record<string, unknown>;
  const viewport = (r.viewport ?? {}) as Record<string, unknown>;
  if (!isNum(rect.x) || !isNum(rect.y) || !isNum(rect.w) || !isNum(rect.h)) return null;
  if (!isNum(viewport.w) || !isNum(viewport.h)) return null;
  const given = (r.styles ?? {}) as Record<string, unknown>;
  const styles = DOM_STYLES.flatMap((name) => {
    const value = clip(given[name], 120);
    return value === '' ? [] : [{ name, value }];
  });
  return {
    path,
    tag: clip(r.tag, 32),
    id: clip(r.id, 64),
    cls: clip(r.cls, 120),
    text: clip(r.text, 60),
    rect: { x: rect.x, y: rect.y, w: rect.w, h: rect.h },
    viewport: { w: viewport.w, h: viewport.h },
    styles,
  };
};

export const readDomMessage = (data: unknown): TDomMessage | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (m.type === 'tk:dom-changed') return { k: 'changed' };
  if (m.type === 'tk:lib-ready') return { k: 'ready' };
  if (m.type === 'tk:dom-inspect' && m.on === false) return { k: 'inspect-off' };
  if (m.type !== 'tk:dom-tree' && m.type !== 'tk:dom-node' && m.type !== 'tk:dom-picked') return null;
  const path = readPath(m.path);
  if (path === null) return null;
  if (m.type === 'tk:dom-picked') return { k: 'picked', path };
  if (!isNum(m.req)) return null;
  const gone = m.gone === true;
  if (m.type === 'tk:dom-tree') {
    return { k: 'tree', req: m.req, path, node: gone ? null : readNode(m.node, 0, { left: MAX_NODES }) };
  }
  return { k: 'node', req: m.req, path, detail: gone ? null : readDetail(m, path) };
};
