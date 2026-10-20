import { useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { Ban, PanelBottom, Search, X } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  TW_MENU_KIND,
  TwButton,
  TwTooltip,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { NetworkConditions } from '@/components/agents/devtools/network-conditions';
import { NetworkPanel } from '@/components/agents/devtools/network-panel';
import { ConsolePanel } from '@/components/agents/devtools/console-panel';
import { ServerPanel } from '@/components/agents/devtools/server-panel';
import { DEVTOOLS_TAB, type TDevtoolsTab, type TPreviewDevtools } from '@/hooks/use-preview-devtools';
import { THROTTLE_LABELS, conditionsChanged } from '@/lib/hl-preview-devtools-view';
import { t } from '@/lib/i18n';

const TABS: { value: TDevtoolsTab; label: string }[] = [
  { value: DEVTOOLS_TAB.network, label: t('devtools.devtoolsDrawer.tabNetwork') },
  { value: DEVTOOLS_TAB.console, label: t('devtools.devtoolsDrawer.tabConsole') },
  { value: DEVTOOLS_TAB.server, label: t('devtools.devtoolsDrawer.tabServer') },
];

const HEIGHT_KEY = 'tw-devtools-height';
const DEFAULT_HEIGHT = 300;
const MIN_HEIGHT = 140;
/** The page above keeps at least this much: the drawer is a tool for the page, not instead of it. */
const PAGE_MIN = 120;
const KEY_STEP = 24;

const readHeight = (): number => {
  try {
    const stored = Number(window.localStorage.getItem(HEIGHT_KEY));
    return Number.isFinite(stored) && stored >= MIN_HEIGHT ? stored : DEFAULT_HEIGHT;
  } catch {
    return DEFAULT_HEIGHT;
  }
};

const writeHeight = (height: number) => {
  try {
    window.localStorage.setItem(HEIGHT_KEY, String(Math.round(height)));
  } catch {
    // A viewer without storage keeps the height for this page only.
  }
};

/**
 * The drawer's height is the person's, per viewer: dragged by the top edge (or the arrow keys on it), kept in
 * localStorage, bounded by the page area it shares. It goes to CSS as a variable on the drawer element.
 */
const useDrawerHeight = () => {
  const ref = useRef<HTMLElement>(null);
  const height = useRef(DEFAULT_HEIGHT);

  const apply = (next: number) => {
    const element = ref.current;
    if (!element) return;
    const room = (element.parentElement?.clientHeight ?? next + PAGE_MIN) - PAGE_MIN;
    height.current = Math.round(Math.max(MIN_HEIGHT, Math.min(next, Math.max(MIN_HEIGHT, room))));
    element.style.setProperty('--tw-devtools-h', `${height.current}px`);
  };

  useLayoutEffect(() => {
    apply(readHeight());
  }, []);

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const startY = event.clientY;
    const startHeight = height.current;
    // The page's iframe would swallow the pointer while it crosses it.
    document.documentElement.setAttribute('data-tw-devtools-resizing', '');
    const move = (moveEvent: globalThis.PointerEvent) => apply(startHeight + startY - moveEvent.clientY);
    const end = () => {
      document.documentElement.removeAttribute('data-tw-devtools-resizing');
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
      writeHeight(height.current);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
    event.preventDefault();
    apply(height.current + (event.key === 'ArrowUp' ? KEY_STEP : -KEY_STEP));
    writeHeight(height.current);
  };

  return { ref, onPointerDown, onKeyDown };
};

interface IDevtoolsDrawerProps {
  devtools: TPreviewDevtools;
  slot: string;
  machineId?: string;
  tab: TDevtoolsTab;
  onTab: (tab: TDevtoolsTab) => void;
  onClose: () => void;
}

/**
 * Chrome-like DevTools under the page of the right panel's browser: the page's requests and console as the
 * runtime saw them, the dev server's terminal problems, and the network conditions the page runs under.
 */
export const DevtoolsDrawer = ({ devtools, slot, machineId, tab, onTab, onClose }: IDevtoolsDrawerProps) => {
  const { ref, onPointerDown, onKeyDown } = useDrawerHeight();
  const [query, setQuery] = useState('');
  const { error, saveError, clearing, clear, dismissSaveError } = devtools;

  const search = (
    <label className="tw-devtools-search">
      <Search aria-hidden />
      <input
        className="tw-devtools-search-input"
        type="search"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder={t('devtools.devtoolsDrawer.filter')}
        aria-label={
          tab === DEVTOOLS_TAB.network
            ? t('devtools.devtoolsDrawer.filterAddress')
            : t('devtools.devtoolsDrawer.filterText')
        }
      />
    </label>
  );

  const onTabKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    const index = TABS.findIndex((item) => item.value === tab);
    const next = TABS[(index + (event.key === 'ArrowRight' ? 1 : TABS.length - 1)) % TABS.length];
    onTab(next.value);
    event.currentTarget.querySelector<HTMLElement>(`[data-tab="${next.value}"]`)?.focus();
  };

  return (
    <section ref={ref} className="tw-devtools" aria-label={t('devtools.devtoolsDrawer.title')}>
      <div
        className="tw-devtools-resize"
        role="separator"
        aria-orientation="horizontal"
        aria-label={t('devtools.devtoolsDrawer.height')}
        tabIndex={0}
        onPointerDown={onPointerDown}
        onKeyDown={onKeyDown}
      />
      <div className="tw-devtools-head">
        <div
          className="tw-devtools-tabs"
          role="tablist"
          aria-label={t('devtools.devtoolsDrawer.sections')}
          onKeyDown={onTabKey}
        >
          {TABS.map((item) => (
            <button
              key={item.value}
              type="button"
              role="tab"
              data-tab={item.value}
              aria-selected={tab === item.value}
              tabIndex={tab === item.value ? 0 : -1}
              className="tw-devtools-tab"
              onClick={() => onTab(item.value)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <span className="tw-devtools-head-gap" />
        {tab !== DEVTOOLS_TAB.server && (
          <TwTooltip content={t('devtools.devtoolsDrawer.clear')}>
            <TwButton
              type="button"
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              className="tw-devtools-icon"
              aria-label={t('devtools.devtoolsDrawer.clear')}
              pending={clearing}
              onClick={() => void clear()}
            >
              <Ban />
            </TwButton>
          </TwTooltip>
        )}
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          className="tw-devtools-icon"
          aria-label={t('devtools.devtoolsDrawer.close')}
          title={t('devtools.devtoolsDrawer.closeShort')}
          onClick={onClose}
        >
          <X />
        </TwButton>
      </div>
      {tab === DEVTOOLS_TAB.network && (
        <div className="tw-devtools-toolbar">
          <NetworkConditions devtools={devtools} />
          {search}
        </div>
      )}
      {saveError && (
        <p className="tw-devtools-alert" role="alert">
          <span>{saveError}</span>
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            className="tw-devtools-icon"
            aria-label={t('devtools.devtoolsDrawer.hideError')}
            onClick={dismissSaveError}
          >
            <X />
          </TwButton>
        </p>
      )}
      {error && (
        <p className="tw-devtools-alert" role="status">
          {error}
        </p>
      )}
      <div
        className="tw-devtools-body"
        role="tabpanel"
        aria-label={TABS.find((item) => item.value === tab)?.label}
      >
        {tab === DEVTOOLS_TAB.network && <NetworkPanel devtools={devtools} query={query} />}
        {tab === DEVTOOLS_TAB.console && <ConsolePanel devtools={devtools} query={query} search={search} />}
        {tab === DEVTOOLS_TAB.server && (
          <ServerPanel slot={slot} machineId={machineId} query={query} search={search} />
        )}
      </div>
    </section>
  );
};

const TOGGLE_LABEL = t('devtools.devtoolsDrawer.title');

/** What the tooltip adds when the page does not run under the person's own network. */
const toggleTip = (devtools: TPreviewDevtools): string => {
  const { throttle, blocked } = devtools.log.settings;
  const notes = [
    throttle.preset !== 'none' ? THROTTLE_LABELS[throttle.preset] : null,
    blocked.length > 0 ? t('devtools.devtoolsDrawer.blockingOn') : null,
  ].filter(Boolean);
  return notes.length > 0 ? `${TOGGLE_LABEL} · ${notes.join(', ')}` : TOGGLE_LABEL;
};

/** The browser bar's button: a dot only while the page is slowed or has blocked addresses, nothing else. */
export const DevtoolsToggle = ({
  devtools,
  open,
  onToggle,
}: {
  devtools: TPreviewDevtools;
  open: boolean;
  onToggle: () => void;
}) => {
  const changed = conditionsChanged(devtools.log.settings);
  return (
    <TwTooltip content={toggleTip(devtools)}>
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        className="tw-devtools-toggle"
        aria-label={TOGGLE_LABEL}
        aria-pressed={open}
        data-changed={changed || undefined}
        onClick={onToggle}
        icon={<PanelBottom />}
      />
    </TwTooltip>
  );
};

/** The same switch in the "More" menu. */
export const devtoolsMenuEntry = (open: boolean, onToggle: () => void): TTwMenuEntry => ({
  kind: TW_MENU_KIND.checkbox,
  id: 'devtools',
  label: TOGGLE_LABEL,
  checked: open,
  onSelect: onToggle,
});
