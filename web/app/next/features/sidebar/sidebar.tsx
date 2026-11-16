import { Menu } from '@base-ui/react/menu';
import { openLookEditor } from '@/next/features/looks/store';
import { useKeyHint } from '@/next/features/keys/keys-help';
import Logo from '@/next/ui/logo';
import { useKeptItems } from '@/next/core/kept';
import {
  cloneElement,
  Fragment,
  useCallback,
  useEffect,
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
  RenameField,
  ROW_VARIANT,
  type IRowHandlers,
  type TRename,
  type TRowVariant,
  HEAD_OF_LOOK,
  useRowVoice,
} from '@/next/features/sidebar/chat-row';
import MiniWave from '@/next/features/voice-live/mini-wave';
import VoiceHead from '@/next/features/voice-live/voice-head';
import { gptConv } from '@/next/features/chatgpt/voice-bridge';
import { useVoiceMascot } from '@/next/stores/me';
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
  favRowCount,
  isFavEmpty,
  neighboursOf,
  resolveDrop,
  shiftGroupPlan,
  shiftPlan,
  type IFavGroupView,
  type TDropPlan,
} from '@/next/features/sidebar/favorites';
import {
  childrenOf,
  DAY_GROUP,
  dayGroupOf,
  folderRows,
  holdSections,
  sectionsOf,
  sidebarLayout,
  type TDayGroup,
} from '@/next/features/sidebar/group';
import type { TShownLayout } from '@/next/core/steady-order';
import type { IWorkingDesign } from '@/next/features/designly/working';
import { designIcon } from '@/next/features/designly/design-icon';
import {
  NAV_ITEM,
  PINNED_APPS,
  appKey,
  appOfKey,
  navLayout,
  navSettingsOf,
  withPlaced,
  type INavLayout,
  type TNavItem,
} from '@/next/features/sidebar/nav-layout';
import type { NavSettings } from '@/next/stores/me';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { toast } from '@/next/ui/toast';
import ProductSwitcher from '@/next/features/sidebar/product-switcher';
import useDrag, { EDGE, afterTouchClick } from '@/next/ui/use-drag';
import MachineAlert from '@/next/features/sidebar/machine-alert';
import useRailTip, { opensRail } from '@/next/features/sidebar/use-rail-tip';
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
import { withDelegationWait } from '@/next/features/sidebar/status';
import { countRender } from '@/next/core/perf-journal';

/** The delegated chats stay folded under their parent; they open by themselves only while one of them waits for the
 *  person (a question, a permission, a plan), unless the person folded them. */
const favGroupKey = (group: { id: string }): string => group.id;

const KIDS_SHOWN_BY: ReadonlySet<TChatStatus> = new Set([
  CHAT_STATUS.question,
  CHAT_STATUS.perm,
  CHAT_STATUS.plan,
]);

const DAY_LABEL: Record<TDayGroup, string> = {
  [DAY_GROUP.today]: t('next.sidebar.today'),
  [DAY_GROUP.yesterday]: t('next.sidebar.yesterday'),
  [DAY_GROUP.week]: t('next.sidebar.week'),
  [DAY_GROUP.older]: t('next.sidebar.older'),
};

/** Rows of the list skeleton under each of its three group labels. */
/** Focus the keyboard brought (a click's focus does not hold the list). */
const focusVisible = (target: Element): boolean => {
  try {
    return target.matches(':focus-visible');
  } catch {
    return false;
  }
};

const SKELETON_GROUPS = [2, 3, 5] as const;

/** A switch of the machine is pending under this key, a chat's action under the chat's id. */

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

// How many rows «Избранное» took the last time this browser drew it, so the skeleton of the next load is as
// tall as the list that replaces it and nothing under it moves. 0: the favorites were not shown.
const FAV_ROWS_KEY = 'hx-next:favorites-rows';

export const readFavRows = (): number | null => {
  try {
    const raw = localStorage.getItem(FAV_ROWS_KEY);
    const rows = raw === null ? Number.NaN : Number(raw);
    return Number.isInteger(rows) && rows >= 0 ? rows : null;
  } catch {
    return null;
  }
};

const keepFavRows = (rows: number) => {
  try {
    localStorage.setItem(FAV_ROWS_KEY, String(rows));
  } catch {
    // No storage: the next load sizes the skeleton by the pinned chats.
  }
};

/**
 * «Избранное» while its layout has not come: its own label and a row of the list's height for each row it
 * will have (sidebar rows and skeleton rows are both 30 px).
 */
