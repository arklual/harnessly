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

/** The subchat a lifted request came from: its id and title. */
export interface IIntentFrom {
  id: string;
  title: string;
}

export type TIntentCommand =
  | {
      kind: 'open';
      tab: string;
      target?: string;
      auto?: boolean;
      reload?: boolean;
      place?: string;
      from?: IIntentFrom;
    }
  | { kind: 'show' };

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

const optionalFrom = (v: unknown): boolean =>
  v === undefined || (isRecord(v) && typeof v.id === 'string' && typeof v.title === 'string');

const isCommand = (v: unknown): v is TIntentCommand => {
  if (!isRecord(v)) return false;
  if (v.kind === 'show') return true;
  return (
    v.kind === 'open' &&
    typeof v.tab === 'string' &&
    (v.target === undefined || typeof v.target === 'string') &&
    optionalBool(v.auto) &&
    optionalBool(v.reload) &&
    (v.place === undefined || typeof v.place === 'string') &&
    optionalFrom(v.from)
  );
};

const isIntent = (v: unknown): v is IPaneIntent =>
  isRecord(v) &&
  typeof v.id === 'string' &&
  typeof v.createdAt === 'number' &&
  isCommand(v.command) &&
  (v.appliedAt === null || typeof v.appliedAt === 'number') &&
  (v.appliedOn === null || v.appliedOn === 'desktop' || v.appliedOn === 'phone');

/** The subchat an intent came up from, by its title (a nameless one is still a subchat's); null for the chat's own. */
export const liftedTitle = (command: TIntentCommand): string | null => {
  if (command.kind !== 'open' || !command.from) return null;
  return command.from.title.trim() || command.from.id;
};

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

/** What the runtime opens by itself, by the person's switches (stores/pane-auto.ts). */
export const PANE_AUTO_KINDS = ['design', 'artifacts', 'browser', 'files'] as const;
export type TPaneAutoKind = (typeof PANE_AUTO_KINDS)[number];
export type TPaneAutoKinds = Readonly<Record<TPaneAutoKind, boolean>>;

/** The tab an auto intent opens, as the switch that lets it: the design, a document or board, the agent's browser, the rest. */
export const autoKindOf = (command: TIntentCommand): TPaneAutoKind => {
  if (command.kind !== 'open') return 'files';
  if (command.tab === 'design') return 'design';
  if (command.tab === 'artifacts') return 'artifacts';
  if (command.tab === 'agent-browser' || command.tab === 'browser') return 'browser';
  return 'files';
};

/**
 * A turn that publishes ten documents does not throw ten tabs at the person: within this window the runtime's own
 * opens are AUTO_PER_WINDOW at most, the newest first; the rest stay cards in the chat, a click away.
 */
export const AUTO_WINDOW_MS = 10 * 60_000;
export const AUTO_PER_WINDOW = 3;

export interface IPlanOptions {
  /** «Открывать то, над чем работает агент»: off, what the runtime asked by itself is settled without acting. */
  auto: boolean;
  /** Which kinds of it: a kind switched off is settled without acting. All on when absent. */
  kinds?: TPaneAutoKinds;
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
    .filter(
      (intent) =>
        !isAuto(intent) || isReload(intent) || (options.kinds?.[autoKindOf(intent.command)] ?? true),
    )
    .sort((a, b) => a.createdAt - b.createdAt);
  // The window's budget: what the runtime opened by itself lately (applied on some page) counts against the new.
  const spent = intents.filter(
    (intent) =>
      known.has(intent.id) &&
      isAuto(intent) &&
      !isReload(intent) &&
      intent.appliedAt !== null &&
      intent.createdAt >= now - AUTO_WINDOW_MS,
  ).length;
  const quiet = live.filter((intent) => isAuto(intent) && !isReload(intent));
  const allowed = new Set(quiet.slice(quiet.length - Math.max(0, AUTO_PER_WINDOW - spent)));
  const opens = new Set(
    live
      .filter((intent) => !isReload(intent) && (!isAuto(intent) || allowed.has(intent)))
      .slice(-INTENTS_AT_ONCE),
  );
  return {
    apply: live.filter((intent) => isReload(intent) || opens.has(intent)),
    settled: fresh.map((intent) => intent.id),
  };
};

/**
 * What opens the pane by itself (docs/pane-opening.md):
 *
 * | trigger                                              | what the person sees                               |
 * |------------------------------------------------------|----------------------------------------------------|
 * | the agent's `open <tab> <target>`, `open show`       | its tab in front, the pane comes out (not on phone) |
 * | the same `open` again within 10 min                  | nothing: the runtime answers «already opened»      |
 * | the runtime's own (auto): a design the agent edits,  | its tab behind, with a dot; the pane stays as it is |
 * | a document it publishes, its browser, a file handed  |                                                    |
 * | a reload of a document published again               | the tab that shows it reloads, nothing opens       |
 * | an address in the agent's answer                     | a chip in the text: the pane opens on its click    |
 * | the person's click on a chip, a card, a view button   | its tab in front, the pane comes out               |
 *
 * The person busy in a tab of the open pane never loses it: an asked tab goes behind it too. On the phone the pane is
 * a sheet over the chat and never comes out by itself.
 */
export interface IIntentRequests {
  /** What the agent asked with `open`: in front, the pane comes out for it. */
  asked: IPaneRequest[];
  /** What the runtime asked by itself on seeing the agent work: behind, the pane stays as it is. */
  quiet: IPaneRequest[];
}

/** The requests of the intents a look applies, split by who asked: the agent, or the runtime by itself. */
export const requestsOf = (intents: readonly IPaneIntent[]): IIntentRequests => {
  const out: IIntentRequests = { asked: [], quiet: [] };
  for (const intent of intents) {
    const request = requestOf(intent.command);
    if (request) (isAuto(intent) ? out.quiet : out.asked).push(request);
  }
  return out;
};

/** Where a request goes in the new pane. */
export type IPaneRequest = { reload: boolean } & (
  | { k: 'show' }
  | { k: 'tools' }
  | { k: 'browser'; target: string | null }
  | { k: 'design'; target: string | null }
  | { k: 'files'; target: string | null }
  | { k: 'app'; target: string | null }
  | { k: 'plan' }
  | { k: 'agent-browser' }
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
    case 'app':
      return { k: 'app', target, reload };
    case 'plan':
      return { k: 'plan', reload };
    case 'agent-browser':
      return { k: 'agent-browser', reload };
    case 'subagents':
      return { k: 'subagent', reload };
    case 'terminal':
      return { k: 'terminal', reload };
    case 'side-chat':
      return { k: 'side', reload };
  }
  return isLegacyKind(command.tab) ? { k: 'legacy', kind: command.tab, target, reload } : null;
};
