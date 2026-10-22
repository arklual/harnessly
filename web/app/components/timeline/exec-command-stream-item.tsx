// Codex: a command the agent ran, with its exit code, duration and (folded) output.
import { memo, useState } from 'react';
import { ChevronRight, Terminal } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { formatDuration } from '@/lib/timeline-items';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineExecCommandStream } from '@/types/timeline';

const ExecCommandStreamItem = ({ entry }: { entry: ITimelineExecCommandStream }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const stdout = entry.stdout ?? '';
  const stderr = entry.stderr ?? '';
  const hasOutput = stdout !== '' || stderr !== '';
  const duration = entry.durationMs != null ? formatDuration(entry.durationMs) : '';
  return (
    <div className="tw-exec" data-status={entry.status}>
      <div className="tw-exec-line">
        <Terminal
          size={12}
          className={cn('tw-exec-icon', entry.status === 'pending' && 'tw-pulse')}
          aria-hidden
        />
        <code className="tw-exec-command">{entry.parsedCommand ?? entry.command}</code>
      </div>
      <div className="tw-exec-meta">
        {entry.exitCode != null && (
          <span className={cn(entry.exitCode !== 0 && 'tw-exec-failed')}>
            {t('timeline.execCommand.exit', { code: entry.exitCode })}
          </span>
        )}
        {duration && <span>{duration}</span>}
        {entry.truncated && <span>{t('timeline.execCommand.truncated')}</span>}
        {hasOutput && (
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
            {open ? t('timeline.execCommand.hideOutput') : t('timeline.execCommand.showOutput')}
          </button>
        )}
      </div>
      {open && (
        <div className="tw-exec-output">
          {stdout && <pre>{stdout}</pre>}
          {stderr && <pre className="tw-exec-stderr">{stderr}</pre>}
        </div>
      )}
    </div>
  );
};

export default memo(ExecCommandStreamItem);
