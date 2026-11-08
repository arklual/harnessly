// The one-time codes the agents' commands of a machine wait for (harnessly secret run --code): read every two seconds
// while the page is visible, since such a code lives half a minute. Every place that shows them (the chat's column
// «Ждут код», later the island of notifications) reads them here and answers through features/safe/api.ts.
import { useEffect, useEffectEvent, useState } from 'react';
import { pageClock } from '@/next/core/clock';
import { isSafeRefusal, loadCodeAsks, type ICodeAsk } from '@/next/features/safe/api';
import type { MachineId } from '@/next/protocol/v2';

export const CODES_POLL_MS = 2000;

export interface ICodeAsksRead {
  /** The asks waiting for the person, oldest first. */
  asks: readonly ICodeAsk[];
  /** The machine's clock minus this page's: `until` is the machine's. */
  skew: number;
  /** Takes an ask off at once (answered or cancelled here), before the next read says so. */
  drop: (id: string) => void;
}

const same = (a: readonly ICodeAsk[], b: readonly ICodeAsk[]) =>
  a.length === b.length && a.every((x, i) => x.id === b[i]?.id && x.until === b[i]?.until);

export const useCodeAsks = (machine: MachineId | null): ICodeAsksRead => {
  const [read, setRead] = useState<{ machine: MachineId; asks: readonly ICodeAsk[]; skew: number } | null>(
    null,
  );
  const [dropped, setDropped] = useState<readonly string[]>([]);

  const look = useEffectEvent(async (m: MachineId) => {
    const res = await loadCodeAsks(m);
    if (!('ok' in res) || isSafeRefusal(res.value)) return;
    const { codes, now } = res.value;
    const skew = now - Date.now();
    setRead((was) =>
      was?.machine === m && same(was.asks, codes) && Math.abs(was.skew - skew) < 1000
        ? was
        : { machine: m, asks: codes, skew },
    );
    setDropped((was) => (was.length === 0 ? was : was.filter((id) => codes.some((c) => c.id === id))));
  });

  useEffect(() => {
    if (!machine) return;
    const clock = pageClock();
    let timer: unknown = null;
    let looking = false;
    const again = () => {
      clock.clearTimeout(timer);
      timer = clock.setTimeout(again, CODES_POLL_MS);
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

  const asks =
    read !== null && read.machine === machine ? read.asks.filter((a) => !dropped.includes(a.id)) : [];
  return {
    asks,
    skew: read?.skew ?? 0,
    drop: (id) => setDropped((was) => (was.includes(id) ? was : [...was, id])),
  };
};
