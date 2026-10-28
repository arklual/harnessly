// Signing an agent in on a machine, three ways: by a link (and a code), by the person in the machine's console, or
// with an API key (pasted here, sealed in the browser to the machine; or put on the machine by the person). The
// credentials stay on the machine: the hub relays a sealed envelope and a status.
import { useEffect, useRef, useState } from 'react';

import { Button, TextField } from '@kit';

import { loginActive, PROVIDER_INFO, safeLoginUrl } from '../lib/agent-view';
import { useApi, useEnv, type PendingWindow } from '../lib/env';
import { errorText } from '../lib/errors';
import { consoleTarget, hhmm } from '../lib/format';
import type { Live } from '../lib/hooks';
import { t } from '../lib/i18n';
import { loginOf } from '../lib/live';
import { SealError } from '../lib/seal';
import type { KeyCheck, Login, Machine, Provider } from '../lib/types';
import { CircleCheck, Terminal } from '../ui/icons';
import { CopyBox, LinkBox, LinkButton, Msg, Note, Segmented, StepLine, Waiting } from '../ui/parts';

type Tab = 'link' | 'console' | 'key';

export const LoginPanel = ({
  machine,
  provider,
  live,
  allowed,
}: {
  machine: Machine;
  provider: Provider;
  live: Live;
  /** The machine's policy lets logins and keys through. */
  allowed: { login: boolean; keys: boolean };
}) => {
  const [tab, setTab] = useState<Tab>('link');
  const [posted, setPosted] = useState<Login | null>(null);
  const streamed = posted ? live.state.logins[posted.id]?.login : undefined;
  // A login begun in another tab of the browser still shows here: the latest of this machine and provider.
  const login = streamed ?? posted ?? loginOf(live.state, machine.id, provider);

  return (
    <div className="hub-item__body">
      <Segmented<Tab>
        value={tab}
        onChange={setTab}
        label={t('login.how')}
        options={[
          { value: 'link', label: t('login.byLink') },
          { value: 'console', label: t('login.inConsole') },
          { value: 'key', label: t('login.apiKey') },
        ]}
      />
      {tab !== 'key' && !allowed.login && <Msg tone="warn">{t('login.policyOff')}</Msg>}
      {tab === 'link' && (
        <LinkLogin
          machine={machine}
          provider={provider}
          login={login}
          onPosted={setPosted}
          disabled={!allowed.login}
          onConsole={() => setTab('console')}
        />
      )}
      {tab === 'console' && (
        <ConsoleLogin
          machine={machine}
          provider={provider}
          login={login}
          onPosted={setPosted}
          disabled={!allowed.login}
        />
      )}
      {tab === 'key' && <KeyLogin machine={machine} provider={provider} live={live} allowed={allowed.keys} />}
    </div>
  );
};

/** The end of a login that did not finish, and the way to start again. */
const LoginEnd = ({
  login,
  onAgain,
  onConsole,
}: {
  login: Login;
  onAgain: () => void;
  onConsole?: () => void;
}) => {
  if (login.state === 'failed') {
    return (
      <>
        <Msg tone="danger">{login.error || t('login.failed')}</Msg>
        <div className="hub-row">
          <Button size="s" onClick={onAgain}>
            {t('common.retryAgain')}
          </Button>
          {onConsole && (
            <LinkButton onClick={onConsole} icon={<Terminal size={14} />}>
              {t('login.tryConsole')}
            </LinkButton>
          )}
        </div>
      </>
    );
  }
  if (login.state === 'expired') {
    return (
      <>
        <Msg tone="warn">{t('login.expired')}</Msg>
        <Button size="s" onClick={onAgain}>
          {t('login.again')}
        </Button>
      </>
    );
  }
  return null;
};

