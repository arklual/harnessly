import { ChevronDown, SquarePen } from 'lucide-react';
import { LAYER_ALIGN, TwKbd, TwMenuButton } from '@/components/agents/ui';
import { newConversationMenuItems } from '@/components/agents/sidebar/menus';
import { useModKey } from '@/components/agents/sidebar/organizer-actions';
import { useSidebarContext } from '@/components/agents/sidebar/sidebar-context';
import { t } from '@/lib/i18n';

/** «New conversation»: the welcome composer; [▾] starts Claude, Codex or a chat at once. */
const NewConversationAction = ({ onNew }: { onNew: () => void }) => {
  const context = useSidebarContext();
  const modKey = useModKey();
  return (
    <div className="tw-snew">
      <button
        type="button"
        className="tw-row tw-snew-main"
        onClick={onNew}
        title={t('sidebar.newConversationAction.newConversationHint')}
      >
        <SquarePen aria-hidden="true" />
        <span className="tw-row-title">{t('sidebar.newConversationAction.newConversation')}</span>
        <TwKbd className="tw-srow-kbd tw-snew-kbd">{modKey}⇧O</TwKbd>
      </button>
      <TwMenuButton
        items={newConversationMenuItems(context)}
        menuLabel={t('sidebar.newConversationAction.startNow')}
        align={LAYER_ALIGN.end}
        className="tw-icon-button tw-snew-more"
        aria-label={t('sidebar.newConversationAction.startNowLabel')}
        title={t('sidebar.newConversationAction.startNow')}
        disabled={context.busy}
      >
        <ChevronDown />
      </TwMenuButton>
    </div>
  );
};

export default NewConversationAction;
