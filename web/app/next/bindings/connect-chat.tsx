// «Где работает…» of a chat on the page: the sheet that asks the chat's machine where it runs and switches it to one of
// the person's computers (Harnessly Connect) or back. Nothing changes until the runtime said yes.
import { useCallback, useState, type ReactNode } from 'react';
import { fetchConnectChat, postConnectChat } from '@/next/features/connect-chat/api';
import ConnectSheet from '@/next/features/connect-chat/connect-sheet';
import { useConnectRequest } from '@/next/stores/connect-chat';
import type { MachineId } from '@/next/protocol/v2';

export function useConnectChat(machine: MachineId, conv: string): ReactNode {
  const [open, setOpen] = useState(false);
  useConnectRequest(conv, () => setOpen(true));
  const load = useCallback(() => fetchConnectChat(machine, conv), [machine, conv]);
  const onSwitch = useCallback(
    (to: { device: string; dir: string } | null) => postConnectChat(machine, conv, to),
    [machine, conv],
  );
  return open ? (
    <ConnectSheet open={open} onClose={() => setOpen(false)} load={load} onSwitch={onSwitch} />
  ) : null;
}
