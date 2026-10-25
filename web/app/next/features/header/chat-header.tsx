import { useState, type KeyboardEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import usePendingList, { PENDING } from '@/next/ui/use-pending-list';
import { UI_REQUEST, useUiRequest } from '@/next/stores/ui-requests';
import GoalDialog, { type IHeaderGoal } from '@/next/features/header/goal-dialog';
import ChatMenu from '@/next/features/sidebar/chat-menu';
import {
  BackgroundButton,
  TerminalButton,
  type IHeaderBackground,
  type IHeaderToggle,
} from '@/next/features/header/header-buttons';
import {
  MACHINE_HEALTH,
  type IChatActions,
  type ISidebarChat,
  type ISidebarProject,
  type TMachineHealth,
} from '@/next/features/sidebar/types';
import '@/next/styles/delegation.css';

const HEALTH_LABEL: Record<TMachineHealth, string> = {
  [MACHINE_HEALTH.ok]: t('next.machine.ok'),
  [MACHINE_HEALTH.warn]: t('next.machine.warn'),
  [MACHINE_HEALTH.critical]: t('next.machine.critical'),
  [MACHINE_HEALTH.bad]: t('next.machine.bad'),
  [MACHINE_HEALTH.off]: t('next.machine.off'),
};

export interface IHeaderMachine {
  name: string;
  health: TMachineHealth;
  /** What is wrong, said under the name on hover. */
  issue: string | null;
}

export type { IHeaderBackground };

export interface IChatHeaderProps {
  /** The open chat; null on the greeting (the header keeps only its buttons). */
  chat: ISidebarChat | null;
  projects: readonly ISidebarProject[] | null;
  actions: IChatActions;
  machine: IHeaderMachine | null;
  drawerOpen: boolean;
  onOpenDrawer: () => void;
  onNewChat?: () => void;
  pane: { open: boolean; onToggle: () => void } | null;
  /** The agent's terminal: shown while the chat's agent runs on a machine that serves it. */
  terminal: IHeaderToggle | null;
  background: IHeaderBackground | null;
  /** The chat's /goal and how to set it; null for an agent that has no such command. */
  goal?: IHeaderGoal | null;
  /** The chat whose agent delegated this one, before the title as the way back to it; null for any other chat. */
  parent?: { title: string; onOpen: () => void } | null;
  bell: { unread: boolean; open: boolean; onToggle: (anchor: HTMLElement) => void };
}

/**
 * The window's header: the chat's title is the largest thing in it, then its machine (an icon with a dot, the rest on
 * hover), «⋯» with the chat's actions and the agent's terminal, the right pane, «В фоне» while something runs, the bell.
 * No agent status here: the sidebar row and the timeline say it.
 */
const ChatHeader = ({
  chat,
  projects,
  actions,
  machine,
  drawerOpen,
  onOpenDrawer,
  onNewChat,
  pane,
  terminal,
  background,
  goal = null,
  parent = null,
  bell,
}: IChatHeaderProps) => {
  const { pending, run } = usePendingList();
  const [rename, setRename] = useState<{ value: string; saving: boolean } | null>(null);
  const [goalOpen, setGoalOpen] = useState(false);
  // «/goal» in the field opens the same dialog as the menu's item.
  useUiRequest(UI_REQUEST.goal, () => {
    if (goal) setGoalOpen(true);
  });
  const busy = chat ? pending.get(chat.id) : undefined;

  const saveRename = () => {
    if (!chat || !rename || rename.saving) return;
    const title = rename.value.trim();
    const onRename = actions.onRename;
    if (!title || title === chat.title || !onRename) {
      setRename(null);
      return;
    }
    setRename({ value: rename.value, saving: true });
    void run(chat.id, t('next.sidebar.renameFailed'), () => onRename(chat.id, title)).finally(() =>
      setRename(null),
    );
  };

  const onRenameKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveRename();
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      setRename(null);
    }
  };

  const chatAction = (failure: string, action: (id: string) => Promise<unknown>) => {
    if (chat) void run(chat.id, failure, () => action(chat.id));
  };

  const machineLabel = machine
    ? [`${machine.name} · ${HEALTH_LABEL[machine.health]}`, machine.issue].filter(Boolean).join('. ')
    : '';

  return (
    <header className="hx-header" data-busy={busy !== undefined || undefined}>
      <button
        type="button"
        className="hx-head-btn hx-header__menu"
        aria-label={t('next.header.openMenu')}
        aria-expanded={drawerOpen}
        onClick={onOpenDrawer}
      >
        <Icon name="menu" />
      </button>

      <div className="hx-header__title hx-enter" data-enter="down" data-eb="300">
        {chat && rename && (
          <input
            className="hx-item__rename hx-header__rename"
            type="text"
            autoComplete="off"
            aria-label={t('next.chatRow.renameLabel')}
            autoFocus
            value={rename.value}
            disabled={rename.saving}
            onFocus={(event) => event.currentTarget.select()}
            onChange={(event) => setRename({ value: event.target.value, saving: false })}
            onKeyDown={onRenameKey}
            onBlur={saveRename}
          />
        )}
        {chat && !rename && parent && (
          <button
            type="button"
            className="hx-header__parent"
            title={t('next.delegation.from', { title: parent.title })}
            aria-label={t('next.delegation.from', { title: parent.title })}
            onClick={parent.onOpen}
          >
            <span className="hx-header__parent-name">{parent.title}</span>
            <Icon name="chevron-right" size="sm" className="hx-header__parent-sep" />
          </button>
        )}
        {chat && !rename && (
          <h1 className="hx-header__name" title={chat.title}>
            {chat.title}
          </h1>
        )}
        {chat && busy !== undefined && (
          <i
            className="hx-icon hx-header__busy"
            data-i="loader-circle"
            data-size="sm"
            role="img"
            aria-label={busy === PENDING.slow ? t('next.chatRow.slow') : t('next.chatRow.busy')}
          />
        )}
        {chat && !rename && busy === undefined && (
          <ChatMenu
            title={chat.title}
            pinned={chat.pinned}
            projectId={chat.projectId}
            projects={projects}
            actions={actions}
            onStartRename={() => setRename({ value: chat.title, saving: false })}
            onPin={() => {
              const onPin = actions.onPin;
              if (onPin)
                chatAction(chat.pinned ? t('next.sidebar.unpinFailed') : t('next.sidebar.pinFailed'), (id) =>
                  onPin(id, !chat.pinned),
                );
            }}
            onMove={(projectId) => {
              const onMove = actions.onMove;
              if (onMove) chatAction(t('next.sidebar.moveFailed'), (id) => onMove(id, projectId));
            }}
            onDelete={() => {
              const onDelete = actions.onDelete;
              if (onDelete) chatAction(t('next.sidebar.deleteFailed'), onDelete);
            }}
            onCopyLink={
              actions.onCopyLink
                ? () => {
                    const onCopyLink = actions.onCopyLink;
                    if (onCopyLink) chatAction(t('next.sidebar.copyFailed'), onCopyLink);
                  }
                : undefined
            }
            onTerminal={actions.onTerminal ? () => actions.onTerminal?.(chat.id) : undefined}
            onStopTerminal={
              actions.onStopTerminal && chat.canStopTerminal
                ? () => {
                    const onStop = actions.onStopTerminal;
                    if (onStop) chatAction(t('next.sidebar.stopTerminalFailed'), onStop);
                  }
                : undefined
            }
            onRetitle={() => {
              const onRetitle = actions.onRetitle;
              if (onRetitle) chatAction(t('next.sidebar.retitleFailed'), onRetitle);
            }}
            onFork={
              actions.onFork && chat.canFork
                ? () => {
                    const onFork = actions.onFork;
                    if (onFork) chatAction(t('next.handoff.forkFailed'), onFork);
                  }
                : undefined
            }
            onMerge={actions.onMerge && chat.canMerge ? () => actions.onMerge?.(chat.id) : undefined}
            onGoal={goal ? () => setGoalOpen(true) : undefined}
            trigger={
              <button
                type="button"
                className="hx-head-btn hx-item__menu hx-header__more"
                aria-label={t('next.rowMenu.label')}
              >
                <Icon name="ellipsis" size="sm" />
              </button>
            }
          />
        )}
      </div>

      {/* As in the design, the machine shows only when something is wrong with it */}
      {chat && machine && machine.health !== MACHINE_HEALTH.ok && (
        <span
          className="hx-headmach hx-tipped"
          data-health={machine.health}
          tabIndex={0}
          aria-label={machineLabel}
        >
          <Icon name="server" size="sm" />
          <span className="hx-headmach__dot" />
          <span className="hx-tip" data-pos="below">
            {machineLabel}
          </span>
        </span>
      )}

      <span className="hx-spacer" />

      {/* The buttons hang on the window's right edge: when the sidebar slides, they stay (use-side-motion.ts). */}
      <div className="hx-header__end">
        {terminal && !pane?.open && <TerminalButton terminal={terminal} />}

        {pane && !pane.open && (
          <button
            type="button"
            className="hx-head-btn hx-tipped hx-header__tools"
            aria-pressed={pane.open}
            aria-label={t('next.header.pane')}
            onClick={pane.onToggle}
          >
            <Icon name="panel-right" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.header.pane')}
              <kbd>⌥</kbd>
              <kbd>⌘</kbd>
              <kbd>S</kbd>
            </span>
          </button>
        )}

        {chat && onNewChat && (
          <button
            type="button"
            className="hx-head-btn hx-header__new"
            aria-label={t('next.sidebar.newChat')}
            title={t('next.sidebar.newChat')}
            onClick={onNewChat}
          >
            <Icon name="square-pen" />
          </button>
        )}

        {background && !pane?.open && <BackgroundButton background={background} />}

        <button
          type="button"
          className="hx-head-btn hx-header__bell"
          aria-expanded={bell.open}
          aria-label={bell.unread ? t('next.header.bellUnread') : t('next.header.bell')}
          onClick={(event) => bell.onToggle(event.currentTarget)}
        >
          <Icon name="bell" />
          {bell.unread && <span className="hx-header__dot" />}
        </button>
      </div>
      {goal && <GoalDialog open={goalOpen} goal={goal} onClose={() => setGoalOpen(false)} />}
    </header>
  );
};

export default ChatHeader;
