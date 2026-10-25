import { Menu } from '@base-ui/react/menu';
import { useRef, useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import type { TPending } from '@/next/ui/use-pending-list';
import { RenameField, ROW_VARIANT, type IRowEnter, type TRowVariant } from '@/next/features/sidebar/chat-row';
import { isFavEmpty, type IFavGroupView, type IFavView } from '@/next/features/sidebar/favorites';
import type { IChatActions, ISidebarChat } from '@/next/features/sidebar/types';
import useFlip from '@/next/ui/use-flip';

/** A group's pending action is kept under this key of the sidebar's pending list. */
export const groupKey = (id: string) => `group:${id}`;

export const GROUP_STATE = { off: 'off', editing: 'editing', saving: 'saving' } as const;
export type TGroupState = (typeof GROUP_STATE)[keyof typeof GROUP_STATE];

interface IGroupMenuProps {
  name: string;
  canRename: boolean;
  canDelete: boolean;
  prev: boolean;
  next: boolean;
  onRename: () => void;
  onDelete: () => void;
  onShift: (direction: -1 | 1) => void;
}

/** «…» of a group: rename, one place up or down (the keys' way to reorder), remove (its chats stay). */
const GroupMenu = ({
  name,
  canRename,
  canDelete,
  prev,
  next,
  onRename,
  onDelete,
  onShift,
}: IGroupMenuProps) => {
  const menuActions = useMenuActions();
  if (!canRename && !canDelete && !prev && !next) return null;
  return (
    <span className="hx-item__end">
      <Menu.Root actionsRef={menuActions}>
        <Menu.Trigger
          render={
            <button
              type="button"
              className="hx-item__more hx-item__menu"
              aria-label={t('next.groupMenu.label', { name })}
            >
              <Icon name="ellipsis" size="sm" />
            </button>
          }
        />
        <MenuPanel
          actions={menuActions}
          className="hx-cmenu"
          label={t('next.groupMenu.label', { name })}
          sideOffset={6}
        >
          {canRename && (
            <Menu.Item className="hx-cmenu__item" onClick={onRename}>
              <Icon name="pencil" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.rename')}</span>
            </Menu.Item>
          )}
          {prev && (
            <Menu.Item className="hx-cmenu__item" onClick={() => onShift(-1)}>
              <Icon name="arrow-up" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.up')}</span>
            </Menu.Item>
          )}
          {next && (
            <Menu.Item className="hx-cmenu__item" onClick={() => onShift(1)}>
              <Icon name="arrow-down" size="sm" />
              <span className="hx-cmenu__txt">{t('next.rowMenu.down')}</span>
            </Menu.Item>
          )}
          {canDelete && (
            <>
              <Menu.Separator className="hx-cmenu__sep" />
              <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={onDelete}>
                <Icon name="trash-2" size="sm" />
                <span className="hx-cmenu__txt">{t('next.groupMenu.delete')}</span>
              </Menu.Item>
            </>
          )}
        </MenuPanel>
      </Menu.Root>
    </span>
  );
};

export interface IFavoritesSectionProps {
  view: IFavView;
  actions: IChatActions;
  renderRow: (chat: ISidebarChat, variant: TRowVariant, enter?: IRowEnter) => ReactNode;
  /** Groups the person folded; the rest are open. */
  folded: ReadonlySet<string>;
  onToggle: (id: string) => void;
  /** A group being named: the field is open (editing) or its name waits for the machine (saving). */
  creating: TGroupState;
  onCreateStart: () => void;
  onCreateEnd: (name: string | null) => void;
  renaming: { id: string; state: TGroupState } | null;
  onRenameStart: (id: string) => void;
  onRenameEnd: (group: IFavGroupView, name: string | null) => void;
  onDelete: (id: string) => void;
  onShift: (id: string, direction: -1 | 1) => void;
  pending: ReadonlyMap<string, TPending>;
}

/**
 * «Избранное»: the groups (a folder each, folding like a project) and then the chats of none. The whole section takes a
 * dropped chat; a group takes one into itself, a chat row takes one before or after it (use-drag reads the attributes).
 */
const FavoritesSection = ({
  view,
  actions,
  renderRow,
  folded,
  onToggle,
  creating,
  onCreateStart,
  onCreateEnd,
  renaming,
  onRenameStart,
  onRenameEnd,
  onDelete,
  onShift,
  pending,
}: IFavoritesSectionProps) => {
  // Groups the section has drawn; one that comes in later is fresh and slides in. null until the first list.
  const [known, setKnown] = useState<ReadonlySet<string> | null>(null);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(new Set());
  if (known === null || view.groups.some((group) => !known.has(group.id))) {
    const ids = new Set(view.groups.map((group) => group.id));
    if (known !== null) setFresh(new Set([...ids].filter((id) => !known.has(id))));
    setKnown(new Set([...(known ?? []), ...ids]));
  }

  const root = useRef<HTMLElement>(null);
  useFlip(root);
  const empty = isFavEmpty(view) && creating === GROUP_STATE.off;
  // The place of each group in the entrance cascade: a head and its chats count one each.
  const heads = view.groups.map((_, i) =>
    view.groups.slice(0, i).reduce((n, g) => n + 1 + g.chats.length, 0),
  );
  const looseFrom = view.groups.reduce((n, g) => n + 1 + g.chats.length, 0);

  return (
    <section ref={root} aria-label={t('next.sidebar.favorites')} className="hx-favs" data-drop="favorites">
      <div className="hx-group__label hx-group__label--static hx-enter" data-enter="up" data-eb="420">
        {t('next.sidebar.favorites')}
        <span className="hx-spacer" />
        {actions.onCreateGroup && (
          <button
            type="button"
            className="hx-nav__add"
            aria-label={t('next.sidebar.newGroup')}
            title={t('next.sidebar.newGroup')}
            disabled={creating !== GROUP_STATE.off}
            onClick={onCreateStart}
          >
            <Icon name="plus" size="sm" />
          </button>
        )}
      </div>

      {view.groups.map((group, i) => {
        const open = !folded.has(group.id);
        const state = renaming?.id === group.id ? renaming.state : GROUP_STATE.off;
        const busy = pending.has(groupKey(group.id));
        const head = heads[i] ?? 0;
        return (
          <div
            key={group.id}
            className={fresh.has(group.id) ? 'hx-nav__group hx-slot-in' : 'hx-nav__group hx-enter'}
            data-enter={fresh.has(group.id) ? undefined : 'up'}
            data-eb={fresh.has(group.id) ? undefined : '440'}
            data-i={fresh.has(group.id) ? undefined : Math.min(head, 24)}
            data-flip={group.id}
            aria-expanded={open}
          >
            <div
              className="hx-item hx-project hx-favgroup"
              data-drag={busy || state !== GROUP_STATE.off ? undefined : 'group'}
              data-drag-id={group.id}
              data-drag-box="parent"
              data-drop="group"
              data-drop-id={group.id}
              data-drop-box="parent"
              data-busy={busy || undefined}
              aria-busy={busy || undefined}
            >
              {state === GROUP_STATE.off ? (
                <button
                  type="button"
                  className="hx-project__open"
                  aria-expanded={open}
                  data-tip={group.name}
                  onClick={() => onToggle(group.id)}
                >
                  <span className="hx-project__mark" aria-hidden="true">
                    <Icon name={open ? 'folder-open' : 'folder'} className="hx-project__icon" />
                    <span className="hx-mono">{group.name.slice(0, 1).toUpperCase()}</span>
                  </span>
                  <span className="hx-item__title" title={group.name}>
                    {group.name}
                  </span>
                  <Icon name="chevron-right" size="sm" className="hx-nav__chev" />
                </button>
              ) : (
                <>
                  <span className="hx-project__mark" aria-hidden="true">
                    <Icon name="folder" className="hx-project__icon" />
                  </span>
                  <RenameField
                    title={group.name}
                    saving={state === GROUP_STATE.saving}
                    label={t('next.groupMenu.nameLabel')}
                    onDone={(name) => onRenameEnd(group, name)}
                  />
                </>
              )}
              {state === GROUP_STATE.off && !busy && (
                <GroupMenu
                  name={group.name}
                  canRename={actions.onRenameGroup !== undefined}
                  canDelete={actions.onDeleteGroup !== undefined}
                  prev={actions.onMoveGroup !== undefined && i > 0}
                  next={actions.onMoveGroup !== undefined && i < view.groups.length - 1}
                  onRename={() => onRenameStart(group.id)}
                  onDelete={() => onDelete(group.id)}
                  onShift={(direction) => onShift(group.id, direction)}
                />
              )}
            </div>
            <div className="hx-nav__kids" inert={!open}>
              <div>
                {group.chats.map((chat) => renderRow(chat, ROW_VARIANT.kid))}
                {group.chats.length === 0 && <p className="hx-snote">{t('next.sidebar.groupEmpty')}</p>}
              </div>
            </div>
          </div>
        );
      })}

      {creating !== GROUP_STATE.off && (
        <div className="hx-item hx-project hx-favgroup hx-slot-in">
          <span className="hx-project__mark" aria-hidden="true">
            <Icon name="folder" className="hx-project__icon" />
          </span>
          <RenameField
            title=""
            saving={creating === GROUP_STATE.saving}
            label={t('next.groupMenu.nameLabel')}
            onDone={onCreateEnd}
          />
        </div>
      )}

      {view.loose.map((chat, i) =>
        renderRow(chat, ROW_VARIANT.pinned, { base: '440', index: looseFrom + i }),
      )}
      {empty && <p className="hx-snote hx-favs__hint">{t('next.sidebar.favoritesEmpty')}</p>}
    </section>
  );
};

export default FavoritesSection;
