import ConversationRow from '@/components/agents/sidebar/conversation-row';
import SidebarSection from '@/components/agents/sidebar/sidebar-section';
import { organizerActions } from '@/components/agents/sidebar/organizer-actions';
import type { ISidebarModel } from '@/lib/hl-sidebar-model';
import { t } from '@/lib/i18n';

/** «Pinned»: ordered by hand, numbered ⌘1… first. Hidden while nothing is pinned. */
const PinnedSection = ({ model }: { model: ISidebarModel }) => {
  if (!model.pinned.length && !model.pinnedStaleRefs.length) return null;
  return (
    <SidebarSection section="pinned" title={t('sidebar.pinnedSection.title')}>
      {model.pinned.map((row) => (
        <ConversationRow key={row.ref} row={row} showProject />
      ))}
      {model.pinnedStaleRefs.length > 0 && (
        <button
          type="button"
          className="tw-more tw-sstale"
          onClick={() => void organizerActions.forget(model.pinnedStaleRefs)}
        >
          {t('sidebar.pinnedSection.stale', { count: model.pinnedStaleRefs.length })}
        </button>
      )}
    </SidebarSection>
  );
};

export default PinnedSection;