const FavoritesSkeleton = ({ rows, canCreate }: { rows: number; canCreate: boolean }) => (
  <div role="status" aria-label={t('next.sidebar.favorites')} data-favorites-wait="">
    <div className="hx-group__label hx-group__label--static">
      {t('next.sidebar.favorites')}
      <span className="hx-spacer" />
      {/* The label's «+» as the list has it, so the label is as tall; it waits with the list. */}
      {canCreate && (
        <button type="button" className="hx-nav__add" aria-label={t('next.sidebar.newGroup')} disabled>
          <Icon name="plus" size="sm" />
        </button>
      )}
    </div>
    <div className="hx-sbsk">
      {Array.from({ length: rows }, (_, i) => (
        <span key={i} className="hx-sk hx-sbsk__row" data-favorites-wait-row="" />
      ))}
    </div>
  </div>
);

const renameOf = (renaming: { id: string; saving: boolean } | null, id: string): TRename => {
  if (renaming?.id !== id) return RENAME.off;
  return renaming.saving ? RENAME.saving : RENAME.editing;
};

export interface ISidebarProps {
  /** The product's name at the top (RUNTIME_APP_NAME). */
  brand: string;
  /** Where the hub is for this person: the name at the top then opens the product switcher (hub / workspace). */
  hubHref?: string | null;
  /** null while the list loads (a skeleton after 150 ms); [] is an empty list. */
  chats: readonly ISidebarChat[] | null;
  projects: readonly ISidebarProject[] | null;
  /** How the favorites are grouped and ordered; null while the layout that will stand has not come (a skeleton). */
  favorites?: ISidebarFavorites | null;
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
  /** The agents' questions to people («People»), under the favorites; hidden on the rail. */
  people?: ReactNode;
  /** The designs at work under «Sinopia Design»: the first five, then «Дальше»; null or empty shows none. */
  designs?: ISidebarDesigns | null;
  /** The installed apps that are on in the person's chats, under «Приложения»; null shows the item with no rows. */
  apps?: ISidebarApps | null;
  /** «ChatGPT»: its plain chats under the item, which starts a new one; null shows no item. */
  gpt?: ISidebarGpt | null;
  /** The menu as the person laid it out (Settings → «Меню»); absent, the default layout. */
  nav?: NavSettings;
  /** Saves the menu the person laid out by hand in the rail (an app carried up to the main buttons and back);
   *  absent, the menu does not move. Rejects when it was not taken. */
  onNav?: (nav: NavSettings) => Promise<unknown>;
  ref?: Ref<HTMLElement>;
}

/** What the rail's menu carries by hand: a main button, an app under «Приложения». */
const NAV_DRAG = { top: 'nav-top', app: 'nav-app' } as const;
type TNavDrag = (typeof NAV_DRAG)[keyof typeof NAV_DRAG];
const NAV_DRAGS: readonly TNavDrag[] = [NAV_DRAG.top, NAV_DRAG.app];

const DEFAULT_NAV: NavSettings = { order: [], hidden: [], apps: [] };

/** An app of harnessly's own under «Приложения», always there (the «Ревью кода»). */
export interface ISidebarBuiltin {
  id: string;
  name: string;
  icon: string;
  current: boolean;
  onOpen: () => void;
}

export interface ISidebarApps {
  /** harnessly's own apps, above the installed ones. */
  builtins?: readonly ISidebarBuiltin[];
  /** chat: the chat the app was last on in; absent for an environment app no chat has on yet. */
  rows: readonly { id: string; name: string; icon?: string; chat?: string }[];
  /** The app on screen. */
  current: string | null;
  /** The list of every installed app is on screen. */
  allCurrent: boolean;
  onOpen: (id: string, chat?: string) => void;
  /** The pointer or the focus is on an app: its window can start getting ready before the click. */
  onPrepare?: (id: string, chat?: string) => void;
  onAll: () => void;
}

/** The plain ChatGPT chats: they stand among the chats of their day, not in a section of their own. */
export interface ISidebarGpt {
  rows: readonly { id: string; title: string; updatedAt: number }[];
  /** The ChatGPT chat on screen. */
  current: string | null;
  onOpen: (id: string) => void;
  /** Renames a chat; refused, it throws. */
  onRename: (id: string, title: string) => Promise<unknown>;
  /** Deletes a chat; refused, it throws. */
  onDelete: (id: string) => Promise<unknown>;
}

export interface ISidebarDesigns {
  rows: readonly IWorkingDesign[];
  /** The design on screen; it is never hidden behind «Дальше». */
  current: string | null;
  onOpen: (id: string) => void;
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
  /** The page it opens, fetched ahead when the person points at it (bindings/shell.tsx, data-warm). */
  warm?: string;
  children?: ReactNode;
  /** The rows under it are a list of its own: no chevron in the item, the click goes where the item goes. */
  list?: boolean;
  /** A list's own chevron beside the item, that folds and unfolds the list. */
  onFold?: () => void;
  /** The item's key in the menu: the person carries it by hand to another place. */
  dragKey?: string;
}

