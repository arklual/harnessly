import type { IPendingMessageOptions, ITimelineEntry, ITimelineUserMessage } from '@/types/timeline';

const WORK_TYPES: ReadonlySet<ITimelineEntry['type']> = new Set([
  'user-message',
  'assistant-message',
  'thinking',
  'tool-call',
  'tool-result',
  'agent-group',
]);

/**
 * The first message of a conversation the shell just launched. It is the command Claude starts with, not
 * a message waiting behind a running turn, so it never wears the «queued» or «not yet delivered» look.
 */
export const launchPendingEntry = (prompt: string, now = Date.now()): ITimelineUserMessage => ({
  id: `pending-${now}-${Math.random().toString(36).slice(2, 8)}`,
  type: 'user-message',
  timestamp: now,
  text: prompt.trim(),
  pending: true,
  launch: true,
});

/** A message just sent from the composer, in the timeline before the agent has recorded it; none for an empty one. */
export const pendingUserEntry = (
  text: string,
  options: IPendingMessageOptions = {},
  now = Date.now(),
): ITimelineUserMessage | null => {
  const trimmed = text.trim();
  const images = options.images ?? [];
  if (!trimmed && images.length === 0) return null;
  return {
    id: `pending-${now}-${Math.random().toString(36).slice(2, 8)}`,
    type: 'user-message',
    timestamp: now,
    text: trimmed,
    pending: true,
    attachmentPlaceholder: options.attachmentPlaceholder,
    ...(images.length > 0 ? { images } : {}),
    ...(options.launch ? { launch: true } : {}),
  };
};

/**
 * Puts the transcript's record in the row of a pending message. Claude Code echoes a launch prompt as «queued»
 * before it takes it; the row keeps its look until the real record arrives, so it neither dims nor jumps.
 */
export const adoptPending = (
  entry: ITimelineUserMessage,
  pending: ITimelineUserMessage,
  now = Date.now(),
): ITimelineUserMessage => {
  // The pictures the person sent are already on screen; the transcript's copies of the same pictures would reload them.
  const own =
    pending.images &&
    pending.images.length > 0 &&
    (!entry.images?.length || entry.images.length === pending.images.length)
      ? { images: pending.images }
      : {};
  const traced = pending.traceId ? { traceId: pending.traceId } : {};
  if (!pending.launch)
    return { ...entry, ...own, ...traced, id: pending.id, deliveredAt: now, sentAt: pending.timestamp };
  const { queued: _queued, ...rest } = entry;
  return { ...rest, ...own, id: pending.id, ...(entry.pending ? { launch: true } : {}) };
};

/** A queued message with no turn before it waits for nothing: it is the launch prompt, however the snapshot found it. */
export const settleLaunchMessage = (entries: ITimelineEntry[]): ITimelineEntry[] => {
  const first = entries.findIndex((entry) => WORK_TYPES.has(entry.type));
  const entry = entries[first];
  if (!entry || entry.type !== 'user-message' || !entry.queued || !entry.pending) return entries;
  const { queued: _queued, ...rest } = entry;
  return entries.map((candidate, i) => (i === first ? { ...rest, launch: true } : candidate));
};
