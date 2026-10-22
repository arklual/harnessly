// The agent's context was compacted: how many tokens it held before and after.
import { memo } from 'react';
import { Combine } from 'lucide-react';
import { compactionSaving, formatTokens } from '@/lib/timeline-items';
import { t } from '@/lib/i18n';
import type { ITimelineContextCompacted } from '@/types/timeline';

const ContextCompactedItem = ({ entry }: { entry: ITimelineContextCompacted }) => {
  const { beforeTokens: before, afterTokens: after } = entry;
  const saving = compactionSaving(before, after);
  return (
    <div className="tw-compacted">
      <Combine size={12} aria-hidden />
      <span>{t('timeline.timelineEntry.contextCompacted')}</span>
      {before != null && after != null && (
        <span className="tw-compacted-tokens">
          {t('timeline.contextCompacted.tokens', {
            before: formatTokens(before),
            after: formatTokens(after),
          })}
          {saving != null && saving > 0 && <span> (-{saving}%)</span>}
        </span>
      )}
    </div>
  );
};

export default memo(ContextCompactedItem);
