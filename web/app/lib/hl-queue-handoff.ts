import type { IQueuedPrompt } from '@/types/server/hl-prompt-queue';
import type { IPendingMessageOptions } from '@/types/timeline';

/**
 * A message this page put in the server's queue shows as a chip above the composer. Once the runtime takes
 * it out — steered into the running turn, or sent when the agent could take it — Claude Code may hold it for a long
 * time before the transcript records it, and the chip is gone. The tracker watches the queue for that moment, so the
 * timeline can show the message as a pending row from then on.
 */
export type THandoffEvent =
  { kind: 'handoff'; tabId: string; item: IQueuedPrompt } | { kind: 'retract'; tabId: string; id: string };

export const LOCAL_ID_PREFIX = 'local-';
export const OWN_TEXT_MAX_AGE_MS = 10 * 60_000;
/** The transcript and the queue stamp times on the machine's clock; this much skew does not make a record «older». */
export const RECORDED_SLACK_MS = 2_000;
const REMEMBERED_IDS = 200;

export const isLocalQueueId = (id: string) => id.startsWith(LOCAL_ID_PREFIX);

interface IOwnText {
  text: string;
  at: number;
}

export interface IHandoffTracker {
  /** This page is sending `text` to the queue: only such messages become rows, never one another page queued. */
  own: (tabId: string, text: string, now?: number) => void;
  disown: (tabId: string, text: string) => void;
  /** The person removes or takes back a queued message: its leaving the queue is not a hand-over. */
  dismiss: (tabId: string, id: string) => void;
  undismiss: (tabId: string, id: string) => void;
  /** The queue of a tab changed: which messages left it for the agent, and which came back after a failed hand-over. */
  observe: (
    tabId: string,
    previous: readonly IQueuedPrompt[],
    next: readonly IQueuedPrompt[],
    now?: number,
  ) => THandoffEvent[];
  /** The server's answer to «add»: a steered message may be gone from the queue before the page ever saw it there. */
  answered: (tabId: string, item: IQueuedPrompt, delivery: string | null, now?: number) => THandoffEvent[];
}

const remember = (ids: Set<string>, id: string) => {
  ids.add(id);
  if (ids.size > REMEMBERED_IDS) ids.delete(ids.values().next().value as string);
};

export const createHandoffTracker = (): IHandoffTracker => {
  const owned = new Map<string, IOwnText[]>();
  const dismissed = new Set<string>();
  const handed = new Set<string>();
  const vanished = new Set<string>();

  const keep = (tabId: string, now: number) =>
    (owned.get(tabId) ?? []).filter((entry) => now - entry.at <= OWN_TEXT_MAX_AGE_MS);

  const consume = (tabId: string, text: string, now: number): boolean => {
    const list = keep(tabId, now);
    const index = list.findIndex((entry) => entry.text === text);
    if (index !== -1) list.splice(index, 1);
    owned.set(tabId, list);
    return index !== -1;
  };

  const own = (tabId: string, text: string, now = Date.now()) =>
    owned.set(tabId, [...keep(tabId, now), { text, at: now }]);
  const keyOf = (tabId: string, id: string) => `${tabId}:${id}`;

  return {
    own,
    disown: (tabId, text) => {
      consume(tabId, text, Date.now());
    },
    dismiss: (tabId, id) => {
      dismissed.add(keyOf(tabId, id));
    },
    undismiss: (tabId, id) => {
      dismissed.delete(keyOf(tabId, id));
    },
    observe: (tabId, previous, next, now = Date.now()) => {
      const events: THandoffEvent[] = [];
      const nextIds = new Set(next.map((item) => item.id));
      const previousIds = new Set(previous.map((item) => item.id));
      for (const item of previous) {
        if (isLocalQueueId(item.id) || nextIds.has(item.id)) continue;
        const key = keyOf(tabId, item.id);
        if (dismissed.delete(key)) {
          consume(tabId, item.text, now);
          continue;
        }
        remember(vanished, key);
        if (handed.has(key) || !consume(tabId, item.text, now)) continue;
        remember(handed, key);
        events.push({ kind: 'handoff', tabId, item });
      }
      for (const item of next) {
        if (isLocalQueueId(item.id) || previousIds.has(item.id)) continue;
        const key = keyOf(tabId, item.id);
        const cameBack = vanished.delete(key) && handed.delete(key);
        if (!cameBack) continue;
        own(tabId, item.text, now);
        events.push({ kind: 'retract', tabId, id: item.id });
      }
      return events;
    },
    answered: (tabId, item, delivery, now = Date.now()) => {
      const key = keyOf(tabId, item.id);
      if (delivery !== 'steered' || isLocalQueueId(item.id) || handed.has(key) || dismissed.has(key))
        return [];
      if (!consume(tabId, item.text, now)) return [];
      remember(handed, key);
      return [{ kind: 'handoff', tabId, item }];
    },
  };
};

/** What the timeline shows for a message the runtime took out of the queue: its text, its pictures, and a guard against a doubled row. */
export const pendingFromHandoff = (
  item: IQueuedPrompt,
  imageUrl: (path: string) => string | null,
): { text: string; options: IPendingMessageOptions } => {
  const images = item.images.flatMap((path) => {
    const url = imageUrl(path);
    return url ? [url] : [];
  });
  return {
    text: item.text,
    options: {
      ...(images.length > 0 ? { images } : {}),
      recordedSince: item.createdAt - RECORDED_SLACK_MS,
    },
  };
};
