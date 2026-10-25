// What the design system picker remembers and how it paints: the last picks (kept in the browser, newest first) and
// the colour of a system's dot. The design colours six systems by name; any other id gets one of the hashed tones.

const RECENT_KEY = 'hx-next-dl-ds-recent';
/** How many picks the recent group keeps. */
export const RECENT_MAX = 3;
/** How many tones the stylesheet has for the systems the design does not name (`.hx-dl-dot[data-tone]`). */
export const DOT_TONES = 8;

/** The systems whose dot the stylesheet colours by id. */
const NAMED_DS = ['acme', 'shop', 'charts', 'hx', 'marketing', 'legacy', 'none'] as const;

/** FNV-1a: stable for an id, spread well enough for eight tones. */
const hashOf = (id: string) => {
  let hash = 0x811c9dc5;
  for (let i = 0; i < id.length; i += 1) {
    hash ^= id.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
};

/** The tone of a system the stylesheet does not know by name; null for the named ones. */
export const dotTone = (id: string): number | null =>
  (NAMED_DS as readonly string[]).includes(id) ? null : hashOf(id) % DOT_TONES;

export const readRecent = (): string[] => {
  try {
    const list: unknown = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]');
    return Array.isArray(list)
      ? list.filter((id): id is string => typeof id === 'string').slice(0, RECENT_MAX)
      : [];
  } catch {
    return [];
  }
};

/** The picks with `id` first; the list is kept and returned even where the browser keeps nothing. */
export const rememberRecent = (list: readonly string[], id: string): string[] => {
  const next = [id, ...list.filter((item) => item !== id)].slice(0, RECENT_MAX);
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch {
    // Private mode or full storage: the picks live for this page only.
  }
  return next;
};
