import { memo, useState, type KeyboardEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { PENDING, type TPending } from '@/next/ui/use-pending-list';
import ChatMenu, { type IChatMenuProps } from '@/next/features/sidebar/chat-menu';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';

export const ROW_VARIANT = { item: 'item', kid: 'kid', pinned: 'pinned', delegated: 'delegated' } as const;
export type TRowVariant = (typeof ROW_VARIANT)[keyof typeof ROW_VARIANT];

const ROW_CLASS: Record<TRowVariant, string> = {
  item: 'hx-item',
  kid: 'hx-item hx-nav__kid',
  pinned: 'hx-item hx-pinned',
  delegated: 'hx-item hx-item--delegated',
};

/** The rename field: its own text while it is open, saved on Enter or when it loses focus, dropped on Escape. */
export const RenameField = ({
  title,
  saving,
  label = t('next.chatRow.renameLabel'),
  onDone,
}: {
  title: string;
  saving: boolean;
  /** What the field is called for a screen reader. */
  label?: string;
  onDone: (title: string | null) => void;
}) => {
  const [value, setValue] = useState(title);
  const commit = () => {
    const next = value.trim();
    onDone(next && next !== title ? next : null);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      commit();
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      onDone(null);
    }
  };
  return (
    <input
      className="hx-item__rename"
      type="text"
      autoComplete="off"
      aria-label={label}
      // The field opens on a click of «Переименовать»: focus goes to it as the menu goes away.
      autoFocus
      value={value}
      disabled={saving}
      onFocus={(event) => event.currentTarget.select()}
      onChange={(event) => setValue(event.target.value)}
      onKeyDown={onKeyDown}
      onBlur={() => {
        if (!saving) commit();
      }}
    />
  );
};

/** What a row does with its chat; one stable object for the whole list, so a row redraws only when its chat does. */
export interface IRowHandlers {
  open: (id: string) => void;
  renameStart: (id: string) => void;
  renameEnd: (chat: ISidebarChat, title: string | null) => void;
  pin: (chat: ISidebarChat) => void;
  move: (id: string, projectId: string | null) => void;
  remove: (id: string) => void;
  /** A chat into the favorites, in a group or (null) ungrouped. */
  place: (chat: ISidebarChat, groupId: string | null) => void;
  /** A favorite one place up (-1) or down (1). */
  shift: (chat: ISidebarChat, direction: -1 | 1) => void;
  copyLink: ((id: string) => void) | undefined;
  openInNewTab: ((id: string) => void) | undefined;
  terminal: ((id: string) => void) | undefined;
  stopTerminal: ((id: string) => void) | undefined;
  fork: ((id: string) => void) | undefined;
  continueIn?: ((id: string) => void) | undefined;
  /** Shows or hides the chats a chat delegated. */
  toggleKids?: (id: string) => void;
}

export const RENAME = { off: 'off', editing: 'editing', saving: 'saving' } as const;
export type TRename = (typeof RENAME)[keyof typeof RENAME];

/**
 * The row's menu is mounted on demand: a list of a hundred chats does not carry a hundred menus. A mouse arms it on
 * hover, so the click lands on the real trigger; a tap or a key presses a plain «…» that mounts the menu open.
 */
const MENU_MOUNT = { none: 'none', ready: 'ready', open: 'open' } as const;
type TMenuMount = (typeof MENU_MOUNT)[keyof typeof MENU_MOUNT];

/** Where a favorite chat stands: its group (null: ungrouped) and whether it can go up or down in that place. */
export interface IFavSlot {
  groupId: string | null;
  prev: boolean;
  next: boolean;
}

export interface IChatRowProps {
  chat: ISidebarChat;
  variant: TRowVariant;
  current: boolean;
  /** An action of this row waits for the server. */
  pending: TPending | undefined;
  /** It came in after the list was drawn: it slides in. */
  fresh: boolean;
  rename: TRename;
  projects: IChatMenuProps['projects'];
  actions: IChatMenuProps['actions'];
  /** The groups of the favorites, for «Избранное» of the menu. */
  favGroups: IChatMenuProps['favGroups'];
  /** Set on a favorite chat: it takes drops and its menu can move it. */
  slot: IFavSlot | null;
  handlers: IRowHandlers;
  /** Its place in the sidebar's entrance: the group's delay and the index in its cascade. */
  enter?: IRowEnter;
  /** Set on a chat that delegated chats: whether they show under it. */
  kidsOpen?: boolean;
}

export interface IRowEnter {
  base: string;
  index: number;
}

/**
 * One chat of the sidebar: the title opens it; the end cell holds the agent's status and, on hover, «…» in its
 * place. On a touch screen a tap on the status opens the menu. A busy row dims and spins until the server answers.
 */
