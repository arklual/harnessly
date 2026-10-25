// The field's prompts (the design's .hx-ph): what the person sent before (the last 50 texts) and the quick prompts they
// saved under a /name. Both live in this browser's localStorage; the filtering and the selection are plain functions.

const HISTORY_KEY = 'hx.next.promptHistory';
const QUICK_KEY = 'hx.next.quickPrompts';
/** How many sent texts are kept. */
export const HISTORY_LIMIT = 50;
/** How many of them the list shows; typing narrows the rest down. */
export const RECENT_SHOWN = 6;
/** A quick prompt's name: one word of 2–20 letters, digits or dashes. */
const NAME_PATTERN = /^[a-zа-яё0-9-]{2,20}$/;
/** «/» at the start of the field with no space yet: the person is looking for a quick prompt by name. */
const SLASH_PATTERN = /^\/\S*$/;

export interface IQuickPrompt {
  name: string;
  text: string;
}

export const ROW_KIND = { quick: 'quick', recent: 'recent' } as const;
export type TPromptRow =
  { kind: typeof ROW_KIND.quick; quick: IQuickPrompt } | { kind: typeof ROW_KIND.recent; text: string };

/** The query behind a «/» at the start of the text; null where the text is not a command. */
export const slashQuery = (text: string): string | null => (SLASH_PATTERN.test(text) ? text.slice(1) : null);

/** The name as it is kept (no «/», lower case); null where it is not one word of 2–20 characters. */
export const quickName = (raw: string): string | null => {
  const name = raw.trim().replace(/^\//, '').toLowerCase();
  return NAME_PATTERN.test(name) ? name : null;
};

/**
 * The rows for a query. After «/» only the quick prompts whose name has it; otherwise the quick prompts whose
 * name or text has it, then the sent texts that have it (the newest first, a few of them).
 */
export const filterRows = (
  recent: readonly string[],
  quick: readonly IQuickPrompt[],
  query: string,
  slash: boolean,
): TPromptRow[] => {
  const needle = query.trim().toLowerCase();
  const quickRows = quick
    .filter((item) => {
      if (!needle) return true;
      if (slash) return item.name.includes(needle);
      return item.name.includes(needle) || item.text.toLowerCase().includes(needle);
    })
    .map((item): TPromptRow => ({ kind: ROW_KIND.quick, quick: item }));
  if (slash) return quickRows;
  const recentRows = recent
    .filter((text) => !needle || text.toLowerCase().includes(needle))
    .slice(0, RECENT_SHOWN)
    .map((text): TPromptRow => ({ kind: ROW_KIND.recent, text }));
  return [...quickRows, ...recentRows];
};

/** The text a row puts into the field. */
export const rowText = (row: TPromptRow): string => (row.kind === ROW_KIND.quick ? row.quick.text : row.text);

/** The selection after ↑ (-1) or ↓ (1): it goes round the list. */
export const moveSelection = (selected: number, step: -1 | 1, count: number): number =>
  count === 0 ? 0 : (selected + step + count) % count;

/** A selection that fell off the end (the list shrank) lands on the last row. */
export const clampSelection = (selected: number, count: number): number =>
  Math.max(0, Math.min(selected, count - 1));

/** A sent text goes to the front; the same text sent before moves up instead of repeating. */
export const addRecent = (recent: readonly string[], text: string): string[] => {
  const clean = text.trim();
  if (!clean) return [...recent];
  return [clean, ...recent.filter((item) => item !== clean)].slice(0, HISTORY_LIMIT);
};

/** A quick prompt is added; one with the same name is replaced, the name is a command. */
export const addQuick = (quick: readonly IQuickPrompt[], item: IQuickPrompt): IQuickPrompt[] => [
  ...quick.filter((existing) => existing.name !== item.name),
  item,
];

const read = (key: string): unknown => {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as unknown) : null;
  } catch {
    return null;
  }
};

const write = (key: string, value: unknown) => {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // The storage is full or closed: the prompts live until the page is closed.
  }
};

export const loadRecent = (): string[] => {
  const stored = read(HISTORY_KEY);
  if (!Array.isArray(stored)) return [];
  return stored.filter((item): item is string => typeof item === 'string').slice(0, HISTORY_LIMIT);
};

export const saveRecent = (recent: readonly string[]) => write(HISTORY_KEY, recent);

export const loadQuick = (): IQuickPrompt[] => {
  const stored = read(QUICK_KEY);
  if (!Array.isArray(stored)) return [];
  return stored.filter(
    (item): item is IQuickPrompt =>
      typeof item === 'object' &&
      item !== null &&
      typeof (item as IQuickPrompt).name === 'string' &&
      typeof (item as IQuickPrompt).text === 'string',
  );
};

export const saveQuick = (quick: readonly IQuickPrompt[]) => write(QUICK_KEY, quick);
