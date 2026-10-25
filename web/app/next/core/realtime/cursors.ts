// Where every channel stopped: the epoch and seq of the last frame applied and, for a conv channel, the last
// transcript entry. Plain memory, not a store — it changes on every event and nothing renders it; the chat cache
// saves it with the feed so a chat resumes after F5 where it was.

import type { Channel, EntryId, MachineId } from '@/next/protocol/v2';
import type { Cursor } from '@/next/core/realtime/client';

interface Resume {
  cursor: Cursor | null;
  lastEntryId: EntryId | null;
}

const resumes = new Map<string, Resume>();
const k = (m: MachineId, ch: Channel) => `${m} ${ch}`;

export function resumeOf(m: MachineId, ch: Channel): Resume {
  return resumes.get(k(m, ch)) ?? { cursor: null, lastEntryId: null };
}

export function noteCursor(m: MachineId, ch: Channel, cursor: Cursor): void {
  const r = resumes.get(k(m, ch));
  resumes.set(k(m, ch), { cursor, lastEntryId: r?.lastEntryId ?? null });
}

export function noteLastEntry(m: MachineId, ch: Channel, id: EntryId): void {
  if (id.startsWith('live:')) return;
  const r = resumes.get(k(m, ch));
  resumes.set(k(m, ch), { cursor: r?.cursor ?? null, lastEntryId: id });
}

export function setResume(m: MachineId, ch: Channel, r: Resume): void {
  resumes.set(k(m, ch), r);
}

export function forgetResume(m: MachineId, ch: Channel): void {
  resumes.delete(k(m, ch));
}

export function resetResumes(): void {
  resumes.clear();
}
