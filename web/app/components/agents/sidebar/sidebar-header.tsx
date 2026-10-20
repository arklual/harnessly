import { appConfig } from '@/platform/config';
import { useState } from 'react';
import {
  ArrowLeft,
  ArrowRight,
  ChevronDown,
  Keyboard,
  Monitor,
  Moon,
  PanelLeftClose,
  Search,
  Sun,
} from 'lucide-react';
import HlMark from '@/components/agents/hl-mark';
import { TW_MENU_KIND, TwMenuButton, type TTwMenuEntry } from '@/components/agents/ui';
import ShortcutsDialog from '@/components/agents/sidebar/shortcuts-dialog';
import NotificationCenter from '@/components/agents/sidebar/notification-center';
import type { Conversation } from '@/lib/hl-conversations';
import { t } from '@/lib/i18n';

export type TThemeMode = 'system' | 'light' | 'dark';

export const THEME_LABELS: Record<TThemeMode, string> = {
  system: t('sidebar.sidebarHeader.themeSystemLabel'),
  light: t('sidebar.sidebarHeader.themeLightLabel'),
  dark: t('sidebar.sidebarHeader.themeDarkLabel'),
};

const THEME_OPTIONS = [
  { mode: 'system', label: t('sidebar.sidebarHeader.themeSystem'), icon: <Monitor /> },
  { mode: 'light', label: t('sidebar.sidebarHeader.themeLight'), icon: <Sun /> },
  { mode: 'dark', label: t('sidebar.sidebarHeader.themeDark'), icon: <Moon /> },
] as const;

export interface ISidebarHistory {
  canBack: boolean;
  canForward: boolean;
  onBack: () => void;
  onForward: () => void;
}

interface ISidebarHeaderProps {
  history: ISidebarHistory;
  themeMode: TThemeMode;
  onThemeChange: (mode: TThemeMode) => void;
  conversations: Conversation[];
  onOpenPalette: () => void;
  onOpenSearch: () => void;
  onCollapse: () => void;
}

const SidebarHeader = ({
  history,
  themeMode,
  onThemeChange,
  conversations,
  onOpenPalette,
  onOpenSearch,
  onCollapse,
}: ISidebarHeaderProps) => {
  const [shortcutsOpen, setShortcutsOpen] = useState(false);

  const appMenu: TTwMenuEntry[] = [
    {
      kind: TW_MENU_KIND.submenu,
      id: 'theme',
      label: t('sidebar.sidebarHeader.theme'),
      icon: <Monitor />,
      items: THEME_OPTIONS.map((option) => ({
        kind: TW_MENU_KIND.radio,
        id: `theme-${option.mode}`,
        label: option.label,
        icon: option.icon,
        checked: themeMode === option.mode,
        onSelect: () => onThemeChange(option.mode),
      })),
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'shortcuts',
      label: t('sidebar.sidebarHeader.shortcuts'),
      icon: <Keyboard />,
      onSelect: () => setShortcutsOpen(true),
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'collapse',
      label: t('sidebar.sidebarHeader.collapse'),
      icon: <PanelLeftClose />,
      shortcut: '⌘B',
      onSelect: onCollapse,
    },
  ];

  return (
    <div className="tw-shead">
      <div className="tw-shead-bar">
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('sidebar.sidebarHeader.hide')}
          title={t('sidebar.sidebarHeader.hideHint')}
          onClick={onCollapse}
        >
          <PanelLeftClose />
        </button>
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('sidebar.sidebarHeader.back')}
          title={t('sidebar.sidebarHeader.back')}
          disabled={!history.canBack}
          onClick={history.onBack}
        >
          <ArrowLeft />
        </button>
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('sidebar.sidebarHeader.forward')}
          title={t('sidebar.sidebarHeader.forward')}
          disabled={!history.canForward}
          onClick={history.onForward}
        >
          <ArrowRight />
        </button>
      </div>
      <div className="tw-shead-title">
        <TwMenuButton
          items={appMenu}
          menuLabel={appConfig().productName}
          className="tw-shead-brand"
          aria-label={t('sidebar.sidebarHeader.appMenu', { product: appConfig().productName })}
        >
          <HlMark className="tw-mark" />
          <span className="tw-brand-name">{appConfig().productName}</span>
          <ChevronDown className="tw-shead-brand-chevron" aria-hidden="true" />
        </TwMenuButton>
        <span className="tw-shead-spacer" />
        <button
          type="button"
          className="tw-icon-button"
          aria-label={t('sidebar.sidebarHeader.search')}
          title={t('sidebar.sidebarHeader.searchHint')}
          onClick={onOpenPalette}
        >
          <Search />
        </button>
        <NotificationCenter conversations={conversations} />
      </div>
      <button type="button" className="tw-shead-search" onClick={onOpenSearch} aria-haspopup="dialog">
        <Search aria-hidden="true" />
        <span>{t('sidebar.sidebarHeader.searchConversations')}</span>
      </button>
      <ShortcutsDialog open={shortcutsOpen} onClose={() => setShortcutsOpen(false)} />
    </div>
  );
};

export default SidebarHeader;
