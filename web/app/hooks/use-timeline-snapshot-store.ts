import { create } from 'zustand';
import type { IArtifactLink } from '@/lib/hl-artifacts';
import type { ITaskItem, ITimelineEntry } from '@/types/timeline';

export interface ITimelineSnapshot {
  entries: ITimelineEntry[];
  jsonlPath: string | null;
  sessionId: string | null;
  tasks?: ITaskItem[];
  /** The links of the answers, when the runtime found them itself: `entries` then hold no answers to look in. */
  links?: IArtifactLink[];
  /** A number that moves whenever the conversation does, when `entries` are not all of its lines. */
  activity?: number;
}

interface ITimelineSnapshotStore {
  bySession: Record<string, ITimelineSnapshot>;
  publish: (sessionName: string, snapshot: ITimelineSnapshot) => void;
}

/** Mirrors each mounted timeline so side panels can read the active conversation without a second subscription. */
const useTimelineSnapshotStore = create<ITimelineSnapshotStore>((set) => ({
  bySession: {},
  publish: (sessionName, snapshot) =>
    set((state) => ({ bySession: { ...state.bySession, [sessionName]: snapshot } })),
}));

export default useTimelineSnapshotStore;
