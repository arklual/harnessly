// Frames into stores: the Sink of every RealtimeClient. The client has already checked seq and epoch; here every
// frame merges by id and rev, so a replay, a resent snapshot or a snapshot after a restart changes only what is new.

import { machineNow, noteServerTime, pageClock } from '@/next/core/clock';
import { noteCursor, noteLastEntry, resumeOf } from '@/next/core/realtime/cursors';
import type { Sink } from '@/next/core/realtime/client';
import { shownConnection } from '@/next/core/realtime/grace';
import { connectionStore } from '@/next/stores/connection';
import {
  applyBackground,
  applyConvSnapshot,
  applyEntryReplace,
  applyEntryUpdate,
  applyGateState,
  applyNow,
  applyPermission,
  applyQuestion,
  applyQueue,
  applyStep,
  applyTurn,
  dropConversationData,
  feedStore,
  markFeedError,
  removeBackground,
  resetFeed,
  setShots,
  setPictures,
} from '@/next/stores/conversation';
import { conversationsStore, convsCachedStore, convsLoadedStore } from '@/next/stores/conversations';
import { favoritesStore } from '@/next/stores/favorites';
import { applyRemoteDraft, applyRemoteDrafts } from '@/next/core/drafts-cloud';
import { defaultCwdStore, projectKey, projectsStore } from '@/next/stores/projects';
import { machineOrderStore, machinesStore } from '@/next/stores/machines';
import {
  applyMessage,
  applyPromptState,
  convMessagesStore,
  forgetEndedElsewhere,
  isTerminal,
  messagesStore,
  returnToQueue,
} from '@/next/stores/messages';
import { touchConversation } from '@/next/stores/cache';
import type {
  ConvChannel,
  ConvEvent,
  ConvId,
  Conversation,
  ConvsEvent,
  Machine,
  MachineEvent,
  MachineId,
} from '@/next/protocol/v2';

const convOf = (ch: ConvChannel): ConvId => ch.slice(5) as ConvId;

function patchMachine(id: MachineId, fn: (m: Machine) => Machine): void {
  machinesStore.patch(id, (v) => (v ? { ...v, machine: fn(v.machine) } : v));
}

/**
 * A conversation belongs to the machine whose channel first showed it: the id alone is not a key across machines, so
 * a frame of another machine never overwrites, patches or deletes a record, a feed or a message it does not own.
 */
function owns(machine: MachineId, conv: ConvId): boolean {
  const record = conversationsStore.get(conv);
  if (record && record.machineId !== machine) return false;
  const feed = feedStore.get(conv);
  return !feed || feed.machineId === machine;
}

function putConversation(machine: MachineId, c: Conversation): void {
  if (!owns(machine, c.id)) return;
  // The channel names the machine, not the record: a record that claims another one is stored as this machine's.
  const own = c.machineId === machine ? c : { ...c, machineId: machine };
  conversationsStore.patch(c.id, (have) => (have && have.rev >= own.rev ? have : own));
}

/** The fleet has loaded and no longer lists the machine: it was disabled or removed, nothing of it can answer. */
const goneFromFleet = (machine: MachineId): boolean => {
  const order = machineOrderStore.get();
  return order !== null && !order.includes(machine);
};

/**
 * GitHub #33. The same runtime data enrolled again under a new machine id: the browser still holds its chats under
 * the old id, and `owns` keeps the new machine out of them, so they wait for a machine that is never coming back.
 * The new machine takes such a chat over only when the old one is gone from the fleet and the record is the same
 * chat (made at the same moment in the same folder). An id that merely collides — the other machine still listed, or
 * a different chat behind the same id — stays with its owner.
 *
 * The cached feed goes (the new machine's snapshot draws it again). The chat's messages move with it; one the runtime
 * never confirmed is not sent again by itself: it goes back over the field as not sent, and Повторить sends it to the
 * new machine — the person's word, not a replay to another identity.
 */
