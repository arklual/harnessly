// The view's selection on its two ways: to the field of the chat as a chip, at once, and to the machine for the agent's
// file_selection, a moment after it settles. A chip the person dismissed is told to the machine too: they took it back.
// One that went with a message stays there, since the agent may ask about it again.
import { runtimePath } from '@/next/core/net/machine';
import { putFileSelectionChip } from '@/next/stores/file-selection';
import type { IFileSelection } from '@/next/features/pane/file-selection';
import type { ConvId, MachineId } from '@/next/protocol/v2';

type TPath = Parameters<typeof runtimePath>[0];

const SETTLE_MS = 250;
const timers = new Map<ConvId, ReturnType<typeof setTimeout>>();

const tellMachine = (machine: MachineId, conv: ConvId, path: string, selection: IFileSelection | null) => {
  const waiting = timers.get(conv);
  if (waiting !== undefined) clearTimeout(waiting);
  timers.set(
    conv,
    setTimeout(() => {
      timers.delete(conv);
      // Lost, the agent hears that nothing is selected: the chip in the field still carries it with the message.
      void fetch(runtimePath('/api/v2/pane-selection' as TPath, machine), {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conversation: conv, selection: selection && { path, ...selection } }),
      }).catch(() => undefined);
    }, SETTLE_MS),
  );
};

export const reportFileSelection = (
  machine: MachineId,
  conv: ConvId,
  path: string,
  selection: IFileSelection | null,
): void => {
  putFileSelectionChip(conv, selection && { path, selection });
  tellMachine(machine, conv, path, selection);
};

export const dismissFileSelection = (machine: MachineId, conv: ConvId): void => {
  putFileSelectionChip(conv, null);
  tellMachine(machine, conv, '', null);
};
