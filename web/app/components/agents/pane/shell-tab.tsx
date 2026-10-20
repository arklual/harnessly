import { useCallback, useEffect, useState } from 'react';
import { RotateCw } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import { TerminalView } from '@/terminal/terminal-view';
import { appFetch } from '@/lib/app-path';
import { t } from '@/lib/i18n';

export const closePaneShell = (tabId: string): void => {
  void appFetch(`/api/platform/pane-shell?tabId=${encodeURIComponent(tabId)}`, { method: 'DELETE' }).catch(
    () => {},
  );
};

const startShell = async (tabId: string): Promise<string> => {
  const response = await appFetch('/api/platform/pane-shell', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tabId }),
  });
  const body = (await response.json().catch(() => null)) as { sessionName?: string; error?: string } | null;
  if (!response.ok || !body?.sessionName)
    throw new Error(body?.error ?? t('pane.shellTab.failedStatus', { status: response.status }));
  return body.sessionName;
};

/**
 * The terminal tab of the right panel: a shell of its own in the conversation's folder (like Codex), not the
 * agent's session, drawn by the page's terminal view.
 */
const ShellTab = ({ tabId, active }: { tabId: string; active: boolean }) => {
  const [sessionName, setSessionName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    startShell(tabId).then(
      (name) => {
        if (alive) setSessionName(name);
      },
      (failure: unknown) => {
        if (alive) setError(failure instanceof Error ? failure.message : t('pane.shellTab.failed'));
      },
    );
    return () => {
      alive = false;
    };
  }, [tabId, attempt]);

  const retry = useCallback(() => {
    setError(null);
    setSessionName(null);
    setAttempt((n) => n + 1);
  }, []);

  return (
    <div className="tw-rpane-shell">
      {sessionName && (
        <TerminalView
          key={sessionName}
          session={sessionName}
          focus={active}
          onRestart={() => startShell(tabId).then(setSessionName)}
        />
      )}
      {error && (
        <div className="tw-rpane-shell-note" role="status">
          <span>{error}</span>
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            icon={<RotateCw />}
            onClick={retry}
          >
            {t('pane.shellTab.restart')}
          </TwButton>
        </div>
      )}
    </div>
  );
};

export default ShellTab;
