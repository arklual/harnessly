// The live view of the machine's Chrome (runtime's /api/chrome, internal/chromeview): what its socket sends and where a
// point of the pane falls on the page. Pure.

export interface IChromeTab {
  id: string;
  title: string;
  url: string;
  active: boolean;
}

export type TChromeMessage = { t: 'status'; up: boolean; error: string } | { t: 'tabs'; tabs: IChromeTab[] };

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const isTab = (v: unknown): v is IChromeTab =>
  isRecord(v) &&
  typeof v.id === 'string' &&
  typeof v.title === 'string' &&
  typeof v.url === 'string' &&
  typeof v.active === 'boolean';

/** A text message of the socket; null for what is not one. */
export const parseChromeMessage = (text: string): TChromeMessage | null => {
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isRecord(v)) return null;
  if (v.t === 'status' && typeof v.up === 'boolean')
    return { t: 'status', up: v.up, error: typeof v.error === 'string' ? v.error : '' };
  if (v.t === 'tabs' && (v.tabs === null || Array.isArray(v.tabs)))
    return { t: 'tabs', tabs: (v.tabs ?? []).filter(isTab) };
  return null;
};

/** A binary message: the page's size in CSS pixels (two big-endian uint32), then the JPEG. */
export const parseChromeFrame = (data: ArrayBuffer): { w: number; h: number; jpeg: Blob } | null => {
  if (data.byteLength <= 8) return null;
  const head = new DataView(data, 0, 8);
  const w = head.getUint32(0);
  const h = head.getUint32(4);
  if (w === 0 || h === 0) return null;
  return { w, h, jpeg: new Blob([data.slice(8)], { type: 'image/jpeg' }) };
};

/**
 * The point of the page under a point of the view: the page is drawn whole in the view's box (object-fit: contain), so
 * the bands around it fall on nothing (null).
 */
export const pagePoint = (
  box: { left: number; top: number; width: number; height: number },
  page: { w: number; h: number },
  at: { x: number; y: number },
): { x: number; y: number } | null => {
  if (box.width <= 0 || box.height <= 0) return null;
  const scale = Math.min(box.width / page.w, box.height / page.h);
  const x = (at.x - box.left - (box.width - page.w * scale) / 2) / scale;
  const y = (at.y - box.top - (box.height - page.h * scale) / 2) / scale;
  if (x < 0 || y < 0 || x > page.w || y > page.h) return null;
  return { x: Math.round(x), y: Math.round(y) };
};

/** The tab's line in the strip: its title, else its address without the scheme. */
export const tabLabel = (tab: IChromeTab): string => tab.title.trim() || tab.url.replace(/^[a-z]+:\/\//i, '');
