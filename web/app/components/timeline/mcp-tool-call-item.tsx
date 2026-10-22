// Codex: a call to a tool of a connected server, with its arguments and result on demand.
import { memo, useState } from 'react';
import { ChevronRight, Plug } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineMcpToolCall } from '@/types/timeline';

const McpToolCallItem = ({ entry }: { entry: ITimelineMcpToolCall }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const hasDetails = !!(entry.argumentsSummary || entry.resultSummary);
  return (
    <div className="tw-mcp" data-status={entry.status}>
      <div className="tw-mcp-head">
        <Plug size={12} className={cn(entry.status === 'pending' && 'tw-pulse')} aria-hidden />
        <strong>MCP</strong>
        <code>{entry.server || '?'}</code>
        <span aria-hidden>/</span>
        <code>{entry.tool}</code>
        {entry.status === 'error' && (
          <span className="tw-exec-failed">{t('timeline.mcpToolCall.failed')}</span>
        )}
      </div>
      {hasDetails && (
        <button
          type="button"
          className="tw-fold-toggle"
          aria-expanded={open}
          onClick={() => {
            holdPosition();
            setOpen((v) => !v);
          }}
        >
          <ChevronRight size={10} className={cn('tw-fold-chevron', open && 'tw-fold-chevron-open')} />
          {open ? t('timeline.mcpToolCall.hide') : t('timeline.mcpToolCall.show')}
        </button>
      )}
      {open && (
        <div className="tw-exec-output">
          {entry.argumentsSummary && (
            <>
              <div className="tw-exec-label">{t('timeline.mcpToolCall.arguments')}</div>
              <pre>{entry.argumentsSummary}</pre>
            </>
          )}
          {entry.resultSummary && (
            <>
              <div className="tw-exec-label">{t('timeline.mcpToolCall.result')}</div>
              <pre>{entry.resultSummary}</pre>
            </>
          )}
        </div>
      )}
    </div>
  );
};

export default memo(McpToolCallItem);
