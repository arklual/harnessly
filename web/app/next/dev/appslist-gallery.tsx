// The «Приложения» list on made-up data, for the eye and for screenshots: ?dev=appslist, &state=full|empty|loading,
// &theme=light|dark, &lang=en|ru, &q=<search>. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/sidebar.css';
import './apps-gallery.css';

import { useEffect, useRef, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import AppsList from '@/next/features/app-main/apps-list';
import AppsCatalog from '@/next/features/app-catalog/apps-catalog';
import PublicCatalog from '@/next/features/app-catalog/public-catalog';
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { ITeamState } from '@/next/features/app-catalog/use-team';
import type { IPublicState } from '@/next/features/app-catalog/use-public';

const app = (over: Partial<IInstalledApp> & Pick<IInstalledApp, 'id' | 'name'>): IInstalledApp => ({
  description: '',
  version: '1.0.0',
  trust: 'sandboxed',
  targets: ['pane'],
  permissions: [],
  views: [],
  mcpServers: [],
  ...over,
});

const MINE: IInstalledApp[] = [
  app({
    id: 'a.notes',
    name: 'Заметки',
    icon: 'file-text',
    description: 'Короткие заметки к чату, которые агент видит и дополняет.',
    targets: ['chat', 'pane'],
  }),
  app({
    id: 'a.tracker',
    name: 'Трекер задач',
    icon: 'list-checks',
    description: 'Задачи команды рядом с разговором: статус, исполнитель, переходы.',
    targets: ['pane', 'sinopia.tasks'],
  }),
  app({
    id: 'a.gitlab',
    name: 'Merge requests',
    icon: 'git-pull-request',
    description: 'Открытые запросы на слияние и состояние их проверок.',
    targets: ['chat', 'pane'],
    state: 'waiting',
    reason: 'connection',
    detail: 'GitLab',
  } as Partial<IInstalledApp> & Pick<IInstalledApp, 'id' | 'name'>),
  app({
    id: 'a.slides',
    name: 'Слайды',
    icon: 'presentation',
    description: 'Редактор презентаций в Sinopia с шаблонами команды.',
    targets: ['sinopia.slides', 'app'],
    consent: true,
  }),
  app({
    id: 'a.reports',
    name: 'Отчёты',
    icon: 'chart-column',
    description: 'Сводки по продажам и расходам, обновляются по расписанию.',
    targets: ['pane'],
    off: true,
  }),
];

const CATALOG = [
  ['a.calendar', 'Календарь', 'Встречи и свободные окна, чтобы агент не назначал поверх.'],
  ['a.wiki', 'База знаний', 'Поиск по страницам вики прямо из чата.'],
  ['a.mail', 'Почта', 'Черновики писем и ответы на входящие.'],
] as const;

const team = (): ITeamState => ({
  info: {
    space: 'team',
    role: 'owner',
    muted: [],
    catalog: CATALOG.map(([id, name, description]) => ({
      id,
      channel: 'stable',
      version: '1.0.0',
      name,
      description,
      trust: 'sandboxed',
      permissions: [],
      by: 'ann',
      installed: false,
    })),
  },
  error: null,
  busy: null,
  failure: null,
  act: async () => {},
});

const publicState = (): IPublicState => ({
  entries: [
    ['p.weather', 'Погода', 'Прогноз на неделю для командировок.'],
    ['p.translate', 'Переводчик', 'Перевод выделенного текста с сохранением терминов.'],
    ['p.chess', 'Шахматы', 'Партия с агентом на доске в панели.'],
  ].map(([id, name, description]) => ({
    id: id ?? '',
    version: '1.0.0',
    name: name ?? '',
    description: description ?? '',
    trust: 'sandboxed',
    permissions: [],
    sum: 'x',
    by: 'harnessly',
  })),
  query: '',
  setQuery: () => {},
  error: null,
  busy: null,
  failure: null,
  install: async () => {},
});

const AppsListGallery = () => {
  const { query: params } = useRouter();
  const dark = params.theme === 'dark';
  pinLocale(params.lang === 'en' ? 'en' : 'ru');
  const first = (v: string | string[] | undefined): string | undefined => (Array.isArray(v) ? v[0] : v);
  const state = first(params.state) ?? 'full';
  const [q, setQ] = useState(first(params.q) ?? '');
  const portal = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  const empty = state === 'empty' && q === '';
  const apps = { loading: null, empty: [] as IInstalledApp[], full: MINE }[
    state === 'loading' || state === 'empty' ? state : 'full'
  ];
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-gallery-apps">
        <main className="hx-main">
          <AppsList
            apps={apps}
            error={null}
            query={q}
            onQuery={setQ}
            onCreate={() => {}}
            onOpen={() => {}}
            onOpenDrawer={() => {}}
            onHelp={() => {}}
            safeMode={false}
            onSafeMode={async () => {}}
            footer={
              state === 'loading' ? null : (
                <>
                  <AppsCatalog team={team()} query={q} limit={empty ? 3 : undefined} />
                  <PublicCatalog catalog={publicState()} installed={[]} limit={empty ? 3 : undefined} />
                </>
              )
            }
          />
        </main>
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default AppsListGallery;
