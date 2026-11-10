// The design's chats on every machine of the person (HRN-675). A design is the same project on each machine (the
// library is shared), and each machine knows its own chats of it: the ones started from it and the ones that edited its
// files. The open machine's answer comes with its library; the others are asked when the design opens and again each
// time the switch opens, the way the sidebar lists the chats of every machine.
import { useEffect, useMemo, useState } from 'react';
import { fetchDesignChats } from '@/next/features/designly/api';
import type { IDesignChatsFound } from '@/next/features/designly/design-conv';
import { useMachines } from '@/next/stores/machines';
import type { MachineId } from '@/next/protocol/v2';

const NO_FOUND: readonly IDesignChatsFound[] = [];

/**
 * What each machine said of the design's chats: the open machine's own list (`here`) and the other machines' answers.
 * A machine that does not answer (offline, an older runtime) adds nothing; its chats come back with its next answer.
 */
export const useDesignChatsFound = (
  machine: MachineId,
  design: string,
  here: readonly string[],
  ask: number,
): readonly IDesignChatsFound[] => {
  const machines = useMachines();
  const others = useMemo(
    () =>
      (machines ?? [])
        .filter((v) => v.machine.id !== machine && v.runtime === 'v2' && v.features.includes('designly'))
        .map((v) => v.machine.id),
    [machines, machine],
  );
  const othersKey = others.join('|');
  const [answers, setAnswers] = useState<{ key: string; found: readonly IDesignChatsFound[] }>({
    key: '',
    found: NO_FOUND,
  });
  const key = `${design}|${othersKey}`;

  useEffect(() => {
    if (others.length === 0) return;
    const abort = new AbortController();
    void Promise.allSettled(
      others.map(async (m) => ({ machine: m, convs: await fetchDesignChats(m, design, abort.signal) })),
    ).then((all) => {
      if (abort.signal.aborted) return;
      const found = all.flatMap((r) =>
        r.status === 'fulfilled' && r.value.convs.length > 0 ? [r.value] : [],
      );
      setAnswers({ key, found });
    });
    return () => abort.abort();
    // others is othersKey's list: a new array with the same machines asks nothing new.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, ask]);

  const elsewhere = answers.key === key ? answers.found : NO_FOUND;
  return useMemo(() => [{ machine, convs: here }, ...elsewhere], [machine, here, elsewhere]);
};
