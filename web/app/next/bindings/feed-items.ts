// The open conversation's stores as the feed's items: a turn with its prompt, steps and answer, or a message the
// runtime has not shown yet. The feed draws a long conversation, so a change rebuilds only the items it touched: an
// item whose inputs are the same objects as last time is the same object, and the feed's rows do not render again.
import { useCallback, useMemo, useSyncExternalStore } from 'react';
import { toPageTime } from '@/next/core/clock';
import type {
  AutoMark,
  FeedItem,
  Message,
  ModelSwitchItem,
  NoticeEntry,
  OriginItem,
  OutgoingItem,
  PeerEntry,
  RewoundItem,
  SwitchModel,
  SayEntry,
  SubagentEntry,
  TurnItem,
  UserEntry,
} from '@/next/features/chat/types';
import type {
  AgentStateKind,
  AutoContinue,
  ConvId,
  Entry,
  MachineId,
  ModelSwitch,
  Now,
  RewindMark,
} from '@/next/protocol/v2';
import { connectionStore } from '@/next/stores/connection';
import {
  entryStore,
  fedStore,
  feedStore,
  gatesStore,
  key,
  nowStore,
  turnStore,
  type TurnView,
} from '@/next/stores/conversation';
import { conversationsStore } from '@/next/stores/conversations';
import {
  convMessagesStore,
  isPassingRefusal,
  messagesStore,
  waitsInQueue,
  type MessageView,
} from '@/next/stores/messages';

interface ICachedTurn {
  view: TurnView;
  prompt: UserEntry | null;
  woke: SubagentEntry | null;
  peer: PeerEntry | null;
  note: NoticeEntry | null;
  answer: SayEntry | null;
  steps: Entry[] | null;
  now: Now | null;
  wait: TurnItem['wait'];
  auto: AutoMark | undefined;
  item: TurnItem;
}

const NO_CLIENTS: ReadonlySet<string> = new Set();
/** The states of a message the agent is taking: typed, in its box, accepted but its entry not read yet. */
const TAKEN: ReadonlySet<string> = new Set(['typing', 'in-input', 'accepted']);

const sameList = <T>(a: readonly T[] | null, b: readonly T[] | null): boolean =>
  a === b || (!!a && !!b && a.length === b.length && a.every((x, i) => x === b[i]));

/**
 * Messages on their way and not in the queue strip: the feed shows them where they will land, at the end, until a
 * turn's prompt is the same message (by its client id, or the entry the runtime confirmed it as).
 */
const isOutgoing = (
  m: MessageView,
  agent: AgentStateKind | undefined,
  prompted: ReadonlySet<string>,
  fed: ReadonlySet<string>,
): boolean =>
  !fed.has(m.clientId) &&
  m.state.k !== 'cancelled' &&
  m.state.k !== 'done' &&
  m.state.k !== 'in-turn' &&
  !prompted.has(m.clientId) &&
  !(m.state.k === 'accepted' && m.state.entryId !== null && prompted.has(m.state.entryId)) &&
  !waitsInQueue(m, agent);

const isRefused = (m: MessageView): boolean => m.state.k === 'failed' && m.refused !== null;

/** The marks are shared, so an item whose message went on by itself is the same object while nothing changes. */
const MARKS = {
  'limit:own': { why: 'limit', own: true },
  'limit:sent': { why: 'limit', own: false },
  'restart:own': { why: 'restart', own: true },
  'restart:sent': { why: 'restart', own: false },
} as const satisfies Record<string, AutoMark>;

/**
 * Whether the runtime went on by itself with this message, and how: a message it wrote itself says so (`auto`, or
 * `own` in the conversation's list of such times once the message itself is gone from the page); the person's queued
 * message it sent at the reset is named only by that list. The words are never compared: they are the runtime's, and
 * the person can write the same.
 */
export const autoMarkOf = (
  clientId: string | null,
  message: Pick<MessageView, 'auto'> | undefined,
  goes: ReadonlyMap<string, AutoContinue>,
): AutoMark | undefined => {
  if (message?.auto) return MARKS[`${message.auto}:own`];
  const go = clientId === null ? undefined : goes.get(clientId);
  if (!go) return undefined;
  return go.own ? MARKS[`${go.why}:own`] : MARKS[`${go.why}:sent`];
};

const userOf = (entry: Entry): UserEntry | null => (entry.t === 'user' ? entry : null);

