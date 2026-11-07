// What the bridge of a preview slot (web/app/lib/hl-preview/bridge-core.ts) tells the pane's browser tab about the page
// in it: it is up, it went to another address, the person picked an element, picking ended; and what the slot's offer
// page asks (open the site a redirect leads to). The page is foreign code: every field is checked, the rest dropped.
import { readBridgeMessage, type IAnchorReport } from '@/next/features/designly/page-bridge';

export interface IFramePicked {
  /** The number the page's bridge gave the block (it follows the block by it); null when it gave none. */
  pick: number | null;
  selector: string;
  tag: string;
  text: string;
  /** In the page's viewport, in its CSS pixels. */
  rect: { x: number; y: number; width: number; height: number };
  html: string;
  /** The component the element belongs to and where it is written, when the page's framework tells it. */
  component: { name: string; file: string | null; line: number | null } | null;
  /** The page's address in the slot. */
  url: string;
}

export type TFrameMessage =
  /** `nonce` names this document of the frame: a fill token is minted for it and dies with it. */
  | { k: 'hello'; url: string; nonce: string | null }
  | { k: 'nav'; url: string; title: string }
  | { k: 'pick'; on: boolean }
  | { k: 'picked'; picked: IFramePicked }
  /** Where the blocks the tab asked it to follow (`tk:anchors`) show now. */
  | { k: 'anchored'; report: IAnchorReport }
  /** The offer page of a redirect to another site: the person pressed «Открыть». */
  | { k: 'open'; url: string }
  /** The site redirected to another one: the offer page says so as it loads, before anyone presses anything. */
  | { k: 'redirect'; url: string }
  /** The proxy could not fetch the site (a reset, a timeout, a certificate): its failure page says so as it loads. */
  | { k: 'failed'; status: number; text: string }
  /** The page asked for a security key or Touch ID, which the slot's origin can never give (WebAuthn). */
  | { k: 'key-step' }
  /** The outcome of an agent's action (`tk:act`); rect is its element's box in the page's viewport. */
  | { k: 'acted'; acted: IFrameActed }
  /** The answer to `tk:probe-login`: whether the page asks for a password, and whether its form takes a fill. */
  | { k: 'login-probe'; passwordField: boolean; fit: TFillFit }
  /** The answer to `tk:fill`; the reason is never the value, only a word of the known set. */
  | { k: 'filled'; ok: boolean; submitted: boolean; reason: TFillReason | null };

export const FILL_REASONS = [
  'no-form',
  'not-login-form',
  'foreign-action',
  'service-worker',
  'expired',
  'moved',
  'failed',
] as const;
export type TFillReason = (typeof FILL_REASONS)[number];
export type TFillFit = 'ok' | TFillReason;

const NONCE = /^[0-9a-f]{32}$/;
const reasonOf = (v: unknown): TFillReason => FILL_REASONS.find((known) => known === v) ?? 'failed';

export interface IFrameActed {
  id: string;
  ok: boolean;
  text: string;
  rect: IFramePicked['rect'] | null;
}

/** An agent's action's words are its own: 64 KiB is the machine's limit too. */
const MAX_ACTED = 64 * 1024;

const MAX_TEXT = 200;
const MAX_SELECTOR = 400;
const MAX_HTML = 2048;

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const rectOf = (v: unknown): IFramePicked['rect'] | null =>
  isRecord(v) && isNum(v.x) && isNum(v.y) && isNum(v.width) && isNum(v.height)
    ? { x: v.x, y: v.y, width: v.width, height: v.height }
    : null;
const str = (v: unknown, max: number): string => (typeof v === 'string' ? v.slice(0, max) : '');
/** One line: the page's words go into the chat's field, where a line break would start a line of their own. */
const line = (v: string): string => v.replace(/\s+/g, ' ').trim();

const httpUrl = (v: unknown): string | null => {
  if (typeof v !== 'string') return null;
  try {
    const u = new URL(v);
    return (u.protocol === 'https:' || u.protocol === 'http:') && !u.username && !u.password ? u.href : null;
  } catch {
    return null;
  }
};

const componentOf = (v: unknown): IFramePicked['component'] => {
  if (!isRecord(v) || typeof v.name !== 'string' || !v.name) return null;
  const source = isRecord(v.source) ? v.source : null;
  const file = source && typeof source.file === 'string' ? line(source.file.slice(0, MAX_TEXT)) : null;
  const at = source && isNum(source.line) ? Math.floor(source.line) : null;
  return { name: line(v.name.slice(0, MAX_TEXT)), file, line: at };
};

const pickedOf = (m: Record<string, unknown>): IFramePicked | null => {
  const rect = rectOf(m.rect);
  const url = httpUrl(m.url);
  if (typeof m.selector !== 'string' || !m.selector || url === null || rect === null) return null;
  return {
    pick: Number.isInteger(m.pick) && (m.pick as number) > 0 ? (m.pick as number) : null,
    selector: line(m.selector.slice(0, MAX_SELECTOR)),
    tag: str(m.tag, 40)
      .toLowerCase()
      .replace(/[^a-z0-9-]/g, ''),
    text: line(str(m.text, MAX_TEXT)),
    rect,
    html: str(m.outerHTML, MAX_HTML),
    component: componentOf(m.component),
    url,
  };
};

