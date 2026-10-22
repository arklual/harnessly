// A plan the agent proposes before it works: a line with its title that opens the whole plan; while the agent waits
// for approval it stands out as a card (the approval itself is the menu above the composer).
import { memo, useState } from 'react';
import { Check, ChevronRight, ClipboardList, X } from 'lucide-react';
import CachedMarkdown from '@/components/timeline/cached-markdown';
import { MESSAGE_COMPONENTS, MESSAGE_PLUGINS, PROSE } from '@/components/timeline/markdown-parts';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import { planTitle } from '@/lib/timeline-items';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';
import type { ITimelinePlan } from '@/types/timeline';

const PlanItem = ({ entry }: { entry: ITimelinePlan }) => {
  const holdPosition = useHoldTimelinePosition();
  const [open, setOpen] = useState(false);
  const pending = entry.status === 'pending';
  const approved = entry.status === 'success';
  const rejected = entry.status === 'error' || entry.status === 'rejected';
  const title = planTitle(entry.markdown) || t('timeline.timelineEntry.plan');
  return (
    <div className={cn('tw-plan-card', pending && 'tw-plan-card-pending')} data-status={entry.status}>
      <button
        type="button"
        className="tw-plan-card-head"
        aria-expanded={open}
        onClick={() => {
          holdPosition();
          setOpen((v) => !v);
        }}
      >
        <ClipboardList size={14} />
        <span className="tw-plan-card-label">{t('timeline.timelineEntry.plan')}</span>
        <span className="tw-plan-card-title">{title}</span>
        {approved && <Check size={12} aria-label={t('timeline.planItem.approved')} />}
        {rejected && <X size={12} aria-label={t('timeline.planItem.rejected')} />}
        <ChevronRight size={14} className="tw-plan-card-chevron" />
      </button>
      {pending && <p className="tw-plan-card-hint">{t('timeline.planItem.waiting')}</p>}
      {open && (
        <div className={cn(PROSE, 'tw-plan-card-body')}>
          <CachedMarkdown cacheKey="plan" remarkPlugins={MESSAGE_PLUGINS} components={MESSAGE_COMPONENTS}>
            {entry.markdown}
          </CachedMarkdown>
        </div>
      )}
    </div>
  );
};

export default memo(PlanItem);
