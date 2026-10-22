// Codex: the short account of its reasoning the model chose to show.
import { memo } from 'react';
import { Brain } from 'lucide-react';
import { t } from '@/lib/i18n';
import type { ITimelineReasoningSummary } from '@/types/timeline';

const ReasoningSummaryItem = ({ entry }: { entry: ITimelineReasoningSummary }) => {
  if (entry.summary.length === 0 && !entry.hasEncryptedContent) return null;
  return (
    <div className="tw-reasoning">
      <div className="tw-reasoning-head">
        <Brain size={12} aria-hidden />
        <span>{t('timeline.reasoning.title')}</span>
      </div>
      {entry.summary.length > 0 && (
        <ul>
          {entry.summary.map((line, index) => (
            <li key={index}>{line}</li>
          ))}
        </ul>
      )}
      {entry.hasEncryptedContent && <p className="tw-reasoning-note">{t('timeline.reasoning.encrypted')}</p>}
    </div>
  );
};

export default memo(ReasoningSummaryItem);
