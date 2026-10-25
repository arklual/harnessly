// Frames into stores: the Sink of every RealtimeClient. The client has already checked seq and epoch; here every
// frame merges by id and rev, so a replay, a resent snapshot or a snapshot after a restart changes only what is new.

import { noteServerTime, pageClock } from '@/next/core/clock';
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
import { defaultCwdStore, projectsStore } from '@/next/stores/projects';
import { machinesStore } from '@/next/stores/machines';
import { applyMessage, applyPromptState, forgetEndedElsewhere } from '@/next/stores/messages';
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
      projectsStore.set(e.project.id, { ...e.project, machineId: machine });
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
    machinesStore.set(machine, {
      machine: { ...w.machine, id: machine },
      runtime: 'v2',
      epoch: w.epoch,
      features: w.features,
      limits: w.limits,
    });
  },
  snapshot(machine, s) {
    if (s.ch === 'machine') {
      patchMachine(machine, () => ({ ...s.data.machine, id: machine }));
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
      for (const c of s.data.conversations) putConversation(machine, c);
      const projectIds = new Set(s.data.projects.map((p) => p.id));
      for (const p of [...projectsStore.values()]) {
        if (p.machineId === machine && !projectIds.has(p.id)) projectsStore.delete(p.id);
      }
      for (const p of s.data.projects) projectsStore.set(p.id, { ...p, machineId: machine });
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