const NavItem = ({
  icon,
  label,
  current,
  dot,
  expanded,
  index,
  onClick,
  children,
  list,
  onFold,
  warm,
  dragKey,
}: INavItemProps) => (
  <div
    className="hx-nav__group hx-enter"
    data-enter="up"
    data-eb="220"
    data-i={index}
    aria-expanded={children ? expanded : undefined}
    data-drag={dragKey ? NAV_DRAG.top : undefined}
    data-drag-id={dragKey}
    data-drop={dragKey ? NAV_DRAG.top : undefined}
    data-drop-id={dragKey}
  >
    <button
      type="button"
      className="hx-nav__item"
      aria-current={current ? 'page' : undefined}
      aria-expanded={children ? expanded : undefined}
      data-tip={label}
      data-warm={warm}
      onClick={(event) => onClick(event.currentTarget)}
    >
      <span className="hx-nav__ic">
        <Icon name={icon} />
        {dot && <span className="hx-nav__dot" />}
      </span>
      <span className="hx-nav__label">{label}</span>
      {children && !list && <Icon name="chevron-right" size="sm" className="hx-nav__chev" />}
    </button>
    {onFold && (
      <button
        type="button"
        className="hx-nav__fold"
        aria-label={expanded ? t('next.sidebar.foldList') : t('next.sidebar.unfoldList')}
        aria-expanded={expanded}
        onClick={onFold}
      >
        <Icon name="chevron-right" size="sm" className="hx-nav__chev" />
      </button>
    )}
    {children && (
      <div className="hx-nav__kids" inert={!expanded}>
        <div>{children}</div>
      </div>
    )}
  </div>
);

/** The designs under «Sinopia Design» folded by its chevron; per device. */
const DESIGNS_FOLDED_KEY = 'hx-sidebar-designs-folded';

const readDesignsFolded = (): boolean => {
  try {
    return localStorage.getItem(DESIGNS_FOLDED_KEY) !== '0';
  } catch {
    return true;
  }
};

const keepDesignsFolded = (folded: boolean) => {
  try {
    localStorage.setItem(DESIGNS_FOLDED_KEY, folded ? '1' : '0');
  } catch {
    // Blocked storage: the fold holds for this page.
  }
};

/** The apps under «Приложения» folded by its chevron; per device. */
const APPS_FOLDED_KEY = 'hx-sidebar-apps-folded';

const readAppsFolded = (): boolean => {
  try {
    return localStorage.getItem(APPS_FOLDED_KEY) !== '0';
  } catch {
    return true;
  }
};

const keepAppsFolded = (folded: boolean) => {
  try {
    localStorage.setItem(APPS_FOLDED_KEY, folded ? '1' : '0');
  } catch {
    // Blocked storage: the fold holds for this page.
  }
};

/** The last apps that are on in chats; all the installed ones open from «Приложения» itself. */
const APP_ROWS = 3;
const AppRows = ({
  apps,
  ids,
  named,
}: {
  apps: ISidebarApps;
  ids: readonly string[];
  named: readonly string[];
}) => {
  // The apps the person ordered (or the pinned boards and documents) and harnessly's own are always listed; of the
  // rest, the last few that are on in chats.
  let rest = 0;
  const shown = ids.filter(
    (id) => named.includes(id) || apps.builtins?.some((b) => b.id === id) || rest++ < APP_ROWS,
  );
  return (
    <>
      {shown.map((id) => {
        const b = apps.builtins?.find((x) => x.id === id);
        if (b)
          return (
            <button
              key={b.id}
              type="button"
              className="hx-nav__kid"
              aria-current={b.current ? 'page' : undefined}
              data-drag={NAV_DRAG.app}
              data-drag-id={appKey(b.id)}
              data-drop={NAV_DRAG.app}
              data-drop-id={appKey(b.id)}
              onClick={b.onOpen}
            >
              <Icon name={b.icon} size="sm" className="hx-nav__kid-icon" />
              <span className="hx-item__title">{b.name}</span>
            </button>
          );
        const a = apps.rows.find((x) => x.id === id);
        if (!a) return null;
        return (
          <button
            key={a.id}
            type="button"
            className="hx-nav__kid"
            aria-current={a.id === apps.current ? 'page' : undefined}
            data-drag={NAV_DRAG.app}
            data-drag-id={appKey(a.id)}
            data-drop={NAV_DRAG.app}
            data-drop-id={appKey(a.id)}
            onClick={() => apps.onOpen(a.id, a.chat)}
            onPointerEnter={() => apps.onPrepare?.(a.id, a.chat)}
            onFocus={() => apps.onPrepare?.(a.id, a.chat)}
          >
            <Icon name={a.icon || 'layout-grid'} size="sm" className="hx-nav__kid-icon" />
            <span className="hx-item__title">{a.name}</span>
          </button>
        );
      })}
    </>
  );
};

