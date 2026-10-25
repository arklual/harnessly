// The background agents the finished answer waits for: the subagents and dynamic workflows still running while the chat
// has no live turn of its own to show (the agent is done talking, or its turn is over and it only waits for them to
// report). Only a task the agent still lists as running counts: one with an end, or a command, does not.
import { useMemo } from 'react';
import type { BackgroundTask, ConvId } from '@/next/protocol/v2';
import { useBackground } from '@/next/stores/conversation';
import { useConversation } from '@/next/stores/conversations';

export const useWaitingAgents = (conv: ConvId): readonly BackgroundTask[] => {
  const tasks = useBackground(conv);
  const state = useConversation(conv)?.state.k;
  const answering = state === 'working' || state === 'ready' || state === 'idle';
  return useMemo(
    () =>
      answering
        ? tasks.filter(
            (x) =>
              (x.kind === 'subagent' || x.kind === 'workflow') &&
              x.status === 'running' &&
              x.endedAt === null,
          )
        : [],
    [answering, tasks],
  );
};
