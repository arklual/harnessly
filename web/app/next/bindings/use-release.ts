// Whether the runtime knows a newer harnessly release than the one running. The runtime looks on its own (at start and
// every 12 hours); the page asks it when it opens and once an hour while open, which costs the runtime nothing.
import { useCallback, useEffect, useState } from 'react';
import { loadRelease, readDismissed, writeDismissed, type IRelease } from '@/next/features/update/release';
import type { MachineId } from '@/next/protocol/v2';
import { appConfig } from '@/platform/config';

const ASK_EVERY_MS = 60 * 60_000;

export const useRelease = (machine: MachineId | null) => {
  const [release, setRelease] = useState<IRelease | null>(null);
  const [dismissed, setDismissed] = useState<string | null>(readDismissed);
  // A machine a hub carries is updated by the hub: nothing to tell.
  const asks = machine !== null && !appConfig().hubUrl;

  useEffect(() => {
    if (!asks || machine === null) return;
    let live = true;
    const ask = () =>
      void loadRelease(machine).then((found) => {
        if (live) setRelease(found);
      });
    ask();
    const timer = window.setInterval(ask, ASK_EVERY_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [asks, machine]);

  const dismiss = useCallback(() => {
    if (!release) return;
    writeDismissed(release.version);
    setDismissed(release.version);
  }, [release]);

  return { release: release && release.version !== dismissed ? release : null, dismiss };
};
