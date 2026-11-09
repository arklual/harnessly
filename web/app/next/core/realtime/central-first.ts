// HRN-879, spec v10: reading goes through the hub, the machine is only for what is live. A chat opened on a machine
// whose chats the cluster keeps, with nothing of it kept in the page, is drawn from the cluster's copy first; the
// machine's channel is asked only after it, from the copy's newest entry (a snapshot "after"), not for its whole tail.

import { noteLastEntry, resumeOf } from '@/next/core/realtime/cursors';
import { isMachineOff, keepsHistory, loadCentralChat } from '@/next/stores/central';
import { hasFeature } from '@/next/stores/machines';
import type { Channel, ConvId, MachineId } from '@/next/protocol/v2';

/** How long the machine's channel waits for the copy: past it, the machine sends its whole snapshot as before. */
export const CENTRAL_FIRST_MS = 3_000;

/**
 * Whether the machine's channel of the chat waits for the copy: it keeps the machine's chats, the page nothing of it,
 * and the machine's snapshot after an entry carries the turns from that entry's on (snapshot-after). An older runtime
 * sends only the turns later entries touch: a turn the copy saw running would stay running on the page, so such a
 * machine sends its whole snapshot as before.
 */
export function waitsForCentral(m: MachineId, conv: ConvId): boolean {
  return (
    keepsHistory(m) && hasFeature(m, 'snapshot-after') && !resumeOf(m, `conv:${conv}` as Channel).lastEntryId
  );
}

/**
 * Reads the chat's page from the copy (the same request the chat's feed makes) and notes its newest entry as where the
 * machine's channel goes on from. A copy that does not answer in time, has nothing, or a machine that is off notes
 * nothing: the channel then asks for the whole snapshot.
 */
export async function noteCentralCursor(
  m: MachineId,
  conv: ConvId,
  waitMs = CENTRAL_FIRST_MS,
): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const late = new Promise<null>((resolve) => {
    timer = setTimeout(() => resolve(null), waitMs);
  });
  const res = await Promise.race([loadCentralChat(conv), late]);
  clearTimeout(timer);
  if (!res || !('ok' in res) || isMachineOff(m)) return;
  const ch = `conv:${conv}` as Channel;
  if (resumeOf(m, ch).lastEntryId) return;
  const last = [...res.value.entries].reverse().find((e) => !e.id.startsWith('live:'));
  if (last) noteLastEntry(m, ch, last.id);
}
