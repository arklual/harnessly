// An error or a warning from the agent's runtime. Long messages fold to their first lines.
import { memo, useState } from 'react';
import { AlertTriangle, ChevronRight, Shield, WifiOff, XCircle } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { noticePreview } from '@/lib/timeline-items';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelineErrorNotice } from '@/types/timeline';

const KINDS = {
  error: XCircle,
  warning: AlertTriangle,
  'stream-error': WifiOff,
  'guardian-warning': Shield,
} as const;

const ErrorNoticeItem = ({ entry }: { entry: ITimelineErrorNotice }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const kind = entry.severity in KINDS ? (entry.severity as keyof typeof KINDS) : 'warning';
  const Icon = KINDS[kind];
  const { text, long } = noticePreview(entry.message ?? '', open);
  return (
    <div className="tw-error-notice" data-kind={kind} role="alert">
      <div className="tw-error-notice-head">
        <Icon size={14} aria-hidden />
        <strong>{t(`timeline.errorNotice.${kind}`)}</strong>
        {kind === 'stream-error' && entry.retryStatus && <span className="tw-chip">{entry.retryStatus}</span>}
        {entry.errorCode && <code className="tw-chip">{entry.errorCode}</code>}
      </div>
      <p className="tw-error-notice-text">{text}</p>
      {long && (
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
          {open ? t('timeline.errorNotice.less') : t('timeline.errorNotice.more')}
        </button>
      )}
    </div>
  );
};

export default memo(ErrorNoticeItem);