export const readFrameMessage = (data: unknown): TFrameMessage | null => {
  if (!isRecord(data)) return null;
  switch (data.type) {
    case 'tk:hello': {
      const url = httpUrl(data.url);
      const nonce = typeof data.nonce === 'string' && NONCE.test(data.nonce) ? data.nonce : null;
      return url ? { k: 'hello', url, nonce } : null;
    }
    case 'tk:nav': {
      const url = httpUrl(data.url);
      return url ? { k: 'nav', url, title: str(data.title, MAX_TEXT) } : null;
    }
    case 'tk:pick':
      return typeof data.on === 'boolean' ? { k: 'pick', on: data.on } : null;
    case 'tk:picked': {
      const picked = pickedOf(data);
      return picked ? { k: 'picked', picked } : null;
    }
    case 'tk:anchored': {
      const heard = readBridgeMessage(data);
      return heard?.k === 'anchored' ? { k: 'anchored', report: heard.report } : null;
    }
    case 'tk:open': {
      const url = httpUrl(data.url);
      return url ? { k: 'open', url } : null;
    }
    case 'tk:key-step':
      return { k: 'key-step' };
    case 'tk:redirect': {
      const url = httpUrl(data.url);
      return url ? { k: 'redirect', url } : null;
    }
    case 'tk:failed':
      return typeof data.status === 'number' &&
        Number.isInteger(data.status) &&
        data.status >= 400 &&
        data.status <= 599
        ? { k: 'failed', status: data.status, text: line(str(data.text, MAX_TEXT)) }
        : null;
    case 'tk:acted':
      return typeof data.id === 'string' && data.id && typeof data.ok === 'boolean'
        ? {
            k: 'acted',
            acted: { id: data.id, ok: data.ok, text: str(data.text, MAX_ACTED), rect: rectOf(data.rect) },
          }
        : null;
    case 'tk:login-probe':
      return typeof data.passwordField === 'boolean'
        ? {
            k: 'login-probe',
            passwordField: data.passwordField,
            fit: data.fit === 'ok' ? 'ok' : reasonOf(data.fit),
          }
        : null;
    case 'tk:filled':
      return typeof data.ok === 'boolean' && typeof data.submitted === 'boolean'
        ? {
            k: 'filled',
            ok: data.ok,
            submitted: data.submitted,
            reason: data.reason === undefined ? null : reasonOf(data.reason),
          }
        : null;
    default:
      return null;
  }
};

/** The page's own address for an address in its slot: the slot's origin is the site's (or the server's). */
export const siteUrlOf = (slotUrl: string, slotOrigin: string, siteOrigin: string): string | null => {
  try {
    const u = new URL(slotUrl);
    if (u.origin !== slotOrigin) return null;
    return `${siteOrigin}${u.pathname}${u.search}${u.hash}`;
  } catch {
    return null;
  }
};

const HTML_IN_WORDS = 600;

/** A fence longer than any run of backticks in the page's text, so the text cannot close it. */
const fenceFor = (text: string, least: number): string => {
  const longest = Math.max(0, ...(text.match(/`+/g) ?? []).map((run) => run.length));
  return '`'.repeat(Math.max(least, longest + 1));
};

const inlineCode = (text: string): string => {
  const fence = fenceFor(text, 1);
  const pad = text.startsWith('`') || text.endsWith('`') ? ' ' : '';
  return `${fence}${pad}${text}${pad}${fence}`;
};

/** What a comment on an element puts into the chat's field: where it is, what it is and what the person said. */
export const pickedWords = (
  picked: IFramePicked,
  pageUrl: string,
  comment: string,
  labels: IPickedLabels,
): string => {
  const lines = [`${labels.element} ${pageUrl}`];
  const { text } = picked;
  const what = text
    ? `<${picked.tag}> ${inlineCode(picked.selector)} — «${text}»`
    : `<${picked.tag}> ${inlineCode(picked.selector)}`;
  lines.push(what);
  if (picked.component) {
    const where = picked.component.file
      ? ` (${picked.component.file}${picked.component.line !== null ? `:${picked.component.line}` : ''})`
      : '';
    lines.push(`${labels.component} ${picked.component.name}${where}`);
  }
  if (picked.html && picked.html.length <= HTML_IN_WORDS) {
    const fence = fenceFor(picked.html, 3);
    lines.push(`${fence}html`, picked.html, fence);
  }
  if (comment.trim()) lines.push(`${labels.comment} ${comment.trim()}`);
  return lines.join('\n');
};

export interface IPickedLabels {
  element: string;
  component: string;
  comment: string;
}
