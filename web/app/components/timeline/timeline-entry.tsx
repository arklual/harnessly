// The body of one timeline entry that is not a reply or a run of tool calls: commands, plans, questions, notices.
import { Ban, LogOut } from 'lucide-react';
import SlashCommandItem from '@/components/timeline/slash-command-item';
import SubagentCallItem from '@/components/timeline/subagent-call-item';
import PeerMessageOutItem from '@/components/timeline/peer-message-out-item';
import SentFilesItem from '@/components/timeline/sent-files-item';
import UserMessageItem from '@/components/timeline/user-message-item';
import AssistantMessageItem from '@/components/timeline/assistant-message-item';
import AgentGroupItem from '@/components/timeline/agent-group-item';
import ApprovalRequestItem from '@/components/timeline/approval-request-item';
import ContextCompactedItem from '@/components/timeline/context-compacted-item';
import ErrorNoticeItem from '@/components/timeline/error-notice-item';
import ExecCommandStreamItem from '@/components/timeline/exec-command-stream-item';
import McpToolCallItem from '@/components/timeline/mcp-tool-call-item';
import PatchApplyItem from '@/components/timeline/patch-apply-item';
import ReasoningSummaryItem from '@/components/timeline/reasoning-summary-item';
import TaskNotificationItem from '@/components/timeline/task-notification-item';
import WebSearchItem from '@/components/timeline/web-search-item';
import PlanItem from '@/components/timeline/plan-item';
import AskQuestionItem from '@/components/timeline/ask-question-item';
import ToolGroupItem from '@/components/timeline/tool-group-item';
import { isSwitchOutput, slashCommandEffort, slashCommandModel } from '@/lib/hl-command-output';
import { modelSwitchOf } from '@/lib/hl-models';
import type { TGroupedItem } from '@/lib/timeline-grouping';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineEntry } from '@/types/timeline';

interface ITimelineItemBodyProps {
  item: TGroupedItem;
  sessionName?: string;
  tabId?: string;
  provider?: string;
}

const Notice = ({
  icon,
  tone = 'muted',
  children,
}: {
  icon: React.ReactNode;
  tone?: 'muted' | 'warn' | 'error';
  children: React.ReactNode;
}) => (
  <div className={cn('tw-notice', `tw-notice-${tone}`)}>
    <span className="tw-notice-icon" aria-hidden>
      {icon}
    </span>
    <div className="tw-notice-text">{children}</div>
  </div>
);

/** A quiet rule across the feed where the model or the depth of thinking changed. */
const SwitchLine = ({ children }: { children: React.ReactNode }) => (
  <div className="tw-model-switch" role="status">
    <span aria-hidden />
    {children}
    <span aria-hidden />
  </div>
);

const EntryBody = ({ entry, provider }: { entry: ITimelineEntry; provider?: string }) => {
  switch (entry.type) {
    case 'user-message': {
      const model = modelSwitchOf(entry.text);
      if (model) return <SwitchLine>{t('timeline.switchLine.model', { model })}</SwitchLine>;
      return <UserMessageItem entry={entry} />;
    }
    case 'assistant-message':
      return <AssistantMessageItem entry={entry} provider={provider} showActions={false} />;
    case 'slash-command': {
      const model = slashCommandModel(entry);
      if (model?.switched)
        return <SwitchLine>{t('timeline.switchLine.model', { model: model.label })}</SwitchLine>;
      const effort = slashCommandEffort(entry);
      if (effort) return <SwitchLine>{t('timeline.switchLine.effort', { effort: effort.label })}</SwitchLine>;
      return <SlashCommandItem entry={entry} />;
    }
    case 'command-output':
      // Output of `/model` or `/effort` that lost its command: the quiet line already says it.
      if (isSwitchOutput(entry.output ?? '')) return null;
      return <SlashCommandItem entry={entry} />;
    case 'tool-call':
      if (entry.toolName === 'Task' || entry.toolName === 'Agent') return <SubagentCallItem call={entry} />;
      if (entry.peerMessage && typeof entry.peerMessage.text === 'string')
        return <PeerMessageOutItem call={entry as never} />;
      if (entry.sentFiles) return <SentFilesItem call={entry as never} />;
      return <ToolGroupItem toolCalls={[entry]} toolResults={[]} />;
    case 'agent-group':
      return <AgentGroupItem entry={entry} />;
    case 'plan':
      return <PlanItem entry={entry} />;
    case 'ask-user-question':
      return <AskQuestionItem entry={entry} />;
    case 'task-notification':
      return <TaskNotificationItem entry={entry} />;
    case 'error-notice':
      return <ErrorNoticeItem entry={entry} />;
    case 'context-compacted':
      return <ContextCompactedItem entry={entry} />;
    case 'interrupt':
      return <Notice icon={<Ban size={14} />}>{t('timeline.timelineEntry.interrupted')}</Notice>;
    case 'session-exit':
      return <Notice icon={<LogOut size={14} />}>{t('timeline.timelineEntry.sessionEnded')}</Notice>;
    case 'reasoning-summary':
      return <ReasoningSummaryItem entry={entry} />;
    case 'web-search':
      return <WebSearchItem entry={entry} />;
    case 'mcp-tool-call':
      return <McpToolCallItem entry={entry} />;
    case 'patch-apply':
      return <PatchApplyItem entry={entry} />;
    case 'approval-request':
      return <ApprovalRequestItem entry={entry} />;
    case 'exec-command-stream':
      return <ExecCommandStreamItem entry={entry} />;
    // The checklist above the feed shows the progress of tasks; a row per change would only repeat it.
    case 'task-progress':
      return null;
    default:
      return null;
  }
};

export function TimelineItemBody({ item, provider }: ITimelineItemBodyProps) {
  if (item.type === 'tool-group')
    return <ToolGroupItem toolCalls={item.toolCalls} toolResults={item.toolResults} />;
  return <EntryBody entry={item.entry} provider={provider} />;
}
