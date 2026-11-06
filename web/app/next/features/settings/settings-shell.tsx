// The Settings screen's frame (the design's .hx-set): the sections on the left in three groups (the app, the agents,
// the machine), the open one on the right. On a phone the list is the screen and the open section slides over it from
// the right, with a way back; the sidebar opens from the list's head with the same glyph as the sidebar's own toggle.
import '@/next/styles/settings.css';

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
  claude: { agent: 'claude' },
  codex: { agent: 'codex' },
  skills: { icon: 'blocks' },
  machines: { icon: 'server' },
  apps: { icon: 'layout-grid' },
  connections: { icon: 'plug' },
  design: { icon: 'pen-tool' },
  extensions: { icon: 'blocks' },
  beta: { icon: 'sparkles' },
};

/** The groups of the list, in the order of SETTINGS_SECTIONS. */
const NAV_GROUPS = [
  { id: 'app', sections: ['general', 'appearance', 'keys', 'notifications', 'voice', 'takes'] },
  { id: 'agents', sections: ['accounts', 'claude', 'codex', 'skills', 'safe', 'browser'] },
  { id: 'machine', sections: ['machines', 'apps', 'connections', 'design', 'extensions', 'beta'] },
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
  onPick: (section: SettingsSection) => void;
  onBack: () => void;
  onOpenDrawer: () => void;
  children: ReactNode;
}

const SettingsShell = ({
  section,
  pageOpen,
  lead,
  hidden = [],
  onPick,
  onBack,
  onOpenDrawer,
  children,
}: ISettingsShellProps) => {
  const page = useRef<HTMLDivElement>(null);
  // A section opens from its top.
  useEffect(() => {
    page.current?.scrollTo({ top: 0 });
  }, [section]);
  return (
    <section className="hx-set" data-page={pageOpen}>
      <nav className="hx-set__nav" aria-label={t('next.settings.nav')}>
        <div className="hx-set__nav-head">
          <button
            type="button"
            className="hx-head-btn hx-set__menu"
            aria-label={t('next.settings.menu')}
            onClick={onOpenDrawer}
          >
            <Icon name="panel-left" />
          </button>
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
    </section>
  );
};

export default SettingsShell;
