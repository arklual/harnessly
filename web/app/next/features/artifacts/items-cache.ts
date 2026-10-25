// The links of the conversations last opened, kept in localStorage: the tool draws them at once on the next
// load of the page while the runtime reads the whole transcript again (the first read of a long one takes seconds).
import { ARTIFACT_KIND, type IArtifactItem, type TArtifactKind } from '@/next/features/artifacts/model';

const KEY = 'hx-next:artifacts:items:v1';
const CONVERSATIONS = 12;
const ITEMS = 120;

const KINDS: readonly string[] = Object.values(ARTIFACT_KIND);

const storage = (): Storage | null => {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
};

const str = (v: unknown): v is string => typeof v === 'string';

const readItem = (v: unknown): IArtifactItem | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (!str(o.url) || !str(o.kind) || !KINDS.includes(o.kind) || !str(o.fallback) || !str(o.where))
    return null;
  return {
    url: o.url,
    kind: o.kind as TArtifactKind,
    key: str(o.key) ? o.key : null,
    label: str(o.label) ? o.label : null,
    fallback: o.fallback,
    where: o.where,
    github: o.github === true,
    readable: o.readable === true,
    at: typeof o.at === 'number' ? o.at : 0,
  };
};

const readAll = (): Record<string, IArtifactItem[]> => {
  try {
    const raw = storage()?.getItem(KEY);
    const doc: unknown = raw ? JSON.parse(raw) : null;
    if (typeof doc !== 'object' || doc === null) return {};
    const out: Record<string, IArtifactItem[]> = {};
    for (const [conv, list] of Object.entries(doc)) {
      if (!Array.isArray(list)) continue;
      out[conv] = list.map(readItem).filter((item): item is IArtifactItem => item !== null);
    }
    return out;
  } catch {
    return {};
  }
};

/** The items last seen in the conversation; empty when there are none. */
export const cachedItems = (conv: string): IArtifactItem[] => readAll()[conv] ?? [];

/** Remembers the items of a conversation; the least recently written conversations are forgotten. */
export const rememberItems = (conv: string, items: readonly IArtifactItem[]): void => {
  if (items.length === 0) return;
  try {
    const all = readAll();
    delete all[conv];
    const next = { ...all, [conv]: items.slice(0, ITEMS) };
    const keys = Object.keys(next);
    for (const old of keys.slice(0, Math.max(0, keys.length - CONVERSATIONS))) delete next[old];
    storage()?.setItem(KEY, JSON.stringify(next));
  } catch {
    // storage full or blocked
  }
};
