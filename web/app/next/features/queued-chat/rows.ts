// The sidebar's rows of the new chats that wait in the hub for their machine: a chat the machine has made is
// listed by the machine itself, so a delivered one is no row here.
import { queuedChatId, type QueuedChat } from '@/next/commands/queued-chats';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';
import type { MachineId } from '@/next/protocol/v2';

const TITLE_MAX = 80;

const titleOf = (text: string): string => {
  const line = text.trim().split('\n')[0]?.trim() ?? '';
  return line.length > TITLE_MAX ? `${line.slice(0, TITLE_MAX - 1)}…` : line;
};

export const queuedRows = (
  items: readonly QueuedChat[],
  machineOf: (m: MachineId) => string | null,
): ISidebarChat[] =>
  items
    .filter((x) => x.state !== 'delivered')
    .map((x) => {
      const machine = machineOf(x.machineId);
      return {
        id: queuedChatId(x.id),
        title: titleOf(x.text),
        status: x.state === 'failed' ? CHAT_STATUS.failed : CHAT_STATUS.waitingMachine,
        pinned: false,
        projectId: null,
        createdAt: x.createdAt,
        lastPromptAt: x.createdAt,
        lastActivityAt: x.createdAt,
        terminal: false,
        canStopTerminal: false,
        ...(machine ? { machine } : {}),
      };
    });
