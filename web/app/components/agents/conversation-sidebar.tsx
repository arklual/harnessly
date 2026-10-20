import { useEffect, useId, useState, useSyncExternalStore } from 'react';
import { Monitor, Moon, RotateCw, Sun } from 'lucide-react';
import MachinesPanel, { MachinesChip, useMachinesOpen } from '@/components/agents/machines-panel';
import SidebarHeader, {
  THEME_LABELS,
  type ISidebarHistory,
  type TThemeMode,
} from '@/components/agents/sidebar/sidebar-header';
import CentreNav from '@/components/agents/sidebar/centre-nav';
import NewConversationAction from '@/components/agents/sidebar/new-conversation-action';
import PinnedSection from '@/components/agents/sidebar/pinned-section';
import ProjectsSection from '@/components/agents/sidebar/projects-section';
import ConversationsSection from '@/components/agents/sidebar/conversations-section';
import DocsSection from '@/components/agents/sidebar/docs-section';
import type { Conversation } from '@/lib/hl-conversations';
import { ORGANIZER_MACHINE_DOWN } from '@/hooks/use-hl-organizer';
import type { ISidebarModel } from '@/lib/hl-sidebar-model';
import { t } from '@/lib/i18n';

export { STATUS_LABEL } from '@/components/agents/sidebar/conversation-row';
export { THEME_LABELS, type TThemeMode };

/** @deprecated The options moved to `@/lib/hl-new-conversation`; pick them with `newConversationOptions(features)`. */
export { NEW_CONVERSATION_OPTIONS } from '@/lib/hl-new-conversation';

export const initials = (owner: string) => {
  const parts = owner.split('@')[0].split(/[._-]/).filter(Boolean);
  return (parts.length > 1 ? parts[0][0] + parts[parts.length - 1][0] : owner.slice(0, 2)).toUpperCase();
};

const THEME_ICONS = { system: Monitor, light: Sun, dark: Moon } as const;

interface IConversationSidebarProps {
  owner: string;
  selectedMachineId?: string;
  onSelectMachine: (id: string) => void;
  /** Every conversation, for the notifications list; the sections take theirs from `model`. */
  conversations: Conversation[];
  model: ISidebarModel;
  /** Conversation lists still loading. Projects and pins come from the default machine and load on their own. */
  loading: boolean;
  /** The default machine holding projects and pins did not answer; null when it did. */
  organizerError: string | null;
  organizerReady: boolean;
  organizerMachineName: string | null;
  onRetryOrganizer: () => void;
  hasMore: boolean;
  history: ISidebarHistory;
  themeMode: TThemeMode;
  onToggleTheme: () => void;
  onThemeChange: (mode: TThemeMode) => void;
  onNew: () => void;
  onLoadMore: () => void;
  onOpenPalette: () => void;
  onOpenSearch: () => void;
  statsOpen?: boolean;
  onOpenStats?: () => void;
  efficiencyOpen?: boolean;
  efficiencyAlert?: boolean;
  onOpenEfficiency?: () => void;
  designOpen?: boolean;
  onOpenDesign?: () => void;
  onCollapse: () => void;
}

const subscribeNever = () => () => {};
const isClient = () => true;
const isServer = () => false;

const SKELETON_ROWS = 5;
const ORGANIZER_SKELETON_ROWS = 2;
const ORGANIZER_WAIT_MS = 1_500;