const sameWords = (a: string, b: string): boolean => a.trim() === b.trim();

/** Whether a turn that began after the message was written opened with the same words: it was delivered after all. */
const deliveredSince = (items: readonly FeedItem[], m: MessageView): boolean =>
  items.some(
    (item) =>
      item.kind === 'turn' &&
      item.prompt !== null &&
      item.turn.startedAt >= m.createdAt &&
      sameWords(item.prompt.text, m.text),
  );

/**
 * A refused message keeps its chronological place: before the first turn that began after it was written, not at the
 * end under whatever the agent did since. A message of the latest moment is at the end like any other.
 */
const placeOfRefused = (items: readonly FeedItem[], m: MessageView): number => {
  const at = items.findIndex((item) => item.kind === 'turn' && item.turn.startedAt > m.createdAt);
  return at === -1 ? items.length : at;
};

/**
 * A steer the runtime typed into the agent and the agent has not read: it is in the agent's own queue until the end of
 * its step, which may take minutes. The runtime confirms the read with the transcript entry (accepted), not before.
 */
export const isHanded = (m: MessageView): boolean => m.mode === 'steer' && m.state.k === 'in-input';

/**
 * Delivered, but the transcript has not shown it yet (the hook accepted it, no entry): it sits in the agent's own queue
 * and is read after the current step, minutes later. It stays in the feed, pending, until its user turn arrives.
 */
export const isAwaitingEntry = (m: MessageView): boolean =>
  m.state.k === 'accepted' && m.state.entryId === null;

const messageOf = (m: MessageView, handed: boolean): Message => ({
  clientId: m.clientId,
  text: m.text,
  attachments: m.attachments,
  refs: m.refs,
  mode: m.mode,
  createdAt: m.createdAt,
  stateSince: toPageTime(m.machineId, m.stateAt),
  state: m.state,
  handed,
  waitsMachine: m.issue !== null,
  refusal: m.refused,
  resendable: m.state.k === 'failed' && isPassingRefusal(m.refused),
});

/** A message on its way as the feed draws it, for a chat whose feed is not there yet (a design's first message). */
export const outgoingItem = (m: MessageView): OutgoingItem => ({
  kind: 'outgoing',
  key: m.clientId,
  message: messageOf(m, isHanded(m)),
});

const sameModel = (a: SwitchModel | null, b: SwitchModel | null): boolean =>
  a === b || (!!a && !!b && a.id === b.id && a.effort === b.effort && !!a.ultracode === !!b.ultracode);

/**
 * The applied changes of model among the turns: each stands before the first turn that started after it, so it reads
 * where it happened. One older than the first turn on screen belongs to history that is not loaded and waits for it.
 */
export const withSwitches = (
  turns: readonly FeedItem[],
  log: readonly ModelSwitch[],
  hasOlder: boolean,
  itemOf: (sw: ModelSwitch) => ModelSwitchItem,
): FeedItem[] => {
  if (log.length === 0) return [...turns];
  const out: FeedItem[] = [];
  const pending = [...log].sort((a, b) => a.at - b.at);
  let next = 0;
  for (const item of turns) {
    const startedAt = item.kind === 'turn' ? item.turn.startedAt : Infinity;
    while (next < pending.length && pending[next].at < startedAt) {
      if (out.length > 0 || !hasOlder) out.push(itemOf(pending[next]));
      next += 1;
    }
    out.push(item);
  }
  for (; next < pending.length; next += 1) out.push(itemOf(pending[next]));
  return out;
};

/** «Вернуть» stays this long after a rewind, fewer than the runtime allows: the page's clock may be off by a little. */
export const REWIND_UNDO_SHOWN_MS = 10_000;

/**
 * The rewinds among the turns: each folds into its line right after the last turn it kept (null: before the first),
 * where the turns it left behind stood. A turn of that id that started after the rewind is another turn of the same
 * place (Codex names turns by where they are in the transcript): the line is not drawn after it. A rewind whose turn
 * is not on screen belongs to history not loaded yet, or to a branch a later rewind left behind, and is not drawn.
 */
