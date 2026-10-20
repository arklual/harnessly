import { useId, useRef, useState } from 'react';
import { Ellipsis, Gauge, Link2, Pin, PinOff, Search, Share } from 'lucide-react';
import {
  LAYER_ALIGN,
  MENU_INITIAL_FOCUS,
  TW_MENU_KIND,
  TwMenu,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import type { IHeaderSlotProps } from '@/components/agents/header/header-slot';
import {
  ECONOMICS_STATE,
  openEconomicsIsland,
  useEconomicsSummary,
  type TEconomicsSummary,
} from '@/components/agents/header/economics-slot';
import { usePinState } from '@/components/agents/header/project-slot';
import SharePopover from '@/components/agents/share/share-popover';
import { copyConversationLink } from '@/lib/hl-conversation-link';
import { t } from '@/lib/i18n';

type TMenuInitialFocus = (typeof MENU_INITIAL_FOCUS)[keyof typeof MENU_INITIAL_FOCUS];

const SHAREABLE_PROVIDERS = ['claude', 'codex'] as const;

const economicsHint = (summary: TEconomicsSummary): string | null => {
  if (summary.state === ECONOMICS_STATE.loading) return t('header.conversationMenu.computing');
  if (summary.state === ECONOMICS_STATE.error)
    return t('header.conversationMenu.error', { error: summary.error });
  if (summary.state === ECONOMICS_STATE.ready) return summary.label;
  return null;
};

interface IConversationMenuProps extends IHeaderSlotProps {
  /** Opens the search over this conversation (⌘J); absent when the conversation has none. */
  onSearch?: () => void;
}

/**
 * «⋯» of the conversation on screen: everything the header used to show in a row —
 * economics, search, pin, share — behind one calm button.
 */
const ConversationMenu = (props: IConversationMenuProps) => {
  const { conversation, onSearch } = props;
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuId = useId();
  const shareId = useId();
  const [focus, setFocus] = useState<TMenuInitialFocus | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const economics = useEconomicsSummary(props);
  const pin = usePinState(props);

  const agent = conversation?.kind === 'agent' ? conversation : null;
  const shareable = agent !== null && (SHAREABLE_PROVIDERS as readonly string[]).includes(agent.provider);
  const hasAddress = conversation !== null && conversation.id !== 'new' && conversation.key !== 'shared';
  const economicsLine = economicsHint(economics);

  const items: TTwMenuEntry[] = [];
  if (economicsLine) {
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'economics',
      label: t('header.conversationMenu.economics'),
      hint: economicsLine,
      icon: <Gauge />,
      onSelect: openEconomicsIsland,
    });
  }
  if (onSearch) {
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'search',
      label: t('header.conversationMenu.search'),
      hint: t('header.conversationMenu.searchHint'),
      icon: <Search />,
      shortcut: '⌘J',
      onSelect: onSearch,
    });
  }
  const actions: TTwMenuEntry[] = [];
  if (pin) {
    actions.push({
      kind: TW_MENU_KIND.action,
      id: 'pin',
      label: pin.pinned ? t('header.conversationMenu.unpin') : t('header.conversationMenu.pin'),
      icon: pin.pinned ? <PinOff /> : <Pin />,
      disabled: pin.pending || pin.blockedReason !== null,
      disabledReason: pin.pending ? t('header.conversationMenu.saving') : (pin.blockedReason ?? undefined),
      onSelect: pin.toggle,
    });
  }
  if (conversation && hasAddress) {
    actions.push({
      kind: TW_MENU_KIND.action,
      id: 'copy-link',
      label: t('header.conversationMenu.copyLink'),
      hint: t('header.conversationMenu.copyLinkHint'),
      icon: <Link2 />,
      onSelect: () => void copyConversationLink(conversation.key),
    });
  }
  if (shareable) {
    actions.push({
      kind: TW_MENU_KIND.action,
      id: 'share',
      label: t('header.conversationMenu.share'),
      hint: t('header.conversationMenu.shareHint'),
      icon: <Share />,
      onSelect: () => setShareOpen(true),
    });
  }
  if (items.length > 0 && actions.length > 0) items.push({ kind: TW_MENU_KIND.separator, id: 'sep' });
  items.push(...actions);
  if (items.length === 0) return null;

  const open = focus !== null;
  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="tw-icon-button tw-header-more"
        data-open={open || shareOpen ? '' : undefined}
        aria-label={t('header.conversationMenu.actions')}
        title={t('header.conversationMenu.actions')}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={(event) =>
          setFocus((current) => {
            if (current) return null;
            return event.detail === 0 ? MENU_INITIAL_FOCUS.first : MENU_INITIAL_FOCUS.menu;
          })
        }
        onKeyDown={(event) => {
          if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
          event.preventDefault();
          setFocus(event.key === 'ArrowDown' ? MENU_INITIAL_FOCUS.first : MENU_INITIAL_FOCUS.last);
        }}
      >
        <Ellipsis />
      </button>
      <TwMenu
        open={open}
        onClose={() => setFocus(null)}
        anchor={triggerRef}
        triggerRef={triggerRef}
        items={items}
        aria-label={t('header.conversationMenu.actions')}
        align={LAYER_ALIGN.end}
        initialFocus={focus ?? MENU_INITIAL_FOCUS.first}
        id={menuId}
        className="tw-header-menu"
      />
      {agent && shareable && (
        <SharePopover
          key={agent.key}
          open={shareOpen}
          onClose={() => setShareOpen(false)}
          anchorRef={triggerRef}
          id={shareId}
          tabId={agent.id}
          machineId={agent.machineId}
        />
      )}
    </>
  );
};

export default ConversationMenu;
