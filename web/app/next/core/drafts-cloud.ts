// The composers' drafts on the machine (text and uploaded files), so another device of the person sees what was
// typed, dictated or attached here (draft.set and draft_upsert on the convs channel). The field is the truth on this
// device: it is written to the machine a moment after it changes, with the next revision of its base; the machine
// takes a write only as the next revision, so a device that missed a write (a clear after a send, say) is refused and
// catches up instead of putting an older field back. A draft from elsewhere replaces the field only while the field is
// pristine; a field changed here keeps what it has and its next write wins. A machine whose runtime does not announce
// `drafts` keeps them on this device only, and nothing says so.
//
// A refusal as stale normally ends with the other device's draft arriving on the convs channel. When none arrives (the
// machine lost its newer revision: a restart before its saver ran, or the draft fell out of the ones it keeps), the
// page asks for the machine's whole list again and takes what the machine has as the base, keeping the field: what the
// person typed is written again as the machine's next revision.

import { pageClock } from '@/next/core/clock';
import { batch } from '@/next/core/store';
import { forgetOp, runCommand } from '@/next/commands/run';
import { hub } from '@/next/core/realtime/hub';
import {
  DRAFT_ATTACHMENTS_MAX,
  DRAFT_NEW,
  DRAFT_TEXT_MAX,
  type ConvId,
  type Draft,
  type MachineId,
} from '@/next/protocol/v2';
import { conversationsStore } from '@/next/stores/conversations';
import { draftsStore, NEW_CHAT, setDraft, type DraftKey as AnyDraftKey } from '@/next/stores/drafts';
import {
  draftBaseStore,
  fieldOf,
  judgeRemote,
  nextWrite,
  REMOTE,
  sameField,
  type DraftField,
} from '@/next/stores/draft-sync';
import { activeMachineStore, hasFeature, machinesStore } from '@/next/stores/machines';

/** A pause in typing this long writes the field. */
export const CLOUD_DELAY_MS = 1000;
/** While the person types without such a pause, the field is written once in this long. */
export const CLOUD_EVERY_MS = 2000;
/** How long a field refused as stale waits for the machine's current draft before it asks for the machine's list. */
export const BEHIND_MS = 4000;
/** The longest such wait, when the machine keeps refusing. */
export const BEHIND_MAX_MS = 60_000;
/** A write that did not get through (no network, a timeout) is tried again after this long. */
export const CLOUD_RETRY_MS = 5000;

interface IKeyState {
  timer: unknown;
  /** When the pending write is due at the latest (page time); null while none is pending. */
  dueBy: number | null;
  /** A write is on its way. */
  busy: boolean;
  /** The field changed while it was: write again after. */
  again: boolean;
  /** Refused as stale: the next write waits for the machine's current draft (an event or a snapshot). */
  behind: boolean;
  /** Ends the wait of `behind`. */
  behindTimer: unknown;
  /** Refusals as stale in a row: each waits twice as long as the one before. */
  conflicts: number;
  /** A field the machine refused for good (not about timing): it is not sent again until the field changes. */
  refused: string | null;
}

/** The fields the machine keeps: a chat's and the new chat's. A design's field (no chat yet) stays on this device. */
type DraftKey = ConvId | typeof NEW_CHAT;
const synced = (k: AnyDraftKey): k is DraftKey => !k.startsWith('design:');

const states = new Map<DraftKey, IKeyState>();
const stateOf = (k: DraftKey): IKeyState => {
  let st = states.get(k);
  if (!st) {
    st = {
      timer: null,
      dueBy: null,
      busy: false,
      again: false,
      behind: false,
      behindTimer: null,
      conflicts: 0,
      refused: null,
    };
    states.set(k, st);
  }
  return st;
};

/**
 * The writes this page sent, the last few of each field: a draft the machine shows that is one of them is this
 * page's own word coming back, never another device's. A write whose answer did not come (a loaded machine, the road)
 * leaves the base behind it; the field sent meanwhile is empty again, as pristine as that base, and its own echo would
 * otherwise «take» it — the words just sent come back into the field, files and all, and go a second time.
 */
const sentWrites = new Map<DraftKey, { rev: number; sig: string }[]>();
const SENT_KEPT = 8;

function noteSent(k: DraftKey, write: DraftField & { rev: number }): void {
  const list = sentWrites.get(k) ?? [];
  list.push({ rev: write.rev, sig: signature(write) });
  sentWrites.set(k, list.slice(-SENT_KEPT));
}

const isOwnEcho = (k: DraftKey, d: Draft): boolean => {
  const sig = signature(fieldOf(d));
  return (sentWrites.get(k) ?? []).some((w) => w.rev === d.rev && w.sig === sig);
};

