// A background task finished or a monitor fired: a divider in the feed, the result on click.
import { memo, useState } from 'react';
import { Activity, CheckCircle2, ChevronRight, Clock, XCircle, Zap } from 'lucide-react';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { formatDuration } from '@/lib/timeline-items';
import { cn } from '@/lib/utils';
import { plural } from '@/lib/i18n';
import type { ITimelineTaskNotification } from '@/types/timeline';

const TaskNotificationItem = ({ entry }: { entry: ITimelineTaskNotification }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const isEvent = entry.status === 'event';
  const isDone = entry.status === 'completed';
  const hasDetail = !!entry.result;
  const Icon = isEvent ? Activity : isDone ? CheckCircle2 : XCircle;
  const duration = entry.usage?.durationMs != null ? formatDuration(entry.usage.durationMs) : '';
  return (
    <div
      className={cn('tw-event', isEvent ? 'tw-event-monitor' : isDone ? 'tw-event-done' : 'tw-event-failed')}
    >
      <button
        type="button"
        className="tw-event-line"
        disabled={!hasDetail}
        aria-expanded={hasDetail ? open : undefined}
        onClick={() => {
          holdPosition();
          setOpen((v) => !v);
        }}
      >
        <Icon className="tw-event-icon" aria-hidden />
        <span className="tw-event-text">{entry.summary}</span>
        {duration && (
          <span className="tw-event-meta">
            <Clock aria-hidden />
            {duration}
          </span>
        )}
        {hasDetail && (
          <ChevronRight className={cn('tw-event-chevron', open && 'tw-event-chevron-open')} aria-hidden />
        )}
      </button>
      {open && entry.result && (
        <div className="tw-event-detail">
          <pre>{entry.result}</pre>
          {entry.usage?.toolUses != null && (
            <div className="tw-event-meta">
              <Zap aria-hidden />
              {plural(entry.usage.toolUses, 'timeline.taskNotification.toolUses')}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default memo(TaskNotificationItem);
