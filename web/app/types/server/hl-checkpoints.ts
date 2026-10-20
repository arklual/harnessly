// Shapes of what the runtime answers; the runtime itself is Go.

export type TCheckpointKind = 'turn' | 'before-rewind';

export interface ICheckpoint {
  id: string;
  kind: TCheckpointKind;
  at: number;
  tree: string;
  files: number;
}

export interface ICheckpointFile {
  path: string;
  /** Seen from the current state: `A` comes back, `D` goes away, `M` and `T` change. */
  status: 'A' | 'D' | 'M' | 'T';
  additions: number;
  deletions: number;
}

export interface ICheckpointDiff {
  diff: string;
  truncated: boolean;
  files: ICheckpointFile[];
}

export interface IRewindRecord {
  at: number;
  targetId: string;
  targetAt: number;
  beforeId: string;
  changed: number;
  skipped: number;
}

export interface ISkippedFile {
  path: string;
  reason: 'changed' | 'unsafe' | 'unsupported' | 'failed';
  /** The content found in the file when it turned out to have changed, kept in the shadow repository (`git cat-file blob`). */
  blob?: string;
}
