import type { ITimelineEntry } from '@/types/timeline';

const signature = (entry: ITimelineEntry): string => {
  const { id: _id, ...content } = entry;
  return JSON.stringify(content);
};

export const mergeTimelineReconnect = (cached: ITimelineEntry[], fresh: ITimelineEntry[]) => {
  const persisted = cached.filter((entry) => entry.type !== 'user-message' || !entry.pending);
  const first = fresh[0];
  if (!first) return { entries: fresh, retainedHistory: false };
  const firstSignature = signature(first);
  const overlap = persisted.findIndex((entry) => signature(entry) === firstSignature);
  if (overlap < 0) return { entries: fresh, retainedHistory: false };

  // The server assigns fresh IDs when it replays a file. Reuse matching IDs so
  // React preserves the existing message DOM and the user's scroll anchor.
  const ids = new Map<string, string[]>();
  for (const entry of persisted.slice(overlap)) {
    const key = signature(entry);
    const matches = ids.get(key) ?? [];
    matches.push(entry.id);
    ids.set(key, matches);
  }
  const entries = fresh.map((entry) => {
    const id = ids.get(signature(entry))?.shift();
    return id ? { ...entry, id } : entry;
  });
  return { entries: [...persisted.slice(0, overlap), ...entries], retainedHistory: overlap > 0 };
};
