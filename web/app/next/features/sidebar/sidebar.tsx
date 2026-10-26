import { Menu } from '@base-ui/react/menu';
import Logo from '@/next/ui/logo';
import {
  cloneElement,
  Fragment,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from 'react';
import { Skeleton } from '@/next/ui/skeleton';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { useDelayed as useDelayedFlag } from '@/next/ui/skeleton';
import usePendingList from '@/next/ui/use-pending-list';
import ChatRow, {
  type IFavSlot,
  type IRowEnter,
  RENAME,
  ROW_VARIANT,
  type IRowHandlers,
  type TRename,
  type TRowVariant,
} from '@/next/features/sidebar/chat-row';
import FavoritesSection, {
  GROUP_STATE,
  groupKey,
  type TGroupState,
} from '@/next/features/sidebar/favorites-section';
import {
  DRAG,
  DRAG_KINDS,
  DROP,
  DROP_KINDS,
  EMPTY_VIEW,
  PLAN,
  favoritesOf,
  groupOfChat,
  isFavEmpty,
  neighboursOf,
  resolveDrop,
  shiftGroupPlan,
  shiftPlan,
  type IFavGroupView,
  type TDropPlan,
} from '@/next/features/sidebar/favorites';
import { childrenOf, DAY_GROUP, sectionsOf, type TDayGroup } from '@/next/features/sidebar/group';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import useDrag, { afterTouchClick } from '@/next/ui/use-drag';
import MachineAlert from '@/next/features/sidebar/machine-alert';
import MachineCard from '@/next/features/sidebar/machine-card';
import useRailTip, { isRail } from '@/next/features/sidebar/use-rail-tip';
import useRowCard from '@/next/features/sidebar/row-card';
import '@/next/styles/delegation.css';
import {
  CHAT_STATUS,
  NO_FAVORITES,
  SIDEBAR_VIEW,
  type IChatActions,
  type IMachineCard,
  type IMachineChoice,
  type IMoreItem,
  type ISidebarChat,
  type ISidebarFavorites,
  type ISidebarPerson,
  type ISidebarProject,
  type ISidebarTheme,
  type TChatStatus,
  type TSidebarView,
} from '@/next/features/sidebar/types';

/** The delegated chats show under their parent, unless the person folded them, while one of them is in this state. */
const KIDS_SHOWN_BY: ReadonlySet<TChatStatus> = new Set([
  CHAT_STATUS.thinking,
  CHAT_STATUS.background,
  CHAT_STATUS.question,
  CHAT_STATUS.perm,
  CHAT_STATUS.plan,
  CHAT_STATUS.waitingMachine,
]);

const DAY_LABEL: Record<TDayGroup, string> = {
  [DAY_GROUP.today]: t('next.sidebar.today'),
  [DAY_GROUP.yesterday]: t('next.sidebar.yesterday'),
  [DAY_GROUP.week]: t('next.sidebar.week'),
  [DAY_GROUP.older]: t('next.sidebar.older'),
};

/** Rows of the list skeleton under each of its three group labels. */
const SKELETON_GROUPS = [2, 3, 5] as const;

/** A switch of the machine is pending under this key, a chat's action under the chat's id. */
const MACHINE_KEY = 'machine';

const ListSkeleton = () => (
  <div className="hx-sbsk" role="status" aria-label={t('next.sidebar.loading')}>
    {SKELETON_GROUPS.map((rows, group) => (
      <div key={group} className="hx-sbsk__group">
        <span className="hx-sk hx-sbsk__label" />
        {Array.from({ length: rows }, (_, i) => (
          <span key={i} className="hx-sk hx-sbsk__row" />
        ))}
      </div>
    ))}
  </div>
);

const renameOf = (renaming: { id: string; saving: boolean } | null, id: string): TRename => {
  if (renaming?.id !== id) return RENAME.off;
  return renaming.saving ? RENAME.saving : RENAME.editing;
};

export interface ISidebarProps {
  /** The product's name at the top (RUNTIME_APP_NAME). */
  brand: string;
  /** null while the list loads (a skeleton after 150 ms); [] is an empty list. */
  chats: readonly ISidebarChat[] | null;
  projects: readonly ISidebarProject[] | null;
  /** How the favorites are grouped and ordered; none until the machine says. */
  favorites?: ISidebarFavorites;
  /** The list did not load; with chats === null the sidebar says so and offers to try again. */
  loadError: string | null;
  onRetryLoad?: () => void | Promise<unknown>;
  currentId: string | null;
  view: TSidebarView;
  /** The clock of the day groups; the owner moves it on (a minute is enough). */
  now: number;
  /** null while the profile loads: a placeholder of the same size stands in, not a fallback name. */
  person: ISidebarPerson | null;
  machine: IMachineCard | null;
  otherMachines: readonly IMachineChoice[];
  /** Something in the notification centre is unread. */
  unread: boolean;
  notificationsOpen: boolean;
  searchOpen: boolean;
  more: readonly IMoreItem[];
  /** The profile's menu (settings, the way back to the current interface). */
  profile: readonly IMoreItem[];
  /** The theme's switch in the footer; none where the owner has no theme. */
  theme?: ISidebarTheme;
  actions: IChatActions;
  onOpen: (id: string) => void;
  onNewChat: () => void;
  onSearch: () => void;
  onDesignly?: () => void;
  onNotifications: (anchor: HTMLElement) => void;
  onToggleRail: () => void;
  onCloseDrawer: () => void;
  onPickMachine?: (id: string) => Promise<void>;
  onManageMachines?: () => void;
  /** Each new value opens the machine's card (a notification about the machine). */
  machineReveal?: number;
  /** What the folded rail lists in place of the projects (the live chats); shown only on the rail. */
  rail?: ReactNode;
  ref?: Ref<HTMLElement>;
}

interface INavItemProps {
  icon: string;
  label: string;
  current?: boolean;
  dot?: boolean;
  expanded?: boolean;
  /** The place in the entrance cascade of the nav. */
  index: number;
  onClick: (anchor: HTMLElement) => void;
  children?: ReactNode;
}

const NavItem = ({ icon, label, current, dot, expanded, index, onClick, children }: INavItemProps) => (
  <div
    className="hx-nav__group hx-enter"
    data-enter="up"
    data-eb="220"
    data-i={index}
    aria-expanded={children ? expanded : undefined}
  >
    <button
      type="button"
      className="hx-nav__item"
      aria-current={current ? 'page' : undefined}
      aria-expanded={children ? expanded : undefined}
      data-tip={label}
      onClick={(event) => onClick(event.currentTarget)}
    >
      <span className="hx-nav__ic">
        <Icon name={icon} />
        {dot && <span className="hx-nav__dot" />}
      </span>
      <span className="hx-nav__label">{label}</span>
      {children && <Icon name="chevron-right" size="sm" className="hx-nav__chev" />}
    </button>
    {children && (
      <div className="hx-nav__kids" inert={!expanded}>
        <div>{children}</div>
      </div>
    )}
  </div>
);

/** The theme's switch of the footer: the moon in a light page, the sun in a dark one. */
const ThemeButton = ({ theme }: { theme: ISidebarTheme }) => {
  const label = theme.dark ? t('next.sidebar.themeLight') : t('next.sidebar.themeDark');
  return (
    <button
      type="button"
      className="hx-btn hx-tipped hx-user__theme"
      data-variant="ghost"
      data-size="sm"
      data-shape="icon"
      aria-label={label}
      onClick={theme.onToggle}
    >
      <Icon name={theme.dark ? 'sun' : 'moon'} />
      <span className="hx-tip" data-pos="above" data-align="end">
        {label}
      </span>
    </button>
  );
};

const ProfileButton = ({
  person,
  items,
  current,
}: {
  person: ISidebarPerson | null;
  items: readonly IMoreItem[];
  /** Settings is on screen. */
  current: boolean;
}) => {
  const menuActions = useMenuActions();
  const button = (
    <button
      type="button"
      className="hx-user__open"
      data-tip={person?.name}
      aria-current={current ? 'page' : undefined}
    >
      <span className="hx-avatar" aria-hidden="true">
        {person ? person.name.slice(0, 1).toUpperCase() : <Skeleton className="hx-avatar__sk" />}
      </span>
      <span className="hx-user__text">
        {person ? <span className="hx-user__name">{person.name}</span> : <Skeleton className="hx-user__sk" />}
        {person && items.length === 1 && <span className="hx-user__sub">{items[0].label}</span>}
      </span>
    </button>
  );
  if (items.length === 0) return button;
  // One entry (settings) is a link: the person goes there at once, without a menu of one.
  if (items.length === 1) return cloneElement(button, { onClick: items[0].onSelect });
  return (
    <Menu.Root actionsRef={menuActions}>
      <Menu.Trigger render={button} />
      <MenuPanel
        actions={menuActions}
        className="hx-cmenu"
        label={t('next.sidebar.profile')}
        side="top"
        sideOffset={8}
      >
        {items.map((item) => (
          <Menu.Item key={item.id} className="hx-cmenu__item" onClick={item.onSelect}>
            <Icon name={item.icon} size="sm" />
            <span className="hx-cmenu__txt">{item.label}</span>
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};

/**
 * The left column: actions (new chat, search, Designly, notifications, more), the pinned chats, projects with their
 * chats and the rest by day, and the person with their machine at the bottom. Data comes in through the props only;
 * every change goes out through `actions` and the row waits for the server's answer.
 */
const Sidebar = ({
  brand,
  chats,
  projects,
  favorites = NO_FAVORITES,
  loadError,
  onRetryLoad,
  currentId,
  view,
  now,
  person,
  machine,
  otherMachines,
  unread,
  notificationsOpen,
  searchOpen,
  more,
  profile,
  theme,
  actions,
  onOpen,
  onNewChat,
  onSearch,
  onDesignly,
  onNotifications,
  onToggleRail,
  onCloseDrawer,
  onPickMachine,
  onManageMachines,
  machineReveal = 0,
  rail,
  ref,
}: ISidebarProps) => {
  const { pending, run } = usePendingList();
  const [renaming, setRenaming] = useState<{ id: string; saving: boolean } | null>(null);
  const [moreOpen, setMoreOpen] = useState(false);
  const [projectChoice, setProjectChoice] = useState<ReadonlyMap<string, boolean>>(new Map());
  const [foldedDays, setFoldedDays] = useState<ReadonlySet<TDayGroup>>(new Set());
  // The person's choice of showing the chats a chat delegated; without one they show while one of them is at work.
  const [kidsChoice, setKidsChoice] = useState<ReadonlyMap<string, boolean>>(new Map());
  // Ids the list has drawn; one that comes in later is fresh and slides in. null until the first list.
  const [known, setKnown] = useState<ReadonlySet<string> | null>(null);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(new Set());
  const [pickedMachine, setPickedMachine] = useState<string | null>(null);
  const [alertReveal, setAlertReveal] = useState(0);
  const [foldedGroups, setFoldedGroups] = useState<ReadonlySet<string>>(new Set());
  const [creatingGroup, setCreatingGroup] = useState<TGroupState>(GROUP_STATE.off);
  const [renamingGroup, setRenamingGroup] = useState<{ id: string; state: TGroupState } | null>(null);
  const railTip = useRailTip();
  const listRef = useRef<HTMLDivElement>(null);
  const rowCard = useRowCard();
  const loading = chats === null && loadError === null;
  const showSkeleton = useDelayedFlag(loading);

  if (chats !== null && (known === null || chats.some((chat) => !known.has(chat.id)))) {
    const ids = new Set(chats.map((chat) => chat.id));
    if (known !== null) setFresh(new Set([...ids].filter((id) => !known.has(id))));
    setKnown(new Set([...(known ?? []), ...ids]));
  }

  const sections = useMemo(
    () => (chats === null ? null : sectionsOf(chats, projects ?? [], now)),
    [chats, projects, now],
  );

  const favView = useMemo(
    () => (chats === null ? EMPTY_VIEW : favoritesOf(chats, favorites)),
    [chats, favorites],
  );
  // The groups for the menus, and where each favorite chat stands: both keep their identity while the view does.
  const favGroups = useMemo(() => favView.groups.map(({ id, name }) => ({ id, name })), [favView]);
  const slots = useMemo(() => {
    const out = new Map<string, IFavSlot>();
    for (const chat of [...favView.groups.flatMap((group) => group.chats), ...favView.loose]) {
      const groupId = groupOfChat(favView, chat.id) ?? null;
      out.set(chat.id, { groupId, ...neighboursOf(favView, chat.id) });
    }
    return out;
  }, [favView]);

  const renameEnd = (chat: ISidebarChat, title: string | null) => {
    const rename = actions.onRename;
    if (title === null || !rename) {
      setRenaming(null);
      return;
    }
    setRenaming({ id: chat.id, saving: true });
    void run(chat.id, t('next.sidebar.renameFailed'), () => rename(chat.id, title)).finally(() =>
      setRenaming((current) => (current?.id === chat.id ? null : current)),
    );
  };

  // Whether a chat's delegated chats show: undefined for a chat that delegated none.
  const kidsOpen = (id: string): boolean | undefined => {
    const kids = sections ? childrenOf(sections, id) : [];
    if (kids.length === 0) return undefined;
    return kidsChoice.get(id) ?? kids.some((kid) => KIDS_SHOWN_BY.has(kid.status));
  };
  const toggleKids = (id: string) => {
    const open = kidsOpen(id);
    if (open === undefined) return;
    setKidsChoice((current) => new Map(current).set(id, !open));
  };

  // The rows get one handlers object for the life of the list (they are memoized); it reads the latest props here.
  const latest = useRef({ actions, run, onOpen, renameEnd, favView, toggleKids });
  useLayoutEffect(() => {
    latest.current = { actions, run, onOpen, renameEnd, favView, toggleKids };
  });
  const { onCopyLink, onOpenInNewTab, onTerminal, onStopTerminal, onFork, onContinue } = actions;
  const hasCopyLink = onCopyLink !== undefined;
  const hasNewTab = onOpenInNewTab !== undefined;
  const hasTerminal = onTerminal !== undefined;
  const hasStopTerminal = onStopTerminal !== undefined;
  const hasFork = onFork !== undefined;
  const hasContinue = onContinue !== undefined;
  const handlers = useMemo<IRowHandlers>(() => {
    const act = (id: string, failure: string, action: (now: IChatActions) => Promise<unknown> | undefined) =>
      void latest.current.run(id, failure, () => action(latest.current.actions) ?? Promise.resolve());
    return {
      open: (id) => latest.current.onOpen(id),
      renameStart: (id) => setRenaming({ id, saving: false }),
      renameEnd: (chat, title) => latest.current.renameEnd(chat, title),
      pin: (chat) =>
        act(chat.id, chat.pinned ? t('next.sidebar.unpinFailed') : t('next.sidebar.pinFailed'), (now) =>
          now.onPin?.(chat.id, !chat.pinned),
        ),
      move: (id, projectId) => act(id, t('next.sidebar.moveFailed'), (now) => now.onMove?.(id, projectId)),
      place: (chat, groupId) =>
        act(chat.id, t('next.sidebar.placeFailed'), (now) => now.onPlaceFavorite?.(chat.id, groupId, null)),
      shift: (chat, direction) => {
        const plan = shiftPlan(latest.current.favView, chat.id, direction);
        if (plan?.op === PLAN.place)
          act(chat.id, t('next.sidebar.placeFailed'), (now) =>
            now.onPlaceFavorite?.(plan.id, plan.groupId, plan.beforeId),
          );
      },
      remove: (id) => act(id, t('next.sidebar.deleteFailed'), (now) => now.onDelete?.(id)),
      copyLink: hasCopyLink
        ? (id) => act(id, t('next.sidebar.copyFailed'), (now) => now.onCopyLink?.(id))
        : undefined,
      openInNewTab: hasNewTab ? (id) => latest.current.actions.onOpenInNewTab?.(id) : undefined,
      terminal: hasTerminal ? (id) => latest.current.actions.onTerminal?.(id) : undefined,
      stopTerminal: hasStopTerminal
        ? (id) => act(id, t('next.sidebar.stopTerminalFailed'), (now) => now.onStopTerminal?.(id))
        : undefined,
      fork: hasFork ? (id) => act(id, t('next.handoff.forkFailed'), (now) => now.onFork?.(id)) : undefined,
      continueIn: hasContinue ? (id) => latest.current.actions.onContinue?.(id) : undefined,
      toggleKids: (id) => latest.current.toggleKids(id),
    };
  }, [hasCopyLink, hasNewTab, hasTerminal, hasStopTerminal, hasFork, hasContinue]);

  const renderRow = (chat: ISidebarChat, variant: TRowVariant, enter?: IRowEnter, open?: boolean) => (
    <ChatRow
      key={chat.id}
      enter={enter}
      chat={chat}
      variant={variant}
      current={chat.id === currentId}
      pending={pending.get(chat.id)}
      fresh={fresh.has(chat.id)}
      rename={renameOf(renaming, chat.id)}
      projects={projects}
      actions={actions}
      favGroups={favGroups}
      slot={slots.get(chat.id) ?? null}
      handlers={handlers}
      kidsOpen={open}
    />
  );

  // A chat with the chats it delegated under it, folded or shown; a delegated chat that delegated in turn the same.
  const renderTree = (chat: ISidebarChat, variant: TRowVariant, enter?: IRowEnter): ReactNode => {
    const open = kidsOpen(chat.id);
    if (open === undefined || !sections) return renderRow(chat, variant, enter);
    return (
      <div key={chat.id} className="hx-tree" aria-expanded={open}>
        {renderRow(chat, variant, enter, open)}
        <div className="hx-tree__kids" inert={!open}>
          <div>{childrenOf(sections, chat.id).map((kid) => renderTree(kid, ROW_VARIANT.delegated))}</div>
        </div>
      </div>
    );
  };

  // The chats of one launch (a prompt on several models) stand together under the prompt, which names the group.
  const renderRows = (
    list: readonly ISidebarChat[],
    variant: TRowVariant,
    enterOf?: (index: number) => IRowEnter,
  ) =>
    list.map((chat, i) => {
      const row = renderTree(chat, variant, enterOf?.(i));
      if (!chat.launch || list[i - 1]?.launch?.id === chat.launch.id) return row;
      return (
        <Fragment key={chat.id}>
          <div className="hx-launch" title={chat.launch.prompt}>
            {chat.launch.prompt}
          </div>
          {row}
        </Fragment>
      );
    });

  // ── The favorites' groups: every change waits for the machine, as a chat's does ──
  const toggleGroup = (id: string) =>
    setFoldedGroups((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const createGroupEnd = (name: string | null) => {
    const create = actions.onCreateGroup;
    if (name === null || !create) {
      setCreatingGroup(GROUP_STATE.off);
      return;
    }
    setCreatingGroup(GROUP_STATE.saving);
    void run('group:new', t('next.sidebar.groupCreateFailed'), () => create(name)).then((ok) =>
      setCreatingGroup(ok ? GROUP_STATE.off : GROUP_STATE.editing),
    );
  };

  const renameGroupEnd = (group: IFavGroupView, name: string | null) => {
    const rename = actions.onRenameGroup;
    if (name === null || !rename) {
      setRenamingGroup(null);
      return;
    }
    setRenamingGroup({ id: group.id, state: GROUP_STATE.saving });
    void run(groupKey(group.id), t('next.sidebar.groupRenameFailed'), () => rename(group.id, name)).finally(
      () => setRenamingGroup((current) => (current?.id === group.id ? null : current)),
    );
  };

  const deleteGroup = (id: string) =>
    void run(
      groupKey(id),
      t('next.sidebar.groupDeleteFailed'),
      () => actions.onDeleteGroup?.(id) ?? Promise.resolve(),
    );

  const shiftGroup = (id: string, direction: -1 | 1) => {
    const plan = shiftGroupPlan(favView, id, direction);
    if (plan?.op === PLAN.moveGroup) runPlan(plan);
  };

  // ── Drag and drop: what a drop asks for goes out as the commands the menus send ──
  const runPlan = (plan: TDropPlan) => {
    const now = actions;
    const go = (key: string, failure: string, action: () => Promise<unknown> | undefined) =>
      void run(key, failure, () => action() ?? Promise.resolve());
    switch (plan.op) {
      case PLAN.place:
        go(plan.id, t('next.sidebar.placeFailed'), () =>
          now.onPlaceFavorite?.(plan.id, plan.groupId, plan.beforeId),
        );
        return;
      case PLAN.moveGroup:
        go(groupKey(plan.id), t('next.sidebar.groupMoveFailed'), () =>
          now.onMoveGroup?.(plan.id, plan.beforeId),
        );
        return;
      case PLAN.move:
        go(plan.id, t('next.sidebar.moveFailed'), async () => {
          await now.onMove?.(plan.id, plan.projectId);
          if (plan.unpin) await now.onPin?.(plan.id, false);
        });
        return;
      case PLAN.unpin:
        go(plan.id, t('next.sidebar.unpinFailed'), () => now.onPin?.(plan.id, false));
        return;
      case PLAN.attach:
        now.onAttachChat?.(plan.id);
    }
  };

  useDrag({
    listRef,
    dragKinds: DRAG_KINDS,
    dropKinds: DROP_KINDS,
    lined: (source, target) =>
      target.kind === DROP.chat || (target.kind === DROP.group && source.kind === DRAG.group),
    resolve: (source, target) => {
      const plan = resolveDrop(source, target, favView, chats ?? []);
      if (!plan) return null;
      const can: Record<TDropPlan['op'], boolean> = {
        [PLAN.place]: actions.onPlaceFavorite !== undefined,
        [PLAN.moveGroup]: actions.onMoveGroup !== undefined,
        [PLAN.move]: actions.onMove !== undefined,
        [PLAN.unpin]: actions.onPin !== undefined,
        [PLAN.attach]: actions.onAttachChat !== undefined,
      };
      return can[plan.op] ? plan : null;
    },
    onDrop: runPlan,
    // A chat carried onto the composer of the open chat is attached to its next message.
    external: {
      selector: '[data-chat-drop]',
      resolve: (source) =>
        source.kind === DRAG.chat && actions.onAttachChat ? { op: PLAN.attach, id: source.id } : null,
    },
    // A finger held on a chat and let go where it was: its «…» menu, as a long press opens one elsewhere.
    onHoldRelease: (source) => {
      if (source.kind !== DRAG.chat) return;
      afterTouchClick(() =>
        listRef.current
          ?.querySelector<HTMLElement>(`[data-chat="${CSS.escape(source.id)}"] .hx-item__menu`)
          ?.click(),
      );
    },
  });

  const projectOpen = (id: string, chatsOfProject: readonly ISidebarChat[]) =>
    projectChoice.get(id) ?? chatsOfProject.some((chat) => chat.id === currentId);

  const toggleProject = (id: string, open: boolean) =>
    setProjectChoice((current) => new Map(current).set(id, !open));

  const toggleDay = (group: TDayGroup) =>
    setFoldedDays((current) => {
      const next = new Set(current);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });

  const empty =
    sections !== null &&
    sections.pinned.length === 0 &&
    sections.projects.length === 0 &&
    sections.days.length === 0;

  const showFavorites =
    sections !== null &&
    (!isFavEmpty(favView) || (actions.onPlaceFavorite !== undefined && chats !== null && chats.length > 0));

  const switching = pending.get(MACHINE_KEY);
  // Day groups share one cascade, a label and its chats in a row, as the design staggers them.
  let cascade = 0;

  return (
    <aside
      className="hx-sidebar hx-enter"
      data-enter="left"
      data-ed="640"
      aria-label={t('next.sidebar.label')}
      ref={ref}
      {...railTip.handlers}
      {...rowCard.handlers}
    >
      {rowCard.card}
      <div className="hx-brand hx-enter" data-enter="up" data-eb="160">
        <span className="hx-brand__mark">
          <Logo className="hx-orb" />
          <button
            type="button"
            className="hx-brand__open"
            aria-label={t('next.sidebar.expand')}
            data-tip={t('next.sidebar.expand')}
            data-kbd="⌘B"
            onClick={onToggleRail}
          >
            <Icon name="panel-left" />
          </button>
        </span>
        <span className="hx-brand__name">{brand}</span>
        <span className="hx-spacer" />
        <button
          type="button"
          className="hx-btn hx-tipped hx-brand__close"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          aria-label={t('next.sidebar.collapse')}
          onClick={onToggleRail}
        >
          <Icon name="panel-left" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t('next.sidebar.collapse')}
            <kbd>⌘B</kbd>
          </span>
        </button>
        <button
          type="button"
          className="hx-btn hx-brand__drawer"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          aria-label={t('next.sidebar.closeMenu')}
          onClick={onCloseDrawer}
        >
          <Icon name="x" />
        </button>
      </div>

      <nav className="hx-nav" aria-label={t('next.sidebar.actions')}>
        <NavItem
          icon="square-pen"
          label={t('next.sidebar.newChat')}
          current={false}
          index={0}
          onClick={onNewChat}
        />
        <NavItem
          icon="search"
          label={t('next.sidebar.search')}
          current={searchOpen}
          index={1}
          onClick={onSearch}
        />
        {onDesignly && (
          <NavItem
            icon="palette"
            label={t('next.sidebar.designly')}
            current={view === SIDEBAR_VIEW.designly}
            index={2}
            onClick={onDesignly}
          />
        )}
        <NavItem
          icon="bell"
          label={t('next.sidebar.notifications')}
          current={notificationsOpen}
          dot={unread}
          index={3}
          onClick={onNotifications}
        />
        {more.length > 0 && (
          <NavItem
            icon="layout-grid"
            label={t('next.sidebar.more')}
            expanded={moreOpen}
            index={4}
            onClick={(anchor) => {
              if (isRail(anchor)) onToggleRail();
              setMoreOpen((current) => !current);
            }}
          >
            {more.map((item) => (
              <button
                key={item.id}
                type="button"
                className="hx-nav__kid"
                aria-current={item.current ? 'page' : undefined}
                onClick={item.onSelect}
              >
                <Icon name={item.icon} size="sm" className="hx-nav__kid-icon" />
                <span className="hx-item__title">{item.label}</span>
              </button>
            ))}
          </NavItem>
        )}
      </nav>

      <MachineAlert alert={machine?.alert ?? null} onMore={() => setAlertReveal((n) => n + 1)} />

      <div className="hx-sidebar__list" ref={listRef}>
        {rail}
        {showSkeleton && <ListSkeleton />}
        {chats === null && loadError !== null && onRetryLoad && (
          <div className="hx-snote">
            <LoadFailed
              size={LOAD_FAILED_SIZE.inline}
              title={t('next.load.chats')}
              reason={loadError}
              onRetry={onRetryLoad}
            />
          </div>
        )}
        {empty && <p className="hx-snote">{t('next.sidebar.empty')}</p>}

        {showFavorites && (
          <FavoritesSection
            view={favView}
            actions={actions}
            renderRow={renderRow}
            folded={foldedGroups}
            onToggle={toggleGroup}
            creating={creatingGroup}
            onCreateStart={() => setCreatingGroup(GROUP_STATE.editing)}
            onCreateEnd={createGroupEnd}
            renaming={renamingGroup}
            onRenameStart={(id) => setRenamingGroup({ id, state: GROUP_STATE.editing })}
            onRenameEnd={renameGroupEnd}
            onDelete={deleteGroup}
            onShift={shiftGroup}
            pending={pending}
          />
        )}

        {sections && (sections.projects.length > 0 || actions.onNewProject) && (
          <section aria-label={t('next.sidebar.projects')} data-empty={sections.projects.length === 0}>
            <div className="hx-group__label hx-group__label--static hx-enter" data-enter="up" data-eb="480">
              {t('next.sidebar.projects')}
              <span className="hx-spacer" />
              {actions.onNewProject && (
                <button
                  type="button"
                  className="hx-nav__add"
                  aria-label={t('next.sidebar.newProject')}
                  title={t('next.sidebar.newProject')}
                  onClick={actions.onNewProject}
                >
                  <Icon name="plus" size="sm" />
                </button>
              )}
            </div>
            {sections.projects.map(({ project, chats: inProject }, i) => {
              const open = projectOpen(project.id, inProject);
              return (
                <div
                  key={project.id}
                  className="hx-nav__group hx-enter"
                  data-enter="up"
                  data-eb="520"
                  data-i={Math.min(i, 24)}
                  aria-expanded={open}
                >
                  <div
                    className="hx-item hx-project"
                    data-drop="project"
                    data-drop-id={project.id}
                    data-drop-box="parent"
                  >
                    <button
                      type="button"
                      className="hx-project__open"
                      aria-expanded={open}
                      data-tip={project.name}
                      onClick={() => toggleProject(project.id, open)}
                    >
                      <span className="hx-project__mark" aria-hidden="true">
                        <Icon name={open ? 'folder-open' : 'folder'} className="hx-project__icon" />
                        <span className="hx-mono">{project.name.slice(0, 1).toUpperCase()}</span>
                      </span>
                      <span className="hx-item__title">{project.name}</span>
                      <Icon name="chevron-right" size="sm" className="hx-nav__chev" />
                    </button>
                    {actions.onNewChatIn && (
                      <button
                        type="button"
                        className="hx-nav__add hx-project__add"
                        aria-label={t('next.sidebar.newChatIn', { project: project.name })}
                        title={t('next.sidebar.newChatIn', { project: project.name })}
                        onClick={() => actions.onNewChatIn?.(project.id)}
                      >
                        <Icon name="plus" size="sm" />
                      </button>
                    )}
                  </div>
                  <div className="hx-nav__kids" inert={!open}>
                    <div>
                      {renderRows(inProject, ROW_VARIANT.kid)}
                      {inProject.length === 0 && <p className="hx-snote">{t('next.sidebar.projectEmpty')}</p>}
                    </div>
                  </div>
                </div>
              );
            })}
          </section>
        )}

        <div className="hx-recent" data-drop="recent">
          {sections?.days.map(({ group, chats: ofDay }) => {
            const open = !foldedDays.has(group);
            const head = cascade;
            cascade += 1 + ofDay.length;
            return (
              <section key={group} className="hx-group" aria-expanded={open} aria-label={DAY_LABEL[group]}>
                <button
                  type="button"
                  className="hx-group__label hx-enter"
                  data-enter="up"
                  data-eb="560"
                  data-i={Math.min(head, 24)}
                  aria-expanded={open}
                  onClick={() => toggleDay(group)}
                >
                  {DAY_LABEL[group]}
                  <Icon name="chevron-down" size="sm" className="hx-group__chev" />
                </button>
                <div className="hx-group__body" inert={!open}>
                  <div>
                    {renderRows(ofDay, ROW_VARIANT.item, (i) => ({ base: '560', index: head + 1 + i }))}
                  </div>
                </div>
              </section>
            );
          })}
        </div>
      </div>

      <div className="hx-user hx-enter" data-enter="up" data-eb="820" data-health={machine?.health}>
        <ProfileButton person={person} items={profile} current={view === SIDEBAR_VIEW.settings} />
        {theme && <ThemeButton theme={theme} />}
        <MachineCard
          machine={machine}
          others={otherMachines}
          switching={switching && pickedMachine ? { id: pickedMachine, pending: switching } : null}
          onPick={
            onPickMachine
              ? (id) => {
                  setPickedMachine(id);
                  void run(MACHINE_KEY, t('next.sidebar.machineFailed'), () => onPickMachine(id));
                }
              : undefined
          }
          onManage={onManageMachines}
          reveal={machineReveal + alertReveal}
        />
      </div>
      {railTip.tip}
    </aside>
  );
};

export default Sidebar;
