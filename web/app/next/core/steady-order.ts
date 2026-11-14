// A list a person is looking at does not move under them: a newer answer keeps the rows already shown where
// they stood and adds the rows it brings after them. The ⌘K results and the sidebar's chats hold their order this way.

/** Which rows each list showed, list by list in the order they stood. */
export type TShownLayout = ReadonlyMap<string, readonly string[]>;

/** The items in the order `shown` had them (the ones still here), then the new ones in their own order. */
export const steadyOrder = <T>(
  items: readonly T[],
  shown: readonly string[] | undefined,
  idOf: (item: T) => string,
): readonly T[] => {
  if (!shown || shown.length === 0) return items;
  const byId = new Map(items.map((item) => [idOf(item), item] as const));
  const kept = shown.flatMap((id) => {
    const item = byId.get(id);
    return item === undefined ? [] : [item];
  });
  const was = new Set(shown);
  return [...kept, ...items.filter((item) => !was.has(idOf(item)))];
};

/**
 * Lists of rows (the ⌘K groups) held in the layout they were shown in: the lists already shown first, in their order,
 * each with its shown rows first; lists and rows that came later go after them.
 */
export const steadyLists = <L extends { id: string }, T>(
  lists: readonly L[],
  layout: TShownLayout | null,
  itemsOf: (list: L) => readonly T[],
  withItems: (list: L, items: readonly T[]) => L,
  idOf: (item: T) => string,
): readonly L[] => {
  if (layout === null || layout.size === 0) return lists;
  const order = [...layout.keys()];
  const ordered = steadyOrder(lists, order, (list) => list.id);
  return ordered.map((list) => {
    const shown = layout.get(list.id);
    if (!shown) return list;
    const items = itemsOf(list);
    const steady = steadyOrder(items, shown, idOf);
    return steady === items ? list : withItems(list, steady);
  });
};

/** The layout lists show now. */
export const layoutOf = <L extends { id: string }, T>(
  lists: readonly L[],
  itemsOf: (list: L) => readonly T[],
  idOf: (item: T) => string,
): TShownLayout => new Map(lists.map((list) => [list.id, itemsOf(list).map(idOf)] as const));

/** One string per layout: two layouts with the same string show the same rows in the same places. */
export const layoutKey = (layout: TShownLayout | null): string =>
  layout === null ? '' : [...layout].map(([id, rows]) => `${id}:${rows.join(',')}`).join('|');
