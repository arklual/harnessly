// The HTTP pages of a conversation (spec §2): older turns above the feed, a turn's steps when it unfolds, a tool's
// full output. Each merges into the stores by id, so what is on screen keeps its place.

import { gatewayPath } from '@/next/core/net/machine';
import { isMachineOff, keepsHistory, loadCentralChat } from '@/next/stores/central';
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { batch } from '@/next/core/store';
import { applyOlderTurns, applyTurnEntries, entryStore, key, turnStore } from '@/next/stores/conversation';
import { limitsOf } from '@/next/stores/machines';
import {
  parsePage,
  type ConvId,
  type EntryId,
  type MachineId,
  type AgentSession,
  type Dir,
  type LegacyTab,
  type OpenTarget,
  type ChatMade,
  type ConvContext,
  type HandoffDraft,
  type HandoffDraftRequest,
  type ChatDigest,
  type MessageSearch,
  type OutputPage,
  type RewindPlan,
  type Task,
  type TurnsPage,
  type Terminal,
  type TurnId,
} from '@/next/protocol/v2';

const enc = encodeURIComponent;

export async function loadOlderTurns(
  m: MachineId,
  conv: ConvId,
  before: TurnId,
  limit = 20,
): Promise<PageOutcome<true>> {
  // the older turns come from the cluster's copy whenever it keeps the machine's chats, on or off; the
  // machine is asked only for a chat the copy does not have yet, and only when it is there to answer.
  if (keepsHistory(m)) {
    const central = await loadCentralChat(conv, before, limit);
    // A page past what the copy keeps of a chat it does not hold whole yet: the rest is the machine's, when it is there.
    const pastCopy = 'ok' in central && central.value.turns.length === 0 && central.value.complete === false;
    if ('ok' in central && !(pastCopy && !isMachineOff(m))) {
      const { turns, entries, beforeTurnId } = central.value;
      batch(() => applyOlderTurns(conv, turns, entries, beforeTurnId));
      return { ok: true, value: true };
    }
    if (!('ok' in central) && isMachineOff(m)) return central;
  }
  const url = machineApi(m).http(`/conversations/${enc(conv)}/turns?before=${enc(before)}&limit=${limit}`);
  const res = await getPage(url, parsePage.turns, limitsOf(m).cmdTimeoutMs);
  if (!('ok' in res)) return res;
  batch(() => applyOlderTurns(conv, res.value.turns, res.value.entries, res.value.beforeTurnId));
  return { ok: true, value: true };
}

export async function loadEntries(m: MachineId, conv: ConvId, turn: TurnId): Promise<PageOutcome<true>> {
  // a turn's steps from the cluster's copy first; a turn the copy has no steps of yet (the one under way), or
  // fewer than the turn counts (a chat backfilled by pages without them), is the machine's to tell, when it is there.
  if (keepsHistory(m)) {
    const central = await getPage(
      gatewayPath(`/history/conversations/${enc(conv)}/turns/${enc(turn)}/entries`),
      parsePage.entries,
      limitsOf(m).cmdTimeoutMs,
    );
    const steps = turnStore.get(key(conv, turn))?.turn?.stepCount ?? 0;
    const held = 'ok' in central && central.value.entries.length > 0 && central.value.entries.length >= steps;
    if ('ok' in central && (held || isMachineOff(m))) {
      batch(() => applyTurnEntries(conv, central.value.turnId, central.value.entries));
      return { ok: true, value: true };
    }
    if (!('ok' in central) && isMachineOff(m)) return central;
  }
  const url = machineApi(m).http(`/conversations/${enc(conv)}/turns/${enc(turn)}/entries`);
  const res = await getPage(url, parsePage.entries, limitsOf(m).cmdTimeoutMs);
  if (!('ok' in res)) return res;
  batch(() => applyTurnEntries(conv, res.value.turnId, res.value.entries));
  return { ok: true, value: true };
}

