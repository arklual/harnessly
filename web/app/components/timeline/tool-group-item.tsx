// A run of tool calls as one line: icons of the first kinds, what it did, counts; expands into one row per call.
import { useState } from 'react';
import {
  Bot,
  Check,
  ChevronRight,
  FilePen,
  FilePlus2,
  FileText,
  Plug,
  Search,
  SquareTerminal,
  Wrench,
  X,
  type LucideIcon,
} from 'lucide-react';
import { toolStatusOf } from '@/lib/timeline-grouping';
import { toolDisplayName, toolKindOf, toolVerbOf, type TToolKind } from '@/lib/tool-kind';
import Spinner from '@/components/ui/spinner';
import { cn } from '@/lib/utils';
import { plural, t } from '@/lib/i18n';
import type { ITimelineToolCall, ITimelineToolResult } from '@/types/timeline';

const ICON: Record<TToolKind, LucideIcon> = {
  read: FileText,
  edit: FilePen,
  create: FilePlus2,
  command: SquareTerminal,
  search: Search,
  agent: Bot,
  integration: Plug,
  other: Wrench,
};

const countLabel = (n: number) => plural(n, 'timeline.toolGroupItem.actions');

interface IToolGroupItemProps {
  toolCalls: ITimelineToolCall[];
  toolResults: ITimelineToolResult[];
  quiet?: boolean;
  defaultExpanded?: boolean;
}

export default function ToolGroupItem({
  toolCalls,
  toolResults,
  quiet,
  defaultExpanded = false,
}: IToolGroupItemProps) {
  const [open, setOpen] = useState(defaultExpanded);
  const statuses = toolCalls.map((call) => toolStatusOf(call, toolResults));
  const pending = statuses.includes('pending');
  const errors = statuses.filter((s) => s === 'error').length;
  const kinds = [...new Set(toolCalls.map((c) => toolKindOf(c.toolName)))].slice(0, 3);
  const last = toolCalls[toolCalls.length - 1];
  const summary = last ? `${toolVerbOf(last)} ${last.summary || toolDisplayName(last.toolName)}` : '';
  return (
    <div
      className={cn(
        'agent-tool-group tw-tools',
        pending && !quiet && 'tw-tools-live',
        open && 'tw-tools-open',
      )}
    >
      <button
        type="button"
        className="tw-tools-head"
        data-testid="tool-group-toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="tw-tools-stack" aria-hidden>
          {kinds.map((kind) => {
            const Icon = ICON[kind];
            return (
              <span key={kind} className={`tw-tools-dot tw-tool-kind-${kind}`}>
                <Icon size={11} />
              </span>
            );
          })}
        </span>
        <span className="tw-tools-summary">{summary}</span>
        {errors > 0 && (
          <span className="tw-tools-error">{t('timeline.toolGroupItem.errors', { n: errors })}</span>
        )}
        {toolCalls.length > 1 && <span className="tw-tools-count">{countLabel(toolCalls.length)}</span>}
        <ChevronRight size={14} className="tw-tools-chevron" />
      </button>
      {open && (
        <div className="tw-tools-list">
          {toolCalls.map((call, i) => {
            const kind = toolKindOf(call.toolName);
            const Icon = ICON[kind];
            const status = statuses[i] ?? 'pending';
            const result = toolResults.find((r) => r.toolUseId === call.toolUseId);
            return (
              <div
                key={call.id}
                className={cn('tw-tool', `tw-tool-kind-${kind}`, `tw-tool-${status}`)}
                data-entry-id={call.id}
              >
                <div className="tw-tool-row">
                  <span className="tw-tool-icon" aria-hidden>
                    <Icon />
                  </span>
                  <span className="tw-tool-text">
                    <span className="tw-tool-verb">{toolVerbOf(call)}</span>
                    <span
                      className={cn('tw-tool-target', kind === 'command' && 'tw-tool-mono')}
                      data-testid="tool-target"
                      title={call.summary}
                    >
                      {call.summary || toolDisplayName(call.toolName)}
                    </span>
                  </span>
                  <span
                    className="tw-tool-state"
                    aria-label={
                      status === 'pending'
                        ? t('timeline.toolGroupItem.running')
                        : status === 'error'
                          ? t('timeline.toolGroupItem.failed')
                          : t('timeline.toolGroupItem.done')
                    }
                  >
                    {status === 'pending' ? <Spinner /> : status === 'error' ? <X /> : <Check />}
                  </span>
                </div>
                {result?.isError && result.summary && (
                  <div className="tw-tool-result tw-tool-result-error">{result.summary}</div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
