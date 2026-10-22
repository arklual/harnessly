// A run of tool calls as one line: icons of the first kinds, what it did, counts; expands into one row per call.
import { lazy, Suspense, useMemo, useState } from 'react';
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
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { isInterruptedResult } from '@/lib/running-step';
import { unifiedDiff } from '@/lib/line-diff';
import { toolStatusOf } from '@/lib/timeline-grouping';
import { toolDisplayName, toolKindOf, toolVerbOf, type TToolKind } from '@/lib/tool-kind';
import Spinner from '@/components/ui/spinner';
import { cn } from '@/lib/utils';
import { plural, t } from '@/lib/i18n';
import type { ITimelineToolCall, ITimelineToolResult, TToolStatus } from '@/types/timeline';

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

const DiffFileList = lazy(() => import('@/components/workspace/diff-file-list'));

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
  const holdPosition = useHoldTimelinePosition();
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
        onClick={() => {
          holdPosition();
          setOpen((v) => !v);
        }}
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
          {toolCalls.map((call, i) => (
            <ToolRow
              key={call.id}
              call={call}
              status={statuses[i] ?? 'pending'}
              result={toolResults.find((r) => r.toolUseId === call.toolUseId)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolRow({
  call,
  status,
  result,
}: {
  call: ITimelineToolCall;
  status: TToolStatus;
  result: ITimelineToolResult | undefined;
}) {
  const holdPosition = useHoldTimelinePosition();
  const [diffOpen, setDiffOpen] = useState(false);
  const kind = toolKindOf(call.toolName);
  const Icon = ICON[kind];
  const diff = useMemo(
    () =>
      diffOpen && call.diff ? unifiedDiff(call.diff.filePath, call.diff.oldString, call.diff.newString) : '',
    [diffOpen, call.diff],
  );
  const hasDiff = !!call.diff && (!!call.diff.oldString || !!call.diff.newString);
  const interrupted = isInterruptedResult(result);
  // An edit's own diff says more than "The file has been updated" does.
  const showResult = !!result?.summary && !interrupted && (result.isError || !hasDiff);
  return (
    <div
      className={cn('tw-tool', `tw-tool-kind-${kind}`, `tw-tool-${interrupted ? 'interrupted' : status}`)}
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
            interrupted
              ? t('timeline.toolGroupItem.interrupted')
              : status === 'pending'
                ? t('timeline.toolGroupItem.running')
                : status === 'error'
                  ? t('timeline.toolGroupItem.failed')
                  : t('timeline.toolGroupItem.done')
          }
        >
          {interrupted || status === 'error' ? <X /> : status === 'pending' ? <Spinner /> : <Check />}
        </span>
      </div>
      {interrupted && <div className="tw-tool-result">{t('timeline.toolGroupItem.interrupted')}</div>}
      {showResult && (
        <div className={cn('tw-tool-result', result.isError && 'tw-tool-result-error')}>{result.summary}</div>
      )}
      {hasDiff && (
        <>
          <button
            type="button"
            className="tw-tool-diff-toggle"
            aria-expanded={diffOpen}
            onClick={() => {
              holdPosition();
              setDiffOpen((v) => !v);
            }}
          >
            {diffOpen ? t('timeline.toolGroupItem.diffHide') : t('timeline.toolGroupItem.diffShow')}
          </button>
          {diffOpen && (
            <div className="tw-tool-diff">
              <Suspense fallback={<Spinner />}>
                <DiffFileList diff={diff} />
              </Suspense>
            </div>
          )}
        </>
      )}
    </div>
  );
}
