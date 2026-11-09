// The page of an app on made-up data, for the eye and for screenshots: ?dev=apppage, &state=installed|dev|catalog|waiting|
// loading, &theme=light|dark, &lang=en|ru. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import './apps-gallery.css';
import './apppage-gallery.css';

import { useEffect, useRef } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import { Button } from '@/next/ui/button';
import AccessList from '@/next/features/app-consent/access-list';
import AppPage from '@/next/features/app-main/app-page';
import { OwnSwitch } from '@/next/features/app-main/app-switches';
import type { IAccess, IInstalledApp } from '@/next/features/app-views/api';

const ACCESS: IAccess[] = [
  {
    key: 'perm:files.write:src/**',
    code: 'files.write:src/**',
    class: 'medium',
    label: { ru: 'Менять файлы в src/**', en: 'Change the files in src/**' },
  },
  {
    key: 'perm:chat.read',
    code: 'chat.read',
    class: 'low',
    label: { ru: 'Читать сообщения чата', en: 'Read the chat messages' },
  },
];

const APP = {
  id: 'a.tracker',
  name: 'Трекер задач',
  description: 'Задачи команды рядом с разговором: статус, исполнитель, переходы.',
  version: '1.4.2',
  trust: 'sandboxed',
  targets: ['pane'],
  permissions: [],
  views: [],
  mcpServers: [],
} satisfies IInstalledApp;

const Fake = () => (
  <div className="hx-gallery-apppage__fake" aria-hidden="true">
    <i />
    <i />
    <i />
    <i />
  </div>
);

const LABEL: Record<string, string> = {
  installed: 'Открыть',
  dev: 'Открыть',
  catalog: 'Поставить',
  waiting: 'Посмотреть доступы',
};

const first = (v: string | string[] | undefined): string | undefined => (Array.isArray(v) ? v[0] : v);

const AppPageGallery = () => {
  const { query: params } = useRouter();
  const dark = params.theme === 'dark';
  pinLocale(params.lang === 'en' ? 'en' : 'ru');
  const state = first(params.state) ?? 'installed';
  const portal = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  const catalog = state === 'catalog';
  const waiting = state === 'waiting';
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-gallery-apppage">
        <main className="hx-main">
          <AppPage
            loading={state === 'loading'}
            name={APP.name}
            icon="list-checks"
            line={APP.description}
            version={APP.version}
            by={catalog || state === 'installed' ? 'Анна' : undefined}
            note={waiting ? 'Ждёт вашего согласия' : null}
            actions={<Button variant="primary">{LABEL[state] ?? LABEL.installed}</Button>}
            preview={catalog || waiting ? null : <Fake />}
            can={
              catalog
                ? []
                : [
                    'Сводка по задачам',
                    '/sync — Обновить из трекера',
                    'Создать задачу',
                    'Находит задачи по «@»',
                  ]
            }
            rights={<AccessList access={ACCESS} />}
            manage={catalog ? null : <OwnSwitch app={APP} onSwitch={async () => {}} />}
            journal={state === 'dev' ? <p>Записи журнала</p> : null}
            onBack={() => {}}
            onOpenDrawer={() => {}}
          />
        </main>
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default AppPageGallery;