const EMPTY: DraftField = { text: '', attachments: [] };
/** The field as it is now; a field without a draft is empty. */
const fieldNow = (k: DraftKey): DraftField => draftsStore.get(k) ?? EMPTY;
/** How a refused field is remembered: its text and the ids of its files. */
const signature = (f: DraftField): string => JSON.stringify([f.text, f.attachments.map((a) => a.id)]);

/** The machine a draft lives on: the conversation's, or for the new-chat field the active one. */
const machineOf = (k: DraftKey): MachineId | undefined =>
  k === NEW_CHAT ? (activeMachineStore.get() ?? undefined) : conversationsStore.get(k)?.machineId;

const syncs = (m: MachineId): boolean => hasFeature(m, 'drafts');

const tooLong = (f: DraftField): boolean =>
  [...f.text].length > DRAFT_TEXT_MAX || f.attachments.length > DRAFT_ATTACHMENTS_MAX;

function schedule(k: DraftKey, delay = CLOUD_DELAY_MS): void {
  const st = stateOf(k);
  const clock = pageClock();
  clock.clearTimeout(st.timer);
  st.timer = clock.setTimeout(() => {
    st.timer = null;
    st.dueBy = null;
    void push(k);
  }, delay);
}

/** The field changed: it is written after a pause in typing, and while the typing goes on, once in CLOUD_EVERY_MS. */
function scheduleTyped(k: DraftKey): void {
  const st = stateOf(k);
  const now = pageClock().now();
  const dueBy = st.dueBy ?? now + CLOUD_EVERY_MS;
  schedule(k, Math.max(0, Math.min(CLOUD_DELAY_MS, dueBy - now)));
  st.dueBy = dueBy;
}

/** Writes every field that waits for its pause now: the person left the field or the page. */
export function flushDrafts(): void {
  for (const [k, st] of states) if (st.timer !== null) schedule(k, 0);
}

function clearBehind(st: IKeyState): void {
  st.behind = false;
  pageClock().clearTimeout(st.behindTimer);
  st.behindTimer = null;
}

/**
 * The machine refused the write as stale. Its current draft is awaited on the convs channel; when none comes in time,
 * the machine's whole list is asked for (it is what rebases a field the machine fell behind of), and the write is
 * tried again whatever came.
 */
function waitForMachine(k: DraftKey, m: MachineId): void {
  const st = stateOf(k);
  const clock = pageClock();
  st.behind = true;
  st.conflicts += 1;
  clock.clearTimeout(st.behindTimer);
  const wait = Math.min(BEHIND_MAX_MS, BEHIND_MS * 2 ** (st.conflicts - 1));
  st.behindTimer = clock.setTimeout(() => {
    st.behindTimer = null;
    if (!st.behind) return;
    hub.client(m)?.resnapshot('convs');
    // Without an answer the flag does not hold the field for good: the next wait is longer.
    st.behindTimer = clock.setTimeout(() => {
      st.behindTimer = null;
      if (!st.behind) return;
      st.behind = false;
      schedule(k, 0);
    }, wait);
  }, wait);
}

async function push(k: DraftKey): Promise<void> {
  const st = stateOf(k);
  if (st.busy) {
    st.again = true;
    return;
  }
  const m = machineOf(k);
  if (!m || !syncs(m) || st.behind) return;
  const write = nextWrite(fieldNow(k), draftBaseStore.get(k));
  if (!write || tooLong(write) || signature(write) === st.refused) return;
  st.busy = true;
  noteSent(k, write);
  const handle = runCommand(m, 'draft.set', {
    key: k,
    text: write.text,
    ...(write.attachments.length > 0 ? { attachments: [...write.attachments] } : {}),
    ...(write.refs && write.refs.length > 0 ? { refs: [...write.refs] } : {}),
    rev: write.rev,
  });
  const end = await handle.done;
  forgetOp(handle.opId);
  st.busy = false;
  if (end.k === 'done') {
    st.conflicts = 0;
    const base = draftBaseStore.get(k);
    if (!base || base.rev < write.rev) draftBaseStore.set(k, write);
  } else if (end.failure.r === 'refused' && end.failure.code === 'conflict') {
    // Another device wrote first. Its draft is on the convs channel; it may already be here.
    const base = draftBaseStore.get(k);
    if (!base || base.rev < write.rev) waitForMachine(k, m);
    else st.again = true;
  } else if (end.failure.r === 'refused') {
    // The machine will not keep this draft (a refusal that is not about timing): it stays on this device.
    st.refused = signature(write);
    return;
  } else {
    schedule(k, CLOUD_RETRY_MS);
    return;
  }
  if (st.again) {
    st.again = false;
    schedule(k, 0);
  }
}

const keyOf = (key: Draft['key']): DraftKey => (key === DRAFT_NEW ? NEW_CHAT : (key as ConvId));

