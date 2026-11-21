// Signing an agent in on the machine from its card: the machine starts the provider's sign-in, the card shows
// its link and, when the provider shows a code after the sign-in, a field for it. The credentials stay on the machine:
// the hub relays the link, the code and the states. The states come on the tenant's stream as `login` events; the card
// is also read again while the sign-in waits, so «вход выполнен» shows without reloading the page even without the stream.
import { useEffect, useRef, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type { AgentLogin, MachineAgent } from '../../../org/api/machines';
import { useOrg } from '../../../org/context';
import { dataMode } from '../../../org/mode';
import { Btn } from '../../ui';

const POLL_MS = 4_000;
const FINAL = new Set<AgentLogin['state']>(['done', 'failed', 'expired', 'cancelled']);

/** The provider's own sign-in hosts: a link anywhere else is not shown, whatever the machine says. */
const HOSTS: Record<MachineAgent['provider'], readonly string[]> = {
  claude: ['claude.ai', 'console.anthropic.com', 'platform.claude.com'],
  codex: ['auth.openai.com'],
};
const VENDOR: Record<MachineAgent['provider'], string> = { claude: 'Anthropic', codex: 'OpenAI' };

export const safeLoginUrl = (provider: MachineAgent['provider'], raw: string | null): string | null => {
  if (!raw) return null;
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.port !== '' || url.username !== '' || url.password !== '') return null;
  return HOSTS[provider].includes(url.hostname) ? url.href : null;
};

/** Follows the tenant's stream for the states of one sign-in, in the live hub only. */
const useLoginEvents = (
  tenantId: string,
  machineId: string,
  loginId: string | null,
  on: (l: AgentLogin) => void,
) => {
  const cb = useRef(on);
  useEffect(() => {
    cb.current = on;
  });
  useEffect(() => {
    if (!loginId || dataMode() !== 'live' || typeof EventSource === 'undefined') return;
    const es = new EventSource(`/api/spaces/${encodeURIComponent(tenantId)}/events`, {
      withCredentials: true,
    });
    es.addEventListener('login', (e) => {
      try {
        const data = JSON.parse((e as MessageEvent<string>).data) as {
          machineId?: string;
          login?: AgentLogin;
        };
        if (data.machineId === machineId && data.login?.id === loginId) cb.current(data.login);
      } catch {
        // a frame that does not read is skipped: the card is read again anyway
      }
    });
    return () => es.close();
  }, [tenantId, machineId, loginId]);
};

export const AgentSignIn = ({
  machineId,
  tenantId,
  provider,
  onPoll,
}: {
  machineId: string;
  tenantId: string;
  provider: MachineAgent['provider'];
  /** Reads the card again: once the agent reports it is signed in, the card stops showing this. */
  onPoll: () => void;
}) => {
  const { api } = useOrg();
  const [login, setLogin] = useState<AgentLogin | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [sent, setSent] = useState(false);
  const active = login !== null && !FINAL.has(login.state);

  useLoginEvents(tenantId, machineId, active ? login.id : null, (l) =>
    setLogin((cur) => (cur ? { ...cur, ...l } : l)),
  );

  const poll = useRef(onPoll);
  useEffect(() => {
    poll.current = onPoll;
  });
  useEffect(() => {
    if (!active && login?.state !== 'done') return;
    const t = setInterval(() => poll.current(), POLL_MS);
    return () => clearInterval(t);
  }, [active, login?.state]);

  const start = async () => {
    setBusy(true);
    setError(null);
    setSent(false);
    setCode('');
    try {
      setLogin(await api.startAgentLogin(machineId, provider));
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(false);
    }
  };
  const sendCode = async () => {
    if (!login || !code.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await api.sendLoginCode(machineId, login.id, code.trim());
      setSent(true);
      setCode('');
      poll.current();
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(false);
    }
  };
  const cancel = async () => {
    if (!login) return;
    const id = login.id;
    setLogin(null);
    setError(null);
    try {
      await api.cancelAgentLogin(machineId, id);
    } catch {
      // the machine may have ended it already: there is nothing to cancel
    }
  };

  const vendor = VENDOR[provider];
  const err = error && (
    <div className="mut sm" role="alert" style={{ color: 'var(--danger, #d33)', marginTop: 4 }}>
      {error}
    </div>
  );

  if (!login || login.state === 'cancelled') {
    return (
      <div style={{ marginTop: 6 }}>
        <Btn variant="primary" size="sm" icon="log-in" busy={busy} onClick={() => void start()}>
          Войти по ссылке
        </Btn>
        {err}
      </div>
    );
  }
  if (login.state === 'done' || sent) {
    return (
      <div className="mut sm" role="status" style={{ marginTop: 4 }}>
        {login.state === 'done'
          ? 'Вход выполнен, машина обновляет статус…'
          : 'Код отправлен, проверяем вход…'}
      </div>
    );
  }
  if (login.state === 'failed' || login.state === 'expired') {
    return (
      <div style={{ marginTop: 6 }}>
        <div className="mut sm" role="alert">
          {login.state === 'expired' ? 'Ссылка устарела.' : login.error || 'Войти не вышло.'}
        </div>
        <Btn size="sm" icon="refresh-cw" busy={busy} onClick={() => void start()} style={{ marginTop: 6 }}>
          Ещё раз
        </Btn>
        {err}
      </div>
    );
  }
  const url = safeLoginUrl(provider, login.url);
  return (
    <div style={{ marginTop: 6, display: 'grid', gap: 6 }}>
      {login.state === 'starting' || !login.url ? (
        <div className="mut sm" role="status">
          Машина готовит ссылку…
        </div>
      ) : !url ? (
        <div className="mut sm" role="alert">
          Машина прислала ссылку не на страницу {vendor}: открывать её не стоит.
        </div>
      ) : (
        <>
          <div className="mut sm">
            Откройте страницу {vendor} и войдите в аккаунт, на котором агент будет работать.
          </div>
          <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
            <a
              className="hx-btn"
              data-variant="primary"
              data-size="sm"
              href={url}
              target="_blank"
              rel="noopener noreferrer"
            >
              Открыть в браузере
            </a>
            <Btn size="sm" variant="ghost" onClick={() => void cancel()}>
              Отмена
            </Btn>
          </div>
          {login.needsCode && (
            <form
              className="row"
              style={{ gap: 8 }}
              onSubmit={(e) => {
                e.preventDefault();
                void sendCode();
              }}
            >
              <input
                className="hx-set__input"
                value={code}
                autoComplete="off"
                spellCheck={false}
                aria-label="Код со страницы входа"
                placeholder={`Код, который показал ${vendor} после входа`}
                onChange={(e) => setCode(e.target.value)}
              />
              <Btn type="submit" size="sm" busy={busy} disabled={!code.trim()}>
                Отправить
              </Btn>
            </form>
          )}
          {!login.needsCode && <div className="mut sm">После входа статус обновится сам.</div>}
        </>
      )}
      {err}
    </div>
  );
};
