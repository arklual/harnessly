import { useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { answerLogin, type ILoginAsk } from './login-api';
import '@/next/styles/browser-login.css';

const label = (key: string) => t(`next.agentBrowser.${key}`);
export function LoginBanner({ machine, ask }: { machine: MachineId; ask: ILoginAsk }) {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null);
  const answer = async (action: 'done' | 'cancel') => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await answerLogin(machine, ask.id, action);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="hx-browser-login" aria-label={label('loginTitle')}>
      <div>
        <strong>
          {label('loginSite')} {ask.url}
        </strong>
        <p>{ask.why}</p>
        <small>{label('loginPrivate')}</small>
      </div>
      <div className="hx-browser-login__actions">
        <button type="button" disabled={busy} onClick={() => void answer('cancel')}>
          {label('cancel')}
        </button>
        <button type="button" disabled={busy} onClick={() => void answer('done')}>
          {label('loginDone')}
        </button>
      </div>
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
