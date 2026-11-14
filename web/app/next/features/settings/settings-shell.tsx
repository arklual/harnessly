// The Settings screen's frame (the design's .hx-set): the sections on the left in three groups (the app, the agents,
// the machine), the open one on the right. On a phone the list is the screen and the open section slides over it from
// the right, with a way back; the sidebar opens from the list's head with the same glyph as the sidebar's own toggle.
import '@/next/styles/settings.css';
import '@/next/styles/settings-changes.css';

import { useEffect, useRef, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import { SETTINGS_SECTIONS, type SettingsSection } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';

/** What a section's entry in the list draws: a glyph of the icon set, or the logo of an agent. */
const SECTION_MARK: Readonly<Record<SettingsSection, { icon: string } | { agent: 'claude' | 'codex' }>> = {
  general: { icon: 'sliders-horizontal' },
  appearance: { icon: 'palette' },
  keys: { icon: 'keyboard' },
  voice: { icon: 'mic' },
  notifications: { icon: 'bell' },
  safe: { icon: 'lock' },
  browser: { icon: 'agent-browser' },
  takes: { icon: 'mic' },
  accounts: { icon: 'key-round' },
  smart: { icon: 'layers' },
  claude: { agent: 'claude' },
  codex: { agent: 'codex' },
  skills: { icon: 'blocks' },
  machines: { icon: 'server' },
  apps: { icon: 'layout-grid' },
  connections: { icon: 'plug' },
  design: { icon: 'pen-tool' },
  extensions: { icon: 'blocks' },
  plugins: { icon: 'blocks' },
  beta: { icon: 'sparkles' },
};

/** The groups of the list, in the order of SETTINGS_SECTIONS. */
const NAV_GROUPS = [
  { id: 'app', sections: ['general', 'appearance', 'keys', 'notifications', 'voice', 'takes'] },
  { id: 'agents', sections: ['claude', 'codex', 'accounts', 'smart', 'skills', 'safe', 'browser'] },
  { id: 'machine', sections: ['machines', 'apps', 'connections', 'design', 'extensions', 'plugins', 'beta'] },
] as const satisfies readonly { id: string; sections: readonly SettingsSection[] }[];

interface ISettingsShellProps {
  /** The section on the page. */
  section: SettingsSection;
  /** On a phone: the page is over the list (an address with a section), or the list is shown; a wide window shows both. */
  pageOpen: boolean;
  /** The line under the section's title; the binding knows which machine and file it is about. */
  lead: string;
  /** Sections the list leaves out (Extensions, where no app fills a slot). */
  hidden?: readonly SettingsSection[];
  /** Sections where an agent has just changed a setting: their entries carry a dot until the person opens them. */
  changed?: readonly SettingsSection[];
  onPick: (section: SettingsSection) => void;
  onBack: () => void;
  /** The agent's field, kept at the bottom of the page over what scrolls. */
  island?: ReactNode;
  children: ReactNode;
}

const SettingsShell = ({
  section,
  pageOpen,
  lead,
  hidden = [],
  changed = [],
  onPick,
  onBack,
  island,
  children,
}: ISettingsShellProps) => {
  const page = useRef<HTMLDivElement>(null);
  // A section opens from its top.
  useEffect(() => {
    page.current?.scrollTo({ top: 0 });
  }, [section]);
  // The agent's field lies over the page: the page keeps as much room under its last row as the field takes now (it
  // grows with what is typed, and with the agent's answer over it), so nothing of the page stays under it.
  const root = useRef<HTMLElement>(null);
  const isle = useRef<HTMLDivElement>(null);
  const hasIsland = Boolean(island);
  useEffect(() => {
    const set = root.current;
    const el = isle.current;
    if (!set || !el || !hasIsland || typeof ResizeObserver === 'undefined') return;
    const follow = () =>
      set.style.setProperty('--hx-isle-h', `${Math.ceil(el.getBoundingClientRect().height)}px`);
    const watcher = new ResizeObserver(follow);
    watcher.observe(el);
    follow();
    return () => {
      watcher.disconnect();
      set.style.removeProperty('--hx-isle-h');
    };
  }, [hasIsland]);
  return (
    <section className="hx-set" data-page={pageOpen} ref={root}>
      <nav className="hx-set__nav" aria-label={t('next.settings.nav')}>
        <div className="hx-set__nav-head">
          <span className="hx-set__h1">{t('next.settings.title')}</span>
        </div>
        {NAV_GROUPS.map((group) => (
          <div
            key={group.id}
            className="hx-set__nav-group"
            role="group"
            aria-labelledby={`hx-set-nav-${group.id}`}
          >
            <div className="hx-set__nav-caption" id={`hx-set-nav-${group.id}`}>
              {t(`next.settings.navGroup.${group.id}`)}
            </div>
            {group.sections
              .filter((id) => !hidden.includes(id))
              .map((id) => {
                const mark = SECTION_MARK[id];
                return (
                  <button
                    key={id}
                    type="button"
                    className="hx-set__nav-item"
                    data-id={id}
                    aria-current={id === section ? true : undefined}
                    ref={(element) =>
                      element?.style.setProperty('--i', String(SETTINGS_SECTIONS.indexOf(id)))
                    }
                    onClick={() => onPick(id)}
                  >
                    <span className="hx-set__nav-ic">
                      {'agent' in mark ? (
                        <i className="hx-agent" data-agent={mark.agent} aria-hidden="true" />
                      ) : (
                        <Icon name={mark.icon} size="sm" />
                      )}
                    </span>
                    <span className="hx-set__nav-label">{t(`next.settings.section.${id}`)}</span>
                    {changed.includes(id) && id !== section && (
                      <span
                        className="hx-set__nav-dot"
                        role="img"
                        aria-label={t('next.settingsChanges.dot')}
                      />
                    )}
                    <Icon name="chevron-right" size="sm" className="hx-set__nav-chev" />
                  </button>
                );
              })}
          </div>
        ))}
      </nav>
      <div className="hx-set__page" ref={page}>
        <div className="hx-set__page-in" data-sec={section}>
          <div className="hx-set__head">
            <button
              type="button"
              className="hx-head-btn hx-set__back"
              aria-label={t('next.settings.back')}
              onClick={onBack}
            >
              <Icon name="chevron-left" />
            </button>
            <div className="hx-set__titles">
              <h2 className="hx-set__title">{t(`next.settings.section.${section}`)}</h2>
              <p className="hx-set__lead">{lead}</p>
            </div>
          </div>
          {children}
        </div>
      </div>
      {island && (
        <div className="hx-set__isle" ref={isle}>
          {island}
        </div>
      )}
    </section>
  );
};

export default SettingsShell;
