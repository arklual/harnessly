import { useContext } from 'react';
import useBackgroundTasks from '@/hooks/use-background-tasks';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import type { Conversation } from '@/lib/hl-conversations';

/** Background tasks of an agent conversation on the machine serving this page; other machines do not report theirs here. */
const useConversationBackground = (conversation: Conversation | null) => {
  const native = useContext(RuntimeMachineContext);
  const local =
    conversation?.kind === 'agent' && (!conversation.machineId || conversation.machineId === native);
  return useBackgroundTasks(local ? conversation.id : null);
};

export default useConversationBackground;
