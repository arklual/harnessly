// How full the chat's agent window is, for the composer's ring: read when the chat opens and again whenever its agent
// stops working (a turn's answer, a compaction). Nothing until the first answer says.
import { useEffect, useState } from 'react';
import { loadContext } from '@/next/commands/pages';
import { useLinkRestored } from '@/next/stores/connection';
import type { ConvContext, ConvId, MachineId } from '@/next/protocol/v2';

export const useContextWindow = (
  machine: MachineId | null,
  conv: ConvId,
  working: boolean,
): ConvContext | null => {
  // The answer keeps the chat it is of: another chat shows nothing until its own comes.
  const [value, setValue] = useState<{ conv: ConvId; window: ConvContext } | null>(null);
  // The ring shows nothing while the machine is away; when the link returns it is read again, quietly.
  const restored = useLinkRestored(machine);
  useEffect(() => {
    if (!machine || working) return;
    let live = true;
    void loadContext(machine, conv).then((outcome) => {
      if (live && 'ok' in outcome) setValue({ conv, window: outcome.value });
    });
    return () => {
      live = false;
    };
  }, [machine, conv, working, restored]);
  return value?.conv === conv && value.window.usedTokens > 0 ? value.window : null;
};
