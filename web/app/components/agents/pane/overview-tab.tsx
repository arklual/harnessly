import { useState, type ComponentType, type ReactNode } from 'react';
import { ArrowUpRight, ChevronDown, Sparkles } from 'lucide-react';
import { TwSwitch, cx } from '@/components/agents/ui';
import { PANE_KINDS } from '@/components/agents/pane/pane-registry';
import { usePaneAutoSetting } from '@/hooks/use-pane-auto-open';
import usePaneSurfaceShown, { PaneSurfaceShownContext } from '@/hooks/use-pane-surface-shown';
import type { TPaneTabKind } from '@/lib/hl-pane-tabs';
import { t } from '@/lib/i18n';

export interface IOverviewIsland {
  kind: TPaneTabKind;
  /** Shown once there is something in it; it stays mounted meanwhile so its count keeps up. */
  visible: boolean;
  count?: number;
  /** Something is running right now (subagents, a pipeline). */
  live?: boolean;
  icon?: ComponentType<{ className?: string }>;
  body: ReactNode;
}

// The key the islands used before they were tabs (1.56.0): what a person had opened stays opened.
const OPEN_KEY = 'hl:islands-open';

const readOpen = (): string[] => {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(OPEN_KEY) || '[]');
    return Array.isArray(saved) ? saved.filter((item): item is string => typeof item === 'string') : [];
  } catch {
    return [];
  }
};

interface IOverviewTabProps {
  islands: readonly IOverviewIsland[];
  /** The island's own tab, beside the overview. */
  onOpenTab: (kind: TPaneTabKind) => void;
  /** Dev servers exist only for a conversation with a machine. */
  withServers: boolean;
}

/**
 * The first tab of the panel: the conversation at a glance, an island per tool that has something to show.
 * Islands start folded to their headers; a row inside opens its own tab.
 */
const OverviewTab = ({ islands, onOpenTab, withServers }: IOverviewTabProps) => {
  const [open, setOpen] = useState(readOpen);
  const shown = usePaneSurfaceShown();
  const [auto, setAuto] = usePaneAutoSetting();
  const toggle = (kind: TPaneTabKind) => {
    const next = open.includes(kind) ? open.filter((item) => item !== kind) : [...open, kind];
    setOpen(next);
    try {
      localStorage.setItem(OPEN_KEY, JSON.stringify(next));
    } catch {}
  };
  const anyVisible = islands.some((island) => island.visible);

  return (
    <div className="tw-rpane-overview">
      {!anyVisible && (
        <div className="tw-rpane-overview-empty">
          <Sparkles />
          <p>{t('pane.overviewTab.empty')}</p>
        </div>
      )}
      {islands.map(({ kind, visible, count, live, icon, body }) => {
        const { title, icon: KindIcon } = PANE_KINDS[kind];
        const Icon = icon ?? KindIcon;
        const folded = !open.includes(kind);
        const bodyId = `tw-rpane-isle-${kind}`;
        return (
          <section
            key={kind}
            className="tw-rpane-isle"
            data-kind={kind}
            data-folded={folded || undefined}
            aria-label={title}
            hidden={!visible}
          >
            <header className="tw-rpane-isle-head">
              <button
                type="button"
                className="tw-rpane-isle-toggle"
                aria-expanded={!folded}
                aria-controls={bodyId}
                onClick={() => toggle(kind)}
              >
                <span className="tw-rpane-isle-icon">
                  <Icon />
                </span>
                <span className="tw-rpane-isle-title">{title}</span>
                {!!count && (
                  <span className={cx('tw-rpane-count', live && 'tw-rpane-count-live')}>{count}</span>
                )}
                <ChevronDown className="tw-rpane-isle-chevron" />
              </button>
              <button
                type="button"
                className="tw-icon-button tw-rpane-isle-open"
                aria-label={t('pane.overviewTab.openAsTabFor', { title })}
                title={t('pane.overviewTab.openAsTab')}
                onClick={() => onOpenTab(kind)}
              >
                <ArrowUpRight />
              </button>
            </header>
            <div id={bodyId} className="tw-rpane-isle-body" hidden={folded}>
              <PaneSurfaceShownContext.Provider value={shown && visible && !folded}>
                {body}
              </PaneSurfaceShownContext.Provider>
            </div>
          </section>
        );
      })}
      <TwSwitch
        className="tw-rpane-overview-auto"
        checked={auto}
        onCheckedChange={setAuto}
        label={t('pane.overviewTab.autoOpen')}
        description={
          withServers ? t('pane.overviewTab.autoOpenWithServers') : t('pane.overviewTab.autoOpenDescription')
        }
      />
    </div>
  );
};

export default OverviewTab;
