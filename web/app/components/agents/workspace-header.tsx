import { appConfig } from '@/platform/config';
import { PanelLeftOpen, SquarePen } from 'lucide-react';
import ProviderIcon, { PROVIDER_LABELS } from '@/components/agents/provider-icon';
import NetworkStatus from '@/components/agents/network-status';
import { MachineLoadIndicator } from '@/components/agents/machine-load';
import ProjectSlot from '@/components/agents/header/project-slot';
import EconomicsSlot from '@/components/agents/header/economics-slot';
import ConversationMenu from '@/components/agents/header/conversation-menu';
import type { IHeaderSlotProps } from '@/components/agents/header/header-slot';
import { cleanConversationTitle, type Conversation } from '@/lib/hl-conversations';
import type { ReactNode } from 'react';
import { t } from '@/lib/i18n';

interface IWorkspaceHeaderProps {
  selected: Conversation | null;
  sidebarOpen: boolean;
  onToggleSidebar: () => void;
  onHome: () => void;
  onNew: () => void;
  /** What every header slot knows about the conversation on screen. */
  slot: IHeaderSlotProps;
  /** Panel toggles at the right edge, after the conversation menu. */
  actions?: ReactNode;
  /** Search over the conversation on screen (⌘J), offered in its menu. */
  onSearch?: () => void;
  /** Crumb for a view that is not a conversation, e.g. «Statistics». */
  heading?: string;
  /** A link names a conversation the list does not have yet: its title is a skeleton. */
  opening?: boolean;
}

const agentLabel = (conversation: Conversation) =>
  conversation.kind === 'chat'
    ? t('agents.workspaceHeader.chatgpt')
    : [PROVIDER_LABELS[conversation.provider] ?? conversation.provider, conversation.machineName]
        .filter(Boolean)
        .join(' · ');

const WorkspaceHeader = ({
  selected,
  sidebarOpen,
  onToggleSidebar,
  onHome,
  onNew,
  slot,
  actions,
  onSearch,
  heading,
  opening,
}: IWorkspaceHeaderProps) => {
  return (
    <header className="tw-header">
      {!sidebarOpen && (
        <div className="tw-header-lead">
          <button
            type="button"
            className="tw-icon-button"
            aria-label={t('agents.workspaceHeader.showConversations')}
            title={t('agents.workspaceHeader.showConversationsHint')}
            onClick={onToggleSidebar}
          >
            <PanelLeftOpen />
          </button>
          <button
            type="button"
            className="tw-icon-button"
            aria-label={t('agents.workspaceHeader.newConversation')}
            title={t('agents.workspaceHeader.newConversationHint')}
            onClick={onNew}
          >
            <SquarePen />
          </button>
        </div>
      )}
      <button type="button" className="tw-crumb" data-testid="home-crumb" onClick={onHome}>
        {appConfig().productName}
      </button>
      {!selected && heading && (
        <>
          <span className="tw-crumb-sep" aria-hidden="true">
            /
          </span>
          <span className="tw-header-title">{heading}</span>
        </>
      )}
      {!selected && opening && (
        <>
          <span className="tw-crumb-sep" aria-hidden="true">
            /
          </span>
          <span
            className="tw-skeleton tw-header-title-skeleton"
            role="status"
            aria-label={t('agents.workspaceHeader.opening')}
          />
        </>
      )}
      <ProjectSlot {...slot} />
      {selected && (
        <>
          <span className="tw-crumb-sep" aria-hidden="true">
            /
          </span>
          <span className="tw-header-title" title={cleanConversationTitle(selected.title)}>
            {cleanConversationTitle(selected.title)}
          </span>
          <span className="tw-header-agent-mark" title={agentLabel(selected)}>
            <ProviderIcon provider={selected.provider} size="sm" />
            <span className="tw-header-agent">
              {selected.kind === 'chat'
                ? t('agents.workspaceHeader.chatgpt')
                : selected.machineName || PROVIDER_LABELS[selected.provider] || selected.provider}
            </span>
          </span>
        </>
      )}
      <MachineLoadIndicator machine={slot.machineId ?? ''} />
      <NetworkStatus />
      <span className="tw-header-spacer" />
      <div className="tw-header-slots">
        <EconomicsSlot {...slot} />
        <ConversationMenu {...slot} onSearch={onSearch} />
      </div>
      {actions}
    </header>
  );
};

export default WorkspaceHeader;
