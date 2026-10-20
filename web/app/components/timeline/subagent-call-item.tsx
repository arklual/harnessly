import { createContext, useContext, useEffect } from 'react';
import ToolGroupItem from '@/components/timeline/tool-group-item';
import { SubagentCard } from '@/components/agents/inspector/subagents-tab';
import useSubagents from '@/hooks/use-subagents';
import type { ITimelineToolCall } from '@/types/timeline';

const SessionJsonlContext = createContext<string | null>(null);
export const SessionJsonlProvider = SessionJsonlContext.Provider;

/** A running call whose subagent the last answer did not list yet is asked about again this soon. */
const NEW_SUBAGENT_POKE_MS = 1_500;

/** Claude Code keeps subagent steps in their own transcript, so the call is matched to it by tool_use id. */
const SubagentCallItem = ({ call }: { call: ITimelineToolCall }) => {
  const jsonlPath = useContext(SessionJsonlContext);
  const { agents, supported, poke } = useSubagents(jsonlPath);
  const agent = agents?.find((a) => a.toolUseId === call.toolUseId);
  // The shared list polls slowly while nothing runs; a call that just started must not wait for that to show its card.
  const waiting = supported && agents !== null && !agent && call.status === 'pending';
  useEffect(() => {
    if (waiting) poke(NEW_SUBAGENT_POKE_MS);
  }, [waiting, agents, poke]);
  if (!jsonlPath || !agent) return <ToolGroupItem toolCalls={[call]} toolResults={[]} />;
  return (
    <div className="tw-agent-inline">
      <SubagentCard agent={agent} jsonlPath={jsonlPath} />
    </div>
  );
};

export default SubagentCallItem;
