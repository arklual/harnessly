import type { Conversation } from '@/lib/hl-conversations';

export interface IMachineFrame {
  conversation: Conversation;
  /** Order of the last visit; the larger, the more recent. */
  seq: number;
}

export type TMachineFrames = Record<string, IMachineFrame>;

/**
 * A machine frame is a whole second app on the shell's main thread: terminal, sockets, pollers.
 * Only the frame on screen and the most recently left one stay mounted, so going back and forth between
 * two machines is instant while the rest reload from their server when opened again.
 */
export const MAX_HIDDEN_MACHINE_FRAMES = 1;

/** Updates in place so a mounted frame keeps its DOM position: moving an iframe reloads it. */
export const visitMachineFrame = (
  frames: TMachineFrames,
  machineId: string,
  conversation: Conversation,
): TMachineFrames => {
  const seq = Math.max(0, ...Object.values(frames).map((frame) => frame.seq)) + 1;
  return { ...frames, [machineId]: { conversation, seq } };
};

export const retainMachineFrames = (
  frames: TMachineFrames,
  visibleMachineId: string | null,
  maxHidden: number = MAX_HIDDEN_MACHINE_FRAMES,
): TMachineFrames => {
  const keep = new Set(
    Object.entries(frames)
      .filter(([id]) => id !== visibleMachineId)
      .sort(([, a], [, b]) => b.seq - a.seq)
      .slice(0, maxHidden)
      .map(([id]) => id),
  );
  if (visibleMachineId && frames[visibleMachineId]) keep.add(visibleMachineId);
  if (keep.size === Object.keys(frames).length) return frames;
  return Object.fromEntries(Object.entries(frames).filter(([id]) => keep.has(id)));
};