function adoptStray(machine: MachineId, c: Conversation): void {
  const record = conversationsStore.get(c.id);
  const feed = feedStore.get(c.id);
  const was = record?.machineId ?? feed?.machineId;
  if (!was || was === machine || !goneFromFleet(was)) return;
  if (feed && feed.machineId !== was) return;
  if (!record) {
    // Only a drawing of the old machine's feed, nothing to prove it is this chat: the drawing goes, its messages stay.
    dropConversationData(c.id);
    return;
  }
  if (record.createdAt !== c.createdAt || record.cwd !== c.cwd) return;
  const at = machineNow(machine);
  for (const id of convMessagesStore.get(c.id) ?? []) {
    const m = messagesStore.get(id);
    if (!m || m.machineId !== was || m.conversationId !== c.id) continue;
    messagesStore.set(id, { ...m, machineId: machine });
    // A draft stays a draft and a failure keeps its reason; one on its way (or waiting to go) stops and waits.
    if (m.rev === 0 && !isTerminal(m.state) && m.state.k !== 'failed' && m.state.k !== 'draft')
      returnToQueue(id, at);
  }
  dropConversationData(c.id);
  conversationsStore.delete(c.id);
}

const ownRecord = (machine: MachineId, id: ConvId): boolean =>
  conversationsStore.get(id)?.machineId === machine;

export function applyMachineEvent(machine: MachineId, e: MachineEvent): void {
  if (e.type === 'machine_status') {
    // A source that does not judge the health (a gateway telling a reach) leaves the one the page holds.
    patchMachine(machine, (m) => ({
      ...m,
      reach: e.reach,
      runtime: e.runtime,
      agents: e.agents,
      ...(e.health ? { health: e.health } : {}),
    }));
  } else {
    patchMachine(machine, (m) => ({ ...m, load: e.load }));
  }
}

export function applyConvsEvent(machine: MachineId, e: ConvsEvent): void {
  switch (e.type) {
    case 'conversation_upsert':
      putConversation(machine, e.conv);
      return;
    case 'conversation_state':
      if (!ownRecord(machine, e.id)) return;
      conversationsStore.patch(e.id, (c) => (c && c.rev < e.rev ? { ...c, state: e.state, rev: e.rev } : c));
      return;
    case 'conversation_seen':
      if (!ownRecord(machine, e.id)) return;
      conversationsStore.patch(e.id, (c) =>
        c && c.rev < e.rev ? { ...c, seenAt: Math.max(c.seenAt, e.seenAt), rev: e.rev } : c,
      );
      return;
    case 'conversation_removed':
      if (ownRecord(machine, e.id)) conversationsStore.delete(e.id);
      return;
    case 'project_upsert':
      projectsStore.set(projectKey(machine, e.project.id), { ...e.project, machineId: machine });
      return;
    case 'favorites_upsert':
      favoritesStore.set(machine, e.favorites);
      return;
    case 'draft_upsert':
      applyRemoteDraft(machine, e.draft);
  }
}

export function applyConvEvent(machine: MachineId, conv: ConvId, e: ConvEvent): void {
  if (!owns(machine, conv)) return;
  const ch: ConvChannel = `conv:${conv}`;
  switch (e.type) {
    case 'turn_started':
    case 'turn_finished':
    case 'turn_failed':
      applyTurn(conv, e.turn);
      return;
    case 'quota_exhausted':
      conversationsStore.patch(conv, (c) => {
        if (!c) return c;
        const before = c.terminalFailure;
        if (
          before?.kind === e.failure.kind &&
          before.at === e.failure.at &&
          before.detail === e.failure.detail &&
          before.terminal === e.failure.terminal &&
          before.needsTarget === e.failure.needsTarget
        )
          return c;
        return { ...c, terminalFailure: e.failure };
      });
      return;
    case 'step':
      applyStep(conv, e.entry, e.counts);
      noteLastEntry(machine, ch, e.entry.id);
      return;
    case 'entry_update':
      applyEntryUpdate(conv, e.entry);
      noteLastEntry(machine, ch, e.entry.id);
      return;
    case 'entry_replace':
      applyEntryReplace(conv, e.oldId, e.entry);
      noteLastEntry(machine, ch, e.entry.id);
      return;
    case 'now': {
      const { type: _type, ...now } = e;
      applyNow(conv, now);
      return;
    }
    case 'question_asked':
      applyQuestion(conv, e.question);
      return;
    case 'permission_requested':
    case 'plan_proposed':
      applyPermission(conv, e.permission);
      return;
    case 'gate_updated':
      applyGateState(conv, e.id, e.state);
      return;
    case 'compaction':
      applyEntryUpdate(conv, e.entry);
      noteLastEntry(machine, ch, e.entry.id);
      return;
    case 'queue_changed':
      applyQueue(conv, e.queue);
      return;
    case 'prompt_state':
      applyPromptState(conv, machine, e.clientId, e.state, e.rev);
      return;
    case 'message_upsert':
      applyMessage(machine, e.message);
      return;
    case 'background_upsert':
      applyBackground(conv, e.task);
      return;
    case 'background_removed':
      removeBackground(conv, e.id);
      return;
    case 'shots_set':
      setShots(conv, e.shots);
      return;
    case 'pictures_set':
      setPictures(conv, e.pictures);
      return;
    case 'feed_reset':
      resetFeed(conv, e.turns, e.entries, e.beforeTurnId);
  }
}

