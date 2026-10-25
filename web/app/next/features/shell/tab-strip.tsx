// The strip of tabs over the main window, as a browser draws it: the active tab is the window's own colour and runs
// into it; the others are quiet until hovered. A tab is pressed to switch (a mouse on the press, a finger on the tap),
// closed by its × or the middle button, and opens a menu at the pointer (right click, two-finger tap, a held finger, the
// menu key). A tab is carried along the strip on the shared drag (ui/use-drag: the tab itself follows the pointer, its
// neighbours step aside, it settles into its place on the drop) and the order is kept by the owner. A tab has the
// width of its title between a least and a most; only a crowded strip narrows them, then it scrolls sideways.
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent } from 'react';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import { useLeavingList } from '@/next/ui/presence';
import useDrag, { afterTouchClick, AXIS } from '@/next/ui/use-drag';
import useFlip from '@/next/ui/use-flip';
import TabMenu, { type ITabMenuAt } from '@/next/features/shell/tab-menu';
import { planTabMove, TAB, TAB_KINDS } from '@/next/features/shell/tab-order';
import { t } from '@/next/i18n';

/** A tab's exit, as long as hx-gtab-out in tabs.css. */
const TAB_EXIT_MS = 180;

/** How an agent at work shows on a tab: its icon turns, quickly for a turn, slowly for what runs in the background. */
export const PACE = { working: 'working', background: 'background' } as const;
export type TPace = (typeof PACE)[keyof typeof PACE];

export interface IStripTab {
  id: string;
  title: string;
  icon: string;
  /** The icon turns (an agent at work in the chat); null: it stands still. */
  busy: TPace | null;
  /** What the tab's hint adds to its title (what a chat at work waits for); absent when nothing. */
  note?: string;
  /** Something waits for the person there (a question, a permission). */
  attention: boolean;
}

export interface ITabStripProps {
  tabs: readonly IStripTab[];
  active: string;
  onActivate: (id: string) => void;
  onClose: (id: string) => void;
  /** Every tab but this one goes. */
  onCloseOthers: (id: string) => void;
  /** Every tab after this one goes. */
  onCloseRight: (id: string) => void;
  onNew: () => void;
  /** The tab goes to `index` of the order it has once it is out of its old place. */
  onMove: (id: string, index: number) => void;
}

const tabOf = (list: HTMLElement | null, id: string): HTMLElement | null =>
  list?.querySelector<HTMLElement>(`[data-tab="${CSS.escape(id)}"]`) ?? null;

