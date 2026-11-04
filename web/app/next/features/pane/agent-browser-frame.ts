/** Browser metadata mirrors the additive socket contract in agentbrowser/protocol.go. */
export interface IBrowserTab {
  id: string;
  title: string;
  url: string;
  owner: 'this' | 'other';
  by: 'agent' | 'person';
  viewport: IViewport;
  crashed: boolean;
  control: 'agent' | 'person';
  agentAt: number;
  dialog?: { type: string; message: string; defaultPrompt: string };
}
/** CSS viewport dimensions are independent of encoded JPEG pixel density. */
export interface IViewport {
  mode: 'fit' | 'fixed' | 'mobile';
  w: number;
  h: number;
  dpr: number;
}
/** Native select contents captured by the socket, indexed in DOM option order. */
export interface ISelectMenu {
  t: 'select';
  x: number;
  y: number;
  w: number;
  h: number;
  selected: number;
  options: { value: string; label: string; disabled: boolean }[];
}
/** Server order and generation belong to this viewer, not the agent working tab. */
export interface ITabsMessage {
  t: 'tabs';
  tabs: IBrowserTab[];
  current: string;
  agentTab: string;
  following: boolean;
  gen: number;
}
/** Preparation may report progress before Chrome can accept commands. */
export interface IStatus {
  t: 'status';
  state: 'starting' | 'preparing' | 'up' | 'down' | 'error';
  error?: string;
  progress?: number;
  hint?: string;
  command?: string;
  profile: { id: string; name: string };
}
/** Recoverable notices leave availability and the current page intact. */
export type TBrowserMessage =
  IStatus | ITabsMessage | ISelectMenu | { t: 'notice'; message: string } | { t: 'clipboard'; text: string };
/** Reject short or invalid frame headers before attempting an expensive JPEG decode. */
export function parseBrowserFrame(data: ArrayBuffer) {
  if (data.byteLength <= 12) return null;
  const v = new DataView(data),
    w = v.getUint32(0),
    h = v.getUint32(4),
    gen = v.getUint32(8);
  return w && h ? { w, h, gen, jpeg: new Blob([data.slice(12)], { type: 'image/jpeg' }) } : null;
}
/** Coordinates must use the displayed image, excluding the surrounding letterbox. */
export function pagePoint(
  rect: { left: number; top: number; width: number; height: number },
  page: { w: number; h: number },
  point: { x: number; y: number },
) {
  if (!rect.width || !rect.height) return null;
  const x = ((point.x - rect.left) * page.w) / rect.width,
    y = ((point.y - rect.top) * page.h) / rect.height;
  return x < 0 || y < 0 || x > page.w || y > page.h ? null : { x, y };
}