/** The whole output of a tool entry; the entry in the store gets it, truncated no more. */
export async function loadOutput(
  m: MachineId,
  conv: ConvId,
  entry: EntryId,
): Promise<PageOutcome<OutputPage>> {
  const put = (page: OutputPage) =>
    entryStore.patch(key(conv, entry), (e) => (e && e.t === 'tool' ? { ...e, output: page.output } : e));
  // the cluster's copy keeps a tool's whole output (the agent sends it apart from the stream). The machine is
  // asked only for what the copy does not have whole yet (a tool that has just ended), and only when it is there.
  if (keepsHistory(m)) {
    const central = await getPage(
      gatewayPath(`/history/conversations/${enc(conv)}/entries/${enc(entry)}/output`),
      parsePage.output,
      limitsOf(m).cmdTimeoutMs,
    );
    if ('ok' in central && (central.value.output.truncated === null || isMachineOff(m))) {
      put(central.value);
      return central;
    }
    if (!('ok' in central) && isMachineOff(m)) return central;
  }
  const url = machineApi(m).http(`/conversations/${enc(conv)}/entries/${enc(entry)}/output`);
  const res = await getPage(url, parsePage.output, limitsOf(m).cmdTimeoutMs);
  if ('ok' in res) put(res.value);
  return res;
}

/** What going on from before the message that opened turn `entryId` would leave behind: the turns, the files. */
export const loadRewindPlan = (
  m: MachineId,
  conv: ConvId,
  entryId: TurnId,
): Promise<PageOutcome<RewindPlan>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/rewind?entryId=${enc(entryId)}`),
    parsePage.rewindPlan,
    limitsOf(m).cmdTimeoutMs,
  );

/**
 * The turns the conversation's rewind n (of its rewinds, oldest first) left behind, by their faces; not in the stores.
 * the cluster's copy keeps them (marked by the rewind's live feed_reset); the machine is asked only when the
 * copy has not got them and the machine is there.
 */
export async function loadRewound(m: MachineId, conv: ConvId, n: number): Promise<PageOutcome<TurnsPage>> {
  if (keepsHistory(m)) {
    const central = await getPage(
      gatewayPath(`/history/conversations/${enc(conv)}/rewinds/${n}/turns`),
      parsePage.turns,
      limitsOf(m).cmdTimeoutMs,
    );
    if (('ok' in central && central.value.turns.length > 0) || isMachineOff(m)) return central;
  }
  return getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/rewinds/${n}/turns`),
    parsePage.turns,
    limitsOf(m).cmdTimeoutMs,
  );
}

/** The folders the machine offers for a new project. */
export const loadDirs = (m: MachineId): Promise<PageOutcome<Dir[]>> =>
  getPage(machineApi(m).http('/dirs'), parsePage.dirs, limitsOf(m).cmdTimeoutMs);

/** The sessions terminal agents kept on the machine that no conversation holds. */
export const loadSessions = (m: MachineId): Promise<PageOutcome<AgentSession[]>> =>
  getPage(machineApi(m).http('/sessions'), parsePage.sessions, limitsOf(m).cmdTimeoutMs);

