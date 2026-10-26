import { Menu } from '@base-ui/react/menu';
import { useState, type ReactElement } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import type { IChatActions, ISidebarProject } from '@/next/features/sidebar/types';

const NO_GROUPS: readonly { id: string; name: string }[] = [];

const VIEW = { root: 'root', move: 'move', favorite: 'favorite', remove: 'remove', stop: 'stop' } as const;
type TView = (typeof VIEW)[keyof typeof VIEW];

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
  /** «Цель…»: the header's menu of a Claude chat opens the goal's dialog. */
  onGoal?: () => void;
  onOpenChange?: (open: boolean) => void;
  /** Mounted already open: a row mounts its menu on the first press of «…». */
  defaultOpen?: boolean;
  side?: 'bottom' | 'top' | 'right';
  align?: 'start' | 'end';
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
  onGoal,
  onRetitle,
  onOpenChange,
  defaultOpen = false,
  side = 'bottom',
  align = 'start',
}: IChatMenuProps) => {
  const [view, setView] = useState<TView>(VIEW.root);
  const menuActions = useMenuActions();
  // With groups in the favorites, «Избранное» is a choice of where; without, pinning is one click.
  const grouping = actions.onPlaceFavorite !== undefined && favGroups.length > 0;

  return (
    <Menu.Root
      actionsRef={menuActions}
      defaultOpen={defaultOpen}
      onOpenChange={(open) => onOpenChange?.(open)}
      onOpenChangeComplete={(open) => {
        if (!open) setView(VIEW.root);
      }}
    >
      <Menu.Trigger render={trigger} />
      <MenuPanel
        actions={menuActions}
        className="hx-cmenu"
        label={t('next.rowMenu.label')}
        view={view}
        side={side}
        align={align}
        sideOffset={6}
      >
        {view === VIEW.root && (
          <>
            {onTerminal && (
              <>
                <Menu.Item className="hx-cmenu__item hx-cmenu__term" onClick={onTerminal}>
                  <Icon name="square-terminal" size="sm" />
                  <span className="hx-cmenu__txt">{t('next.rowMenu.terminal')}</span>
                </Menu.Item>
                <Menu.Separator className="hx-cmenu__sep hx-cmenu__term" />
              </>
            )}
            {onOpenInNewTab && (
              <Menu.Item className="hx-cmenu__item" onClick={onOpenInNewTab}>
                <Icon name="external-link" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.openInNewTab')}</span>
              </Menu.Item>
            )}
            {actions.onRename && onStartRename && (
              <Menu.Item className="hx-cmenu__item" onClick={onStartRename}>
                <Icon name="pencil" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.rename')}</span>
              </Menu.Item>
            )}
            {actions.onRetitle && onRetitle && (
              <Menu.Item className="hx-cmenu__item" onClick={onRetitle}>
                <Icon name="sparkles" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.retitle')}</span>
              </Menu.Item>
            )}
            {onFork && (
              <Menu.Item className="hx-cmenu__item" onClick={onFork}>
                <Icon name="git-fork" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.fork')}</span>
              </Menu.Item>
            )}
            {onMerge && (
              <Menu.Item className="hx-cmenu__item" onClick={onMerge}>
                <Icon name="git-merge" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.merge')}</span>
              </Menu.Item>
            )}
            {onContinue && (
              <Menu.Item className="hx-cmenu__item" onClick={onContinue}>
                <Icon name="arrow-left-right" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.continue')}</span>
              </Menu.Item>
            )}
            {onGoal && (
              <Menu.Item className="hx-cmenu__item" onClick={onGoal}>
                <Icon name="locate" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.goal')}</span>
              </Menu.Item>
            )}
            {actions.onPin && !grouping && (
              <Menu.Item className="hx-cmenu__item" onClick={onPin}>
                <Icon name={pinned ? 'pin-off' : 'pin'} size="sm" />
                <span className="hx-cmenu__txt">
                  {pinned ? t('next.rowMenu.unpin') : t('next.rowMenu.pin')}
                </span>
              </Menu.Item>
            )}
            {grouping && (
              <Menu.Item
                className="hx-cmenu__item"
                closeOnClick={false}
                onClick={() => setView(VIEW.favorite)}
              >
                <Icon name="pin" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.favorites')}</span>
                <Icon name="chevron-right" size="sm" className="hx-cmenu__chev" />
              </Menu.Item>
            )}
            {actions.onPlaceFavorite && slot?.prev && (
              <Menu.Item className="hx-cmenu__item" onClick={() => onShift?.(-1)}>
                <Icon name="arrow-up" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.up')}</span>
              </Menu.Item>
            )}
            {actions.onPlaceFavorite && slot?.next && (
              <Menu.Item className="hx-cmenu__item" onClick={() => onShift?.(1)}>
                <Icon name="arrow-down" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.down')}</span>
              </Menu.Item>
            )}
            {actions.onMove && (
              <Menu.Item
                className="hx-cmenu__item"
                closeOnClick={false}
                disabled={projects === null}
                onClick={() => setView(VIEW.move)}
              >
                <Icon name="folder-input" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.move')}</span>
                <Icon name="chevron-right" size="sm" className="hx-cmenu__chev" />
              </Menu.Item>
            )}
            {onCopyLink && (
              <Menu.Item className="hx-cmenu__item" onClick={onCopyLink}>
                <Icon name="link" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.copyLink')}</span>
              </Menu.Item>
            )}
            {(actions.onDelete || onStopTerminal) && <Menu.Separator className="hx-cmenu__sep" />}
            {onStopTerminal && (
              <Menu.Item
                className="hx-cmenu__item"
                data-danger="true"
                closeOnClick={false}
                onClick={() => setView(VIEW.stop)}
              >
                <Icon name="circle-stop" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.stopTerminal')}</span>
              </Menu.Item>
            )}
            {actions.onDelete && (
              <>
                <Menu.Item
                  className="hx-cmenu__item"
                  data-danger="true"
                  closeOnClick={false}
                  onClick={() => setView(VIEW.remove)}
                >
                  <Icon name="trash-2" size="sm" />
                  <span className="hx-cmenu__txt">{t('next.rowMenu.delete')}</span>
                </Menu.Item>
              </>
            )}
          </>
        )}

        {view === VIEW.move && (
          <>
            <Menu.Item className="hx-cmenu__back" closeOnClick={false} onClick={() => setView(VIEW.root)}>
              <Icon name="chevron-left" size="sm" />
              {t('next.rowMenu.move')}
            </Menu.Item>
            {(projects ?? []).map((project) => (
              <Menu.Item
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
              </Menu.Item>
            ))}
            {projectId !== null && (
              <Menu.Item className="hx-cmenu__item" onClick={() => onMove(null)}>
                <Icon name="x" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.unassign')}</span>
              </Menu.Item>
            )}
            {actions.onNewProject && (
              <>
                <Menu.Separator className="hx-cmenu__sep" />
                <Menu.Item className="hx-cmenu__item" onClick={actions.onNewProject}>
                  <Icon name="folder-plus" size="sm" />
                  <span className="hx-cmenu__txt">{t('next.rowMenu.newProject')}</span>
                </Menu.Item>
              </>
            )}
          </>
        )}

        {view === VIEW.favorite && (
          <>
            <Menu.Item className="hx-cmenu__back" closeOnClick={false} onClick={() => setView(VIEW.root)}>
              <Icon name="chevron-left" size="sm" />
              {t('next.rowMenu.favorites')}
            </Menu.Item>
            <Menu.Item
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
            </Menu.Item>
            {favGroups.map((group) => (
              <Menu.Item
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
              </Menu.Item>
            ))}
            {pinned && actions.onPin && (
              <>
                <Menu.Separator className="hx-cmenu__sep" />
                <Menu.Item className="hx-cmenu__item" onClick={onPin}>
                  <Icon name="pin-off" size="sm" />
                  <span className="hx-cmenu__txt">{t('next.rowMenu.unpin')}</span>
                </Menu.Item>
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
              <Menu.Item className="hx-cmenu__btn" closeOnClick={false} onClick={() => setView(VIEW.root)}>
                {t('next.rowMenu.cancel')}
              </Menu.Item>
              <Menu.Item className="hx-cmenu__btn" data-danger="true" onClick={onStopTerminal}>
                {t('next.rowMenu.stopTerminalYes')}
              </Menu.Item>
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
              <Menu.Item className="hx-cmenu__btn" closeOnClick={false} onClick={() => setView(VIEW.root)}>
                {t('next.rowMenu.cancel')}
              </Menu.Item>
              <Menu.Item className="hx-cmenu__btn" data-danger="true" onClick={onDelete}>
                {t('next.rowMenu.delete')}
              </Menu.Item>
            </div>
          </>
        )}
      </MenuPanel>
    </Menu.Root>
  );
};

export default ChatMenu;