const useStart = (
  machine: Machine,
  provider: Provider,
  method: 'link' | 'console',
  onPosted: (l: Login) => void,
) => {
  const api = useApi();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const start = async () => {
    setBusy(true);
    setError(null);
    try {
      const { login } = await api.agents.login(machine.id, provider, method);
      onPosted(login);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return { start, busy, error };
};

const useCancel = (machine: Machine) => {
  const api = useApi();
  const [error, setError] = useState<string | null>(null);
  const cancel = async (login: Login) => {
    setError(null);
    try {
      await api.agents.loginCancel(machine.id, login.id);
    } catch (e) {
      setError(errorText(e));
    }
  };
  return { cancel, error };
};

const LinkLogin = ({
  machine,
  provider,
  login,
  onPosted,
  disabled,
  onConsole,
}: {
  machine: Machine;
  provider: Provider;
  login: Login | null;
  onPosted: (l: Login) => void;
  disabled: boolean;
  onConsole: () => void;
}) => {
  const api = useApi();
  const env = useEnv();
  const info = PROVIDER_INFO[provider];
  const mine = login?.method === 'link' ? login : null;
  const { start, busy, error } = useStart(machine, provider, 'link', onPosted);
  const { cancel, error: cancelError } = useCancel(machine);
  // The link is the machine's word: only a sign-in page of this provider is shown and opened.
  const linkUrl = safeLoginUrl(provider, mine?.url);
  const [code, setCode] = useState('');
  const [sending, setSending] = useState(false);
  const [codeError, setCodeError] = useState<string | null>(null);

  const sendCode = async () => {
    if (!mine || !code.trim()) return;
    setSending(true);
    setCodeError(null);
    try {
      await api.agents.loginCode(machine.id, mine.id, code.trim());
      setCode('');
    } catch (e) {
      setCodeError(errorText(e));
    } finally {
      setSending(false);
    }
  };

  if (!mine || !loginActive(mine)) {
    return (
      <div className="hub-stack">
        {mine && <LoginEnd login={mine} onAgain={() => void start()} onConsole={onConsole} />}
        {(!mine || mine.state === 'cancelled' || mine.state === 'done') && (
          <>
            <Note>{t('login.link.before', { vendor: info.vendor })}</Note>
            <Button
              appearance="primary"
              loading={busy}
              disabled={disabled}
              onClick={() => void start()}
              className="hub-self-start"
            >
              {t('login.link.get')}
            </Button>
          </>
        )}
        {error && <Msg tone="danger">{error}</Msg>}
      </div>
    );
  }

  return (
    <div className="hub-stack">
      {mine.state === 'starting' || !mine.url ? (
        <Waiting>{t('login.link.preparing')}</Waiting>
      ) : !linkUrl ? (
        <Msg tone="danger">{t('login.link.unsafe', { vendor: info.vendor })}</Msg>
      ) : (
        <>
          <StepLine n={1}>{t('login.link.open', { vendor: info.vendor })}</StepLine>
          <LinkBox url={linkUrl} label={t('login.link.openLabel')} onOpen={() => env.openTab(linkUrl)} />
          {mine.needsCode && (
            <>
              <StepLine n={2}>{t('login.link.paste', { vendor: info.vendor })}</StepLine>
              <form
                className="hub-inputrow"
                onSubmit={(e) => {
                  e.preventDefault();
                  void sendCode();
                }}
              >
                <TextField
                  size="m"
                  className="hub-mono"
                  placeholder={t('login.link.codePlaceholder')}
                  autoComplete="off"
                  spellCheck={false}
                  aria-label={t('login.link.codeAria')}
                  value={code}
                  disabled={mine.state === 'verifying'}
                  onChange={(e) => setCode(e.target.value)}
                  error={codeError ?? undefined}
                />
                <Button
                  type="submit"
                  appearance="primary"
                  loading={sending || mine.state === 'verifying'}
                  disabled={!code.trim()}
                >
                  {t('common.confirm')}
                </Button>
              </form>
            </>
          )}
          {mine.userCode && (
            <>
              <StepLine n={2}>{t('login.link.enterCode')}</StepLine>
              <div className="hub-code">
                <b>{mine.userCode}</b>
                <Waiting>{t('login.link.waiting')}</Waiting>
              </div>
            </>
          )}
          {mine.state === 'verifying' && <Waiting>{t('login.link.verifying')}</Waiting>}
        </>
      )}
      <div className="hub-row" data-between>
        {mine.expiresAt ? <Note>{t('login.validUntil', { time: hhmm(mine.expiresAt) })}</Note> : <span />}
        <LinkButton onClick={() => void cancel(mine)}>{t('common.cancel')}</LinkButton>
      </div>
      {cancelError && <Msg tone="danger">{cancelError}</Msg>}
    </div>
  );
};

const ConsoleLogin = ({
  machine,
  provider,
  login,
  onPosted,
  disabled,
}: {
  machine: Machine;
  provider: Provider;
  login: Login | null;
  onPosted: (l: Login) => void;
  disabled: boolean;
}) => {
  const api = useApi();
  const env = useEnv();
  const info = PROVIDER_INFO[provider];
  const mine = login?.method === 'console' ? login : null;
  const { start, busy, error } = useStart(machine, provider, 'console', onPosted);
  const { cancel, error: cancelError } = useCancel(machine);
  const pending = useRef<PendingWindow | null>(null);
  const [openError, setOpenError] = useState<string | null>(null);
  const consoleUrl = mine?.consoleUrl ?? null;

  // The tab was opened inside the click; the terminal's address exists a moment later, when the machine has made the
  // tab. Then the tab goes there.
  useEffect(() => {
    const w = pending.current;
    if (!w || !consoleUrl) return;
    pending.current = null;
    void api.machines
      .open(machine.id)
      .then(({ url }) => w.go(consoleTarget(url, consoleUrl, env.origin(), machine.id)))
      .catch((e: unknown) => {
        w.close();
        setOpenError(errorText(e));
      });
  }, [api, env, machine.id, consoleUrl]);

  const openTerminal = () => {
    setOpenError(null);
    pending.current = env.openBlank();
    if (!mine || !loginActive(mine)) void start();
    else if (consoleUrl && pending.current) {
      // The terminal exists already: only the tab is new.
      const w = pending.current;
      pending.current = null;
      void api.machines
        .open(machine.id)
        .then(({ url }) => w.go(consoleTarget(url, consoleUrl, env.origin(), machine.id)))
        .catch((e: unknown) => {
          w.close();
          setOpenError(errorText(e));
        });
    }
  };

  const active = !!mine && loginActive(mine);
  return (
    <div className="hub-stack">
      <StepLine n={1}>{t('login.console.open')}</StepLine>
      <CopyBox text={info.loginCommand} />
      {!active && mine && <LoginEnd login={mine} onAgain={openTerminal} />}
      {!active && (!mine || mine.state === 'cancelled' || mine.state === 'done') && (
        <Button
          appearance="primary"
          className="hub-self-start"
          icon={<Terminal size={16} />}
          loading={busy}
          disabled={disabled}
          onClick={openTerminal}
        >
          {t('login.console.button')}
        </Button>
      )}
      {active && (
        <>
          <div className="hub-code">
            <Waiting>
              {mine.state === 'starting' ? t('login.console.preparing') : t('login.console.waiting')}
            </Waiting>
          </div>
          <div className="hub-row" data-between>
            <LinkButton onClick={openTerminal} disabled={!consoleUrl} icon={<Terminal size={14} />}>
              {t('login.console.again')}
            </LinkButton>
            <LinkButton onClick={() => void cancel(mine)}>{t('common.cancel')}</LinkButton>
          </div>
        </>
      )}
      {(error || openError || cancelError) && <Msg tone="danger">{error ?? openError ?? cancelError}</Msg>}
    </div>
  );
};

type KeyMode = 'paste' | 'own';

const KeyLogin = ({
  machine,
  provider,
  live,
  allowed,
}: {
  machine: Machine;
  provider: Provider;
  live: Live;
  allowed: boolean;
}) => {
  const api = useApi();
  const env = useEnv();
  const info = PROVIDER_INFO[provider];
  const [mode, setMode] = useState<KeyMode>('paste');
  const [sealable, setSealable] = useState<boolean | null>(null);
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<KeyCheck | null>(null);

  useEffect(() => {
    let alive = true;
    void env.canSeal().then((ok) => {
      if (alive) setSealable(ok);
    });
    return () => {
      alive = false;
    };
  }, [env]);

  const refresh = async () => {
    try {
      const { agents } = await api.agents.list(machine.id, true);
      live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...machine, agents } } } });
    } catch {
      // the agent's own event brings the status
    }
  };

  const finish = (check: KeyCheck) => {
    setResult(check);
    if (check.verdict === 'accepted') void refresh();
  };

  const save = async () => {
    const value = key.trim();
    if (!value) return;
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      const envelope = await env.seal(machine.encKey, machine.id, `key:${provider}`, value);
      const { check } = await api.agents.deliverKey(machine.id, provider, envelope);
      finish(check);
    } catch (e) {
      setError(
        e instanceof SealError
          ? t(e.reason === 'too_long' ? 'login.key.tooLong' : 'login.key.cannotSeal')
          : errorText(e),
      );
    } finally {
      // Whatever came back, the key does not stay in the page.
      setKey('');
      setBusy(false);
    }
  };

  const verify = async () => {
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      finish((await api.agents.verifyKey(machine.id, provider)).check);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const mayPaste = allowed && sealable !== false;
  return (
    <div className="hub-stack">
      {!allowed && <Msg tone="warn">{t('login.key.policyOff')}</Msg>}
      <Segmented<KeyMode>
        value={mode}
        onChange={(m) => {
          setMode(m);
          setResult(null);
          setError(null);
        }}
        label={t('login.key.how')}
        options={[
          { value: 'paste', label: t('login.key.paste') },
          { value: 'own', label: t('login.key.own') },
        ]}
      />
      {mode === 'paste' ? (
        <>
          {sealable === false && <Msg tone="warn">{t('login.key.cannotSeal')}</Msg>}
          <form
            className="hub-inputrow"
            onSubmit={(e) => {
              e.preventDefault();
              if (mayPaste) void save();
            }}
          >
            <TextField
              size="m"
              type="password"
              className="hub-mono"
              placeholder={info.keyPlaceholder}
              autoComplete="off"
              aria-label={t('login.key.aria')}
              value={key}
              disabled={!mayPaste}
              onChange={(e) => setKey(e.target.value)}
            />
            <Button type="submit" loading={busy} disabled={!mayPaste || !key.trim()}>
              {t('common.save')}
            </Button>
          </form>
          <Note>{t('login.key.sealed')}</Note>
        </>
      ) : (
        <>
          <StepLine n={1}>{t('login.key.ownRun')}</StepLine>
          <CopyBox text={`echo 'export ${info.env}=${t('login.key.yourKey')}' >> ~/.profile`} wrap />
          <StepLine n={2}>{t('login.key.ownCheck')}</StepLine>
          <Button
            appearance="primary"
            className="hub-self-start"
            icon={<CircleCheck size={16} />}
            loading={busy}
            onClick={() => void verify()}
          >
            {t('login.key.verify')}
          </Button>
        </>
      )}
      {result && <KeyResult check={result} provider={provider} />}
      {error && <Msg tone="danger">{error}</Msg>}
    </div>
  );
};

const KeyResult = ({ check, provider }: { check: KeyCheck; provider: Provider }) => {
  const info = PROVIDER_INFO[provider];
  if (check.verdict === 'accepted')
    return <Msg tone="success">{t('login.key.accepted', { vendor: info.vendor })}</Msg>;
  if (check.verdict === 'rejected') {
    return (
      <Msg tone="danger">
        {check.status
          ? t('login.key.rejectedStatus', { vendor: info.vendor, status: check.status })
          : t('login.key.rejected', { vendor: info.vendor })}
      </Msg>
    );
  }
  if (check.verdict === 'network')
    return <Msg tone="warn">{t('login.key.network', { vendor: info.vendor })}</Msg>;
  return <Msg tone="warn">{t('login.key.absent', { env: info.env })}</Msg>;
};