const ChatRow = ({
  chat,
  variant,
  current,
  pending,
  fresh,
  rename,
  projects,
  actions,
  favGroups,
  slot,
  handlers,
  enter,
  kidsOpen,
}: IChatRowProps) => {
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuMount, setMenuMount] = useState<TMenuMount>(MENU_MOUNT.none);
  const look = chat.status === CHAT_STATUS.none ? null : STATUS_LOOK[chat.status];
  const busy = pending !== undefined;
  const label = pending === PENDING.slow ? t('next.chatRow.slow') : (chat.waitsFor ?? look?.label);
  const { copyLink, openInNewTab, terminal, stopTerminal, fork, continueIn } = handlers;
  const showMenu = !busy && rename === RENAME.off;
  const rowClass = enter ? `${ROW_CLASS[variant]} hx-enter` : ROW_CLASS[variant];

  return (
    <div
      className={fresh ? `${ROW_CLASS[variant]} hx-slot-in` : rowClass}
      data-enter={enter && !fresh ? 'up' : undefined}
      data-eb={enter && !fresh ? enter.base : undefined}
      data-i={enter && !fresh ? Math.min(enter.index, 24) : undefined}
      data-status={chat.status}
      data-terminal={chat.terminal ? undefined : 'off'}
      data-chat={chat.id}
      data-launch={chat.launch ? '' : undefined}
      data-busy={busy || undefined}
      data-slow={pending === PENDING.slow || undefined}
      data-menu={menuOpen || undefined}
      data-drag={busy ? undefined : 'chat'}
      data-drag-id={busy ? undefined : chat.id}
      data-flip={slot ? chat.id : undefined}
      data-drop={slot ? 'chat' : undefined}
      data-drop-id={slot ? chat.id : undefined}
      aria-current={current ? 'page' : undefined}
      aria-busy={busy || undefined}
      onPointerEnter={(event) => {
        if (event.pointerType === 'mouse' && menuMount === MENU_MOUNT.none) setMenuMount(MENU_MOUNT.ready);
      }}
    >
      {variant === ROW_VARIANT.pinned && (
        <span className="hx-mono" aria-hidden="true">
          {chat.title.slice(0, 1).toUpperCase()}
        </span>
      )}
      {rename === RENAME.off ? (
        <button type="button" className="hx-item__open" onClick={() => handlers.open(chat.id)}>
          <span className="hx-item__title">{chat.launch ? chat.launch.model : chat.title}</span>
        </button>
      ) : (
        <RenameField
          title={chat.title}
          saving={rename === RENAME.saving}
          onDone={(title) => handlers.renameEnd(chat, title)}
        />
      )}
      {kidsOpen !== undefined && rename === RENAME.off && (
        <button
          type="button"
          className="hx-item__fold"
          aria-expanded={kidsOpen}
          aria-label={kidsOpen ? t('next.sidebar.hideDelegated') : t('next.sidebar.showDelegated')}
          title={kidsOpen ? t('next.sidebar.hideDelegated') : t('next.sidebar.showDelegated')}
          onClick={() => handlers.toggleKids?.(chat.id)}
        >
          <Icon name="chevron-right" size="sm" />
        </button>
      )}
      <span className="hx-item__end">
        {busy && (
          <i
            className="hx-icon hx-chat-status"
            data-i="loader-circle"
            data-size="sm"
            data-status="busy"
            role="img"
            aria-label={label ?? t('next.chatRow.busy')}
            title={label ?? t('next.chatRow.busy')}
          />
        )}
        {!busy && look && (
          <i
            className="hx-icon hx-chat-status"
            data-i={look.icon}
            data-size="sm"
            data-status={chat.status}
            role="img"
            aria-label={chat.waitsFor ?? look.label}
            title={chat.waitsFor ?? look.label}
          />
        )}
        {showMenu && menuMount === MENU_MOUNT.none && (
          <button
            type="button"
            className="hx-item__more hx-item__menu"
            aria-label={t('next.rowMenu.label')}
            aria-haspopup="menu"
            onClick={() => {
              setMenuMount(MENU_MOUNT.open);
              setMenuOpen(true);
            }}
          >
            <Icon name="ellipsis" size="sm" />
          </button>
        )}
        {showMenu && menuMount !== MENU_MOUNT.none && (
          <ChatMenu
            defaultOpen={menuMount === MENU_MOUNT.open}
            title={chat.title}
            pinned={chat.pinned}
            projectId={chat.projectId}
            projects={projects}
            actions={actions}
            favGroups={favGroups}
            slot={slot}
            onStartRename={() => {
              // The menu goes with the rename field (it is not drawn while the field is): it must not come back
              // open, nor leave its row marked as holding a menu, when the field is done.
              setMenuMount(MENU_MOUNT.none);
              setMenuOpen(false);
              handlers.renameStart(chat.id);
            }}
            onPin={() => handlers.pin(chat)}
            onPlace={(groupId) => handlers.place(chat, groupId)}
            onShift={(direction) => handlers.shift(chat, direction)}
            onMove={(projectId) => handlers.move(chat.id, projectId)}
            onDelete={() => handlers.remove(chat.id)}
            onCopyLink={copyLink ? () => copyLink(chat.id) : undefined}
            onOpenInNewTab={openInNewTab ? () => openInNewTab(chat.id) : undefined}
            onTerminal={terminal ? () => terminal(chat.id) : undefined}
            onStopTerminal={stopTerminal && chat.canStopTerminal ? () => stopTerminal(chat.id) : undefined}
            onFork={fork && chat.canFork ? () => fork(chat.id) : undefined}
            onContinue={continueIn && chat.canContinue ? () => continueIn(chat.id) : undefined}
            onOpenChange={(open) => {
              setMenuOpen(open);
              // Opened by a tap, it opened once: coming back after a rename or an action it stays shut.
              if (!open) setMenuMount((now) => (now === MENU_MOUNT.open ? MENU_MOUNT.ready : now));
            }}
            trigger={
              <button
                type="button"
                className="hx-item__more hx-item__menu"
                aria-label={t('next.rowMenu.label')}
              >
                <Icon name="ellipsis" size="sm" />
              </button>
            }
          />
        )}
      </span>
    </div>
  );
};

export default memo(ChatRow);
