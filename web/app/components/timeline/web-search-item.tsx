// Codex: a web search the agent made.
import { memo } from 'react';
import { Globe } from 'lucide-react';
import { cn } from '@/lib/utils';
import { plural, t } from '@/lib/i18n';
import type { ITimelineWebSearch } from '@/types/timeline';

const WebSearchItem = ({ entry }: { entry: ITimelineWebSearch }) => {
  const summary =
    entry.resultsSummary ??
    (entry.resultCount != null ? plural(entry.resultCount, 'timeline.webSearch.results') : null);
  return (
    <div className="tw-search" data-status={entry.status}>
      <Globe
        size={12}
        className={cn('tw-search-icon', entry.status === 'pending' && 'tw-pulse')}
        aria-hidden
      />
      <div>
        <span>{t('timeline.webSearch.title')}</span>
        {entry.query && <q className="tw-search-query">{entry.query}</q>}
        {summary && <p className="tw-search-summary">{summary}</p>}
      </div>
    </div>
  );
};

export default memo(WebSearchItem);
