import { t } from '@/lib/i18n';
const LINES = [
  'tw-link-skeleton-line-wide',
  'tw-link-skeleton-line-mid',
  'tw-link-skeleton-line-short',
] as const;

/** A conversation named by a link, not in the list yet: its place on the screen, instead of the welcome. */
const ConversationSkeleton = () => (
  <div className="tw-link-skeleton" role="status" aria-label={t('agents.conversationSkeleton.opening')}>
    {LINES.map((line) => (
      <span key={line} className={`tw-skeleton ${line}`} />
    ))}
  </div>
);

export default ConversationSkeleton;
