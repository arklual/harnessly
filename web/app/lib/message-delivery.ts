import type { ITimelineUserMessage } from '@/types/timeline';

export type TMessageDeliveryState = 'sending' | 'queued' | 'taken' | 'read' | 'failed' | 'delivered';

export const READ_SHOWN_MS = 2000;
export const DELIVERY_FAILED_AFTER_MS = 60_000;
/**
 * The runtime typed the text and pressed Enter, but neither Claude's hook nor the transcript followed: past
 * this the row says it may not have gone in. Claude's own hook settles it for good, the transcript only lags behind it.
 */
export const TAKEN_UNCONFIRMED_AFTER_MS = 15_000;

/**
 * Where a message the person sent stands. A queued message may wait for the whole turn, so only one that is
 * neither queued nor taken by the transcript for a long time counts as lost. A launch prompt shows no state.
 * The agent having it (the runtime typed it in, or Claude's hook fired) shows as delivered at once,
 * without waiting about a second more for the transcript to record it.
 */
export const messageDeliveryState = (entry: ITimelineUserMessage, now: number): TMessageDeliveryState => {
  if (entry.launch) return 'delivered';
  if (entry.pending) {
    if (entry.queued) return 'queued';
    if (entry.takenAt !== undefined) {
      return entry.takenByAgent || now - entry.takenAt < TAKEN_UNCONFIRMED_AFTER_MS ? 'taken' : 'failed';
    }
    if (entry.attachmentPlaceholder) return 'sending';
    return now - entry.timestamp >= DELIVERY_FAILED_AFTER_MS ? 'failed' : 'sending';
  }
  return entry.deliveredAt !== undefined && now - entry.deliveredAt < READ_SHOWN_MS ? 'read' : 'delivered';
};

/** The moment the state of a pending row changes by the clock alone, for its timer; null when none is coming. */
export const deliveryStateBoundary = (entry: ITimelineUserMessage): number | null => {
  if (entry.launch || entry.queued) return null;
  if (entry.pending) {
    if (entry.takenAt !== undefined)
      return entry.takenByAgent ? null : entry.takenAt + TAKEN_UNCONFIRMED_AFTER_MS;
    return entry.attachmentPlaceholder ? null : entry.timestamp + DELIVERY_FAILED_AFTER_MS;
  }
  return entry.deliveredAt !== undefined ? entry.deliveredAt + READ_SHOWN_MS : null;
};

/** The status line stays in the layout from the send until the read mark is gone, so the message never moves. */
export const reservesDeliveryRow = (entry: ITimelineUserMessage) =>
  !entry.launch && (entry.pending === true || entry.deliveredAt !== undefined);
