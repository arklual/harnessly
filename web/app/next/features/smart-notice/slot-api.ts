// The machine's open slot of the smart notice (cmd/runtime/routes_notices.go): what apps and agents ask the person,
// read every second and a half while the page is visible, answered here; and the app view's side (hl.notice), which
// asks through its view session.
import { useEffect, useEffectEvent, useState } from 'react';
import { pageClock } from '@/next/core/clock';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson, RuntimeError } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';
import { SLOT_ANSWER, type ISlotNotice, type ISlotSource, type TSlotAnswer } from './slot-body';

export const SLOT_POLL_MS = 1500;

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

export const useSlotNotices = (machine: MachineId | null): ISlotRead => {
  const [read, setRead] = useState<{ machine: MachineId; notices: readonly ISlotNotice[] } | null>(null);
  const [dropped, setDropped] = useState<readonly string[]>([]);

  const look = useEffectEvent(async (m: MachineId) => {
    const { notices } = await loadSlot(m);
    const shown = notices.filter((n) => !n.queued);
    setRead((was) => (was?.machine === m && same(was.notices, shown) ? was : { machine: m, notices: shown }));
    setDropped((was) => (was.length === 0 ? was : was.filter((id) => shown.some((n) => n.id === id))));
  });

  useEffect(() => {
    if (!machine) return;
    const clock = pageClock();
    let timer: unknown = null;
    let looking = false;
    const again = () => {
      clock.clearTimeout(timer);
      timer = clock.setTimeout(again, SLOT_POLL_MS);
      if (looking || document.visibilityState !== 'visible') return;
      looking = true;
      void look(machine)
        .catch(() => undefined)
        .finally(() => {
          looking = false;
        });
    };
    again();
    const onVisible = () => {
      if (document.visibilityState === 'visible') again();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      clock.clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [machine]);

  const notices =
    read === null || read.machine !== machine ? [] : read.notices.filter((n) => !dropped.includes(n.id));
  return { notices, drop: (id) => setDropped((was) => [...was, id]) };
};
