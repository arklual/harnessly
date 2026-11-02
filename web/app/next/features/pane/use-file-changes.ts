// A file open in the pane hears that it changed on the disk: the agent wrote it, or a render finished. Looked at every few
// seconds while the page is in view, since the machine may be far and watching costs more than asking.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { fileRevision } from '@/next/features/pane/sheet/file-bytes';
import type { MachineId } from '@/next/protocol/v2';

export const FILE_LOOK_MS = 3000;

/** Calls `look` every few seconds while the page is in view. */
export const useFileLook = (look: () => void, active: boolean): void => {
  const lookNow = useEffectEvent(look);
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') lookNow();
    }, FILE_LOOK_MS);
    return () => clearInterval(timer);
  }, [active]);
};

/** How many times the file changed since it was opened: a view adds it to its address to read the file again. */
export const useFileChanges = (machine: MachineId, path: string, active: boolean): number => {
  const seen = useRef<string | null>(null);
  const [changes, setChanges] = useState(0);
  useFileLook(() => {
    void fileRevision(machine, path).then(
      (revision) => {
        if (seen.current !== null && seen.current !== revision) setChanges((n) => n + 1);
        seen.current = revision;
      },
      // Gone or out of reach for now: what is on screen stays until the machine answers again.
      () => undefined,
    );
  }, active);
  return changes;
};
