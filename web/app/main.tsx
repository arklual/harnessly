import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import '@/styles/base.css';
import '@/components/ui/ui.css';
import '@/styles/hl-tokens.css';
import '@/styles/hl-global.css';
import '@/styles/hl-notifications.css';

import { createElement, lazy, StrictMode, Suspense, useEffect, useState, type ComponentType } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/ru';

import { ThemeProvider, useTheme } from '@/platform/theme';
import { locale } from '@/lib/i18n';
import { useRouter } from '@/platform/router';
import { appFetch } from '@/lib/app-path';
import { loadBootstrap } from '@/lib/boot-start';
import { appConfig } from '@/platform/config';
import { auth } from '@/runtime/endpoints';
import useWorkspaceStore, { type IWorkspaceInitialData } from '@/hooks/use-workspace-store';
import useConfigStore, { type IConfigInitialData } from '@/hooks/use-config-store';
import PushBridge from '@/components/agents/push-bridge';
import { captureInstallPrompt } from '@/hooks/use-install-prompt';

// The sign-in and the docs page are drawn with the old stylesheets, which come with them (styles/legacy.ts).
const SignIn = lazy(() => import('@/styles/legacy').then(() => import('@/components/auth/sign-in')));
const Docs = lazy(() =>
  import('@/styles/legacy').then(() => import('@/pages/docs/[slug]')),
) as unknown as ComponentType<Record<string, unknown>>;
// The interface is its own chunk. It is fetched together with the start data rather than after it, which on a phone
// saves the round trips of a whole batch of files; a person who turns out to be signed out has it in the cache.
const loadShell = () => import('@/next/entry');
const NextShell = lazy(loadShell);
// The interface's states on mock data (?dev=<name> opens next/dev/<name>-gallery.tsx).
const GALLERIES: Record<string, ComponentType> = Object.fromEntries(
  Object.entries(import.meta.glob<{ default: ComponentType }>('./next/dev/*-gallery.tsx')).map(
    ([path, load]) => [path.replace(/^.*\/([^/]+)-gallery\.tsx$/, '$1'), lazy(load)],
  ),
);

dayjs.extend(relativeTime);
dayjs.locale(locale());
document.documentElement.lang = locale();

type TBootstrap = Record<string, unknown>;

type TPhase =
  | { kind: 'checking' }
  | { kind: 'signed-out'; needsSetup: boolean; requiresAuth: boolean }
  | { kind: 'ready'; boot: TBootstrap };

/**
 * A machine of a hub has no password to offer: the hub signs the person in and hands the machine's page a session. When
 * that session is gone, the hub is where to go (and back here after). False when the hub was sent the person a moment
 * ago and the page is back again, so the machine's own sign-in shows instead of a loop.
 */
const toHubSignIn = (hubUrl: string): boolean => {
  try {
    if (Date.now() - Number(sessionStorage.getItem('hl-hub-signin') ?? 0) < 15_000) return false;
    sessionStorage.setItem('hl-hub-signin', String(Date.now()));
  } catch {
    // No storage: go anyway, once per page load.
  }
  window.location.assign(`${hubUrl}/signin?next=${encodeURIComponent(location.pathname + location.search)}`);
  return true;
};

/**
 * Signs in when the runtime asks for it, then loads the page's start data (it answers 503 while the core starts). A
 * home machine that is slow to answer does not hold the page: it draws with what the hub knows and takes the start
 * data when it comes (lib/boot-start.ts).
 */
const useBootstrap = (): [TPhase, () => void] => {
  const [phase, setPhase] = useState<TPhase>({ kind: 'checking' });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let alive = true;
    const stop = loadBootstrap((signal) => appFetch('/api/platform/bootstrap', { signal }), {
      onData: (body, meanwhile) => {
        if (!meanwhile) {
          useWorkspaceStore.getState().hydrate(body.initialWorkspace as IWorkspaceInitialData | undefined);
          useConfigStore.getState().hydrate(body.initialConfig as IConfigInitialData | undefined);
        }
        setPhase({ kind: 'ready', boot: body });
      },
      onSignedOut: (res) => {
        // The hub answers a request without its session with a redirect to its sign-in page.
        const hubUrl = appConfig().hubUrl;
        if (hubUrl && toHubSignIn(hubUrl)) return;
        if (res.status !== 401) return;
        void auth
          .state()
          .catch(() => ({ needsSetup: false, requiresAuth: false }))
          .then((state) => {
            if (alive)
              setPhase({
                kind: 'signed-out',
                needsSetup: state.needsSetup,
                requiresAuth: state.requiresAuth,
              });
          });
      },
    });
    return () => {
      alive = false;
      stop();
    };
  }, [attempt]);
  return [phase, () => setAttempt((n) => n + 1)];
};

const ThemedToaster = () => {
  const { resolvedTheme } = useTheme();
  return (
    <Toaster
      position="top-right"
      theme={resolvedTheme}
      className="tw-toaster"
      closeButton
      visibleToasts={4}
      expand
      offset={{ top: 14, right: 16, bottom: 24, left: 16 }}
    />
  );
};

/** The development gallery needs no runtime, so it comes before signing in and loading. */
const Entry = () => {
  const { query } = useRouter();
  const gallery = typeof query.dev === 'string' ? GALLERIES[query.dev] : undefined;
  if (gallery) return <Suspense fallback={null}>{createElement(gallery)}</Suspense>;
  return <App />;
};

const App = () => {
  const { pathname } = useRouter();
  const [phase, reload] = useBootstrap();
  useEffect(() => {
    if (pathname !== '/docs/[slug]') void loadShell();
  }, [pathname]);
  if (phase.kind === 'checking') return null;
  if (phase.kind === 'signed-out') {
    return (
      <Suspense fallback={null}>
        <SignIn needsSetup={phase.needsSetup} requiresAuth={phase.requiresAuth} onSignedIn={reload} />
      </Suspense>
    );
  }
  const boot = phase.boot;
  const props = ((boot.props as Record<string, unknown> | undefined) ?? boot) as Record<string, unknown>;
  return (
    <>
      {pathname === '/docs/[slug]' ? (
        <Suspense fallback={null}>
          <Docs {...props} />
        </Suspense>
      ) : (
        <Suspense fallback={null}>
          <NextShell owner={typeof props.owner === 'string' ? props.owner : ''} />
        </Suspense>
      )}
      <ThemedToaster />
      {/* The documentation page opens no chat: a push's click and the "looking at the app" signal belong to the shell. */}
      {pathname !== '/docs/[slug]' && <PushBridge />}
    </>
  );
};

// The browser offers the install once and early, long before anyone opens the settings.
captureInstallPrompt();

const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <ThemeProvider>
        <Entry />
      </ThemeProvider>
    </StrictMode>,
  );
}