/** The designs at work under «Sinopia Design»: five, then «Дальше» for the rest. */
const DesignRows = ({
  designs,
  all,
  onAll,
}: {
  designs: ISidebarDesigns;
  all: boolean;
  onAll: () => void;
}) => {
  const { shown, hidden } = all
    ? { shown: designs.rows, hidden: 0 }
    : folderRows(designs.rows, designs.current);
  return (
    <>
      {shown.map((d) => (
        <button
          key={d.id}
          type="button"
          className="hx-nav__kid"
          aria-current={d.id === designs.current ? 'page' : undefined}
          onClick={() => designs.onOpen(d.id)}
        >
          <Icon name={designIcon(d)} size="sm" className="hx-nav__kid-icon" />
          <span className="hx-item__title">{d.title}</span>
        </button>
      ))}
      {hidden > 0 && (
        <button type="button" className="hx-folder-more" onClick={onAll}>
          {t('next.sidebar.folderMore')}
        </button>
      )}
    </>
  );
};

/** A ChatGPT chat among the day's chats: the same row, opened by its own screen, with its menu (rename, delete). */
const GptRow = ({
  row,
  current,
  gpt,
}: {
  row: ISidebarGpt['rows'][number];
  current: boolean;
  gpt: ISidebarGpt;
}) => {
  const title = row.title || t('next.chatgpt.newChat');
  // The talk going on in this chat is lit as an agent chat's is (chat-row.tsx).
  const voice = useRowVoice(gptConv(row.id));
  const mascot = useVoiceMascot();
  const menuActions = useMenuActions();
  const [renaming, setRenaming] = useState<{ saving: boolean } | null>(null);
  const [busy, setBusy] = useState(false);
  const renamed = (next: string | null) => {
    if (next === null) return setRenaming(null);
    setRenaming({ saving: true });
    gpt.onRename(row.id, next).then(
      () => setRenaming(null),
      () => {
        setRenaming(null);
        toast(t('next.sidebar.renameFailed'), { tone: 'error' });
      },
    );
  };
  const remove = () => {
    setBusy(true);
    gpt.onDelete(row.id).catch(() => {
      setBusy(false);
      toast(t('next.sidebar.deleteFailed'), { tone: 'error' });
    });
  };
  return (
    <div
      className="hx-item"
      aria-current={current ? 'page' : undefined}
      aria-busy={busy || undefined}
      data-voice={voice ? (voice.closing ? 'out' : 'on') : undefined}
    >
      <span className="hx-item__mark" aria-hidden="true">
        <span className="hx-mono">{title.slice(0, 1).toUpperCase()}</span>
      </span>
      {renaming ? (
        <RenameField title={title} saving={renaming.saving} onDone={renamed} />
      ) : (
        <button type="button" className="hx-item__open" data-tip={title} onClick={() => gpt.onOpen(row.id)}>
          {voice && (
            <span className="hx-item__voice-head">
              <VoiceHead state={HEAD_OF_LOOK[voice.look]} kind={mascot} />
            </span>
          )}
          <span className="hx-item__title">{title}</span>
        </button>
      )}
      {!renaming && (
        <span className="hx-item__end">
          {voice && <MiniWave look={voice.look} />}
          <MenuRoot actionsRef={menuActions}>
            <Menu.Trigger
              render={
                <button
                  type="button"
                  className="hx-item__more hx-item__menu"
                  aria-label={t('next.rowMenu.label')}
                >
                  <Icon name="ellipsis" size="sm" />
                </button>
              }
            />
            <MenuPanel
              actions={menuActions}
              className="hx-cmenu"
              label={t('next.rowMenu.label')}
              side="right"
              align="start"
              sideOffset={8}
            >
              <Menu.Item className="hx-cmenu__item" onClick={() => setRenaming({ saving: false })}>
                <Icon name="pencil" size="sm" />
                <span className="hx-cmenu__txt">{t('next.rowMenu.rename')}</span>
              </Menu.Item>
              <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={remove}>
                <Icon name="trash-2" size="sm" />
                <span className="hx-cmenu__txt">{t('next.chatgpt.side.delete')}</span>
              </Menu.Item>
            </MenuPanel>
          </MenuRoot>
        </span>
      )}
    </div>
  );
};

