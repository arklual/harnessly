import { useEffect, useRef, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Modal from '@/next/features/shell/modal';
import { answerLogin, loadLogins, type ILoginAsk } from '@/next/features/browser/login-api';
import { chatPanes } from '@/next/stores/pane-state';
import { calmPoll } from '@/next/core/net/calm-poll';

/** The step while a login waits or the list moves, and the longest one while it is quiet. */
const LOGINS_POLL_MS = 3000;
const LOGINS_QUIET_MS = 15_000;

const label = (key: string) => t(`next.agentBrowser.${key}`);
export function openLoginPanel(machine: MachineId, ask: ILoginAsk) {
  chatPanes.update(ask.conv, machine, (pane) => {
    const existing = pane.tabs.find((tab) => tab.kind === 'agent-browser');
    const id = existing?.id ?? 'agent-browser-login';
    return {
      ...pane,
      open: true,
      active: id,
      tabs: existing ? pane.tabs : [...pane.tabs, { id, kind: 'agent-browser' }],
      unread: pane.unread.filter((tab) => tab !== id),
      view: { ...pane.view, 'browser-login-tab': ask.tab },
    };
  });
}

/** Global requests remain visible when another chat or Settings is on screen. */
export function BrowserLoginAskHost({
  machine,
  conv,
  onOpen,
}: {
  machine: MachineId | null;
  conv: string | null;
  onOpen: (conv: string) => void;
}) {
  const [read, setRead] = useState<{ machine: MachineId; asks: ILoginAsk[] } | null>(null);
  const [later, setLater] = useState<string[]>([]);
  const opened = useRef(new Set<string>());
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!machine) return;
    let gone = false;
    let last: string | null = null;
    // Read while the page is visible, slower while nothing waits.
    const stop = calmPoll({
      base: LOGINS_POLL_MS,
      max: LOGINS_QUIET_MS,
      wakeOn: 'browser-logins-changed',
      look: async () => {
        // A disconnected machine leaves its requests pending until the page reconnects (calmPoll keeps going).
        const asks = await loadLogins(machine);
        if (gone) return false;
        const said = JSON.stringify(asks);
        const changed = said !== last;
        last = said;
        if (changed) setRead({ machine, asks });
        return changed || asks.length > 0;
      },
    });
    return () => {
      gone = true;
      stop();
    };
  }, [machine]);
  const asks = read?.machine === machine ? read.asks : [];
  const own = asks.find((ask) => ask.conv === conv);
  useEffect(() => {
    if (!machine || !own) return;
    const key = `${machine}:${own.id}`;
    if (opened.current.has(key)) return;
    opened.current.add(key);
    openLoginPanel(machine, own);
  }, [machine, own]);
  const ask = asks.find((ask) => ask.conv !== conv && !later.includes(`${machine}:${ask.id}`));
  if (!machine || !ask) return null;
  const dismiss = () => {
    setLater((ids) => [...ids, `${machine}:${ask.id}`]);
    setError(null);
  };
  return (
    <Modal
      open
      boxClass="hx-browser-profiles"
      onClose={() => {
        if (!busy) dismiss();
      }}
      title={label('loginTitle')}
      lead={ask.title || ask.conv}
    >
      <div className="hx-modal__body">
        <p>
          {label('loginSite')} {ask.url}
        </p>
        <p className="hx-browser-profiles__why">{ask.why}</p>
        <p>{label('loginPrivate')}</p>
        {error && <p role="alert">{error}</p>}
      </div>
      <div className="hx-modal__foot">
        <button
          type="button"
          className="hx-btn"
          disabled={busy}
          onClick={() => {
            setBusy(true);
            setError(null);
            void answerLogin(machine, ask.id, 'cancel')
              .then(dismiss)
              .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
              .finally(() => setBusy(false));
          }}
        >
          {label('cancel')}
        </button>
        <button
          type="button"
          className="hx-btn"
          data-variant="primary"
          disabled={busy}
          onClick={() => {
            openLoginPanel(machine, ask);
            opened.current.add(`${machine}:${ask.id}`);
            onOpen(ask.conv);
            dismiss();
          }}
        >
          {label('loginOpen')}
        </button>
      </div>
    </Modal>
  );
}
