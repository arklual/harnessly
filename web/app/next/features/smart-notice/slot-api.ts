// The machine's open slot of the smart notice (cmd/runtime/routes_notices.go): what apps and agents ask the person,
// read every second and a half while the page is visible and it moves (slower while quiet), answered here; and the app view's side (hl.notice), which
// asks through its view session.
import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react';
import { calmPoll } from '@/next/core/net/calm-poll';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson, RuntimeError } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';
import { openSlotStream, SLOT_LIVE_MS } from './slot-stream';
import { SLOT_ANSWER, type ISlotNotice, type ISlotSource, type TSlotAnswer } from './slot-body';

export const SLOT_POLL_MS = 1500;
/** The longest step while the slot stays the same: the conversations moving bring it back to the base. */
export const SLOT_QUIET_MS = 15_000;

interface ISlotList {
  notices: ISlotNotice[];
  muted: ISlotSource[];
}

const loadSlot = (m: MachineId) => runtimeJson<ISlotList>(m, platformPath('/notices'));

/** The person's answer; a text that looks like a key comes back as secret and the notice stays open. */
export const answerSlot = async (m: MachineId, id: string, value: unknown): Promise<TSlotAnswer> => {
  try {
    await runtimeJson<object>(m, platformPath('/notices/answer'), postJson({ id, value }));
    return SLOT_ANSWER.ok;
  } catch (failure) {
    const said =
      failure instanceof RuntimeError ? (failure.body as { error?: string } | undefined) : undefined;
    return said?.error === 'looks-like-key' ? SLOT_ANSWER.secret : SLOT_ANSWER.failed;
  }
};

export const dismissSlot = async (m: MachineId, id: string): Promise<void> => {
  await runtimeJson<object>(m, platformPath('/notices/answer'), postJson({ id, dismiss: true }));
};

/** Turns a source off (off=true) or back on; its open notices end as muted. */
export const muteSlotSource = async (m: MachineId, source: ISlotSource, off: boolean): Promise<void> => {
  await runtimeJson<object>(m, platformPath('/notices/mute'), postJson({ source, off }));
};

/** A call of an app's view (hl.notice.*), made with its view session. */
export const appNotice = <T>(m: MachineId, body: Record<string, unknown>): Promise<T> =>
  runtimeJson<T>(m, platformPath('/notices/app'), postJson(body));

export interface ISlotRead {
  /** The open notices the page shows (the queued ones wait behind their source's first). */
  notices: readonly ISlotNotice[];
  /** Takes a notice off at once (answered or turned down here), before the next read says so. */
  drop: (id: string) => void;
}

const same = (a: readonly ISlotNotice[], b: readonly ISlotNotice[]) =>
  a.length === b.length &&
  a.every((x, i) => {
    const y = b[i];
    return (
      y !== undefined &&
      x.id === y.id &&
      x.progress === y.progress &&
      x.title === y.title &&
      x.text === y.text
    );
  });

const NO_NOTICES: readonly ISlotNotice[] = [];

export const useSlotNotices = (machine: MachineId | null): ISlotRead => {
  const [read, setRead] = useState<{ machine: MachineId; notices: readonly ISlotNotice[] } | null>(null);
  const [dropped, setDropped] = useState<readonly string[]>([]);

  const last = useRef<{ machine: MachineId; notices: readonly ISlotNotice[] } | null>(null);

  /** One read; true when the slot changed. */
  const look = useEffectEvent(async (m: MachineId): Promise<boolean> => {
    const { notices } = await loadSlot(m);
    const shown = notices.filter((n) => !n.queued);
    const was = last.current;
    const changed = !(was?.machine === m && same(was.notices, shown));
    if (changed) last.current = { machine: m, notices: shown };
    setRead((r) => (r?.machine === m && same(r.notices, shown) ? r : { machine: m, notices: shown }));
    setDropped((was) => (was.length === 0 ? was : was.filter((id) => shown.some((n) => n.id === id))));
    return changed;
  });

  // The runtime's stream says the slot moved; while it is open the page only takes a slow look.
  const [streaming, setStreaming] = useState(false);
  useEffect(() => {
    if (!machine) return;
    const stop = openSlotStream(machine, () => void look(machine).catch(() => undefined), setStreaming);
    return stop;
  }, [machine]);

  // Read while the page is visible, slower while the slot stays the same; with the stream open, a slow look.
  useEffect(() => {
    if (!machine) return;
    if (streaming) {
      // The first look goes after this render, not inside the effect: it ends in setState.
      queueMicrotask(() => void look(machine).catch(() => undefined));
      return calmPoll({ base: SLOT_LIVE_MS, max: SLOT_LIVE_MS, look: () => look(machine) });
    }
    return calmPoll({ base: SLOT_POLL_MS, max: SLOT_QUIET_MS, look: () => look(machine) });
  }, [machine, streaming]);

  // The same list until the read or the drops change: the smart notice recomputes its items on a new one.
  const notices = useMemo(
    () =>
      read === null || read.machine !== machine
        ? NO_NOTICES
        : read.notices.filter((n) => !dropped.includes(n.id)),
    [read, machine, dropped],
  );
  return { notices, drop: (id) => setDropped((was) => [...was, id]) };
};