/** «Ещё»: the rest of the places, in a menu beside the item — not a list that pushes the chats down. */
const MoreMenu = ({ items }: { items: readonly IMoreItem[] }) => {
  const menuActions = useMenuActions();
  const current = items.some((item) => item.current);
  return (
    <div className="hx-nav__group hx-enter" data-enter="up" data-eb="220" data-i={4}>
      <MenuRoot actionsRef={menuActions}>
        <Menu.Trigger
          render={
            <button
              type="button"
              className="hx-nav__item"
              aria-current={current ? 'page' : undefined}
              data-tip={t('next.sidebar.more')}
            >
              <span className="hx-nav__ic">
                <Icon name="ellipsis" />
              </span>
              <span className="hx-nav__label">{t('next.sidebar.more')}</span>
            </button>
          }
        />
        <MenuPanel
          actions={menuActions}
          className="hx-cmenu"
          label={t('next.sidebar.more')}
          side="right"
          align="start"
          sideOffset={8}
        >
          {items.map((item) => (
            <Menu.Item key={item.id} className="hx-cmenu__item" onClick={item.onSelect}>
              <Icon name={item.icon} size="sm" />
              <span className="hx-cmenu__txt">{item.label}</span>
              {item.external && <Icon name="arrow-up-right" size="sm" />}
            </Menu.Item>
          ))}
        </MenuPanel>
      </MenuRoot>
    </div>
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
      data-warm="settings"
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
    <MenuRoot actionsRef={menuActions}>
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
    </MenuRoot>
  );
};

/**
 * The left column: actions (new chat, search, Designly, notifications, more), the pinned chats, projects with their
 * chats and the rest by day, and the person with their machine at the bottom. Data comes in through the props only;
 * every change goes out through `actions` and the row waits for the server's answer.
 */
