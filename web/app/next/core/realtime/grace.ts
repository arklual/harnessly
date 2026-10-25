// The first connection after a page load. A socket that is still on its way — or whose first try did not
// make it and the second is a few seconds off — is not a machine that went away: «Машина не отвечает», «Ожидание
// машины» and the «Нет связи» pill would claim a loss that is only the page starting. For FIRST_LINK_GRACE_MS from the
// first word about a machine's link, and only until it has been live once, a failed try of the network or of the
// gateway reads as «connecting»; a real loss (it was live and dropped), a refusal (unauthorized, proto) and a connect
// that is still not there after the grace read as what they are.

import { pageClock } from '@/next/core/clock';
import type { Connection, MachineId, Ms } from '@/next/protocol/v2';

export const FIRST_LINK_GRACE_MS = 8000;
let graceMs = FIRST_LINK_GRACE_MS;

/** Tests that look at the socket's raw states put the grace away (0) and back (FIRST_LINK_GRACE_MS). */
export function setLinkGrace(ms: number): void {
  graceMs = ms;
}

const QUIET_CAUSES: ReadonlySet<string> = new Set(['network', 'machine-unreachable']);
const firstHeardAt = new Map<MachineId, Ms>();
const wasLive = new Set<MachineId>();
const timers = new Map<MachineId, unknown>();
const latest = new Map<MachineId, Connection>();

const CONNECTING: Connection = { k: 'connecting', attempt: 1 };

/** What the page shows of the socket's `c`: the same, or «connecting» while the first connect is in its grace. */
export function shownConnection(
  machine: MachineId,
  c: Connection,
  show: (machine: MachineId, c: Connection) => void,
): void {
  const clock = pageClock();
  latest.set(machine, c);
  if (c.k === 'live') {
    wasLive.add(machine);
    clock.clearTimeout(timers.get(machine));
    timers.delete(machine);
    show(machine, c);
    return;
  }
  if (wasLive.has(machine)) {
    show(machine, c);
    return;
  }
  const now = clock.now();
  const first = firstHeardAt.get(machine) ?? now;
  firstHeardAt.set(machine, first);
  const left = first + graceMs - now;
  if (c.k !== 'offline' || !QUIET_CAUSES.has(c.cause) || left <= 0) {
    show(machine, c);
    return;
  }
  show(machine, CONNECTING);
  if (timers.has(machine)) return;
  // When the grace is over and the link is still not there, the page says what the socket says.
  timers.set(
    machine,
    clock.setTimeout(() => {
      timers.delete(machine);
      const now = latest.get(machine);
      if (now && now.k !== 'live') show(machine, now);
    }, left),
  );
}

export function resetLinkGrace(): void {
  for (const t of timers.values()) pageClock().clearTimeout(t);
  timers.clear();
  firstHeardAt.clear();
  wasLive.clear();
  latest.clear();
}
