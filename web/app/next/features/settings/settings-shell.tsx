// The Settings screen's frame (the design's .hx-set): the sections on the left, the open one on the right. On a phone
// the list is the screen and the open section slides over it from the right, with a way back.
import '@/next/styles/settings.css';

import { useEffect, useRef, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import { SETTINGS_SECTIONS, type SettingsSection } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';

/** What a section's entry in the list draws: a glyph of the icon set, or the logo of an agent. */
const SECTION_MARK: Readonly<Record<SettingsSection, { icon: string } | { agent: 'claude' | 'codex' }>> = {
  general: { icon: 'sliders-horizontal' },
  notifications: { icon: 'bell' },
  safe: { icon: 'lock' },
  accounts: { icon: 'key-round' },
  claude: { agent: 'claude' },
  codex: { agent: 'codex' },
  skills: { icon: 'blocks' },
  machines: { icon: 'server' },
  apps: { icon: 'layout-grid' },
  beta: { icon: 'sparkles' },
};

interface ISettingsShellProps {
  /** The section on the page. */
  section: SettingsSection;
  /** On a phone: the page is over the list (an address with a section), or the list is shown; a wide window shows both. */
  pageOpen: boolean;
  /** The line under the section's title; the binding knows which machine and file it is about. */
  lead: string;
  onPick: (section: SettingsSection) => void;
  onBack: () => void;
  onOpenDrawer: () => void;
  children: ReactNode;
}

const SettingsShell = ({
  section,
  pageOpen,
  lead,
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
            <Icon name="menu" />
          </button>
          <span className="hx-set__h1">{t('next.settings.title')}</span>
        </div>
        {SETTINGS_SECTIONS.map((id, index) => {
          const mark = SECTION_MARK[id];
          return (
            <button
              key={id}
              type="button"
              className="hx-set__nav-item"
              aria-current={id === section ? true : undefined}
              ref={(element) => element?.style.setProperty('--i', String(index))}
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