const Sidebar = ({
  brand,
  hubHref = null,
  chats: chatsIn,
  projects,
  favorites = NO_FAVORITES,
  loadError,
  onRetryLoad,
  currentId,
  view,
  now,
  person,
  machine,
  unread,
  notificationsOpen,
  searchOpen,
  more,
  profile,
  actions,
  onOpen,
  onNewChat,
  onSearch,
  onDesignly,
  apps = null,
  onNotifications,
  onToggleRail,
  onCloseDrawer,
  onManageMachines,
  people,
  designs = null,
  gpt = null,
  nav = DEFAULT_NAV,
  onNav,
  ref,
}: ISidebarProps) => {
  countRender('sidebar');
  // A chat free itself while its delegated chats work shows that it waits for them.
  const chats = useMemo(() => (chatsIn === null ? null : withDelegationWait(chatsIn)), [chatsIn]);
  const railKey = useKeyHint('global.rail');
  const { pending, run: runPending } = usePendingList();
  // The person asked for a change: the favorites' next move slides (favorites-section.tsx); a failure moves
  // nothing, so it is taken back.
  // The layout the list holds while the pointer or the keyboard is in it (holdSections below).
  const [held, setHeld] = useState<TShownLayout | null>(null);
  const movingRef = useRef(false);
  const run = useCallback(
    (key: string, failure: string, action: () => Promise<unknown>): Promise<boolean> => {
      movingRef.current = true;
      return runPending(key, failure, action).then((ok) => {
        // The person's own change is shown where it goes: the hold starts again from the list as it is now.
        setHeld(null);
        if (!ok) movingRef.current = false;
        return ok;
      });
    },
    [runPending],
  );
  const [renaming, setRenaming] = useState<{ id: string; saving: boolean } | null>(null);
  const [projectChoice, setProjectChoice] = useState<ReadonlyMap<string, boolean>>(new Map());
  // Folders whose «Дальше» was pressed: they show all their chats.
  const [longFolders, setLongFolders] = useState<ReadonlySet<string>>(new Set());
  const [allDesigns, setAllDesigns] = useState(false);
  const [appsFolded, setAppsFolded] = useState(readAppsFolded);
  const foldApps = () => {
    keepAppsFolded(!appsFolded);
    setAppsFolded(!appsFolded);
  };
  const layout = useMemo(() => {
    const items: TNavItem[] = [NAV_ITEM.newChat, NAV_ITEM.search];
    if (onDesignly) items.push(NAV_ITEM.designly);
    if (apps) items.push(NAV_ITEM.apps);
    const ids = apps ? [...(apps.builtins ?? []).map((b) => b.id), ...apps.rows.map((r) => r.id)] : [];
    return navLayout(nav, items, ids);
  }, [nav, apps, onDesignly]);
  // The menu by hand: a main button among the others, an app between «Приложения» and the main buttons.
  const navRef = useRef<HTMLElement>(null);
  useDrag<TNavDrag, TNavDrag, INavLayout>({
    listRef: navRef,
    dragKinds: onNav ? NAV_DRAGS : [],
    dropKinds: NAV_DRAGS,
    lined: (source, target) => source.kind === target.kind,
    tracked: false,
    resolve: (source, target) =>
      target.id && target.edge ? withPlaced(layout, source.id, target.id, target.edge === EDGE.after) : null,
    onDrop: (next) =>
      onNav?.(navSettingsOf(next)).catch(() => toast(t('next.sidebar.navFailed'), { tone: 'error' })),
  });
  const appIds = layout.apps.filter((id) => !layout.hidden.has(`app:${id}`));
  const namedApps = nav.apps.length > 0 ? nav.apps : PINNED_APPS;
  const appRowCount = appIds.length;
  const [designsFolded, setDesignsFolded] = useState(readDesignsFolded);
  const foldDesigns = () => {
    keepDesignsFolded(!designsFolded);
    setDesignsFolded(!designsFolded);
  };
  const [foldedDays, setFoldedDays] = useState<ReadonlySet<TDayGroup>>(new Set());
  // The person's choice of showing the chats a chat delegated; without one they show while one of them is at work.
  const [kidsChoice, setKidsChoice] = useState<ReadonlyMap<string, boolean>>(new Map());
  // Ids the list has drawn; one that comes in later is fresh and slides in. null until the first list.
  const [known, setKnown] = useState<ReadonlySet<string> | null>(null);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(new Set());
  const [foldedGroups, setFoldedGroups] = useState<ReadonlySet<string>>(new Set());
  const [creatingGroup, setCreatingGroup] = useState<TGroupState>(GROUP_STATE.off);
  const [renamingGroup, setRenamingGroup] = useState<{ id: string; state: TGroupState } | null>(null);
  const railTip = useRailTip();
  const listRef = useRef<HTMLDivElement>(null);
  const rowCard = useRowCard();
  const loading = chats === null && loadError === null;
  const showSkeleton = useDelayedFlag(loading);

  // The first list is the one the favorites' layout came with: chats of a list that comes in pieces on a
  // load are not fresh and do not slide in one by one.
  const layoutKnown = favorites !== null;
  // The favorites' rows of the last load in this browser, read once: the skeleton's height until the layout comes.
  const [favRowsBefore] = useState(readFavRows);
  if (chats !== null && layoutKnown && (known === null || chats.some((chat) => !known.has(chat.id)))) {
    const ids = new Set(chats.map((chat) => chat.id));
    if (known !== null) setFresh(new Set([...ids].filter((id) => !known.has(id))));
    setKnown(new Set([...(known ?? []), ...ids]));
  }

  const natural = useMemo(
    () => (chats === null ? null : sectionsOf(chats, projects ?? [], now)),
    [chats, projects, now],
  );
  // While the pointer or the keyboard is in the list, its rows hold their places (group.ts, holdSections).
  const [pointerIn, setPointerIn] = useState(false);
  const [keysIn, setKeysIn] = useState(false);
  const holding = pointerIn || keysIn;
  if (holding && held === null && natural !== null) setHeld(sidebarLayout(natural));
  else if (!holding && held !== null) setHeld(null);
  const sections = useMemo(
    () => (natural === null || !holding ? natural : holdSections(natural, held)),
    [natural, holding, held],
  );

  // The ChatGPT chats join the chats of their day; a day with only them shows too.
  const daysWithGpt = useMemo(() => {
    const rows = gpt?.rows ?? [];
    return Object.values(DAY_GROUP)
      .map((group) => ({
        group,
        chats: sections?.days.find((d) => d.group === group)?.chats ?? [],
        gptRows: rows.filter((r) => dayGroupOf(r.updatedAt, now) === group),
      }))
      .filter((d) => d.chats.length > 0 || d.gptRows.length > 0);
  }, [sections, gpt?.rows, now]);

  const favView = useMemo(
    () => (chats === null || favorites === null ? EMPTY_VIEW : favoritesOf(chats, favorites)),
    [chats, favorites],
  );
  // The groups for the menus, and where each favorite chat stands: both keep their identity while the view does.
  const favGroups = useKeptItems(
    useMemo(() => favView.groups.map(({ id, name }) => ({ id, name })), [favView]),
    favGroupKey,
  );
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
  const { onCopyLink, onOpenInNewTab, onTerminal, onStopTerminal, onFork, onContinue, onMoveVm } = actions;
  const hasCopyLink = onCopyLink !== undefined;
  const hasNewTab = onOpenInNewTab !== undefined;
  const hasTerminal = onTerminal !== undefined;
  const hasStopTerminal = onStopTerminal !== undefined;
  const hasFork = onFork !== undefined;
  const hasContinue = onContinue !== undefined;
  const hasMoveVm = onMoveVm !== undefined;
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
      moveVm: hasMoveVm ? (id) => latest.current.actions.onMoveVm?.(id) : undefined,
      toggleKids: (id) => latest.current.toggleKids(id),
    };
  }, [hasCopyLink, hasNewTab, hasTerminal, hasStopTerminal, hasFork, hasContinue, hasMoveVm]);

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
    // A chat steps among chats and a group among groups; a group carried past the loose chats would show a place it
    // cannot take (groups stand first) and jump back on the drop.
    lined: (source, target) =>
      (source.kind === DRAG.chat && target.kind === DROP.chat) ||
      (source.kind === DRAG.group && target.kind === DROP.group),
    tracked: false,
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

  // A folder's first chats, then «Дальше» for the rest; opened once, it shows them all.
  const renderFolder = (id: string, list: readonly ISidebarChat[]) => {
    const { shown, hidden } = longFolders.has(id) ? { shown: list, hidden: 0 } : folderRows(list, currentId);
    return (
      <>
        {renderRows(shown, ROW_VARIANT.kid)}
        {hidden > 0 && (
          <button
            type="button"
            className="hx-folder-more"
            onClick={() => setLongFolders((current) => new Set(current).add(id))}
          >
            {t('next.sidebar.folderMore')}
          </button>
        )}
      </>
    );
  };

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
    sections.apps.length === 0 &&
    sections.days.length === 0;

  const pinnedCount = (chats ?? []).filter((chat) => chat.pinned).length;
  const skeletonRows = favRowsBefore ?? pinnedCount;
  const favoritesWait = sections !== null && !layoutKnown && skeletonRows > 0;
  const showFavorites =
    sections !== null &&
    layoutKnown &&
    (!isFavEmpty(favView) || (actions.onPlaceFavorite !== undefined && chats !== null && chats.length > 0));
  // What the next load's skeleton is sized by: the rows drawn now, once the layout is known.
  let favRows: number | null = null;
  if (sections !== null && layoutKnown) favRows = showFavorites ? favRowCount(favView) : 0;
  useEffect(() => {
    if (favRows !== null) keepFavRows(favRows);
  }, [favRows]);

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
      onClick={(event) => {
        if (opensRail(event.target)) onToggleRail();
      }}
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
            data-kbd={railKey ?? undefined}
            onClick={onToggleRail}
          >
            <Icon name="panel-left" />
          </button>
        </span>
        {hubHref ? (
          <ProductSwitcher brand={brand} hubHref={hubHref} />
        ) : (
          <span className="hx-brand__name">{brand}</span>
        )}
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
            {railKey && <kbd>{railKey}</kbd>}
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

      <nav ref={navRef} className="hx-nav" aria-label={t('next.sidebar.actions')}>
        {layout.top
          .filter((key) => !layout.hidden.has(key))
          .map((key, index) => {
            if (key === NAV_ITEM.newChat)
              return (
                <NavItem
                  key={key}
                  dragKey={key}
                  icon="square-pen"
                  label={t('next.sidebar.newChat')}
                  current={false}
                  index={index}
                  onClick={onNewChat}
                />
              );
            if (key === NAV_ITEM.search)
              return (
                <NavItem
                  key={key}
                  dragKey={key}
                  icon="search"
                  label={t('next.sidebar.search')}
                  current={searchOpen}
                  index={index}
                  onClick={onSearch}
                />
              );
            if (key === NAV_ITEM.designly && onDesignly)
              return (
                <NavItem
                  key={key}
                  dragKey={key}
                  icon="palette"
                  label={t('next.sidebar.designly')}
                  warm="designly"
                  current={view === SIDEBAR_VIEW.designly && !designs?.current}
                  index={index}
                  onClick={onDesignly}
                  expanded={designs !== null && designs.rows.length > 0 && !designsFolded}
                  list
                  onFold={designs !== null && designs.rows.length > 0 ? foldDesigns : undefined}
                >
                  {designs !== null && designs.rows.length > 0 && (
                    <DesignRows designs={designs} all={allDesigns} onAll={() => setAllDesigns(true)} />
                  )}
                </NavItem>
              );
            if (key === NAV_ITEM.apps && apps)
              return (
                <NavItem
                  key={key}
                  dragKey={key}
                  icon="layout-grid"
                  label={t('next.sidebar.apps')}
                  current={apps.allCurrent}
                  index={index}
                  onClick={apps.onAll}
                  expanded={appRowCount > 0 && !appsFolded}
                  list
                  onFold={appRowCount > 0 ? foldApps : undefined}
                >
                  {appRowCount > 0 && <AppRows apps={apps} ids={appIds} named={namedApps} />}
                </NavItem>
              );
            const id = appOfKey(key);
            if (id === null || !apps) return null;
            const own = apps.builtins?.find((b) => b.id === id);
            if (own)
              return (
                <NavItem
                  key={key}
                  dragKey={key}
                  icon={own.icon}
                  label={own.name}
                  current={own.current}
                  index={index}
                  onClick={own.onOpen}
                />
              );
            const row = apps.rows.find((r) => r.id === id);
            if (!row) return null;
            return (
              <NavItem
                key={key}
                dragKey={key}
                icon={row.icon || 'layout-grid'}
                label={row.name}
                current={row.id === apps.current}
                index={index}
                onClick={() => apps.onOpen(row.id, row.chat)}
              />
            );
          })}
        {more.length > 0 && <MoreMenu items={more} />}
      </nav>

      <MachineAlert alert={machine?.alert ?? null} onMore={() => onManageMachines?.()} />

      <div
        className="hx-sidebar__list"
        ref={listRef}
        onPointerEnter={() => setPointerIn(true)}
        onPointerLeave={() => setPointerIn(false)}
        onFocus={(event) => setKeysIn(focusVisible(event.target))}
        onBlur={(event) => {
          if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)))
            setKeysIn(false);
        }}
      >
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

        {favoritesWait && (
          <FavoritesSkeleton rows={skeletonRows} canCreate={actions.onCreateGroup !== undefined} />
        )}
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
            movingRef={movingRef}
          />
        )}

        {people}

        {sections && sections.apps.length > 0 && (
          <section aria-label={t('next.sidebar.appChats')}>
            <div className="hx-group__label hx-group__label--static hx-enter" data-enter="up" data-eb="470">
              {t('next.sidebar.appSection')}
            </div>
            {sections.apps.map(({ app, label, chats: ofApp }, i) => {
              // An app's folder is open until the person folds it: its chats are there to be glanced at.
              const open = projectChoice.get(`app:${app}`) ?? true;
              return (
                <div
                  key={app}
                  className="hx-nav__group hx-enter"
                  data-enter="up"
                  data-eb="500"
                  data-i={Math.min(i, 24)}
                  aria-expanded={open}
                >
                  <div className="hx-item hx-project">
                    <button
                      type="button"
                      className="hx-project__open"
                      aria-expanded={open}
                      data-tip={t('next.sidebar.appFolderTip', { app: label })}
                      onClick={() => toggleProject(`app:${app}`, open)}
                    >
                      <span className="hx-project__mark" aria-hidden="true">
                        <Icon name="app-window" className="hx-project__icon" />
                      </span>
                      <span className="hx-item__title">{label}</span>
                      <Icon name="chevron-right" size="sm" className="hx-nav__chev" />
                    </button>
                  </div>
                  <div className="hx-nav__kids" inert={!open}>
                    <div>{renderFolder(`app:${app}`, ofApp)}</div>
                  </div>
                </div>
              );
            })}
          </section>
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
                    <button
                      type="button"
                      className="hx-nav__add hx-project__add"
                      aria-label={t('next.sidebar.projectLook', { project: project.name })}
                      title={t('next.sidebar.projectLook', { project: project.name })}
                      onClick={() => openLookEditor({ k: 'folder', id: project.id })}
                    >
                      <Icon name="palette" size="sm" />
                    </button>
                    {actions.onProjectSettings && (
                      <button
                        type="button"
                        className="hx-nav__add hx-project__add"
                        aria-label={t('next.sidebar.projectSettings', { project: project.name })}
                        title={t('next.sidebar.projectSettings', { project: project.name })}
                        onClick={() => actions.onProjectSettings?.(project.id)}
                      >
                        <Icon name="settings" size="sm" />
                      </button>
                    )}
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
                      {renderFolder(project.id, inProject)}
                      {inProject.length === 0 && <p className="hx-snote">{t('next.sidebar.projectEmpty')}</p>}
                    </div>
                  </div>
                </div>
              );
            })}
          </section>
        )}

        <div className="hx-recent" data-drop="recent">
          {daysWithGpt.map(({ group, chats: ofDay, gptRows }) => {
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
                    {gpt &&
                      gptRows.map((row) => (
                        <GptRow key={row.id} row={row} current={row.id === gpt.current} gpt={gpt} />
                      ))}
                  </div>
                </div>
              </section>
            );
          })}
        </div>
      </div>

      <div className="hx-user hx-enter" data-enter="up" data-eb="820" data-health={machine?.health}>
        <ProfileButton person={person} items={profile} current={view === SIDEBAR_VIEW.settings} />
        <button
          type="button"
          className="hx-btn hx-tipped hx-user__bell"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          data-tip={t('next.sidebar.notifications')}
          aria-label={t('next.sidebar.notifications')}
          aria-pressed={notificationsOpen}
          onClick={(event) => onNotifications(event.currentTarget)}
        >
          <Icon name="bell" />
          <span className="hx-user__dot" data-on={unread} aria-hidden />
        </button>
      </div>
      {railTip.tip}
    </aside>
  );
};

export default Sidebar;
