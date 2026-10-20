import { adoptPending } from '@/lib/timeline-launch';
import type { ITimelineEntry, ITimelineUserMessage } from '@/types/timeline';

// Claude Code records a long paste as <pasted_content id="…">…</pasted_content id="…"> and a picture as [Image #n].
const PASTE_TAG_RE = /<\/?pasted_content\b[^>]*>/g;
const IMAGE_TOKEN_RE = /\[Image #\d+\]/g;
const UNTERMINATED_PASTE_RE = /<pasted_content\b[^>]*>(?:(?!<\/pasted_content\b)[\s\S])*$/;

/** What two texts of one message have in common: the paste wrapper, image tokens and the spacing do not count. */
export const pendingMatchKey = (text: string) =>
  text.replace(PASTE_TAG_RE, ' ').replace(IMAGE_TOKEN_RE, ' ').replace(/\s+/g, ' ').trim();

/**
 * Whether the transcript's message is the one the composer put in a pending row. A paste Claude Code cut off without
 * closing its wrapper holds only the start of the text. A placeholder for pictures in flight takes any message.
 */
export const matchesPending = (
  entry: ITimelineUserMessage,
  pending: ITimelineUserMessage,
  allowPlaceholder = false,
) => {
  if (allowPlaceholder && pending.attachmentPlaceholder) return true;
  const recorded = pendingMatchKey(entry.text);
  const typed = pendingMatchKey(pending.text);
  if (recorded === typed) return true;
  return recorded.length > 0 && UNTERMINATED_PASTE_RE.test(entry.text) && typed.startsWith(recorded);
};

/** Puts the transcript's messages in the rows of the pending ones they match; what matched nothing stays in `rest`. */
export const adoptPendingInto = (
  entries: ITimelineEntry[],
  pendings: ITimelineUserMessage[],
  allowPlaceholder: boolean,
) => {
  const rest = [...pendings];
  const merged = entries.map((entry) => {
    if (entry.type !== 'user-message') return entry;
    const index = rest.findIndex((pending) => matchesPending(entry, pending, allowPlaceholder));
    if (index === -1) return entry;
    const [matched] = rest.splice(index, 1);
    return adoptPending(entry, matched);
  });
  return { merged, rest };
};

const isWaiting = (entry: ITimelineEntry): entry is ITimelineUserMessage =>
  entry.type === 'user-message' && !!entry.pending && !!entry.queued && !entry.launch;

/**
 * A queue holds a message only until the running turn ends: Claude Code takes it into the next turn then. A turn end
 * that came after the message while the transcript still has no record of it means the match failed or the text never
 * got there, so the row stops claiming «in queue» and shows as an ordinary sent message. Nothing is removed.
 * «After» is the order of arrival, or the time when a snapshot moved the row to the end.
 */
export const settleStaleQueued = (entries: ITimelineEntry[]): ITimelineEntry[] => {
  if (!entries.some(isWaiting)) return entries;
  const ends = entries.flatMap((entry, index) =>
    entry.type === 'turn-end' ? [{ index, at: entry.timestamp }] : [],
  );
  if (ends.length === 0) return entries;
  let changed = false;
  const settled = entries.map((entry, index) => {
    if (!isWaiting(entry) || !ends.some((end) => end.index > index || end.at > entry.timestamp)) return entry;
    changed = true;
    const { queued: _queued, ...rest } = entry;
    return rest;
  });
  return changed ? settled : entries;
};
