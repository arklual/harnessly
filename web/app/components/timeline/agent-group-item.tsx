// What a subagent did, folded under its description; opens to the messages, plans and tool runs inside.
import { memo, useMemo, useState } from 'react';
import { ChevronRight } from 'lucide-react';
import AssistantMessageItem from '@/components/timeline/assistant-message-item';
import PlanItem from '@/components/timeline/plan-item';
import ToolGroupItem from '@/components/timeline/tool-group-item';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { groupTimelineItems } from '@/lib/timeline-grouping';
import { cn } from '@/lib/utils';
import type { ITimelineAgentGroup } from '@/types/timeline';

const AgentGroupItem = ({ entry }: { entry: ITimelineAgentGroup }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const items = useMemo(
    () =>
      groupTimelineItems(entry.entries).filter(
        (item) =>
          item.type === 'tool-group' || item.entry.type === 'assistant-message' || item.entry.type === 'plan',
      ),
    [entry.entries],
  );
  return (
    <div className="tw-agent-group">
      <button
        type="button"
        className="tw-agent-group-head"
        disabled={items.length === 0}
        aria-expanded={open}
        onClick={() => {
          holdPosition();
          setOpen((v) => !v);
        }}
      >
        <ChevronRight
          size={14}
          className={cn('tw-fold-chevron', open && 'tw-fold-chevron-open')}
          aria-hidden
        />
        <span>{entry.description || entry.agentType}</span>
      </button>
      {open && (
        <div className="tw-agent-group-body">
          {items.map((item) => (
            <div key={item.id}>
              {item.type === 'tool-group' ? (
                <ToolGroupItem toolCalls={item.toolCalls} toolResults={item.toolResults} />
              ) : item.entry.type === 'plan' ? (
                <PlanItem entry={item.entry} />
              ) : item.entry.type === 'assistant-message' ? (
                <AssistantMessageItem entry={item.entry} showActions={false} />
              ) : null}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default memo(AgentGroupItem);
