import { isPaneTabKind, type TPaneTabKind } from '@/lib/hl-panel-command';
import {
  activePaneTab,
  openTab,
  PANE_SEEN_LIMIT,
  type IPaneState,
  type TPaneTabKind as TAnyPaneTabKind,
} from '@/lib/hl-pane-tabs';
import { locale, t } from '@/lib/i18n';

/**
 * What an agent asked its right pane to show, kept with the conversation instead of sent to a page that may not
 * be there: the page that shows the conversation applies it when it does, now or hours later, on any device. Pure: the
 * runtime keeps the intents behind `IPaneIntentStore`, the page decides here which of them to apply.
 */

export type TPaneIntentCommand = { kind: 'open'; tab: TPaneTabKind; target?: string } | { kind: 'show' };

export interface IPaneIntent {
  id: string;
  command: TPaneIntentCommand;
  createdAt: number;
  /** When a page first applied it, and on what; null while no page has shown the conversation since. */
  appliedAt: number | null;
  appliedOn: TPaneIntentDevice | null;
}

export type TPaneIntentDevice = 'desktop' | 'phone';

/** Where the intents of a conversation live. The runtime keeps them in a file today; the cluster will. */
export interface IPaneIntentStore {
  add: (tabId: string, command: TPaneIntentCommand) => Promise<IPaneIntent>;
  list: (tabId: string) => Promise<IPaneIntent[]>;
  /** Marks the first application of each id; later ones and unknown ids change nothing. */
  markApplied: (tabId: string, ids: readonly string[], device: TPaneIntentDevice) => Promise<IPaneIntent[]>;
}

/** The sync socket names the conversation whose intents changed; the page that shows it asks for them. */
export const PANE_INTENTS_EVENT = 'hl:pane-intents' as const;

export interface IPaneIntentsChanged {
  type: 'pane-intents';
  tabId: string;
}

/** An intent nobody came to see for a day is history, not something to throw at the person. */
export const PANE_INTENT_TTL_MS = 24 * 60 * 60_000;
export const PANE_INTENTS_PER_TAB = 20;
/** A burst of commands opens the newest few; the rest are remembered as seen. */
export const PANE_INTENTS_AT_ONCE = 3;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

export const isPaneIntentCommand = (value: unknown): value is TPaneIntentCommand => {
  if (!isRecord(value)) return false;
  if (value.kind === 'show') return true;
  return (
    value.kind === 'open' &&
    isPaneTabKind(value.tab) &&
    (value.target === undefined || typeof value.target === 'string')
  );
};

export const isPaneIntent = (value: unknown): value is IPaneIntent =>
  isRecord(value) &&
  typeof value.id === 'string' &&
  typeof value.createdAt === 'number' &&
  isPaneIntentCommand(value.command) &&
  (value.appliedAt === null || typeof value.appliedAt === 'number') &&
  (value.appliedOn === null || value.appliedOn === 'desktop' || value.appliedOn === 'phone');

/** Fresh ones only, the newest at the end, at most a tab's worth. */
export const pruneIntents = (intents: readonly IPaneIntent[], now: number): IPaneIntent[] =>
  intents.filter((intent) => intent.createdAt >= now - PANE_INTENT_TTL_MS).slice(-PANE_INTENTS_PER_TAB);

/** What the pane's `seen` list remembers for an intent, next to the ids auto-open keeps there. */
export const intentSeenId = (id: string): string => `intent:${id}`;

export interface IIntentPlan {
  /** Oldest first: the last one ends up in front. */
  apply: IPaneIntent[];
  /** Every id this look settles, applied or not. */
  seen: string[];
}

/**
 * Which intents a page applies on this look: the fresh ones it has not acted on yet, the newest few of them. An intent
 * acts once per device, so a tab the person closed is not opened again, and a phone that showed it first does not keep
 * it from the desktop. One `defer` holds back stays unseen and is asked about again on the next look.
 */
export const planIntents = (
  intents: readonly IPaneIntent[],
  seen: readonly string[] | undefined,
  now: number,
  defer: (intent: IPaneIntent) => boolean = () => false,
): IIntentPlan | null => {
  const known = new Set(seen ?? []);
  const fresh = intents.filter((intent) => !known.has(intentSeenId(intent.id)));
  const chosen = fresh
    .filter((intent) => intent.createdAt >= now - PANE_INTENT_TTL_MS)
    .sort((a, b) => a.createdAt - b.createdAt)
    .slice(-PANE_INTENTS_AT_ONCE);
  const held = new Set(chosen.filter(defer).map((intent) => intent.id));
  const settled = fresh.filter((intent) => !held.has(intent.id));
  if (settled.length === 0) return null;
  return {
    apply: chosen.filter((intent) => !held.has(intent.id)),
    seen: settled.map((intent) => intentSeenId(intent.id)),
  };
};

/** Tabs the person is not doing anything in: what the agent asked for may take their place in front. */
const IDLE_KINDS: readonly TAnyPaneTabKind[] = ['overview', 'tools'];

/**
 * The pane after a plan: each asked tab opened, behind the tab the person is busy in (marked unread) or in front of an
 * idle one, and the settled ids remembered. `show` says whether the hidden pane should come out for it.
 */
export const applyIntentPlan = (
  state: IPaneState,
  plan: IIntentPlan,
  paneVisible: boolean,
): { state: IPaneState; show: boolean } => {
  const busy = paneVisible && !IDLE_KINDS.includes(activePaneTab(state).kind);
  const opened = plan.apply.reduce(
    (next, intent) =>
      intent.command.kind === 'open'
        ? openTab(next, intent.command.tab, intent.command.target ?? null, { background: busy })
        : next,
    state,
  );
  return {
    state: { ...opened, seen: [...(state.seen ?? []), ...plan.seen].slice(-PANE_SEEN_LIMIT) },
    show: plan.apply.length > 0 && !paneVisible,
  };
};

const describeCommand = (command: TPaneIntentCommand): string =>
  command.kind === 'show'
    ? t('lib.hlPaneIntents.show')
    : command.target
      ? t('lib.hlPaneIntents.openAt', { tab: command.tab, target: command.target })
      : t('lib.hlPaneIntents.open', { tab: command.tab });

const clock = (at: number): string =>
  new Date(at).toLocaleTimeString(locale(), {
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'Europe/Moscow',
  });

/** One line per intent for the agent's `hl status`. */
export const describeIntents = (intents: readonly IPaneIntent[]): string[] =>
  intents.map((intent) => {
    const what = describeCommand(intent.command);
    const when = clock(intent.createdAt);
    return intent.appliedAt === null
      ? t('lib.hlPaneIntents.waiting', { when, what })
      : t(intent.appliedOn === 'phone' ? 'lib.hlPaneIntents.appliedOnPhone' : 'lib.hlPaneIntents.applied', {
          when,
          what,
          at: clock(intent.appliedAt),
        });
  });