export const withRewinds = (
  items: readonly FeedItem[],
  marks: readonly RewindMark[],
  hasOlder: boolean,
  itemOf: (mark: RewindMark, n: number) => RewoundItem,
): FeedItem[] => {
  if (marks.length === 0) return [...items];
  const after = new Map<string, RewoundItem[]>();
  const head: RewoundItem[] = [];
  marks.forEach((mark, n) => {
    if (mark.afterTurnId === null) {
      if (!hasOlder) head.push(itemOf(mark, n));
      return;
    }
    const list = after.get(mark.afterTurnId) ?? [];
    list.push(itemOf(mark, n));
    after.set(mark.afterTurnId, list);
  });
  const out: FeedItem[] = [...head];
  for (const item of items) {
    out.push(item);
    if (item.kind !== 'turn') continue;
    for (const r of after.get(item.key) ?? []) if (item.turn.startedAt <= r.at) out.push(r);
  }
  return out;
};

/**
 * Where the conversation came from, in its place: after the turn a fork branched at when that turn is in the feed (a
 * fork of the agent's own session has the history up to it), else above everything once the feed reaches its start.
 */
export const withOrigin = (
  items: readonly FeedItem[],
  origin: OriginItem | null,
  atTurn: string | null,
  hasOlder: boolean,
): readonly FeedItem[] => {
  if (!origin) return items;
  const at = atTurn === null ? -1 : items.findIndex((item) => item.kind === 'turn' && item.key === atTurn);
  if (at >= 0) return [...items.slice(0, at + 1), origin, ...items.slice(at + 1)];
  return hasOlder ? items : [origin, ...items];
};

export class ItemsBuilder {
  private switches = new Map<number, ModelSwitchItem>();
  private rewinds = new Map<number, RewoundItem>();
  private turns = new Map<string, ICachedTurn>();
  private gateEntries = new Map<string, Entry>();
  private outgoing = new Map<string, { source: MessageView; handed: boolean; item: OutgoingItem }>();
  private sent = new Map<string, { source: MessageView; entry: UserEntry }>();
  private last: FeedItem[] | null = null;
  private stale = true;

  constructor(
    private conv: ConvId,
    private machine: MachineId,
  ) {}

  invalidate(): void {
    this.stale = true;
  }

  /** The items as of now; the same array until something they read changes. */
  snapshot(): FeedItem[] | null {
    if (this.stale) {
      this.last = this.build();
      this.stale = false;
    }
    return this.last;
  }

  private build(): FeedItem[] | null {
    const feed = feedStore.get(this.conv);
    if (!feed?.loaded) return null;
    const now = nowStore.get(this.conv) ?? null;
    const away = connectionStore.get(this.machine)?.k;
    const stalled = away === 'stale' || away === 'offline';
    const items: FeedItem[] = [];
    const seen = new Set<string>();
    const goes = new Map<string, AutoContinue>(
      (conversationsStore.get(this.conv)?.autoContinues ?? []).map((a) => [a.clientId, a]),
    );
    const prompted = new Set<string>();
    // A message the agent is taking now: the turn it opens is its own, and the turn row can come a moment before the
    // message's entry. The row waits for it, so the message always stands above the turn that answers it and the live
    // indicator is never drawn over it.
    const fed = fedStore.get(this.conv) ?? NO_CLIENTS;
    const taking = (convMessagesStore.get(this.conv) ?? []).some((id) => {
      const m = messagesStore.get(id);
      return !!m && !fed.has(id) && TAKEN.has(m.state.k);
    });
    for (const row of feed.rows) {
      if (row.k !== 'turn') continue;
      const view = turnStore.get(key(this.conv, row.id));
      // An entry of a turn arrived before the turn: it joins the feed with turn_started or the next snapshot.
      if (!view?.turn) continue;
      if (taking && view.turn.status === 'live' && !entryStore.has(key(this.conv, row.id))) continue;
      seen.add(row.id);
      const item = this.turnItem(row.id, view, now, stalled && view.turn.status === 'live', goes);
      if (item.prompt) prompted.add(item.prompt.id);
      if (item.prompt?.clientId) prompted.add(item.prompt.clientId);
      items.push(item);
    }
    for (const id of [...this.turns.keys()]) if (!seen.has(id)) this.turns.delete(id);
    for (const id of [...this.sent.keys()]) if (!seen.has(id)) this.sent.delete(id);
    const log = conversationsStore.get(this.conv)?.modelLog ?? [];
    const marks = conversationsStore.get(this.conv)?.rewinds ?? [];
    const placed = withRewinds(
      withSwitches(items, log, feed.beforeTurnId !== null, (sw) => this.switchItem(sw)),
      marks,
      feed.beforeTurnId !== null,
      (mark, n) => this.rewoundItem(mark, n, n === marks.length - 1),
    );
    items.length = 0;
    items.push(...placed);

    const kept = new Set<string>();
    const agent = conversationsStore.get(this.conv)?.state.k;
    // A message the feed holds as an entry — a prompt, or steered into a turn — is drawn there, once.
    for (const id of convMessagesStore.get(this.conv) ?? []) {
      const m = messagesStore.get(id);
      if (!m || !isOutgoing(m, agent, prompted, fed)) continue;
      // The runtime's own «go on» that failed is the runtime's to try again: it is not the person's to retry.
      if (m.auto && m.state.k === 'failed') continue;
      const refusal = isRefused(m);
      // A refused message whose words were delivered later is answered: it does not stand there for ever.
      if ((refusal || isAwaitingEntry(m)) && deliveredSince(items, m)) continue;
      kept.add(id);
      const handed = isHanded(m) || isAwaitingEntry(m);
      const auto = autoMarkOf(id, m, goes);
      const have = this.outgoing.get(id);
      let item: OutgoingItem;
      if (have && have.source === m && have.handed === handed && have.item.auto === auto) item = have.item;
      else {
        item = { kind: 'outgoing', key: id, message: messageOf(m, handed), ...(auto && { auto }) };
        this.outgoing.set(id, { source: m, handed, item });
      }
      if (refusal) items.splice(placeOfRefused(items, m), 0, item);
      else items.push(item);
    }
    for (const id of [...this.outgoing.keys()]) if (!kept.has(id)) this.outgoing.delete(id);

    return this.last && sameList(this.last, items) ? this.last : items;
  }

