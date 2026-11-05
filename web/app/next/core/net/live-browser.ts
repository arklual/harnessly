// The machine's live browser: a headless Chrome of the machine shows an outside site in the pane's browser
// tab. Its socket, /api/browser/live?tab=<id>, carries the page's pictures as binary JPEG messages and everything else
// as JSON: what the page opened and whether it loads, the person's mouse, keys, wheel and paste back.
import { runtimeSocket } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

type TPath = Parameters<typeof runtimeSocket>[0];

/** The socket of a tab's page. */
export const liveBrowserSocketUrl = (m: MachineId, tab: string): string =>
  runtimeSocket(`/api/browser/live?tab=${encodeURIComponent(tab)}` as TPath, m);

/** What the machine says of the page. */
export interface ILivePage {
  url: string;
  title: string;
  canBack: boolean;
  canForward: boolean;
  loading: boolean;
}

/** An element's box in the page's viewport, CSS pixels. */
export interface ILiveRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** What an agent's action came to; rect is its element's box, for the agent's cursor. */
export interface ILiveActed {
  id: string;
  ok: boolean;
  text: string;
  rect: ILiveRect | null;
}

export type TLiveMessage =
  | { t: 'ready' }
  | ({ t: 'page' } & ILivePage)
  | { t: 'clip'; text: string }
  | ({ t: 'acted' } & ILiveActed)
  | { t: 'error'; text: string }
  | { t: 'end'; why: string };

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
/** An action's words are the page's: the machine's limit on an outcome is 64 KiB too. */
const MAX_ACTED = 64 * 1024;
const rectOf = (v: unknown): ILiveRect | null =>
  isRecord(v) && isNum(v.x) && isNum(v.y) && isNum(v.width) && isNum(v.height)
    ? { x: v.x, y: v.y, width: v.width, height: v.height }
    : null;

/** The machine's message, checked; null for anything else. The page's words (its title, an error) are data. */
export const readLiveMessage = (data: unknown): TLiveMessage | null => {
  if (!isRecord(data)) return null;
  switch (data.t) {
    case 'ready':
      return { t: 'ready' };
    case 'page':
      return {
        t: 'page',
        url: str(data.url),
        title: str(data.title),
        canBack: data.canBack === true,
        canForward: data.canForward === true,
        loading: data.loading === true,
      };
    case 'clip':
      return { t: 'clip', text: str(data.text) };
    case 'acted':
      if (typeof data.id !== 'string' || !data.id) return null;
      return {
        t: 'acted',
        id: data.id,
        ok: data.ok === true,
        text: str(data.text).slice(0, MAX_ACTED),
        rect: rectOf(data.rect),
      };
    case 'error':
      return { t: 'error', text: str(data.text) };
    case 'end':
      return { t: 'end', why: str(data.why) };
    default:
      return null;
  }
};

/** The modifier bits of the DevTools protocol. */
export const modifiersOf = (e: {
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}): number => (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);

/** The button names of the protocol, by the pointer event's button. */
export const buttonOf = (button: number): string => {
  switch (button) {
    case 0:
      return 'left';
    case 1:
      return 'middle';
    case 2:
      return 'right';
    case 3:
      return 'back';
    case 4:
      return 'forward';
    default:
      return 'none';
  }
};

const VIRTUAL_KEYS: Record<string, number> = {
  Backspace: 8,
  Tab: 9,
  Enter: 13,
  Shift: 16,
  Control: 17,
  Alt: 18,
  Pause: 19,
  CapsLock: 20,
  Escape: 27,
  ' ': 32,
  PageUp: 33,
  PageDown: 34,
  End: 35,
  Home: 36,
  ArrowLeft: 37,
  ArrowUp: 38,
  ArrowRight: 39,
  ArrowDown: 40,
  Insert: 45,
  Delete: 46,
  Meta: 91,
  ContextMenu: 93,
  F1: 112,
  F2: 113,
  F3: 114,
  F4: 115,
  F5: 116,
  F6: 117,
  F7: 118,
  F8: 119,
  F9: 120,
  F10: 121,
  F11: 122,
  F12: 123,
};

/** The Windows virtual key code the protocol wants for a key (0 when none is known). */
export const virtualKeyOf = (key: string, code: string): number => {
  if (VIRTUAL_KEYS[key] !== undefined) return VIRTUAL_KEYS[key];
  const letter = /^Key([A-Z])$/.exec(code);
  if (letter) return letter[1].charCodeAt(0);
  const digit = /^Digit(\d)$/.exec(code);
  if (digit) return 48 + Number(digit[1]);
  const pad = /^Numpad(\d)$/.exec(code);
  if (pad) return 96 + Number(pad[1]);
  if (key.length === 1) return key.toUpperCase().charCodeAt(0);
  return 0;
};

/** What a key types, for the protocol's `text`: one printable character, Enter as a return, nothing otherwise. */
export const textOfKey = (key: string, ctrlOrMeta: boolean): string => {
  if (ctrlOrMeta) return '';
  if (key === 'Enter') return '\r';
  return key.length === 1 ? key : '';
};

/** The image the browser can decode from a frame's bytes. */
export const decodeFrame = (bytes: ArrayBuffer): Promise<ImageBitmap> =>
  createImageBitmap(new Blob([bytes], { type: 'image/jpeg' }));
