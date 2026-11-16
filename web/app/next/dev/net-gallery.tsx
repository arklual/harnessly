// The pills at the top of the window (.hx-net) on made-up states, for the eye and for screenshots: ?dev=net.
// &s=lost (no connection, a countdown and «Переподключиться»), machine (the machine away), stale, flip (stale turning
// into a countdown as the pill comes in), retry («Подключаемся»), back («Снова на связи»), update («Вышла новая
// версия»), rollout («Идёт обновление»), both (no connection and a new version at once); &tabs=1 puts the window's tab strip over it; &theme=light|dark&lang=en|ru the look.
// Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/tabs.css';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import ConnectionBanner from '@/next/features/connection/banner';
import UpdateBanner from '@/next/features/update/update-banner';
import RolloutBanner from '@/next/features/rollout/rollout-banner';
import { ROLLOUT, rolloutStore } from '@/next/features/rollout/rollout';
import type { Connection, Ms } from '@/next/protocol/v2';

const TABS = ['Новый лендинг', 'Ревью «Поделиться»', 'Отчёт за неделю'] as const;

const connectionOf = (s: string, at: number): Connection => {
  if (s === 'stale' || s === 'flip') return { k: 'stale', since: at as Ms };
  if (s === 'machine') return { k: 'offline', cause: 'machine-unreachable', retryAt: (at + 18_000) as Ms };
  return { k: 'offline', cause: 'network', retryAt: (at + 18_000) as Ms };
};

const NetGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const s = typeof query.s === 'string' ? query.s : 'lost';
  const tabs = query.tabs === '1';
  const portalRef = useRef<HTMLDivElement>(null);
  const [at] = useState(() => Date.now());
  // «Подключаемся» and «Снова на связи» come after a loss: the gallery loses the link first, then brings it back.
  const [connection, setConnection] = useState<Connection>(() => connectionOf(s, at));
  // flip: «связь нестабильна» turns into «нет связи · через N с» while the pill is still coming in — the moment its
  // width used to be measured scaled down and stayed so, the button cut in half.
  useEffect(() => {
    if (s !== 'flip') return;
    const timer = setTimeout(() => setConnection(connectionOf('lost', Date.now())), 120);
    return () => clearTimeout(timer);
  }, [s]);
  useEffect(() => {
    if (s !== 'retry' && s !== 'back') return;
    const timers = [
      setTimeout(() => setConnection({ k: 'connecting', attempt: 1 }), 900),
      s === 'back' ? setTimeout(() => setConnection({ k: 'live', since: Date.now() as Ms }), 2000) : 0,
    ];
    return () => timers.forEach((timer) => clearTimeout(timer));
  }, [s]);
  useEffect(() => {
    rolloutStore.set(s === 'rollout' ? ROLLOUT.active : ROLLOUT.idle);
  }, [s]);

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);

  const lost = s !== 'update' && s !== 'rollout';
  // The pills mount once the portal is there, as in the app, where the frame is up before them.
  const [mounted, setMounted] = useState(false);
  const portal = useCallback((node: HTMLDivElement | null) => {
    portalRef.current = node;
    setMounted(node !== null);
  }, []);
  return (
    <PortalContext value={portalRef}>
      <div className="hx-root hx-app" data-tabs={tabs ? 'on' : undefined}>
        {tabs && (
          <div className="hx-app__tabs">
            <div className="hx-gtabs">
              <div className="hx-gtabs__list" role="tablist">
                {TABS.map((title, n) => (
                  <div key={title} className="hx-gtab" role="tab" aria-selected={n === 1}>
                    <span className="hx-gtab__name">{title}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
        <main className="hx-main">
          <div className="hx-main__body" />
        </main>
        <div ref={portal} className="hx-portal" />
        {mounted && lost && (
          <ConnectionBanner
            connection={connection}
            machineName="runner-2"
            onRetryNow={() => undefined}
            graceMs={0}
          />
        )}
        {mounted && (
          <UpdateBanner
            available={s === 'update' || s === 'both'}
            onReload={() => undefined}
            onDismiss={() => undefined}
          />
        )}
        {mounted && s === 'rollout' && <RolloutBanner />}
      </div>
    </PortalContext>
  );
};

export default NetGallery;
