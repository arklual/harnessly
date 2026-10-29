// Where a message is, by the runtime's word (GET /api/v2/conversations/{id}/messages?ids=…). The runtime leads every
// message and the page mirrors its events; when the events did not come — the send was answered from the runtime's
// memory of its opId after an F5 while the message's frames went out long before, a socket missed them — the page
// would say «дольше обычного» for ever. So it asks, by clientId, about the messages that should have moved and did not:
// a send answered without its message's event, a send without an answer for 30 s, a message typed into the agent and
// still not taken past the runtime's own 30 s term — a steer too, every 30 s while it waits in the agent's queue. The
// answer is taken as it is: known — its record, by rev; expired — delivered long ago, the copy goes; unknown — the
// runtime does not have it, and it goes back to the queue over the field with Повторить, never lost. An answer that
// does not come (an older runtime, the road) decides nothing: the message is asked about again later. The runtime
// keeps the terms; the page only asks.

import { machineNow, pageClock, toPageTime } from '@/next/core/clock';
import { getPage } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { batch } from '@/next/core/store';
import { connectionStore } from '@/next/stores/connection';
import { limitsOf } from '@/next/stores/machines';
import {
  applyMessage,
  forgetMessage,
  isTerminal,
  messagesStore,
  returnToQueue,
  type MessageView,
} from '@/next/stores/messages';
import { opsStore } from '@/next/stores/ops';
import {
  CAPS_QUERY,
  parsePage,
  type ClientId,
  type ConvId,
  type MachineId,
  type MessageStatus,
} from '@/next/protocol/v2';

/** A send answered without its message's event is asked about this soon: the event had its chance to come first. */
export const STATUS_SOON_MS = 1500;
/** A send without an answer, or a message the runtime should have moved by its own term (30 s), is asked about then. */
export const STATUS_AFTER_MS = 30_000;
/** Past the runtime's term of a typed message, the page waits this much more for its event before it asks. */
const STATUS_GRACE_MS = 5000;
/** One message is asked about at most this often. */
export const STATUS_EVERY_MS = 30_000;
/** At most this many client ids in one question (wire.MessageStatusMax). */
const STATUS_MAX = 20;

const asked = new Map<ClientId, number>();

/** When (page time) the page asks about m next; null: it has nothing to ask (the runtime's events move it). */
function dueAt(m: MessageView): number | null {
  if (!m.conversationId) return null;
  let at: number | null = null;
  if (m.rev === 0 && m.state.k === 'submitting') {
    const op = opsStore.get(m.clientId);
    // A send on its way is the outbox's; one the runtime answered is asked about soon, one unanswered after 30 s.
    if (op?.k === 'pending') return null;
    at = op?.k === 'done' ? op.at + STATUS_SOON_MS : toPageTime(m.machineId, m.stateAt) + STATUS_AFTER_MS;
  } else if (
    m.rev > 0 &&
    (m.state.k === 'typing' ||
      m.state.k === 'in-input' ||
      m.state.k === 'queued' ||
      (m.state.k === 'failed' && m.state.retryable))
  ) {
    // a failed one the runtime may have queued again (a retry whose frame the page missed) — the strip showed
    // «Не удалось отправить» over a head the runtime held paused, with no way on.
    // a queued one too — it stands in the strip over the field until a frame moves it, and a chat whose
    // frames stopped (its channel refused on a loaded machine) would keep a row the agent answered minutes ago.
    // A steer waits in the agent's own queue for minutes, and is asked about all that while: the frame that took it
    // into the turn may be the one the page missed, and the strip over the field must not hold it on a guess.
    at = toPageTime(m.machineId, m.stateAt) + STATUS_AFTER_MS + STATUS_GRACE_MS;
  }
  if (at === null) return null;
  const last = asked.get(m.clientId);
  return last === undefined ? at : Math.max(at, last + STATUS_EVERY_MS);
}

/** The runtime's answer for a machine's messages, each taken as it is. */
export function applyStatuses(machine: MachineId, statuses: readonly MessageStatus[]): void {
  batch(() => {
    for (const s of statuses) {
      const m = messagesStore.get(s.clientId);
      if (!m || m.machineId !== machine) continue;
      if (s.k === 'known') applyMessage(machine, s.message);
      else if (s.k === 'expired') forgetMessage(s.clientId);
      else if (!isTerminal(m.state) && m.state.k !== 'failed') returnToQueue(s.clientId, machineNow(machine));
    }
  });
}

/** Asks the runtime about some of a conversation's messages; false when no answer came (nothing is decided then). */
export async function askStatus(
  machine: MachineId,
  conv: ConvId,
  ids: readonly ClientId[],
): Promise<boolean> {
  const list = ids.slice(0, STATUS_MAX).map(encodeURIComponent).join(',');
  const url = machineApi(machine).http(
    `/conversations/${encodeURIComponent(conv)}/messages?ids=${list}&${CAPS_QUERY}`,
  );
  const res = await getPage(url, parsePage.messageStatus, limitsOf(machine).cmdTimeoutMs);
  if (!('ok' in res)) return false;
  applyStatuses(machine, res.value.messages);
  return true;
}

/** Asks about every message that is due, a question per conversation; a machine that is not live is asked later. */
function askDue(): void {
  const now = pageClock().now();
  const groups = new Map<string, { machine: MachineId; conv: ConvId; ids: ClientId[] }>();
  for (const m of messagesStore.values()) {
    const at = dueAt(m);
    if (at === null || at > now || !m.conversationId) continue;
    asked.set(m.clientId, now);
    if (connectionStore.get(m.machineId)?.k !== 'live') continue;
    const k = `${m.machineId} ${m.conversationId}`;
    const g = groups.get(k) ?? { machine: m.machineId, conv: m.conversationId, ids: [] };
    if (g.ids.length < STATUS_MAX) g.ids.push(m.clientId);
    groups.set(k, g);
  }
  for (const g of groups.values()) void askStatus(g.machine, g.conv, g.ids);
}

/** Keeps one timer at the next due question while some message is late; stopping it forgets what was asked. */
export function startMessageStatus(): () => void {
  const clock = pageClock();
  let timer: unknown = null;
  let timerAt = Infinity;
  const schedule = () => {
    let next = Infinity;
    for (const m of messagesStore.values()) {
      const at = dueAt(m);
      if (at !== null && at < next) next = at;
    }
    if (next === timerAt) return;
    clock.clearTimeout(timer);
    timer = null;
    timerAt = next;
    if (next === Infinity) return;
    timer = clock.setTimeout(
      () => {
        timer = null;
        timerAt = Infinity;
        askDue();
        schedule();
      },
      Math.max(0, next - clock.now()),
    );
  };
  const offMessages = messagesStore.subscribeAll((changed) => {
    for (const id of changed) if (!messagesStore.has(id)) asked.delete(id);
    schedule();
  });
  const offOps = opsStore.subscribeAll(schedule);
  schedule();
  return () => {
    offMessages();
    offOps();
    clock.clearTimeout(timer);
    asked.clear();
  };
}
