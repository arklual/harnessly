import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import '@/styles/base.css';
import '@/components/ui/ui.css';
import '@/styles/hl-tokens.css';
import '@/styles/hl.css';
import '@/styles/hl-links.css';
import '@/styles/hl-stats.css';
import '@/styles/hl-ui.css';
import '@/styles/hl-launch.css';
import '@/styles/hl-commands.css';
import '@/styles/hl-command-output.css';
import '@/styles/hl-economics.css';
import '@/styles/hl-preview.css';
import '@/styles/hl-preview-device.css';
import '@/styles/hl-pane.css';
import '@/styles/hl-devtools.css';
import '@/styles/hl-share.css';
import '@/styles/hl-sidebar.css';
import '@/styles/hl-design-library.css';
import '@/styles/hl-design-home.css';
import '@/styles/hl-notifications.css';
import '@/styles/hl-composer.css';
import '@/styles/hl-text-annotation.css';
import '@/styles/hl-side-chat.css';
import '@/styles/hl-docs.css';
import '@/styles/hl-checkpoints.css';
import '@/styles/hl-gallery.css';
import '@/styles/hl-timeline-items.css';
import '@xterm/xterm/css/xterm.css';

import { StrictMode, useEffect, useState, type ComponentType } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/ru';

import { ThemeProvider, useTheme } from '@/platform/theme';
import { locale } from '@/lib/i18n';
import { useRouter } from '@/platform/router';
import { appFetch } from '@/lib/app-path';
import { auth } from '@/runtime/endpoints';
import SignIn from '@/components/auth/sign-in';
import useWorkspaceStore, { type IWorkspaceInitialData } from '@/hooks/use-workspace-store';
import useConfigStore, { type IConfigInitialData } from '@/hooks/use-config-store';
import Workspace from '@/pages/workspace';
import PushBridge from '@/components/agents/push-bridge';
import { captureInstallPrompt } from '@/hooks/use-install-prompt';
import DocViewerPage from '@/pages/docs/[slug]';

const Docs = DocViewerPage as unknown as ComponentType<Record<string, unknown>>;
const Shell = Workspace as unknown as ComponentType<Record<string, unknown>>;

dayjs.extend(relativeTime);
dayjs.locale(locale());
document.documentElement.lang = locale();

type TBootstrap = Record<string, unknown>;

type TPhase =
  | { kind: 'checking' }
  | { kind: 'signed-out'; needsSetup: boolean; requiresAuth: boolean }
  | { kind: 'ready'; boot: TBootstrap };

/** Signs in when the runtime asks for it, then loads the page's start data (it answers 503 while the core starts). */
const useBootstrap = (): [TPhase, () => void] => {
  const [phase, setPhase] = useState<TPhase>({ kind: 'checking' });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let alive = true;
    let retry: number | undefined;
    const load = async () => {
      const res = await appFetch('/api/platform/bootstrap').catch(() => null);
      if (!alive) return;
      if (res?.status === 401) {
        const state = await auth.state().catch(() => ({ needsSetup: false, requiresAuth: false }));
        if (alive)
          setPhase({ kind: 'signed-out', needsSetup: state.needsSetup, requiresAuth: state.requiresAuth });
        return;
      }
      if (!res || res.status === 503) {
        retry = window.setTimeout(() => void load(), 1000);
        return;
      }
      const body = res.ok ? ((await res.json()) as TBootstrap) : {};
      useWorkspaceStore.getState().hydrate(body.initialWorkspace as IWorkspaceInitialData | undefined);
      useConfigStore.getState().hydrate(body.initialConfig as IConfigInitialData | undefined);
      if (alive) setPhase({ kind: 'ready', boot: body });
    };
    void load();
    return () => {
      alive = false;
      window.clearTimeout(retry);
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

const App = () => {
  const { pathname } = useRouter();
  const [phase, reload] = useBootstrap();
  if (phase.kind === 'checking') return null;
  if (phase.kind === 'signed-out') {
    return <SignIn needsSetup={phase.needsSetup} requiresAuth={phase.requiresAuth} onSignedIn={reload} />;
  }
  const boot = phase.boot;
  const props = ((boot.props as Record<string, unknown> | undefined) ?? boot) as Record<string, unknown>;
  return (
    <>
      {pathname === '/docs/[slug]' ? <Docs {...props} /> : <Shell {...props} />}
      <ThemedToaster />
      <PushBridge />
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
        <App />
      </ThemeProvider>
    </StrictMode>,
  );
}
