// The page of an app, /apps/:id/about: what it is, what it can do, what it is allowed, and the one button for the next
// step («Открыть» or «Поставить»). The name is the largest thing; the preview is the app itself running, when it can.
// A dev install has a second tab, «Журнал». The layout is here; what fills it (the frame, the rights, the switches)
// comes from the binding.
import { useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { Skeleton } from '@/next/ui/skeleton';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';
import '@/next/styles/app-page.css';

const MAX_ABILITIES = 8;

/** What an installed app can do, in the words it gave itself: its screens, commands, buttons and the «@» search. */
export const abilitiesOf = (
  app: Pick<IInstalledApp, 'views' | 'commands' | 'buttons' | 'mentions'>,
): string[] => {
  const lines: string[] = [];
  for (const view of app.views) {
    const title = view.title ?? view.label;
    if (title) lines.push(title);
  }
  for (const command of app.commands ?? []) {
    lines.push(command.description ? `/${command.name} — ${command.description}` : `/${command.name}`);
  }
  for (const button of app.buttons ?? []) lines.push(button.title);
  if (app.mentions) lines.push(t('next.apppage.mentions', { title: app.mentions.title }));
  return [...new Set(lines)].slice(0, MAX_ABILITIES);
};

export interface IAppPageProps {
  /** The page waits for the machine: skeletons. */
  loading?: boolean;
  /** The id is nowhere: not installed and in no catalog. */
  missing?: boolean;
  name: string;
  icon?: string;
  /** What the app does, in a line or two. */
  line?: string;
  version?: string;
  /** Who made it; absent when the machine does not say. */
  by?: string;
  /** What the person has to do before the app works; null when nothing. */
  note?: string | null;
  /** The buttons: «Открыть», «Поставить», the review of what it asks. */
  actions: ReactNode;
  /** The app running, for a look; null when it cannot run now. */
  preview: ReactNode;
  can: readonly string[];
  /** «Что ему можно»: the rights (the access list); null hides the section (the machine's permissions are off). */
  rights: ReactNode;
  /** The switches of an app the person can turn off or on. */
  manage: ReactNode;
  /** The journal of a dev install; null for any other, and then the page has no tabs. */
  journal: ReactNode;
  onBack: () => void;
  onOpenDrawer: () => void;
}

const TAB = { about: 'about', journal: 'journal' } as const;
type TTab = (typeof TAB)[keyof typeof TAB];

const AppPage = (props: IAppPageProps) => {
  const [tab, setTab] = useState<TTab>(TAB.about);
  const hasMain = props.preview !== null || props.can.length > 0;
  const shown = props.journal !== null ? tab : TAB.about;
  let body: ReactNode;
  if (props.loading) {
    body = (
      <div className="hx-apppage__hero" aria-busy="true">
        <Skeleton className="hx-apppage__skel-tile" />
        <div className="hx-apppage__skel-text">
          <Skeleton className="hx-apppage__skel-name" />
          <Skeleton className="hx-apppage__skel-line" />
        </div>
      </div>
    );
  } else if (props.missing) {
    body = <p className="hx-apppage__none">{t('next.apppage.missing')}</p>;
  } else {
    body = (
      <>
        <div className="hx-apppage__hero">
          <span className="hx-apppage__tile" aria-hidden="true">
            <Icon name={props.icon || 'layout-grid'} />
          </span>
          <div className="hx-apppage__titles">
            <h1 className="hx-apppage__name">{props.name}</h1>
            {props.line && <p className="hx-apppage__line">{props.line}</p>}
            {props.note && <p className="hx-apppage__note">{props.note}</p>}
            <div className="hx-apppage__actions">{props.actions}</div>
          </div>
        </div>
        {props.journal !== null && (
          <div className="hx-apppage__tabs" role="tablist" aria-label={props.name}>
            {[TAB.about, TAB.journal].map((id) => (
              <button
                key={id}
                type="button"
                role="tab"
                className="hx-apppage__tab"
                aria-selected={shown === id}
                onClick={() => setTab(id)}
              >
                {t(`next.apppage.tab.${id}`)}
              </button>
            ))}
          </div>
        )}
        {shown === TAB.journal ? (
          <section className="hx-apppage__sec" aria-label={t('next.journal.title')}>
            {props.journal}
          </section>
        ) : (
          <div className="hx-apppage__cols" data-solo={!hasMain || undefined}>
            {hasMain && (
              <div className="hx-apppage__main">
                {props.can.length > 0 && (
                  <section className="hx-apppage__sec">
                    <h2 className="hx-apppage__h2">{t('next.apppage.can')}</h2>
                    <ul className="hx-apppage__can">
                      {props.can.map((line) => (
                        <li key={line}>{line}</li>
                      ))}
                    </ul>
                  </section>
                )}
                {props.preview !== null && (
                  <section className="hx-apppage__preview" aria-label={t('next.apppage.preview')}>
                    {props.preview}
                  </section>
                )}
              </div>
            )}
            <div className="hx-apppage__side">
              {props.rights !== null && (
                <section className="hx-apppage__sec">
                  <h2 className="hx-apppage__h2">{t('next.consent.access')}</h2>
                  {props.rights}
                </section>
              )}
              {props.manage !== null && <section className="hx-apppage__sec">{props.manage}</section>}
              {(props.version || props.by) && (
                <p className="hx-apppage__meta">
                  {[
                    props.version ? t('next.appslist.version', { version: props.version }) : null,
                    props.by ? t('next.apppage.by', { by: props.by }) : null,
                  ]
                    .filter((x) => x !== null)
                    .join(' · ')}
                </p>
              )}
            </div>
          </div>
        )}
      </>
    );
  }
  return (
    <div className="hx-apppage">
      <div className="hx-apppage__bar">
        <button
          type="button"
          className="hx-head-btn hx-appmain__menu"
          aria-label={t('next.header.openMenu')}
          onClick={props.onOpenDrawer}
        >
          <Icon name="menu" />
        </button>
        <button type="button" className="hx-apppage__back" onClick={props.onBack}>
          <Icon name="arrow-left" size="sm" />
          {t('next.sidebar.apps')}
        </button>
      </div>
      {body}
    </div>
  );
};

export default AppPage;
