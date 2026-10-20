// The body of one timeline entry that is not a reply or a run of tool calls: commands, plans, questions, notices.
import {
  AlertTriangle,
  Ban,
  Bot,
  Info,
  ListChecks,
  LogOut,
  MessageCircleQuestion,
  Minimize2,
  Search,
  Wrench,
} from 'lucide-react';
import CachedMarkdown from '@/components/timeline/cached-markdown';
import { MESSAGE_COMPONENTS, MESSAGE_PLUGINS, PROSE } from '@/components/timeline/markdown-parts';
import SlashCommandItem from '@/components/timeline/slash-command-item';
import SubagentCallItem from '@/components/timeline/subagent-call-item';
import PeerMessageOutItem from '@/components/timeline/peer-message-out-item';
import SentFilesItem from '@/components/timeline/sent-files-item';
import UserMessageItem from '@/components/timeline/user-message-item';
import AssistantMessageItem from '@/components/timeline/assistant-message-item';
import ToolGroupItem from '@/components/timeline/tool-group-item';
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

const Markdown = ({ text }: { text: string }) => (
  <div className={PROSE}>
    <CachedMarkdown cacheKey="entry" remarkPlugins={MESSAGE_PLUGINS} components={MESSAGE_COMPONENTS}>
      {text}
    </CachedMarkdown>
  </div>
);

const EntryBody = ({ entry, provider }: { entry: ITimelineEntry; provider?: string }) => {
  switch (entry.type) {
    case 'user-message':
      return <UserMessageItem entry={entry} />;
    case 'assistant-message':
      return <AssistantMessageItem entry={entry} provider={provider} showActions={false} />;
    case 'slash-command':
      return <SlashCommandItem entry={entry} />;
    case 'command-output':
      return (
        <pre className="tw-command-output" data-error={entry.isError || undefined}>
          {entry.output}
        </pre>
      );
    case 'tool-call':
      if (entry.toolName === 'Task' || entry.toolName === 'Agent') return <SubagentCallItem call={entry} />;
      if (entry.peerMessage && typeof entry.peerMessage.text === 'string')
        return <PeerMessageOutItem call={entry as never} />;
      if (entry.sentFiles) return <SentFilesItem call={entry as never} />;
      return <ToolGroupItem toolCalls={[entry]} toolResults={[]} />;
    case 'agent-group':
      return (
        <Notice icon={<Bot size={14} />}>
          <strong>{entry.agentType}</strong> {entry.description}{' '}
          <span className="tw-notice-meta">· {entry.entryCount}</span>
        </Notice>
      );
    case 'plan':
      return (
        <div className="tw-plan-card" data-status={entry.status}>
          <div className="tw-plan-card-head">
            <ListChecks size={14} /> {t('timeline.timelineEntry.plan')}
          </div>
          <Markdown text={entry.markdown} />
        </div>
      );
    case 'ask-user-question':
      return (
        <div className="tw-question-card" data-status={entry.status}>
          {entry.questions.map((q, i) => (
            <div key={i} className="tw-question">
              <div className="tw-question-head">
                <MessageCircleQuestion size={14} /> {q.header ?? t('timeline.timelineEntry.question')}
              </div>
              <p>{q.question}</p>
              <ul>
                {q.options.map((o) => (
                  <li key={o.label} data-picked={entry.answers?.[q.question] === o.label || undefined}>
                    <strong>{o.label}</strong>
                    {o.description ? ` — ${o.description}` : ''}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      );
    case 'task-notification':
      return (
        <Notice icon={<Bot size={14} />}>
          {entry.summary}
          {entry.result ? <Markdown text={entry.result} /> : null}
        </Notice>
      );
    case 'error-notice':
      return (
        <Notice
          icon={entry.severity === 'error' ? <AlertTriangle size={14} /> : <Info size={14} />}
          tone={entry.severity === 'error' ? 'error' : 'warn'}
        >
          {entry.message}
        </Notice>
      );
    case 'context-compacted':
      return <Notice icon={<Minimize2 size={14} />}>{t('timeline.timelineEntry.contextCompacted')}</Notice>;
    case 'interrupt':
      return <Notice icon={<Ban size={14} />}>{t('timeline.timelineEntry.interrupted')}</Notice>;
    case 'session-exit':
      return <Notice icon={<LogOut size={14} />}>{t('timeline.timelineEntry.sessionEnded')}</Notice>;
    case 'reasoning-summary':
      return <Markdown text={entry.summary.join('\n\n')} />;
    case 'web-search':
      return (
        <Notice icon={<Search size={14} />}>
          {t('timeline.timelineEntry.search', { query: entry.query ?? '' })}
        </Notice>
      );
    case 'mcp-tool-call':
      return (
        <Notice icon={<Wrench size={14} />}>
          {entry.server} · {entry.tool}
        </Notice>
      );
    case 'patch-apply':
      return (
        <Notice icon={<Wrench size={14} />}>
          {t('timeline.timelineEntry.changes', { files: entry.files.map((f) => f.path).join(', ') })}
        </Notice>
      );
    case 'approval-request':
      return (
        <Notice icon={<MessageCircleQuestion size={14} />} tone="warn">
          {entry.command ?? t('timeline.timelineEntry.awaitingApproval')}
        </Notice>
      );
    default:
      return null;
  }
};

export function TimelineItemBody({ item, provider }: ITimelineItemBodyProps) {
  if (item.type === 'tool-group')
    return <ToolGroupItem toolCalls={item.toolCalls} toolResults={item.toolResults} />;
  return <EntryBody entry={item.entry} provider={provider} />;
}
