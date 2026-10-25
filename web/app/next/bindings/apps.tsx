// The Apps screen on the machine's feed: asked when the screen opens and every minute while the page is on screen, the
// last good answer kept through a stumble. The picked rows are the page's; «To chat» puts them into the start page's
// field as mentions and opens it. Which app is open is the person's choice, kept for the next visit.
import { useCallback, useEffect, useRef, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { loadApps } from '@/next/features/apps/api';
import AppsView from '@/next/features/apps/apps-view';
import { APP_REGISTRY, appsOf, mentionOf } from '@/next/features/apps/model';
import { setPrefs, usePrefs } from '@/next/features/apps/prefs';
import { t } from '@/next/i18n';
import type { AppId, AppRow, AppsFeed, MachineId } from '@/next/protocol/v2';
import { goTo, HOME, ROUTE } from '@/next/routes/routes';
import { hasFeature } from '@/next/stores/machines';
import { NEW_CHAT } from '@/next/stores/drafts';
import { setMentions } from '@/next/stores/mentions';

/** How often the feed is asked again while the screen is open. */
export const APPS_POLL_MS = 60_000;
/** A failed ask is told to the person after this many in a row (the first load tells it at once). */
const FAILS_BEFORE_TOLD = 2;
/** The «2 hours ago» of the rows move on with this clock. */
const CLOCK_MS = 60_000;

interface IAppsScreenProps {
  machine: MachineId;
  onOpenDrawer: () => void;
}

/** The app that is open: the one the person chose if the machine has it, else the first the machine lists. */
const openApp = (feed: AppsFeed | null, chosen: AppId | null): AppId => {
  const known = feed ? appsOf(feed) : [];
  return known.find((item) => item.id === chosen)?.id ?? known[0]?.id ?? chosen ?? APP_REGISTRY[0].id;
};

const AppsScreen = ({ machine, onOpenDrawer }: IAppsScreenProps) => {
  const supported = hasFeature(machine, 'apps-hub');
  const prefs = usePrefs();
  const [feed, setFeed] = useState<AppsFeed | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [pageOpen, setPageOpen] = useState(false);
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const [now, setNow] = useState(() => Date.now());
  const loaded = useRef(false);
  const app = openApp(feed, prefs.app);

  useEffect(() => {
    if (!supported) return;
    let live = true;
    let fails = 0;
    let timer: number | undefined;
    const schedule = () => {
      timer = window.setTimeout(() => void ask(), APPS_POLL_MS);
    };
    const ask = async () => {
      window.clearTimeout(timer);
      if (document.hidden) {
        schedule();
        return;
      }
      setRefreshing(true);
      const res = await loadApps(machine);
      if (!live) return;
      setRefreshing(false);
      if ('ok' in res) {
        fails = 0;
        loaded.current = true;
        setFeed(res.value);
        setError(null);
        setNow(Date.now());
      } else {
        fails += 1;
        if (!loaded.current || fails >= FAILS_BEFORE_TOLD) setError(pageText(res));
      }
      schedule();
    };
    const resume = () => {
      if (!document.hidden) void ask();
    };
    void ask();
    document.addEventListener('visibilitychange', resume);
    return () => {
      live = false;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', resume);
    };
  }, [machine, supported, attempt]);

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), CLOCK_MS);
    return () => clearInterval(timer);
  }, []);

  const toggle = useCallback(
    (key: string) =>
      setPicked((current) => {
        const next = new Set(current);
        if (!next.delete(key)) next.add(key);
        return next;
      }),
    [],
  );

  const pickApp = (id: AppId) => {
    if (id !== app) setPicked(new Set());
    setPrefs({ app: id });
    setPageOpen(true);
  };

  const toChat = (rows: AppRow[]) => {
    setMentions(
      NEW_CHAT,
      rows.map((row) => mentionOf(app, row)),
    );
    goTo(HOME);
  };

  return (
    <AppsView
      feed={feed}
      error={supported ? error : t('next.apps.unsupported')}
      refreshing={refreshing}
      app={app}
      onApp={pickApp}
      pageOpen={pageOpen}
      onBack={() => setPageOpen(false)}
      picked={picked}
      onToggle={toggle}
      onClear={() => setPicked(new Set())}
      onChat={toChat}
      onRetry={() => {
        setError(null);
        setAttempt((n) => n + 1);
      }}
      onOpenDrawer={onOpenDrawer}
      onSettings={() => goTo({ kind: ROUTE.settings, section: 'apps' })}
      now={now}
    />
  );
};

export default AppsScreen;
