// The designs at work, for the sidebar under «Sinopia Design»: the ones a live chat works on (in its folder or touching
// its files), with the open one always among them, the pinned first, then the latest changed. Pure: the sidebar's hook
// reads the library and the chats and asks this.
import type { IDesign } from '@/next/features/designly/api';
import type { Conversation, DesignAppKind, DesignKind } from '@/next/protocol/v2';

export interface IWorkingDesign {
  id: string;
  title: string;
  kind: DesignKind;
  /** The app's document type, when the project is one (its icon is a document's). */
  appKind?: DesignAppKind;
}

/** A chat stays live this long after its last activity. */
export const LIVE_CHAT_MS = 24 * 60 * 60 * 1000;

/** The chats at work now or active within LIVE_CHAT_MS of `now`. */
export const liveChats = (convs: readonly Conversation[], now: number): ReadonlySet<string> =>
  new Set(
    convs.filter((c) => c.state.k === 'working' || now - c.lastActivityAt < LIVE_CHAT_MS).map((c) => c.id),
  );

export const workingDesigns = (
  designs: readonly IDesign[],
  live: ReadonlySet<string>,
  current: string | null,
): IWorkingDesign[] =>
  designs
    .filter((d) => d.id === current || [...d.convs, ...(d.touchedBy ?? [])].some((id) => live.has(id)))
    .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updatedAt - a.updatedAt)
    .map(({ id, title, kind, appKind }) => ({ id, title, kind, ...(appKind ? { appKind } : {}) }));