  /** The same object for the same rewind; only the last one can be taken back. */
  private rewoundItem(mark: RewindMark, n: number, last: boolean): RewoundItem {
    const undoUntil = last
      ? toPageTime(this.machine, Math.min(mark.undoUntil, mark.at + REWIND_UNDO_SHOWN_MS))
      : null;
    const have = this.rewinds.get(n);
    if (
      have &&
      have.at === mark.at &&
      have.turns === mark.turns &&
      have.files === mark.files &&
      have.undoUntil === undoUntil
    )
      return have;
    const item: RewoundItem = {
      kind: 'rewound',
      key: `rewound:${mark.at}`,
      n,
      at: mark.at,
      turns: mark.turns,
      files: mark.files,
      undoUntil,
    };
    this.rewinds.set(n, item);
    return item;
  }

  /** The same object for the same change, so the feed's rows do not render again. */
  private switchItem(sw: ModelSwitch): ModelSwitchItem {
    const have = this.switches.get(sw.at);
    if (have && sameModel(have.from, sw.from) && sameModel(have.to, sw.to) && have.refused === sw.refused)
      return have;
    const item: ModelSwitchItem = {
      kind: 'switch',
      key: `switch:${sw.at}`,
      at: sw.at,
      from: sw.from,
      to: sw.to,
      ...(sw.refused && { refused: sw.refused }),
    };
    this.switches.set(sw.at, item);
    return item;
  }

  /**
   * A permission has its place among the steps of its turn. The runtime marks it with a `gate` entry; when it has not
   * (yet), the permission stands at the end of its turn all the same, so a card is never missing, and when it is decided
   * the card stays as its answer.
   */
  private unmarkedGates(turnId: string, entries: readonly Entry[]): Entry[] {
    const gates = gatesStore.get(this.conv);
    if (!gates) return [];
    const out: Entry[] = [];
    for (const p of gates.permissions) {
      if (p.turnId !== turnId) continue;
      if (entries.some((e) => e.t === 'gate' && e.permissionId === p.id)) continue;
      let entry = this.gateEntries.get(p.id);
      if (!entry) {
        entry = { t: 'gate', permissionId: p.id, id: `gate:${p.id}`, turnId, at: p.askedAt, rev: 0 };
        this.gateEntries.set(p.id, entry);
      }
      out.push(entry);
    }
    return out;
  }

  /**
   * The prompt of a turn whose user entry the snapshot left out to keep within its size (a message with pictures the
   * transcript holds inline): our own message that opened the turn stands for it, with the pictures as they were sent.
   */
  private sentPrompt(turnId: string): UserEntry | null {
    for (const clientId of convMessagesStore.get(this.conv) ?? []) {
      const m = messagesStore.get(clientId);
      if (!m || m.state.k !== 'done' || m.state.turnId !== turnId) continue;
      const have = this.sent.get(turnId);
      if (have && have.source === m) return have.entry;
      const entry: UserEntry = {
        t: 'user',
        id: turnId,
        turnId,
        at: m.createdAt,
        rev: 0,
        text: m.text,
        attachments: m.attachments,
        clientId: m.clientId,
      };
      this.sent.set(turnId, { source: m, entry });
      return entry;
    }
    return null;
  }

