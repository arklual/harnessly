// The comments of a guest link over the gateway: the link's own address is the guest's right, no session. The guest's name
// is asked once and kept in the browser with the id that tells one guest's comments from another's.
import type { IPicked } from '@/next/features/designly/page-bridge';

export interface IGuestAnchor {
  selector: string;
  dataId: number | null;
  tag: string;
  label: string;
  rect: { x: number; y: number; width: number; height: number };
  /** The board's settings at the moment: name → value. */
  state: Record<string, string | number | boolean>;
}

export interface IGuestComment {
  id: string;
  board: string;
  text: string;
  name: string;
  at: string;
  anchor: IGuestAnchor | null;
  /** Where on a slide a deck comment is, as fractions 0..1 of the slide; null for a comment on a block. */
  point: { x: number; y: number } | null;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');

const anchorOf = (v: unknown): IGuestAnchor | null => {
  if (!isRecord(v) || typeof v.selector !== 'string' || !isRecord(v.rect)) return null;
  const state: IGuestAnchor['state'] = {};
  if (isRecord(v.state)) {
    for (const [key, value] of Object.entries(v.state))
      if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean')
        state[key] = value;
  }
  return {
    selector: v.selector,
    dataId: typeof v.dataId === 'number' ? v.dataId : null,
    tag: str(v.tag),
    label: str(v.label),
    rect: { x: num(v.rect.x), y: num(v.rect.y), width: num(v.rect.width), height: num(v.rect.height) },
    state,
  };
};

const commentOf = (v: unknown): IGuestComment | null => {
  if (!isRecord(v) || (typeof v.id !== 'number' && typeof v.id !== 'string')) return null;
  return {
    id: String(v.id),
    board: str(v.board),
    text: str(v.text),
    name: str(v.name),
    at: str(v.at),
    anchor: anchorOf(v.anchor),
    point:
      typeof v.x === 'number' && typeof v.y === 'number' && v.x >= 0 && v.x <= 1 && v.y >= 0 && v.y <= 1
        ? { x: v.x, y: v.y }
        : null,
  };
};

export class GuestError extends Error {}

/** The comments of the link; null where they cannot be read (the link does not take comments). */
export const fetchComments = async (url: string, signal?: AbortSignal): Promise<IGuestComment[] | null> => {
  try {
    const res = await fetch(url, { cache: 'no-store', signal });
    if (!res.ok) return null;
    const body: unknown = await res.json();
    return Array.isArray(body) ? body.map(commentOf).filter((c) => c !== null) : null;
  } catch {
    return null;
  }
};

export interface IGuestWho {
  guestId: string;
  name: string;
}

const KEY = 'hx-guest-who';

const newId = (): string => `g-${crypto.randomUUID()}`;

export const readWho = (): IGuestWho => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    if (isRecord(raw) && typeof raw.guestId === 'string')
      return { guestId: raw.guestId, name: str(raw.name).slice(0, 60) };
  } catch {
    // Private mode: the guest is asked again next time.
  }
  return { guestId: newId(), name: '' };
};

export const keepWho = (who: IGuestWho): void => {
  try {
    localStorage.setItem(KEY, JSON.stringify(who));
  } catch {
    // Not kept.
  }
};

const MAX_SELECTOR = 400;
const MAX_STATE_KEYS = 20;
const MAX_COORD = 1e6;
const TAG = /^[a-z][a-z0-9-]{0,31}$/;

/** The settings of the board as a comment keeps them: scalars only, few and short; whatever else is dropped. */
export const cleanState = (controls: readonly { name: string; value: unknown }[]): IGuestAnchor['state'] => {
  const state: IGuestAnchor['state'] = {};
  for (const c of controls) {
    if (Object.keys(state).length >= MAX_STATE_KEYS) break;
    if (typeof c.name !== 'string' || c.name === '' || c.name.length > 40) continue;
    if (typeof c.value === 'string') state[c.name] = c.value.slice(0, 80);
    else if (typeof c.value === 'number' && Number.isFinite(c.value)) state[c.name] = c.value;
    else if (typeof c.value === 'boolean') state[c.name] = c.value;
  }
  return state;
};

const finite = (v: number) => Number.isFinite(v) && Math.abs(v) <= MAX_COORD;

/** The anchor of a picked block; null where the page's word does not fit (the comment then goes without a block). */
export const anchorFor = (picked: IPicked, state: IGuestAnchor['state']): IGuestAnchor | null => {
  const { rect } = picked;
  if (picked.selector === '' || picked.selector.length > MAX_SELECTOR || !TAG.test(picked.tag)) return null;
  if (![rect.x, rect.y, rect.width, rect.height].every(finite) || rect.width < 0 || rect.height < 0)
    return null;
  return {
    selector: picked.selector,
    dataId: picked.id,
    tag: picked.tag,
    label: (picked.text || picked.component || picked.tag).slice(0, 120),
    rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
    state: cleanState(Object.entries(state).map(([name, value]) => ({ name, value }))),
  };
};

export interface INewComment {
  who: IGuestWho;
  text: string;
  board: string;
  anchor: IGuestAnchor | null;
  /** The place on a slide, fractions 0..1; absent for a comment on a block. */
  point?: { x: number; y: number };
}

/** Sends a comment and waits for the gateway: the comment as it was taken. */
export const postComment = async (url: string, input: INewComment): Promise<IGuestComment> => {
  let res: Response;
  try {
    res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        guestId: input.who.guestId,
        name: input.who.name,
        text: input.text,
        board: input.board,
        x: input.point?.x ?? null,
        y: input.point?.y ?? null,
        hp: '',
        anchor: input.anchor,
      }),
    });
  } catch {
    throw new GuestError('network');
  }
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok)
    throw new GuestError(isRecord(body) && typeof body.error === 'string' ? body.error : 'refused');
  const taken = commentOf(body);
  return (
    taken ?? {
      id: `local-${Date.now()}`,
      board: input.board,
      text: input.text,
      name: input.who.name,
      at: new Date().toISOString(),
      anchor: input.anchor,
      point: input.point ?? null,
    }
  );
};