/** A conversation's own terminals, oldest first. */
export const loadTerminals = (m: MachineId, conv: ConvId): Promise<PageOutcome<Terminal[]>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/terminals`),
    parsePage.terminals,
    limitsOf(m).cmdTimeoutMs,
  );

/** A new shell in the conversation's folder; a conversation that has the most it may answers a conflict. */
export const openTerminal = (m: MachineId, conv: ConvId): Promise<PageOutcome<Terminal>> =>
  sendPage(
    'POST',
    machineApi(m).http(`/conversations/${enc(conv)}/terminals`),
    parsePage.terminal,
    limitsOf(m).cmdTimeoutMs,
  );

/** Ends one of the conversation's own terminals and its shell. */
export const closeTerminal = (m: MachineId, conv: ConvId, id: string): Promise<PageOutcome<null>> =>
  sendPage(
    'DELETE',
    machineApi(m).http(`/conversations/${enc(conv)}/terminals/${enc(id)}`),
    () => null,
    limitsOf(m).cmdTimeoutMs,
  );

/** The plan the agent keeps (Claude's tasks, Codex's plan), in the order it was made; no socket event: asked again as
 *  the turn moves. */
export const loadTasks = (m: MachineId, conv: ConvId): Promise<PageOutcome<Task[]>> =>
  getPage(machineApi(m).http(`/conversations/${enc(conv)}/tasks`), parsePage.tasks, limitsOf(m).cmdTimeoutMs);

/** How full the agent's window is. */
export const loadContext = (m: MachineId, conv: ConvId): Promise<PageOutcome<ConvContext>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/context`),
    parsePage.context,
    limitsOf(m).cmdTimeoutMs,
  );

/**
 * The messages of the machine's conversations that hold the words of the query. HRN-879: from the cluster's copy, so
 * the search works with the machine off; the machine's own index answers when the copy finds nothing or fails.
 */
export async function searchMessages(
  m: MachineId,
  query: string,
  limit = 40,
): Promise<PageOutcome<MessageSearch>> {
  if (keepsHistory(m)) {
    const central = await getPage(
      gatewayPath(`/history/search?q=${enc(query)}&limit=${limit}&machine=${enc(m)}`),
      parsePage.search,
      limitsOf(m).cmdTimeoutMs,
    );
    // Nothing found while the copy is still taking older chats: the machine's own index has them all.
    if (('ok' in central && central.value.hits.length > 0) || isMachineOff(m)) return central;
  }
  return getPage(
    machineApi(m).http(`/search?q=${enc(query)}&limit=${limit}`),
    parsePage.search,
    limitsOf(m).cmdTimeoutMs,
  );
}

/** A chat cut to the size of a context block for another agent, and the whole of it written to a file on the machine. */
export const loadChatDigest = (m: MachineId, conv: ConvId): Promise<PageOutcome<ChatDigest>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/digest`),
    parsePage.digest,
    limitsOf(m).cmdTimeoutMs * 3,
  );

/** Where a chip or a link of the conversation opens in the right pane: the runtime's rules, the agent's open command's. */
export const loadOpenTarget = (m: MachineId, conv: ConvId, ref: string): Promise<PageOutcome<OpenTarget>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/open?ref=${encodeURIComponent(ref)}`),
    parsePage.open,
    limitsOf(m).cmdTimeoutMs,
  );

/** What the chat made, opened or changed (documents, boards, designs, tracker tasks), newest first. */
export const loadMade = (m: MachineId, conv: ConvId): Promise<PageOutcome<ChatMade>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/made?tasks=1`),
    parsePage.made,
    limitsOf(m).cmdTimeoutMs,
  );

/** The Legacy tab the conversation runs in; null until its agent has a tmux session. */
export const loadLegacyTab = (m: MachineId, conv: ConvId): Promise<PageOutcome<LegacyTab | null>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/legacy`),
    parsePage.legacy,
    limitsOf(m).cmdTimeoutMs,
  );

/** How long a draft may take: an excerpt is at once, a note is written by the agent (the runtime gives it 200 s). */
export const HANDOFF_DRAFT_MS = 210_000;

/** The editable text or private full-history metadata a handoff draft carries. */
export const loadHandoffDraft = (
  m: MachineId,
  conv: ConvId,
  req: HandoffDraftRequest,
  timeoutMs = HANDOFF_DRAFT_MS,
): Promise<PageOutcome<HandoffDraft>> =>
  sendBody(
    'POST',
    machineApi(m).http(`/conversations/${enc(conv)}/handoff-draft`),
    req,
    parsePage.handoffDraft,
    timeoutMs,
  );