/** The sink every machine's client writes into. */
export const storeSink: Sink = {
  welcome(machine, w) {
    noteServerTime(machine, w.serverTime);
    const orgName = machinesStore.get(machine)?.machine.orgName;
    machinesStore.set(machine, {
      machine: { ...w.machine, id: machine, ...(orgName ? { orgName } : {}) },
      runtime: 'v2',
      epoch: w.epoch,
      features: w.features,
      limits: w.limits,
    });
  },
  snapshot(machine, s) {
    if (s.ch === 'machine') {
      // The organization is the hub's word about the machine; the machine's own snapshot does not know it.
      patchMachine(machine, (m) => ({
        ...s.data.machine,
        id: machine,
        ...(m.orgName ? { orgName: m.orgName } : {}),
      }));
      return;
    }
    if (s.ch === 'convs') {
      // What the browser kept gives way to the machine's own list whatever revs it carried (a runtime that started over).
      if (convsCachedStore.has(machine)) {
        for (const c of [...conversationsStore.values()])
          if (c.machineId === machine) conversationsStore.delete(c.id);
        convsCachedStore.delete(machine);
      }
      const ids = new Set(s.data.conversations.map((c) => c.id));
      for (const c of [...conversationsStore.values()]) {
        if (c.machineId === machine && !ids.has(c.id)) conversationsStore.delete(c.id);
      }
      for (const c of s.data.conversations) {
        adoptStray(machine, c);
        putConversation(machine, c);
      }
      const projectIds = new Set(s.data.projects.map((p) => p.id));
      for (const p of [...projectsStore.values()]) {
        if (p.machineId === machine && !projectIds.has(p.id)) projectsStore.delete(projectKey(machine, p.id));
      }
      for (const p of s.data.projects)
        projectsStore.set(projectKey(machine, p.id), { ...p, machineId: machine });
      defaultCwdStore.set(machine, s.data.defaultCwd);
      if (s.data.favorites) favoritesStore.set(machine, s.data.favorites);
      else favoritesStore.delete(machine);
      applyRemoteDrafts(machine, s.data.drafts ?? []);
      convsLoadedStore.set(machine, true);
      return;
    }
    const conv = convOf(s.ch);
    if (!owns(machine, conv)) return;
    applyConvSnapshot(conv, machine, s.mode, s.data, pageClock().now());
    for (const m of s.data.messages) applyMessage(machine, m);
    forgetEndedElsewhere(conv, machine, new Set(s.data.messages.map((m) => m.clientId)));
    const last = [...s.data.entries].reverse().find((e) => !e.id.startsWith('live:'));
    if (last) noteLastEntry(machine, s.ch, last.id);
    touchConversation(conv);
  },
  event(machine, ev) {
    if (ev.ch === 'machine') applyMachineEvent(machine, ev.e);
    else if (ev.ch === 'convs') applyConvsEvent(machine, ev.e);
    else applyConvEvent(machine, convOf(ev.ch), ev.e);
  },
  channelError(machine, ch, code) {
    if (ch.startsWith('conv:') && (code === 'not-found' || code === 'bad-channel')) {
      if (!owns(machine, convOf(ch as ConvChannel))) return;
      markFeedError(convOf(ch as ConvChannel), 'not-found');
    }
  },
  resumeOf,
  noteCursor,
  connection(machine, c) {
    shownConnection(machine, c, (m, shown) => connectionStore.set(m, shown));
  },
};
