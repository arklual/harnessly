// What the person does with the smart notice, for the team's metrics: words from closed lists and numbers
// only — never a notice's text, a chat's title or a path. The binding sends them to the machine
// (cmd/runtime/routes_ui_events.go), which passes them to the team's PostHog; the gallery shows them in its side.
import type { IFrames } from './pager';

export const NOTICE_ACTION = {
  shown: 'shown',
  opened: 'opened',
  answered: 'answered',
  dropped: 'dropped',
  closed: 'closed',
  closedAll: 'closed_all',
  restored: 'restored',
  paged: 'paged',
  chatOpened: 'chat_opened',
} as const;
export type TNoticeAction = (typeof NOTICE_ACTION)[keyof typeof NOTICE_ACTION];

export const NOTICE_INPUT = { mouse: 'mouse', trackpad: 'trackpad', touch: 'touch', keys: 'keys' } as const;
export type TNoticeInput = (typeof NOTICE_INPUT)[keyof typeof NOTICE_INPUT];

export interface IUiEvent {
  name: 'harnessly_notice' | 'harnessly_notice_gesture';
  at: number;
  props: Record<string, string | number>;
}

type TSink = (event: IUiEvent) => void;

let sink: TSink | null = null;
let input: TNoticeInput = NOTICE_INPUT.mouse;

/** Where the events go: the binding's sender, the gallery's list; null stops them. */
export const setNoticeSink = (next: TSink | null) => {
  sink = next;
};

/** The last way the person touched the plate: the events after it carry it. */
export const noteInput = (next: TNoticeInput) => {
  input = next;
};

/** How many plates the stack has, in the three sizes the spec tells apart. */
export const stackSize = (n: number): string => {
  if (n <= 1) return '1';
  if (n <= 3) return '2-3';
  return '4+';
};

export interface INoticeFacts {
  kind?: string;
  stack?: number;
  place?: string;
  /** Since the item first showed, ms. */
  ms?: number;
  count?: number;
}

export const noticeEvent = (action: TNoticeAction, facts: INoticeFacts = {}) => {
  if (!sink) return;
  const props: Record<string, string | number> = { action, input };
  if (facts.kind !== undefined) props.kind = facts.kind;
  if (facts.stack !== undefined) props.stack = stackSize(facts.stack);
  if (facts.place !== undefined) props.place = facts.place;
  if (facts.ms !== undefined) props.ms = Math.max(0, Math.round(facts.ms));
  if (facts.count !== undefined) props.count = facts.count;
  sink({ name: 'harnessly_notice', at: Date.now(), props });
};

/** How smooth the stack moved under a gesture: frames longer than a beat (pager.ts LONG_FRAME_MS). */
export const gestureEvent = (frames: IFrames) => {
  if (!sink) return;
  sink({ name: 'harnessly_notice_gesture', at: Date.now(), props: { input, ...frames } });
};

/** A sender that gathers events for a few seconds and posts them at once (and when the page hides). */
export const batchSender = (post: (events: IUiEvent[]) => Promise<unknown>, delayMs = 5000) => {
  let queue: IUiEvent[] = [];
  let timer: ReturnType<typeof setTimeout> | null = null;
  const flush = () => {
    if (timer !== null) clearTimeout(timer);
    timer = null;
    if (queue.length === 0) return;
    const batch = queue.slice(0, 40);
    queue = queue.slice(40);
    void post(batch).catch(() => undefined);
    if (queue.length > 0) timer = setTimeout(flush, delayMs);
  };
  const onHide = () => {
    if (document.visibilityState === 'hidden') flush();
  };
  document.addEventListener('visibilitychange', onHide);
  return {
    send: (event: IUiEvent) => {
      queue.push(event);
      timer ??= setTimeout(flush, delayMs);
    },
    stop: () => {
      flush();
      document.removeEventListener('visibilitychange', onHide);
    },
  };
};