const TabStrip = ({
  tabs,
  active,
  onActivate,
  onClose,
  onCloseOthers,
  onCloseRight,
  onNew,
  onMove,
}: ITabStripProps) => {
  const listRef = useRef<HTMLDivElement>(null);
  const shown = useLeavingList(tabs, (tab) => tab.id, TAB_EXIT_MS);
  const ids = tabs.map((tab) => tab.id);
  // The menu keeps its tab and place while it leaves, so it does not change under the exit.
  const [menu, setMenu] = useState<ITabMenuAt | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const openMenu = (tabId: string, x: number, y: number) => {
    setMenu({ tabId, x, y });
    setMenuOpen(true);
  };

  const drag = useDrag({
    listRef,
    dragKinds: TAB_KINDS,
    dropKinds: TAB_KINDS,
    axis: AXIS.x,
    lined: () => true,
    resolve: (source, target) =>
      planTabMove(
        tabs.map((tab) => tab.id),
        source,
        target,
      ),
    onDrop: (plan) => onMove(plan.id, plan.index),
    onHoldRelease: (source, point) => afterTouchClick(() => openMenu(source.id, point.x, point.y)),
  });
  useFlip(listRef, true, AXIS.x);

  // The active tab is kept in view when the strip scrolls.
  useEffect(() => {
    tabOf(listRef.current, active)?.scrollIntoView({
      block: 'nearest',
      inline: 'nearest',
      behavior: 'smooth',
    });
  }, [active, tabs.length]);

  const onPointerDown = (event: PointerEvent<HTMLDivElement>, id: string) => {
    if (event.pointerType !== 'mouse' || event.button !== 0) return;
    if ((event.target as Element).closest('.hx-gtab__x')) return;
    onActivate(id);
  };

  const onContextMenu = (event: MouseEvent<HTMLDivElement>, id: string) => {
    // A held finger that has picked the tab up is a drag's: the system's menu on it is cancelled there.
    if (listRef.current?.hasAttribute('data-dragging')) return;
    event.preventDefault();
    const rect = event.currentTarget.getBoundingClientRect();
    // The menu key has no pointer: it opens under the tab.
    const fromKeys = event.clientX === 0 && event.clientY === 0;
    openMenu(id, fromKeys ? rect.left + 12 : event.clientX, fromKeys ? rect.bottom : event.clientY);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>, id: string) => {
    const at = ids.indexOf(id);
    if (event.altKey && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
      event.preventDefault();
      const to = at + (event.key === 'ArrowLeft' ? -1 : 1);
      if (to >= 0 && to < ids.length) onMove(id, to);
      return;
    }
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
      event.preventDefault();
      const rect = event.currentTarget.getBoundingClientRect();
      openMenu(id, rect.left + 12, rect.bottom);
      return;
    }
    let next: IStripTab | undefined;
    if (event.key === 'ArrowRight') next = tabs[at + 1];
    else if (event.key === 'ArrowLeft') next = tabs[at - 1];
    else if (event.key === 'Home') next = tabs[0];
    else if (event.key === 'End') next = tabs[tabs.length - 1];
    else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onActivate(id);
      return;
    } else if (event.key === 'Delete') {
      event.preventDefault();
      onClose(id);
      return;
    }
    if (!next) return;
    event.preventDefault();
    onActivate(next.id);
    tabOf(listRef.current, next.id)?.focus();
  };

  // A tab that goes while its menu is open takes the menu with it.
  const menuShown = menuOpen && menu !== null && ids.includes(menu.tabId);
  const hintOff = drag.dragging || menuShown;

  return (
    <div className="hx-gtabs" data-nogesture>
      <div ref={listRef} className="hx-gtabs__list" role="tablist" aria-label={t('next.tabs.label')}>
        {shown.map(({ item: tab, leaving }) => {
          const selected = tab.id === active && !leaving;
          return (
            <Tooltip
              key={tab.id}
              label={tab.note ? `${tab.title} · ${tab.note}` : tab.title}
              disabled={hintOff || leaving}
            >
              <div
                className="hx-gtab"
                role="tab"
                data-tab={tab.id}
                data-state={leaving ? 'closing' : undefined}
                data-drag={leaving ? undefined : TAB}
                data-drag-id={tab.id}
                data-drop={leaving ? undefined : TAB}
                data-drop-id={tab.id}
                data-flip={leaving ? undefined : tab.id}
                aria-selected={selected}
                tabIndex={selected ? 0 : -1}
                onPointerDown={(event) => onPointerDown(event, tab.id)}
                onClick={() => onActivate(tab.id)}
                onContextMenu={(event) => onContextMenu(event, tab.id)}
                onMouseDown={(event) => {
                  // The middle button would start the browser's autoscroll.
                  if (event.button === 1) event.preventDefault();
                }}
                onAuxClick={(event) => {
                  if (event.button !== 1) return;
                  event.preventDefault();
                  onClose(tab.id);
                }}
                onKeyDown={(event) => onKeyDown(event, tab.id)}
              >
                <Icon
                  name={tab.icon}
                  size="sm"
                  className={tab.busy ? `hx-gtab__ic hx-gtab__ic--${tab.busy}` : 'hx-gtab__ic'}
                />
                {tab.attention && <span className="hx-gtab__dot" aria-hidden="true" />}
                <span className="hx-gtab__name">{tab.title}</span>
                <button
                  type="button"
                  className="hx-gtab__x"
                  data-nodrag
                  tabIndex={-1}
                  aria-label={t('next.tabs.close')}
                  onClick={(event) => {
                    event.stopPropagation();
                    onClose(tab.id);
                  }}
                >
                  <Icon name="x" size="sm" />
                </button>
                <span className="hx-gtab__bar" aria-hidden="true" />
              </div>
            </Tooltip>
          );
        })}
      </div>
      <button
        type="button"
        className="hx-gtabs__add hx-tipped"
        aria-label={t('next.tabs.new')}
        onClick={onNew}
      >
        <Icon name="plus" size="sm" />
        <span className="hx-tip" data-pos="below">
          {t('next.tabs.new')}
          <kbd>⌘T</kbd>
        </span>
      </button>
      <TabMenu
        open={menuShown}
        at={menu}
        ids={ids}
        onOpenChange={setMenuOpen}
        onClose={onClose}
        onCloseOthers={onCloseOthers}
        onCloseRight={onCloseRight}
        returnTo={(id) => tabOf(listRef.current, id) ?? tabOf(listRef.current, active)}
      />
    </div>
  );
};

export default TabStrip;
