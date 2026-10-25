// What an agent asked its right pane to show (`harnessly open …`), and what the runtime asked for it on seeing what the
// agent works on (a design it edits, a document it publishes): kept with the conversation on the machine, applied by
// the page that shows the conversation, now or when the person comes, once per device. Pure: the binding asks the
// machine and keeps what this device has settled.

/** The tabs Legacy's pane draws for the new one (next/legacy/pane.tsx). */
export const LEGACY_KINDS = [
  'changes',
  'artifacts',
  'gitlab',
  'task',
  'economics',
  'peers',
  'gallery',
  'disk',
  'files',
] as const;
export type TLegacyKind = (typeof LEGACY_KINDS)[number];

export type TIntentCommand =
  { kind: 'open'; tab: string; target?: string; auto?: boolean; reload?: boolean } | { kind: 'show' };

export interface IPaneIntent {
  id: string;
  command: TIntentCommand;
  createdAt: number;
  /** When a page first applied it, and on what; null while none has. */
  appliedAt: number | null;
  appliedOn: 'desktop' | 'phone' | null;
}

/** An intent nobody came to see for a day is history, not something to throw at the person. */
export const INTENT_TTL_MS = 24 * 60 * 60_000;
/** A burst of requests opens the newest few; the rest are settled unseen. */
export const INTENTS_AT_ONCE = 3;
/** How many settled ids a device remembers per conversation (the runtime keeps 20 intents a conversation). */
export const SEEN_LIMIT = 100;

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const optionalBool = (v: unknown): boolean => v === undefined || typeof v === 'boolean';

const isCommand = (v: unknown): v is TIntentCommand => {
  if (!isRecord(v)) return false;
  if (v.kind === 'show') return true;
  return (
    v.kind === 'open' &&
    typeof v.tab === 'string' &&
    (v.target === undefined || typeof v.target === 'string') &&
    optionalBool(v.auto) &&
    optionalBool(v.reload)
  );
};

const isIntent = (v: unknown): v is IPaneIntent =>
  isRecord(v) &&
  typeof v.id === 'string' &&
  typeof v.createdAt === 'number' &&
  isCommand(v.command) &&
  (v.appliedAt === null || typeof v.appliedAt === 'number') &&
  (v.appliedOn === null || v.appliedOn === 'desktop' || v.appliedOn === 'phone');

/** The intents of the machine's answer `{intents: [...]}`; what is not one is left out. */
export const parseIntents = (body: unknown): IPaneIntent[] => {
  const list = isRecord(body) ? body.intents : null;
  return Array.isArray(list) ? list.filter(isIntent) : [];
};

const isReload = (intent: IPaneIntent): boolean =>
  intent.command.kind === 'open' && intent.command.reload === true;
const isAuto = (intent: IPaneIntent): boolean =>
  intent.command.kind === 'open' && intent.command.auto === true;

export interface IIntentPlan {
  /** Oldest first: the last one ends up in front. */
  apply: IPaneIntent[];
  /** Every id this look settles, applied or not: none of them acts again on this device. */
  settled: string[];
}

export interface IPlanOptions {
  /** «Открывать то, над чем работает агент»: off, what the runtime asked by itself is settled without acting. */
  auto: boolean;
}

/**
 * Which intents act on this look. Each acts once per device, so a tab the person closed is not opened again. A
 * device's first look at a conversation (nothing settled yet) does not replay what another page already showed. A
 * burst opens its newest few; reloads only touch tabs that are there, so all fresh ones act.
 */
export const planIntents = (
  intents: readonly IPaneIntent[],
  seen: readonly string[] | null,
  now: number,
  options: IPlanOptions,
): IIntentPlan | null => {
  const known = new Set(seen ?? []);
  const fresh = intents.filter((intent) => !known.has(intent.id));
  if (fresh.length === 0) return null;
  const live = fresh
    .filter((intent) => intent.createdAt >= now - INTENT_TTL_MS)
    .filter((intent) => seen !== null || intent.appliedAt === null)
    .filter((intent) => options.auto || !isAuto(intent))
    .sort((a, b) => a.createdAt - b.createdAt);
  const opens = new Set(live.filter((intent) => !isReload(intent)).slice(-INTENTS_AT_ONCE));
  return {
    apply: live.filter((intent) => isReload(intent) || opens.has(intent)),
    settled: fresh.map((intent) => intent.id),
  };
};

/** Where a request goes in the new pane. */
export type IPaneRequest = { reload: boolean } & (
  | { k: 'show' }
  | { k: 'tools' }
  | { k: 'browser'; target: string | null }
  | { k: 'design'; target: string | null }
  | { k: 'files'; target: string | null }
  | { k: 'plan' }
  | { k: 'subagent' }
  | { k: 'terminal' }
  | { k: 'side' }
  | { k: 'legacy'; kind: TLegacyKind; target: string | null }
);

const isLegacyKind = (tab: string): tab is TLegacyKind => (LEGACY_KINDS as readonly string[]).includes(tab);

/** The new pane's place for a command; null for a kind it has none for. */
export const requestOf = (command: TIntentCommand): IPaneRequest | null => {
  if (command.kind === 'show') return { k: 'show', reload: false };
  const reload = command.reload === true;
  const target = command.target ?? null;
  switch (command.tab) {
    case 'overview':
    case 'tools':
      return { k: 'tools', reload };
    case 'browser':
      return { k: 'browser', target, reload };
    case 'design':
      return { k: 'design', target, reload };
    case 'files':
      return { k: 'files', target, reload };
    case 'plan':
      return { k: 'plan', reload };
    case 'subagents':
      return { k: 'subagent', reload };
    case 'terminal':
      return { k: 'terminal', reload };
    case 'side-chat':
      return { k: 'side', reload };
  }
  return isLegacyKind(command.tab) ? { k: 'legacy', kind: command.tab, target, reload } : null;
};
