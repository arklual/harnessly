import {
  clip,
  type IPickedComponent,
  type TFillReason,
  type TFramework,
  type TParentMessage,
} from '@/lib/hl-preview/bridge-core';

/** What the in-page bridge of a preview slot tells the island (see bridge-core `installBridge`). */
export interface IPickedElement {
  selector: string;
  tag: string;
  text: string;
  rect: { x: number; y: number; width: number; height: number };
  outerHTML: string;
  component: IPickedComponent | null;
  framework: TFramework;
  url: string;
  viewport: { w: number; h: number };
}

export type TBridgeMessage =
  /** `nonce` names this document of the frame: a fill token is minted for it and dies with it. */
  | { type: 'tk:hello'; url: string; title: string; framework: TFramework; nonce: string | null }
  | { type: 'tk:nav'; url: string; title: string }
  /** The bridge left pick mode by itself: a pick, Escape or a navigation. */
  | { type: 'tk:pick'; on: boolean }
  /** The offer page of a cross-origin redirect asks to open that site. Only a proposal: the person confirms. */
  | { type: 'tk:open'; url: string }
  /** The page is a stand's sign-in wall, answered by the runtime (not a bridge): the island offers «Sign in». */
  | { type: 'tk:sign-in' }
  | ({ type: 'tk:picked' } & IPickedElement)
  /** What the bridge did with a fill: a result and a reason, never a value. */
  | { type: 'tk:filled'; ok: boolean; submitted: boolean; reason?: TFillReason }
  | { type: 'tk:login-probe'; passwordField: boolean; fit: 'ok' | TFillReason }
  /** A board of the design library failed: the panel puts the error in the composer by itself. */
  | { type: 'tk:board-error'; url: string; message: string; stack: string };

/** Only the frame of this preview, from its slot origin: another frame or tab of the page must not drive the island. */
export const acceptsBridgeEvent = (
  event: { source: unknown; origin: string },
  frame: Window | null,
  previewOrigin: string,
): boolean => frame !== null && event.source === frame && event.origin === previewOrigin;

/** The bridge clips to these already; a page can post its own messages, so the island clips again. */
const MAX_TEXT = 120;
const MAX_HTML = 2048;
const MAX_SELECTOR = 512;
const MAX_ERROR = 600;
const MAX_STACK = 1500;

const FILL_REASONS: readonly TFillReason[] = [
  'no-form',
  'not-login-form',
  'foreign-action',
  'service-worker',
  'expired',
  'moved',
  'failed',
];

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null;
const isNumber = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value);
const text = (value: unknown): string => (typeof value === 'string' ? value : '');

const httpUrl = (value: string): URL | null => {
  try {
    const url = new URL(value);
    return (url.protocol === 'https:' || url.protocol === 'http:') && !url.username && !url.password
      ? url
      : null;
  } catch {
    return null;
  }
};

const framework = (value: unknown): TFramework =>
  value === 'angular' || value === 'react' ? value : 'unknown';

const component = (value: unknown): IPickedComponent | null => {
  if (!isRecord(value) || typeof value.name !== 'string' || !value.name) return null;
  const picked: IPickedComponent = { name: clip(value.name, MAX_TEXT) };
  if (typeof value.hostTag === 'string' && value.hostTag) picked.hostTag = clip(value.hostTag, MAX_TEXT);
  const { source } = value;
  if (isRecord(source) && typeof source.file === 'string' && isNumber(source.line))
    picked.source = { file: source.file, line: source.line };
  return picked;
};

const pickedElement = (message: Record<string, unknown>): IPickedElement | null => {
  const { rect, viewport } = message;
  if (typeof message.selector !== 'string' || typeof message.url !== 'string') return null;
  if (
    !isRecord(rect) ||
    !isNumber(rect.x) ||
    !isNumber(rect.y) ||
    !isNumber(rect.width) ||
    !isNumber(rect.height)
  )
    return null;
  if (!isRecord(viewport) || !isNumber(viewport.w) || !isNumber(viewport.h)) return null;
  return {
    selector: clip(message.selector, MAX_SELECTOR),
    tag: clip(text(message.tag), MAX_TEXT),
    text: clip(text(message.text), MAX_TEXT),
    rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
    outerHTML: clip(text(message.outerHTML), MAX_HTML),
    component: component(message.component),
    framework: framework(message.framework),
    url: message.url,
    viewport: { w: viewport.w, h: viewport.h },
  };
};

/** The page is foreign code: every field is checked, unknown ones are dropped. */
export const parseBridgeMessage = (data: unknown): TBridgeMessage | null => {
  if (!isRecord(data)) return null;
  switch (data.type) {
    case 'tk:hello':
      return typeof data.url === 'string'
        ? {
            type: 'tk:hello',
            url: data.url,
            title: text(data.title),
            framework: framework(data.framework),
            nonce: typeof data.nonce === 'string' && /^[0-9a-f]{32}$/.test(data.nonce) ? data.nonce : null,
          }
        : null;
    case 'tk:nav':
      return typeof data.url === 'string' ? { type: 'tk:nav', url: data.url, title: text(data.title) } : null;
    case 'tk:open': {
      const url = typeof data.url === 'string' ? httpUrl(data.url) : null;
      return url ? { type: 'tk:open', url: url.href } : null;
    }
    case 'tk:sign-in':
      return { type: 'tk:sign-in' };
    case 'tk:pick':
      return typeof data.on === 'boolean' ? { type: 'tk:pick', on: data.on } : null;
    case 'tk:picked': {
      const element = pickedElement(data);
      return element ? { type: 'tk:picked', ...element } : null;
    }
    case 'tk:filled': {
      if (typeof data.ok !== 'boolean' || typeof data.submitted !== 'boolean') return null;
      if (data.reason === undefined) return { type: 'tk:filled', ok: data.ok, submitted: data.submitted };
      const reason = FILL_REASONS.find((known) => known === data.reason) ?? 'failed';
      return { type: 'tk:filled', ok: data.ok, submitted: data.submitted, reason };
    }
    case 'tk:board-error': {
      const url = typeof data.url === 'string' ? httpUrl(data.url) : null;
      const message = text(data.message).slice(0, MAX_ERROR);
      return url && message
        ? { type: 'tk:board-error', url: url.href, message, stack: text(data.stack).slice(0, MAX_STACK) }
        : null;
    }
    case 'tk:login-probe': {
      if (typeof data.passwordField !== 'boolean') return null;
      const fit = data.fit === 'ok' ? 'ok' : (FILL_REASONS.find((known) => known === data.fit) ?? 'failed');
      return { type: 'tk:login-probe', passwordField: data.passwordField, fit };
    }
    default:
      return null;
  }
};

/** The address bar shows the path inside the target; a URL of another origin is not this preview's. */
export const pathInTarget = (url: string, previewOrigin: string): string | null => {
  try {
    const parsed = new URL(url);
    return parsed.origin === previewOrigin ? `${parsed.pathname}${parsed.search}${parsed.hash}` : null;
  } catch {
    return null;
  }
};

export const postToBridge = (frame: Window | null, previewOrigin: string, message: TParentMessage): void => {
  frame?.postMessage(message, previewOrigin);
};