/** Left panel composed like the Codex desktop sidebar, in Hl islands. */
const ConversationSidebar = ({
  owner,
  selectedMachineId,
  onSelectMachine,
  conversations,
  model,
  loading,
  organizerError,
  organizerReady,
  organizerMachineName,
  onRetryOrganizer,
  hasMore,
  history,
  themeMode,
  onToggleTheme,
  onThemeChange,
  onNew,
  onLoadMore,
  onOpenPalette,
  onOpenSearch,
  statsOpen = false,
  onOpenStats,
  efficiencyOpen = false,
  efficiencyAlert = false,
  onOpenEfficiency,
  designOpen = false,
  onOpenDesign,
  onCollapse,
}: IConversationSidebarProps) => {
  const hydrated = useSyncExternalStore(subscribeNever, isClient, isServer);
  const mode = hydrated ? themeMode : 'system';
  const ThemeIcon = THEME_ICONS[mode];
  const machineDownNote = organizerMachineName
    ? t('agents.conversationSidebar.organizerDownNamed', { machine: organizerMachineName })
    : t('agents.conversationSidebar.organizerDownMain');
  const [machinesOpen, toggleMachines] = useMachinesOpen();
  const machinesId = useId();
  // Projects and pins sit above the conversations: shown as they came, they pushed the list down a moment
  // after it appeared. The list waits for them briefly and the whole column appears at once.
  const [organizerLate, setOrganizerLate] = useState(false);
  const organizerSettled = organizerReady || !!organizerError || organizerLate;
  useEffect(() => {
    if (loading || organizerSettled) return;
    const timer = setTimeout(() => setOrganizerLate(true), ORGANIZER_WAIT_MS);
    return () => clearTimeout(timer);
  }, [loading, organizerSettled]);
  const listReady = !loading && organizerSettled;

  return (
    <div className="tw-left" aria-label={t('agents.conversationSidebar.label')}>
      <section className="tw-island tw-brand-island">
        <SidebarHeader
          history={history}
          themeMode={mode}
          onThemeChange={onThemeChange}
          conversations={conversations}
          onOpenPalette={onOpenPalette}
          onOpenSearch={onOpenSearch}
          onCollapse={onCollapse}
        />
        <NewConversationAction onNew={onNew} />
        <CentreNav
          statsOpen={statsOpen}
          onOpenStats={onOpenStats}
          efficiencyOpen={efficiencyOpen}
          efficiencyAlert={efficiencyAlert}
          onOpenEfficiency={onOpenEfficiency}
          designOpen={designOpen}
          onOpenDesign={onOpenDesign}
        />
      </section>

      <section className="tw-island tw-list-island">
        <nav
          className="tw-list tw-slist"
          data-ready={listReady || undefined}
          aria-label={t('agents.conversationSidebar.listLabel')}
        >
          {!listReady && (
            <div
              className="tw-skeleton-list"
              aria-label={t('agents.conversationSidebar.loadingConversations')}
            >
              {Array.from({ length: SKELETON_ROWS }, (_, i) => (
                <div key={i} className="tw-skeleton-row">
                  <span className="tw-skeleton tw-skeleton-icon" />
                  <span className="tw-skeleton tw-skeleton-line" />
                </div>
              ))}
            </div>
          )}
          {listReady && !organizerReady && !organizerError && (
            <div className="tw-skeleton-list" aria-label={t('agents.conversationSidebar.loadingOrganizer')}>
              {Array.from({ length: ORGANIZER_SKELETON_ROWS }, (_, i) => (
                <div key={i} className="tw-skeleton-row">
                  <span className="tw-skeleton tw-skeleton-icon" />
                  <span className="tw-skeleton tw-skeleton-line" />
                </div>
              ))}
            </div>
          )}
          {listReady && !organizerReady && organizerError && (
            <div className="tw-sbanner" role="status">
              <p>{machineDownNote}</p>
              {organizerError !== ORGANIZER_MACHINE_DOWN && <small>{organizerError}</small>}
              <button type="button" className="tw-sbanner-retry" onClick={onRetryOrganizer}>
                <RotateCw aria-hidden="true" /> {t('agents.conversationSidebar.retry')}
              </button>
            </div>
          )}
          {listReady && organizerReady && organizerError && (
            <p className="tw-snote" role="status" title={organizerError}>
              {t('agents.conversationSidebar.organizerStale', { error: organizerError })}
            </p>
          )}
          {listReady && organizerReady && <PinnedSection model={model} />}
          {listReady && organizerReady && <ProjectsSection model={model} />}
          {listReady && <ConversationsSection model={model} hasMore={hasMore} onLoadMore={onLoadMore} />}
          {listReady && <DocsSection />}
        </nav>
      </section>

      <section className="tw-island tw-machines-island">
        {machinesOpen && (
          <MachinesPanel
            id={machinesId}
            selectedMachineId={selectedMachineId}
            onSelectMachine={onSelectMachine}
          />
        )}

        <div className="tw-profile">
          <span className="tw-avatar" aria-hidden="true">
            {initials(owner)}
          </span>
          <strong className="tw-profile-name" title={owner}>
            {owner.split('@')[0]}
          </strong>
          <MachinesChip open={machinesOpen} onToggle={toggleMachines} controls={machinesId} />
          <button
            type="button"
            className="tw-icon-button tw-profile-theme"
            aria-label={t('agents.conversationSidebar.themeLabel', { theme: THEME_LABELS[mode] })}
            title={t('agents.conversationSidebar.themeTitle', { theme: THEME_LABELS[mode] })}
            onClick={onToggleTheme}
          >
            <ThemeIcon />
          </button>
        </div>
      </section>
    </div>
  );
};

export default ConversationSidebar;
