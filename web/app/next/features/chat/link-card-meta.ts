// What the hub says of a document or a board a link in the chat names: its name, who changed it last and
// when, and a look at it: a board's picture, an HTML document's first page, a text document's first lines. The hub answers only a person who may read it (GET
// <gateway>/docs/<slug>/card): a link the person cannot open stays a plain card. One read per slug for the page, kept a
// couple of minutes; every card of the same thing shows the same answer.
import { useEffect, useSyncExternalStore } from 'react';
import { gatewayPath } from '@/next/core/net/machine';
import { locale } from '@/next/i18n';

export interface ILinkCardMeta {
  kind: 'artifact' | 'board';
  title: string;
  /** Who changed it last, '' when the service does not say. */
  editor: string;
  /** When they did (ISO), '' when not known. */
  editedAt: string;
  /** The hub has a picture of it: `previewSrc(slug)`. */
  preview: boolean;
  /** An HTML document: its first page is at `previewSrc(slug)`, for a frame. */
  page: boolean;
  /** A text document's first lines, '' for none. */
  excerpt: string;
}

const FRESH_MS = 120_000;
const REQUEST_MS = 6_000;

type TEntry = { meta: ILinkCardMeta | null; at: number };
const entries = new Map<string, TEntry>();
const running = new Set<string>();
const listeners = new Set<() => void>();
let version = 0;

const changed = () => {
  version += 1;
  for (const f of [...listeners]) f();
};

const subscribe = (f: () => void) => {
  listeners.add(f);
  return () => {
    listeners.delete(f);
  };
};

const cardPath = (slug: string) => gatewayPath(`/docs/${encodeURIComponent(slug)}/card` as `/${string}`);

/** Where the page shows a board's picture or a document's first page: through the hub, which keeps keys to itself. */
export const previewSrc = (slug: string): string =>
  gatewayPath(`/docs/${encodeURIComponent(slug)}/preview` as `/${string}`);

const text = (v: unknown, max: number): string => (typeof v === 'string' ? v.trim().slice(0, max) : '');

/** The hub's answer as a card; null for one that is not a card. */
export const readCardMeta = (v: unknown): ILinkCardMeta | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  const kind = o.kind === 'board' ? 'board' : o.kind === 'artifact' ? 'artifact' : null;
  const title = text(o.title, 200);
  if (kind === null || title === '') return null;
  return {
    kind,
    title,
    editor: text(o.editor, 120),
    editedAt: text(o.editedAt, 40),
    preview: o.preview === true,
    page: kind === 'artifact' && o.page === true,
    excerpt: text(o.excerpt, 400),
  };
};

export type TCardFetch = (url: string, signal: AbortSignal) => Promise<Response>;
let fetcher: TCardFetch = (url, signal) => fetch(url, { credentials: 'same-origin', signal });
/** Another fetch under the store (tests). */
export const setCardFetch = (next: TCardFetch | null): void => {
  fetcher = next ?? ((url, signal) => fetch(url, { credentials: 'same-origin', signal }));
  entries.clear();
  running.clear();
};

const read = async (slug: string): Promise<void> => {
  running.add(slug);
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), REQUEST_MS);
  let meta: ILinkCardMeta | null = null;
  try {
    const res = await fetcher(cardPath(slug), ctl.signal);
    if (res.ok) meta = readCardMeta(await res.json());
  } catch {
    meta = null;
  } finally {
    clearTimeout(timer);
    running.delete(slug);
  }
  entries.set(slug, { meta, at: Date.now() });
  changed();
};

/** The hub's card of a slug: null until it answers, and for a slug the person cannot read. */
export const useLinkCardMeta = (slug: string | null): ILinkCardMeta | null => {
  useSyncExternalStore(subscribe, () => version);
  useEffect(() => {
    if (slug === null || running.has(slug)) return;
    const e = entries.get(slug);
    if (e && Date.now() - e.at < FRESH_MS) return;
    void read(slug);
  }, [slug]);
  return slug === null ? null : (entries.get(slug)?.meta ?? null);
};

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['minute', 60],
  ['hour', 24],
  ['day', 7],
];

/** «5 минут назад», «вчера», a date for what is older than a week; '' for no time. */
export const whenText = (iso: string, now = Date.now()): string => {
  const t = Date.parse(iso);
  if (!iso || Number.isNaN(t) || t <= 0) return '';
  let n = Math.round((now - t) / 60_000);
  if (n < 1) return new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' }).format(0, 'second');
  for (const [unit, next] of UNITS) {
    if (n < next) return new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' }).format(-n, unit);
    n = Math.round(n / next);
  }
  return new Date(t).toLocaleDateString(locale(), { day: 'numeric', month: 'short' });
};
