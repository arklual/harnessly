import ConversationRow from '@/components/agents/sidebar/conversation-row';
import SidebarSection from '@/components/agents/sidebar/sidebar-section';
import { CONVERSATION_GROUP_LABELS } from '@/lib/hl-conversations';
import type { ISidebarModel } from '@/lib/hl-sidebar-model';
import { t } from '@/lib/i18n';

interface IConversationsSectionProps {
  model: ISidebarModel;
  hasMore: boolean;
  onLoadMore: () => void;
}

/** «Conversations»: the feed by day, without what is pinned or sits in a project. */
const ConversationsSection = ({ model, hasMore, onLoadMore }: IConversationsSectionProps) => (
  <SidebarSection section="conversations" title={t('sidebar.conversationsSection.title')}>
    {model.conversations.length === 0 && (
      <p className="tw-empty-list">{t('sidebar.conversationsSection.empty')}</p>
    )}
    {model.conversations.map(({ group, items }) => (
      <section key={group} className="tw-group">
        <h3 className="tw-group-title">{CONVERSATION_GROUP_LABELS[group]}</h3>
        {items.map((item) => (
          <ConversationRow key={item.key} item={item} />
        ))}
      </section>
    ))}
    {hasMore && (
      <button type="button" className="tw-more" onClick={onLoadMore}>
        {t('sidebar.conversationsSection.showMore')}
      </button>
    )}
  </SidebarSection>
);

export default ConversationsSection;
