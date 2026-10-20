export interface ISeenRows {
  sessionId: string | null;
  known: ReadonlySet<string>;
  fresh: ReadonlySet<string>;
}

/**
 * Which timeline rows arrived after the conversation was opened: only those play the enter animation. Rows already on
 * screen when it opens, and older history loaded above them, are known without being fresh. Returns `seen` itself
 * when nothing new appeared.
 */
export const trackFreshRows = (
  seen: ISeenRows | null,
  sessionId: string | null,
  ids: readonly string[],
): ISeenRows => {
  if (!seen || seen.sessionId !== sessionId) return { sessionId, known: new Set(ids), fresh: new Set() };
  if (ids.every((id) => seen.known.has(id))) return seen;
  const firstKnown = ids.findIndex((id) => seen.known.has(id));
  const known = new Set(seen.known);
  const fresh = new Set(seen.fresh);
  ids.forEach((id, i) => {
    if (known.has(id)) return;
    known.add(id);
    if (firstKnown >= 0 && i > firstKnown) fresh.add(id);
  });
  return { sessionId, known, fresh };
};
