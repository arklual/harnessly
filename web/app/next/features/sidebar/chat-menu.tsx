import { Menu } from '@base-ui/react/menu';
import { useState, type ReactElement } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { pointAnchor, type IMenuPoint } from '@/next/ui/context-menu';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { MenuItem, MenuSeparator, SheetMenu } from '@/next/ui/sheet-menu';
import { usePhone } from '@/next/ui/use-media';
import type { IChatActions, ISidebarProject } from '@/next/features/sidebar/types';

const NO_GROUPS: readonly { id: string; name: string }[] = [];

const VIEW = { root: 'root', move: 'move', favorite: 'favorite', remove: 'remove', stop: 'stop' } as const;
type TView = (typeof VIEW)[keyof typeof VIEW];

/** An item the phone's header moves into «⋯» (the machine, the right pane, «Слои»): drawn first: on the phone they are the most used. */
export interface IChatMenuExtra {
  key: string;
  icon: string;
  label: string;
  onClick: () => void;
}

const NO_EXTRAS: readonly IChatMenuExtra[] = [];

export interface IChatMenuProps {
  /** The element that opens the menu: «…» of a row or of the header. */
  trigger: ReactElement;
  title: string;
  pinned: boolean;
  projectId: string | null;
  /** null while projects load: «Перенести» waits for them. */
  projects: readonly ISidebarProject[] | null;
  actions: IChatActions;
  /** The groups of the favorites: with any, «Избранное» opens a view to pick one. */
  favGroups?: readonly { id: string; name: string }[];
  /** Set on a favorite chat: where it stands, for «Выше» and «Ниже» and the check in the view. */
  slot?: { groupId: string | null; prev: boolean; next: boolean } | null;
  /** Inline rename of the row (or the header) that opened the menu. */
  onStartRename?: () => void;
  /** Each choice starts a pessimistic action of the owner: the menu closes and the row goes busy. */
  onPin: () => void;
  /** Into the favorites, in a group or (null) ungrouped. */
  onPlace?: (groupId: string | null) => void;
  /** One place up (-1) or down (1) among its neighbours. */
  onShift?: (direction: -1 | 1) => void;
  onMove: (projectId: string | null) => void;
  onDelete: () => void;
  onCopyLink?: () => void;
  onOpenInNewTab?: () => void;
  onTerminal?: () => void;
  /** «Завершить терминал»: set while the chat's terminal runs; asks first, in the menu itself. */
  onStopTerminal?: () => void;
  /** «Обновить название»: a model names the chat anew (a beta function). */
  onRetitle?: () => void;
  /** «Ответвить»: a new chat with the history up to the last finished turn. */
  onFork?: () => void;
  /** «Влить обратно»: a fork's result into the chat it came from (the open chat's sheet). */
  onMerge?: () => void;
  onContinue?: () => void;
  /** «Продолжить на другой ВМ»: the chat goes on, with its history, on another machine of the person. */
  onMoveVm?: () => void;
  /** The machine the chat lives on, said quietly at the top (only when the person has several). */
  machine?: string;
  /** «Цель…»: the header's menu of a Claude chat opens the goal's dialog. */
  onGoal?: () => void;
  /** «Оформление…»: the chat's own look (features/looks). */
  onLook?: () => void;
  /** «Поделиться…» (beta «chatShare»): the header's menu opens the sharing dialog. */
  onShare?: () => void;
  /** The header's own buttons on a narrow phone, where only the bell and «⋯» stay. */
  extras?: readonly IChatMenuExtra[];
  onOpenChange?: (open: boolean) => void;
  /** Mounted already open: a row mounts its menu on the first press of «…». */
  defaultOpen?: boolean;
  side?: 'bottom' | 'top' | 'right';
  align?: 'start' | 'end';
  /** Opened by a right click: the menu stands at that point, not at «…». */
  at?: IMenuPoint | null;
  /** The menu has gone (its exit played). */
  onClosed?: () => void;
}