  private turnItem(
    id: string,
    view: TurnView,
    now: Now | null,
    stalled: boolean,
    goes: ReadonlyMap<string, AutoContinue>,
  ): TurnItem {
    const turn = view.turn!;
    const promptEntry = entryStore.get(key(this.conv, id));
    const prompt = promptEntry ? userOf(promptEntry) : this.sentPrompt(id);
    const answerEntry = turn.answerEntryId ? entryStore.get(key(this.conv, turn.answerEntryId)) : undefined;
    const answer = answerEntry?.t === 'say' ? answerEntry : null;
    const woke = promptEntry?.t === 'subagent' ? promptEntry : null;
    const peer = promptEntry?.t === 'peer' ? promptEntry : null;
    const note = promptEntry?.t === 'notice' ? promptEntry : null;
    const entries = view.entryIds
      .filter((x) => x !== id && x !== turn.answerEntryId)
      .map((x) => entryStore.get(key(this.conv, x)))
      .filter((e): e is Entry => e !== undefined);
    entries.push(...this.unmarkedGates(id, entries));
    // A finished turn that has steps but none loaded yet is folded with its answer only: they load when it unfolds.
    const notLoaded = turn.status !== 'live' && turn.stepCount - (woke ? 1 : 0) > 0 && entries.length === 0;
    const steps = notLoaded ? null : entries;
    const nowOfTurn = now && now.turnId === id ? now : null;
    const wait = stalled ? 'machine' : null;
    const promptId = prompt?.clientId ?? null;
    const auto = autoMarkOf(promptId, promptId === null ? undefined : messagesStore.get(promptId), goes);

    const have = this.turns.get(id);
    if (
      have &&
      have.view.turn === turn &&
      have.prompt === prompt &&
      have.woke === woke &&
      have.peer === peer &&
      have.note === note &&
      have.answer === answer &&
      have.now === nowOfTurn &&
      have.wait === wait &&
      have.auto === auto &&
      sameList(have.steps, steps)
    ) {
      return have.item;
    }
    const item: TurnItem = {
      kind: 'turn',
      key: id,
      turn,
      prompt,
      woke,
      peer,
      note,
      steps,
      answer,
      now: nowOfTurn,
      wait,
      endNote: null,
      ...(auto && { auto }),
    };
    this.turns.set(id, { view, prompt, woke, peer, note, answer, steps, now: nowOfTurn, wait, auto, item });
    return item;
  }
}

/**
 * The feed's items for a conversation; null until the page has anything of it (the feed then draws its skeleton).
 * A lost connection changes only the `wait` of the live turn: the items are never dropped for it.
 */
export function useFeedItems(conv: ConvId, machine: MachineId): FeedItem[] | null {
  const builder = useMemo(() => new ItemsBuilder(conv, machine), [conv, machine]);

  const subscribe = useCallback(
    (notify: () => void) => {
      const bump = () => {
        builder.invalidate();
        notify();
      };
      const own = (keys: ReadonlySet<string>) => {
        for (const k of keys) if (k.startsWith(`${conv} `)) return true;
        return false;
      };
      const offs = [
        feedStore.subscribeKey(conv, bump),
        nowStore.subscribeKey(conv, bump),
        gatesStore.subscribeKey(conv, bump),
        fedStore.subscribeKey(conv, bump),
        convMessagesStore.subscribeKey(conv, bump),
        connectionStore.subscribeKey(machine, bump),
        conversationsStore.subscribeKey(conv, bump),
        turnStore.subscribeAll((keys) => own(keys) && bump()),
        entryStore.subscribeAll((keys) => own(keys) && bump()),
        messagesStore.subscribeAll((ids) => {
          const list = convMessagesStore.get(conv) ?? [];
          for (const id of ids) if (list.includes(id)) return bump();
        }),
      ];
      return () => offs.forEach((off) => off());
    },
    [builder, conv, machine],
  );

  const snapshot = useCallback(() => builder.snapshot(), [builder]);
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