/** A draft the machine showed (event or snapshot), against the field of this device. */
export function applyRemoteDraft(machine: MachineId, d: Draft): void {
  const k = keyOf(d.key);
  // The new-chat field follows the machine it would start the chat on; a chat's field follows its own machine.
  if (machineOf(k) !== machine && (k === NEW_CHAT || conversationsStore.has(k))) return;
  const st = stateOf(k);
  const local = fieldNow(k);
  const remote = fieldOf(d);
  const judged = judgeRemote(local, draftBaseStore.get(k), d, st.busy);
  if (judged === REMOTE.ignore) return;
  // This page's own write coming back moves the base only: the field keeps what it has now (cleared by a send).
  const verdict = judged === REMOTE.take && isOwnEcho(k, d) ? REMOTE.keep : judged;
  clearBehind(st);
  const differs = !sameField(local, remote);
  batch(() => {
    // The base first: the field's change that follows is then already in step and writes nothing.
    draftBaseStore.set(k, { ...remote, rev: d.rev });
    // Text and files together: a clear after a send elsewhere leaves no files here under an empty text.
    if (verdict === REMOTE.take && differs)
      setDraft(k, {
        text: remote.text,
        attachments: [...remote.attachments],
        ...(remote.refs ? { refs: [...remote.refs] } : {}),
      });
  });
  if (verdict === REMOTE.keep && differs) schedule(k, 0);
}

/** The machine's drafts from its convs snapshot; then every field of it that changed here goes out. */
export function applyRemoteDrafts(machine: MachineId, list: readonly Draft[]): void {
  const waited = [...states].filter(([, st]) => st.behind).map(([k]) => k);
  for (const d of list) applyRemoteDraft(machine, d);
  const rebased = rebase(machine, list);
  resync(machine);
  // A field that waited for the machine has waited long enough: it goes now.
  for (const k of new Set([...waited, ...rebased]))
    if (!stateOf(k).behind && nextWrite(fieldNow(k), draftBaseStore.get(k))) schedule(k, 0);
}

/**
 * The machine's whole list against the bases of this device: a base the machine is behind of (it lost the revision, or
 * does not keep the draft any more) would have every write refused for good. The machine's revision becomes the base
 * and the field stays as it is, so what it holds goes out as the machine's next revision. Returns the rebased fields.
 */
function rebase(machine: MachineId, list: readonly Draft[]): DraftKey[] {
  const rebased: DraftKey[] = [];
  if (!syncs(machine)) return rebased;
  const remote = new Map(list.map((d) => [keyOf(d.key), d]));
  batch(() => {
    for (const k of [...draftBaseStore.keys()].filter(synced)) {
      const base = draftBaseStore.get(k);
      const st = stateOf(k);
      // A write on its way moves the base itself when it is answered.
      if (!base || st.busy || machineOf(k) !== machine) continue;
      const d = remote.get(k);
      if (d && d.rev >= base.rev) continue;
      if (d) draftBaseStore.set(k, { ...fieldOf(d), rev: d.rev });
      else draftBaseStore.delete(k);
      clearBehind(st);
      rebased.push(k);
    }
  });
  return rebased;
}

/** Every field of the machine that is ahead of its base is written (the machine came back, or now keeps drafts). */
function resync(machine: MachineId): void {
  if (!syncs(machine)) return;
  const keys = new Set<DraftKey>([...draftsStore.keys(), ...draftBaseStore.keys()].filter(synced));
  for (const k of keys)
    if (machineOf(k) === machine && !stateOf(k).busy && nextWrite(fieldNow(k), draftBaseStore.get(k)))
      schedule(k);
}

/** Starts writing the fields to their machines; returns the stop. */
export function startDraftSync(): () => void {
  const unDrafts = draftsStore.subscribeAll((changed) => {
    for (const k of changed) if (synced(k)) scheduleTyped(k);
  });
  const unMachines = machinesStore.subscribeAll((changed) => {
    for (const m of changed) resync(m);
  });
  const unLeave = watchLeave(flushDrafts);
  return () => {
    unDrafts();
    unMachines();
    unLeave();
    const clock = pageClock();
    for (const st of states.values()) {
      clock.clearTimeout(st.timer);
      clock.clearTimeout(st.behindTimer);
    }
    states.clear();
    sentWrites.clear();
  };
}

/** Calls fn when the person leaves a field (blur) or the page (hidden, pagehide); returns the stop. */
function watchLeave(fn: () => void): () => void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return () => {};
  const onHidden = () => {
    if (document.visibilityState === 'hidden') fn();
  };
  window.addEventListener('blur', fn, true);
  window.addEventListener('pagehide', fn);
  document.addEventListener('visibilitychange', onHidden);
  return () => {
    window.removeEventListener('blur', fn, true);
    window.removeEventListener('pagehide', fn);
    document.removeEventListener('visibilitychange', onHidden);
  };
}