/**
 * «…» of a chat, as in the design: rename, pin, move to a project (a second view with a way back), copy the link,
 * end the chat's terminal and delete (both ask first, in the menu itself). On the phone it also opens the agent's terminal.
 */
const ChatMenu = ({
  trigger,
  title,
  pinned,
  projectId,
  projects,
  actions,
  favGroups = NO_GROUPS,
  slot = null,
  onStartRename,
  onPin,
  onPlace,
  onShift,
  onMove,
  onDelete,
  onCopyLink,
  onOpenInNewTab,
  onTerminal,
  onStopTerminal,
  onFork,
  onMerge,
  onContinue,
  onMoveVm,
  machine,
  onGoal,
  onLook,
  onShare,
  extras = NO_EXTRAS,
  onRetitle,
  onOpenChange,
  defaultOpen = false,
  side = 'bottom',
  align = 'start',
  at = null,
  onClosed,
}: IChatMenuProps) => {
  const [view, setView] = useState<TView>(VIEW.root);
  const menuActions = useMenuActions();
  const phone = usePhone();
  // With groups in the favorites, «Избранное» is a choice of where; without, pinning is one click.
  const grouping = actions.onPlaceFavorite !== undefined && favGroups.length > 0;

  const items = (
    <>
      {view === VIEW.root && (
        <>
          {machine && <div className="hx-cmenu__vm">{t('next.rowMenu.onVm', { machine })}</div>}
          {onMoveVm && (
            <MenuItem className="hx-cmenu__item" onClick={onMoveVm}>
              <Icon name="server" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.moveVm')}</span>
            </MenuItem>
          )}
          {(machine || onMoveVm) && <MenuSeparator className="hx-cmenu__sep" />}
          {onTerminal && (
            <>
              <MenuItem className="hx-cmenu__item hx-cmenu__term" onClick={onTerminal}>
                <Icon name="square-terminal" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.terminal')}</span>
              </MenuItem>
              <MenuSeparator className="hx-cmenu__sep hx-cmenu__term" />
            </>
          )}
          {extras.map((extra) => (
            <MenuItem key={extra.key} className="hx-cmenu__item" onClick={extra.onClick}>
              <Icon name={extra.icon} size="sm" />
              <span className="hx-cmenu__txt">{extra.label}</span>
            </MenuItem>
          ))}
          {extras.length > 0 && <MenuSeparator className="hx-cmenu__sep" />}
          {onOpenInNewTab && (
            <MenuItem className="hx-cmenu__item" onClick={onOpenInNewTab}>
              <Icon name="external-link" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.openInNewTab')}</span>
            </MenuItem>
          )}
          {onContinue && (
            <MenuItem className="hx-cmenu__item" onClick={onContinue}>
              <Icon name="arrow-left-right" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.continue')}</span>
            </MenuItem>
          )}
          {onFork && (
            <MenuItem className="hx-cmenu__item" onClick={onFork}>
              <Icon name="git-fork" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.fork')}</span>
            </MenuItem>
          )}
          {onGoal && (
            <MenuItem className="hx-cmenu__item" onClick={onGoal}>
              <Icon name="locate" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.goal')}</span>
            </MenuItem>
          )}
          {onLook && (
            <MenuItem className="hx-cmenu__item" onClick={onLook}>
              <Icon name="palette" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.look')}</span>
            </MenuItem>
          )}
          {actions.onRename && onStartRename && (
            <MenuItem className="hx-cmenu__item" onClick={onStartRename}>
              <Icon name="pencil" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.rename')}</span>
            </MenuItem>
          )}
          {actions.onRetitle && onRetitle && (
            <MenuItem className="hx-cmenu__item" onClick={onRetitle}>
              <Icon name="sparkles" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.retitle')}</span>
            </MenuItem>
          )}
          {actions.onPin && !grouping && (
            <MenuItem className="hx-cmenu__item" onClick={onPin}>
              <Icon name={pinned ? 'pin-off' : 'pin'} size="sm" />
              <span className="hx-cmenu__txt">
                {pinned ? t('next.rowMenu.unpin') : t('next.rowMenu.pin')}
              </span>
            </MenuItem>
          )}
          {grouping && (
            <MenuItem className="hx-cmenu__item" closeOnClick={false} onClick={() => setView(VIEW.favorite)}>
              <Icon name="pin" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.favorites')}</span>
              <Icon name="chevron-right" size="sm" className="hx-cmenu__chev" />
            </MenuItem>
          )}
          {actions.onPlaceFavorite && slot?.prev && (
            <MenuItem className="hx-cmenu__item" onClick={() => onShift?.(-1)}>
              <Icon name="arrow-up" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.up')}</span>
            </MenuItem>
          )}
          {actions.onPlaceFavorite && slot?.next && (
            <MenuItem className="hx-cmenu__item" onClick={() => onShift?.(1)}>
              <Icon name="arrow-down" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.down')}</span>
            </MenuItem>
          )}
          {onCopyLink && (
            <MenuItem className="hx-cmenu__item" onClick={onCopyLink}>
              <Icon name="share" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.copyLink')}</span>
            </MenuItem>
          )}
          {onShare && (
            <MenuItem className="hx-cmenu__item" onClick={onShare}>
              <Icon name="share" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.share')}</span>
            </MenuItem>
          )}
          {actions.onMove && (
            <MenuItem
              className="hx-cmenu__item"
              closeOnClick={false}
              disabled={projects === null}
              onClick={() => setView(VIEW.move)}
            >
              <Icon name="folder-input" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.move')}</span>
              <Icon name="chevron-right" size="sm" className="hx-cmenu__chev" />
            </MenuItem>
          )}
          {onMerge && (
            <MenuItem className="hx-cmenu__item" onClick={onMerge}>
              <Icon name="git-merge" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.merge')}</span>
            </MenuItem>
          )}
          {(actions.onDelete || onStopTerminal) && <MenuSeparator className="hx-cmenu__sep" />}
          {onStopTerminal && (
            <MenuItem
              className="hx-cmenu__item"
              data-danger="true"
              closeOnClick={false}
              onClick={() => setView(VIEW.stop)}
            >
              <Icon name="circle-stop" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.stopTerminal')}</span>
            </MenuItem>
          )}
          {actions.onDelete && (
            <>
              <MenuItem
                className="hx-cmenu__item"
                data-danger="true"
                closeOnClick={false}
                onClick={() => setView(VIEW.remove)}
              >
                <Icon name="trash-2" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.delete')}</span>
              </MenuItem>
            </>
          )}
        </>
      )}

      {view === VIEW.move && (
        <>
          <MenuItem className="hx-cmenu__back" closeOnClick={false} onClick={() => setView(VIEW.root)}>
            <Icon name="chevron-left" size="sm" />
            {t('next.rowMenu.move')}
          </MenuItem>
          {(projects ?? []).map((project) => (
            <MenuItem
              key={project.id}
              className="hx-cmenu__item"
              role="menuitemradio"
              aria-checked={project.id === projectId}
              onClick={() => {
                if (project.id !== projectId) onMove(project.id);
              }}
            >
              <Icon name="folder" size="sm" />
              <span className="hx-cmenu__txt">{project.name}</span>
              <Icon name="check" size="sm" className="hx-cmenu__check" />
            </MenuItem>
          ))}
          {projectId !== null && (
            <MenuItem className="hx-cmenu__item" onClick={() => onMove(null)}>
              <Icon name="x" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.unassign')}</span>
            </MenuItem>
          )}
          {actions.onNewProject && (
            <>
              <MenuSeparator className="hx-cmenu__sep" />
              <MenuItem className="hx-cmenu__item" onClick={actions.onNewProject}>
                <Icon name="folder-plus" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.newProject')}</span>
              </MenuItem>
            </>
          )}
        </>
      )}

      {view === VIEW.favorite && (
        <>
          <MenuItem className="hx-cmenu__back" closeOnClick={false} onClick={() => setView(VIEW.root)}>
            <Icon name="chevron-left" size="sm" />
            {t('next.rowMenu.favorites')}
          </MenuItem>
          <MenuItem
            className="hx-cmenu__item"
            role="menuitemradio"
            aria-checked={slot !== null && slot.groupId === null}
            onClick={() => {
              if (slot === null || slot.groupId !== null) onPlace?.(null);
            }}
          >
            <Icon name="pin" size="sm" />
            <span className="hx-cmenu__txt">{t('next.rowMenu.noGroup')}</span>
            <Icon name="check" size="sm" className="hx-cmenu__check" />
          </MenuItem>
          {favGroups.map((group) => (
            <MenuItem
              key={group.id}
              className="hx-cmenu__item"
              role="menuitemradio"
              aria-checked={slot?.groupId === group.id}
              onClick={() => {
                if (slot?.groupId !== group.id) onPlace?.(group.id);
              }}
            >
              <Icon name="folder" size="sm" />
              <span className="hx-cmenu__txt">{group.name}</span>
              <Icon name="check" size="sm" className="hx-cmenu__check" />
            </MenuItem>
          ))}
          {pinned && actions.onPin && (
            <>
              <MenuSeparator className="hx-cmenu__sep" />
              <MenuItem className="hx-cmenu__item" onClick={onPin}>
                <Icon name="pin-off" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.unpin')}</span>
              </MenuItem>
            </>
          )}
        </>
      )}

      {view === VIEW.stop && onStopTerminal && (
        <>
          <div className="hx-cmenu__ask">
            <b>{t('next.rowMenu.stopTerminalAsk')}</b>
            <span>{t('next.rowMenu.stopTerminalNote')}</span>
          </div>
          <div className="hx-cmenu__row">
            <MenuItem className="hx-cmenu__btn" closeOnClick={false} onClick={() => setView(VIEW.root)}>
              {t('next.rowMenu.cancel')}
            </MenuItem>
            <MenuItem className="hx-cmenu__btn" data-danger="true" onClick={onStopTerminal}>
              {t('next.rowMenu.stopTerminalYes')}
            </MenuItem>
          </div>
        </>
      )}

      {view === VIEW.remove && (
        <>
          <div className="hx-cmenu__ask">
            <b>{t('next.rowMenu.deleteAsk', { title })}</b>
            <span>{t('next.rowMenu.deleteNote')}</span>
          </div>
          <div className="hx-cmenu__row">
            <MenuItem className="hx-cmenu__btn" closeOnClick={false} onClick={() => setView(VIEW.root)}>
              {t('next.rowMenu.cancel')}
            </MenuItem>
            <MenuItem className="hx-cmenu__btn" data-danger="true" onClick={onDelete}>
              {t('next.rowMenu.delete')}
            </MenuItem>
          </div>
        </>
      )}
    </>
  );

  // On the phone a sheet of our own: a Base UI menu's open is one long task there (ui/sheet-menu.tsx).
  if (phone)
    return (
      <SheetMenu
        trigger={trigger}
        label={t('next.rowMenu.label')}
        className="hx-cmenu hx-sheet-phone"
        view={view}
        defaultOpen={defaultOpen}
        onOpenChange={onOpenChange}
        onClosed={() => {
          setView(VIEW.root);
          onClosed?.();
        }}
      >
        {items}
      </SheetMenu>
    );

  return (
    <MenuRoot
      actionsRef={menuActions}
      defaultOpen={defaultOpen}
      onOpenChange={(open) => onOpenChange?.(open)}
      onOpenChangeComplete={(open) => {
        if (open) return;
        setView(VIEW.root);
        onClosed?.();
      }}
    >
      <Menu.Trigger render={trigger} />
      <MenuPanel
        actions={menuActions}
        className="hx-cmenu"
        label={t('next.rowMenu.label')}
        view={view}
        side={at ? 'bottom' : side}
        align={at ? 'start' : align}
        sideOffset={at ? 2 : 6}
        anchor={at ? pointAnchor(at) : undefined}
      >
        {items}
      </MenuPanel>
    </MenuRoot>
  );
};

export default ChatMenu;
