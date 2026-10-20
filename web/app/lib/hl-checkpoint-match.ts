import type { ICheckpoint, ICheckpointFile, IRewindRecord } from '@/types/server/hl-checkpoints';

export type TCheckpointView = Pick<ICheckpoint, 'id' | 'kind' | 'at' | 'files'>;

export interface IRewindNotice {
  record: IRewindRecord;
  undone: boolean;
  canUndo: boolean;
}

/** The snapshot is taken a moment after the stop is reported, and the transcript's last row can trail it slightly. */
const SNAPSHOT_BEFORE_END_MS = 2000;
const SNAPSHOT_AFTER_END_MS = 30_000;
const MAX_NOTICES = 3;

/** The snapshot taken when this turn ended, told apart from the next turn's by the time window alone. */
export const checkpointForTurn = (
  endedAt: number,
  checkpoints: readonly TCheckpointView[],
): TCheckpointView | null => {
  let found: TCheckpointView | null = null;
  for (const checkpoint of checkpoints) {
    if (
      checkpoint.kind !== 'turn' ||
      checkpoint.at < endedAt - SNAPSHOT_BEFORE_END_MS ||
      checkpoint.at > endedAt + SNAPSHOT_AFTER_END_MS
    )
      continue;
    if (!found || checkpoint.at < found.at) found = checkpoint;
  }
  return found;
};

/** Rewinds to a turn's snapshot, as the message under the turn: whether it was undone, and whether it still can be. */
export const rewindNotices = (
  checkpoint: TCheckpointView | null,
  rewinds: readonly IRewindRecord[],
  checkpoints: readonly TCheckpointView[],
): IRewindNotice[] => {
  if (!checkpoint) return [];
  return rewinds
    .filter((record) => record.targetId === checkpoint.id)
    .sort((a, b) => a.at - b.at)
    .slice(-MAX_NOTICES)
    .map((record) => {
      const undone = rewinds.some((other) => other.targetId === record.beforeId);
      return { record, undone, canUndo: !undone && checkpoints.some((item) => item.id === record.beforeId) };
    });
};

export const summarizeFiles = (files: readonly ICheckpointFile[]) =>
  files.reduce(
    (sum, file) => ({
      restored: sum.restored + (file.status === 'A' ? 1 : 0),
      removed: sum.removed + (file.status === 'D' ? 1 : 0),
      changed: sum.changed + (file.status === 'M' || file.status === 'T' ? 1 : 0),
      additions: sum.additions + file.additions,
      deletions: sum.deletions + file.deletions,
    }),
    { restored: 0, removed: 0, changed: 0, additions: 0, deletions: 0 },
  );
