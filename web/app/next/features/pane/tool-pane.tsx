// The right pane's contents as the design draws them (.hx-pane > .hx-browser): the strip of tabs, «во всю ширину» and
// close; the bar with the tab's title and what it is about; the tab's body. The tools are the first tab of the strip,
// always there and never closed (icon only); what is opened from them opens beside it. On the phone the strip begins
// with «Чат», which takes the person back.
import { useEffect, useRef, useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import { useHScroll } from '@/next/ui/use-h-scroll';
import { t } from '@/next/i18n';
import '@/next/styles/pane.css';

const TAB_OUT_MS = 200;
/** The tabs there while the pane slides in (and its first list loads) do not grow into the strip. */
const SETTLE_MS = 400;

/** The id of the permanent first tab, «Инструменты». */
export const TOOLS_TAB = 'tools';

export interface IPaneTab {
  id: string;
  icon: string;
  name: string;
  /** Stays open: no close button. */
  pinned?: boolean;
  /** Opened behind the tab on screen and not looked at yet: a dot until it is. */
  unread?: boolean;
  /** Holds text that is not saved: a dot by its name. */
  dirty?: boolean;
}

export interface IToolPaneProps {
  tabs: IPaneTab[];
  tab: string;
  /** The open tab draws its own address bar (a browser tab): the pane's bar would repeat it. */
  ownBar?: boolean;
  maxed: boolean;
  onPick: (id: string) => void;
  onCloseTab: (id: string) => void;
  onToggleMax: () => void;
  onClose: () => void;
  /** The open tab's own buttons at the end of its bar. */
  actions?: ReactNode;
  /** The window's buttons that move here from the header while the pane is open: the terminal, «В фоне». */
  strip?: ReactNode;
  children: ReactNode;
}

/**
 * The tabs the pane opens with, or that arrive while it slides in, are already there: only a tab opened later grows
 * into the strip (site.css hx-tab-in), so the strip does not re-lay its titles on every frame of the pane's slide.
 */
const useStillTabs = (ids: readonly string[]): ReadonlySet<string> => {
  const [still, setStill] = useState<ReadonlySet<string>>(() => new Set(ids));
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(true), SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, []);
  if (!settled && ids.some((id) => !still.has(id))) setStill(new Set([...still, ...ids]));
  return still;
};

const ToolPane = ({
  tabs,
  tab,
  ownBar = false,
  maxed,
  onPick,
  onCloseTab,
  onToggleMax,
  onClose,
  actions,
  strip,
  children,
}: IToolPaneProps) => {
  const [closing, setClosing] = useState<string | null>(null);
  const tabsRef = useRef<HTMLDivElement>(null);
  const still = useStillTabs(tabs.map((x) => x.id));
  // A strip fuller than its width scrolls: the open tab is kept in view and the edges with more behind them fade.
  useHScroll(tabsRef);
  const closeTab = (id: string) => {
    setClosing(id);
    window.setTimeout(() => {
      setClosing(null);
      onCloseTab(id);
    }, TAB_OUT_MS);
  };
  const maxLabel = maxed ? t('next.pane.unmax') : t('next.pane.max');

  return (
    <div className="hx-browser hx-toolpane">
      <div className="hx-strip">
        <button type="button" className="hx-tochat" aria-label={t('next.pane.toChat')} onClick={onClose}>
          <Icon name="message-square" size="sm" />
          <span>{t('next.pane.chat')}</span>
        </button>
        <div className="hx-strip__nav" role="tablist" aria-label={t('next.pane.tabs')}>
          <Tooltip label={t('next.pane.tools')}>
            <div
              className="hx-tab"
              role="tab"
              tabIndex={0}
              data-home="true"
              data-still="true"
              aria-selected={tab === TOOLS_TAB}
              aria-label={t('next.pane.tools')}
              onClick={() => onPick(TOOLS_TAB)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && event.target === event.currentTarget) onPick(TOOLS_TAB);
              }}
            >
              <Icon name="layout-grid" size="sm" className="hx-tab__ic" />
            </div>
          </Tooltip>
          <div ref={tabsRef} className="hx-strip__tabs" role="presentation">
            {tabs.map((x) => (
              <Tooltip key={x.id} label={x.name}>
                <div
                  className="hx-tab"
                  role="tab"
                  tabIndex={0}
                  aria-selected={x.id === tab}
                  data-state={closing === x.id ? 'closing' : 'open'}
                  data-still={still.has(x.id) || undefined}
                  data-unread={x.unread || undefined}
                  data-dirty={x.dirty || undefined}
                  onClick={(event) => {
                    if ((event.target as Element).closest('.hx-tab__x')) return;
                    onPick(x.id);
                  }}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && event.target === event.currentTarget) onPick(x.id);
                  }}
                >
                  <Icon name={x.icon} size="sm" className="hx-tab__ic" />
                  <span className="hx-tab__name">{x.name}</span>
                  {!x.pinned && (
                    <button
                      type="button"
                      className="hx-tab__x"
                      aria-label={t('next.pane.closeTab')}
                      onClick={(event) => {
                        event.stopPropagation();
                        closeTab(x.id);
                      }}
                    >
                      <Icon name="x" size="sm" />
                    </button>
                  )}
                </div>
              </Tooltip>
            ))}
          </div>
        </div>
        {strip && <div className="hx-strip__extra">{strip}</div>}
        <button
          type="button"
          className="hx-head-btn hx-tipped hx-strip__max"
          data-keeps-term-focus=""
          aria-label={maxLabel}
          onClick={onToggleMax}
        >
          <Icon name={maxed ? 'minimize-2' : 'maximize-2'} />
          <span className="hx-tip" data-pos="below" data-align="end">
            {maxLabel}
          </span>
        </button>
        <button
          type="button"
          className="hx-head-btn hx-tipped hx-pane__x"
          aria-label={t('next.pane.close')}
          onClick={onClose}
        >
          <Icon name="x" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t('next.pane.closeShort')}
            <kbd>Esc</kbd>
          </span>
        </button>
      </div>
      {/* The tab already names what is open: the bar under it carries only the actions, when there are any. */}
      {tab !== TOOLS_TAB && !ownBar && actions && (
        <div className="hx-browser__bar hx-pane__bar">
          <span className="hx-spacer" />
          {actions}
        </div>
      )}
      <div className="hx-pane__body">{children}</div>
    </div>
  );
};

export default ToolPane;
