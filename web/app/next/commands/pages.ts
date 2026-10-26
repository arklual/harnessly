// The HTTP pages of a conversation (spec §2): older turns above the feed, a turn's steps when it unfolds, a tool's
// full output. Each merges into the stores by id, so what is on screen keeps its place.

import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { batch } from '@/next/core/store';
import { applyOlderTurns, applyTurnEntries, entryStore, key } from '@/next/stores/conversation';
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
  const url = machineApi(m).http(`/conversations/${enc(conv)}/turns?before=${enc(before)}&limit=${limit}`);
  const res = await getPage(url, parsePage.turns, limitsOf(m).cmdTimeoutMs);
  if (!('ok' in res)) return res;
  batch(() => applyOlderTurns(conv, res.value.turns, res.value.entries, res.value.beforeTurnId));
  return { ok: true, value: true };
}

export async function loadEntries(m: MachineId, conv: ConvId, turn: TurnId): Promise<PageOutcome<true>> {
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
  const url = machineApi(m).http(`/conversations/${enc(conv)}/entries/${enc(entry)}/output`);
  const res = await getPage(url, parsePage.output, limitsOf(m).cmdTimeoutMs);
  if ('ok' in res) {
    entryStore.patch(key(conv, entry), (e) => (e && e.t === 'tool' ? { ...e, output: res.value.output } : e));
  }
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

/** The turns the conversation's rewind n (of its rewinds, oldest first) left behind, by their faces; not in the stores. */
export const loadRewound = (m: MachineId, conv: ConvId, n: number): Promise<PageOutcome<TurnsPage>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/rewinds/${n}/turns`),
    parsePage.turns,
    limitsOf(m).cmdTimeoutMs,
  );

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

/** The messages of the machine's conversations that hold the words of the query. */
export const searchMessages = (
  m: MachineId,
  query: string,
  limit = 40,
): Promise<PageOutcome<MessageSearch>> =>
  getPage(
    machineApi(m).http(`/search?q=${enc(query)}&limit=${limit}`),
    parsePage.search,
    limitsOf(m).cmdTimeoutMs,
  );

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

/** The Legacy tab the conversation runs in; not found until its agent has a tmux session. */
export const loadLegacyTab = (m: MachineId, conv: ConvId): Promise<PageOutcome<LegacyTab>> =>
  getPage(
    machineApi(m).http(`/conversations/${enc(conv)}/legacy`),
    parsePage.legacy,
    limitsOf(m).cmdTimeoutMs,
  );

/** How long a draft may take: an excerpt is at once, a note is written by the agent (the runtime gives it 200 s). */
export const HANDOFF_DRAFT_MS = 210_000;

/** The text a handoff (an excerpt or the agent's note) or a fork's merge (its result) would carry, to read and edit. */
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
