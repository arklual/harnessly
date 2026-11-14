// «ChatGPT» in Providers: the sign-in with ChatGPT for a plain conversation on the person's own plan. The
// card is the providers' card; under it the accounts of this machine and the sign-in itself. On a remote
// machine the browser's callback (127.0.0.1) does not reach it, so the person pastes the callback's address back here.
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { gptChatsChanged } from '@/next/features/chatgpt/store';
import {
  activateAccount,
  cancelLogin,
  finishLogin,
  forgetAccount,
  loadState,
  refusalCode,
  signOut,
  startLogin,
} from '@/next/features/chatgpt/api';
import { refusalKey, USAGE_URL, type IGptAccount, type IGptState } from '@/next/features/chatgpt/model';
import ProviderCard, { PROVIDER_CARD_STATE } from '@/next/features/limits/provider-card';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { usePresence, usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/chatgpt.css';

const POLL_MS = 2_000;
const EXIT_MS = 200;
/** The first sign-in's note was read (UI guidelines of Sign in with ChatGPT: shown once). */
const WELCOME_KEY = 'hx-chatgpt-welcomed';

const welcomed = (): boolean => {
  try {
    return localStorage.getItem(WELCOME_KEY) === '1';
  } catch {
    return true;
  }
};

const keepWelcomed = () => {
  try {
    localStorage.setItem(WELCOME_KEY, '1');
  } catch {
    // Blocked storage: the note shows again next time.
  }
};

type TLogin = { account: string | null; url: string } | null;

/** The plan as ChatGPT names it: «Plus», «Pro». */
const planName = (plan: string | undefined): string =>
  plan ? plan.charAt(0).toUpperCase() + plan.slice(1) : '';

const statusOf = (a: IGptAccount): string => {
  if (!a.signedIn) return t('next.chatgpt.acc.signedOut');
  if (!a.planUse) return t('next.chatgpt.acc.noPlan');
  return a.active ? t('next.chatgpt.acc.active') : t('next.chatgpt.acc.ready');
};

interface IAccountRowProps {
  account: IGptAccount;
  busy: boolean;
  onActivate: () => void;
  onSignIn: () => void;
  onSignOut: () => void;
  onForget: () => void;
}

const AccountRow = ({ account, busy, onActivate, onSignIn, onSignOut, onForget }: IAccountRowProps) => (
  <div className="hx-gpt-acc" data-active={account.active || undefined}>
    <div className="hx-gpt-acc__text">
      <b>{account.label || t('next.chatgpt.acc.unnamed')}</b>
      <span>
        {account.plan ? `${planName(account.plan)} · ` : ''}
        {statusOf(account)}
      </span>
    </div>
    <div className="hx-gpt-acc__acts">
      {account.signedIn && account.planUse && !account.active && (
        <SetButton tone="ghost" busy={busy} onClick={onActivate}>
          {t('next.chatgpt.acc.makeActive')}
        </SetButton>
      )}
      {(!account.signedIn || !account.planUse) && (
        <SetButton tone="ghost" busy={busy} onClick={onSignIn}>
          {t('next.chatgpt.acc.signIn')}
        </SetButton>
      )}
      {account.signedIn && (
        <SetButton tone="ghost" busy={busy} onClick={onSignOut}>
          {t('next.chatgpt.acc.signOut')}
        </SetButton>
      )}
      {!account.signedIn && (
        <SetButton tone="danger" busy={busy} onClick={onForget}>
          {t('next.chatgpt.acc.forget')}
        </SetButton>
      )}
    </div>
  </div>
);

interface ILoginPanelProps {
  login: NonNullable<TLogin>;
  busy: boolean;
  error: string | null;
  onPaste: (url: string) => void;
  onCancel: () => void;
}

/** The address the browser lands on at the sign-in's end: the loopback callback with its code. */
const CALLBACK_RE = /^https?:\/\/(127\.0\.0\.1|localhost):\d+\/auth\/callback\?\S*code=/;
const isCallback = (text: string) => CALLBACK_RE.test(text.trim());
/** The page may ask the clipboard (Clipboard API; absent in an insecure context). */
const canReadClipboard = () =>
  typeof navigator !== 'undefined' && 'clipboard' in navigator && 'readText' in navigator.clipboard;

/** The browser's bar at the sign-in's end, drawn: which address to copy. */
const BarPicture = () => (
  <div className="hx-gpt-bar" aria-hidden="true">
    <span className="hx-gpt-bar__dots">
      <i />
      <i />
      <i />
    </span>
    <span className="hx-gpt-bar__url">
      <mark>127.0.0.1:1455/auth/callback?code=…</mark>
    </span>
  </div>
);

const LoginPanel = ({ login, busy, error, onPaste, onCancel }: ILoginPanelProps) => {
  const id = useId();
  const [text, setText] = useState('');
  const sent = useRef('');

  // The address is sent as soon as it is there: pasted into the field, or found in the clipboard when the person comes
  // back to this tab (where the browser lets the page read it; elsewhere nothing happens).
  const take = useCallback(
    (raw: string) => {
      const value = raw.trim();
      if (!isCallback(value) || value === sent.current) return false;
      sent.current = value;
      setText(value);
      onPaste(value);
      return true;
    },
    [onPaste],
  );

  useEffect(() => {
    const look = () => {
      if (document.visibilityState !== 'visible' || !canReadClipboard()) return;
      navigator.clipboard.readText().then(take, () => undefined);
    };
    window.addEventListener('focus', look);
    document.addEventListener('visibilitychange', look);
    return () => {
      window.removeEventListener('focus', look);
      document.removeEventListener('visibilitychange', look);
    };
  }, [take]);

  const fromClipboard = () => {
    navigator.clipboard.readText().then(
      (value) => {
        if (!take(value)) setText(value.trim());
      },
      () => undefined,
    );
  };

  return (
    <form
      className="hx-key-form hx-gpt-login"
      onSubmit={(e) => {
        e.preventDefault();
        if (text.trim()) onPaste(text.trim());
      }}
    >
      <ol className="hx-gpt-login__steps">
        <li>
          {t('next.chatgpt.login.step1')}{' '}
          <a href={login.url} target="_blank" rel="noreferrer noopener">
            {t('next.chatgpt.login.open')}
          </a>
        </li>
        <li>{t('next.chatgpt.login.step2')}</li>
      </ol>
      <BarPicture />
      <label className="hx-voice-words__field" htmlFor={id}>
        <span className="hx-nb__label">{t('next.chatgpt.login.pasteLabel')}</span>
        <input
          id={id}
          className="hx-set__input"
          value={text}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          inputMode="url"
          placeholder="http://127.0.0.1:1455/auth/callback?code=…"
          onPaste={(e) => {
            if (take(e.clipboardData.getData('text'))) e.preventDefault();
          }}
          onChange={(e) => setText(e.target.value)}
        />
      </label>
      {error && <FormError>{t(refusalKey(error))}</FormError>}
      <div className="hx-key-form__row">
        <SetButton
          tone="primary"
          busy={busy}
          disabled={text.trim() === ''}
          onClick={() => onPaste(text.trim())}
        >
          {t('next.chatgpt.login.done')}
        </SetButton>
        {canReadClipboard() && (
          <SetButton tone="ghost" onClick={fromClipboard}>
            {t('next.chatgpt.login.paste')}
          </SetButton>
        )}
        <SetButton tone="ghost" onClick={onCancel}>
          {t('next.chatgpt.login.cancel')}
        </SetButton>
      </div>
    </form>
  );
};

type TLoad = { k: 'loading' } | { k: 'failed' } | { k: 'ready'; state: IGptState };

const ChatGptSettings = ({ machine }: { machine: MachineId }) => {
  const [load, setLoad] = useState<TLoad>({ k: 'loading' });
  const [login, setLogin] = useState<TLogin>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [welcome, setWelcome] = useState(false);
  const panel = usePresence(login, EXIT_MS);
  const note = usePresentFlag(welcome, EXIT_MS);

  const read = useCallback(
    () =>
      loadState(machine).then(
        (state) => {
          setLoad({ k: 'ready', state });
          return state;
        },
        () => {
          setLoad({ k: 'failed' });
          return null;
        },
      ),
    [machine],
  );

  useEffect(() => {
    void read();
  }, [read]);

  const accounts = load.k === 'ready' ? (load.state.accounts ?? []) : [];
  const signedIn = () => {
    gptChatsChanged();
    setLogin(null);
    setError(null);
    if (!welcomed()) setWelcome(true);
  };

  // While a sign-in waits, its end may come through the machine's own callback (a browser on the same computer).
  const waiting = login !== null;
  const count = accounts.filter((a) => a.signedIn).length;
  useEffect(() => {
    if (!waiting) return undefined;
    const timer = window.setInterval(() => {
      void read().then((state) => {
        if (!state || state.pending) return;
        if ((state.accounts ?? []).filter((a) => a.signedIn).length <= count) return;
        gptChatsChanged();
        setLogin(null);
        setError(null);
        if (!welcomed()) setWelcome(true);
      });
    }, POLL_MS);
    return () => window.clearInterval(timer);
  }, [waiting, count, read]);

  const begin = (account: string | null) => {
    setError(null);
    // The window opens on the click itself (a later one is blocked), and goes to the sign-in once its address is made.
    const win = window.open('about:blank', '_blank');
    startLogin(machine, account ?? undefined).then(
      (answer) => {
        if (win) {
          win.opener = null;
          win.location.href = answer.url;
        }
        setLogin({ account, url: answer.url });
      },
      (failure: unknown) => {
        win?.close();
        setError(refusalCode(failure) || 'other');
      },
    );
  };

  const paste = (url: string) => {
    setBusy('login');
    finishLogin(machine, url).then(
      () => {
        setBusy(null);
        signedIn();
        void read();
      },
      (failure: unknown) => {
        setBusy(null);
        setError(refusalCode(failure) || 'other');
      },
    );
  };

  const act = (key: string, run: () => Promise<unknown>) => {
    setBusy(key);
    run().then(
      () => {
        setBusy(null);
        gptChatsChanged();
        void read();
      },
      (failure: unknown) => {
        setBusy(null);
        setError(refusalCode(failure) || 'other');
      },
    );
  };

  const active = accounts.find((a) => a.active) ?? null;
  const on = active !== null;
  let sub = t('next.chatgpt.card.off');
  if (load.k === 'loading') sub = t('next.chatgpt.card.loading');
  else if (load.k === 'failed') sub = t('next.chatgpt.card.failed');
  else if (active) sub = active.plan ? `${active.label} · ${planName(active.plan)}` : active.label;

  return (
    <Group title={t('next.chatgpt.card.group')} foot={t('next.chatgpt.card.foot')} card="hx-pcards">
      <ProviderCard
        logo={<i className="hx-agent" data-agent="codex" aria-hidden="true" />}
        name="ChatGPT"
        sub={sub}
        state={on ? PROVIDER_CARD_STATE.on : PROVIDER_CARD_STATE.off}
        accounts={[]}
        side={
          login === null && load.k === 'ready' && accounts.length === 0 ? (
            <SetButton tone="primary" onClick={() => begin(null)}>
              {t('next.chatgpt.login.start')}
            </SetButton>
          ) : undefined
        }
        expanded={login !== null}
      >
        {note.mounted && (
          <div className="hx-gpt-welcome" data-state={note.state} role="status">
            <b>{t('next.chatgpt.welcome.title')}</b>
            <span>
              {t('next.chatgpt.welcome.text')}{' '}
              <a href={USAGE_URL} target="_blank" rel="noreferrer noopener">
                {t('next.chatgpt.usageLink')}
              </a>
            </span>
            <SetButton
              tone="primary"
              onClick={() => {
                keepWelcomed();
                setWelcome(false);
              }}
            >
              {t('next.chatgpt.welcome.ok')}
            </SetButton>
          </div>
        )}
        {accounts.length > 0 && (
          <div className="hx-gpt-accs">
            {accounts.map((a) => (
              <AccountRow
                key={a.key}
                account={a}
                busy={busy === a.key}
                onActivate={() => act(a.key, () => activateAccount(machine, a.key))}
                onSignIn={() => begin(a.key)}
                onSignOut={() => act(a.key, () => signOut(machine, a.key))}
                onForget={() => act(a.key, () => forgetAccount(machine, a.key))}
              />
            ))}
            {login === null && (
              <button type="button" className="hx-gpt-accs__add" onClick={() => begin(null)}>
                {t('next.chatgpt.acc.add')}
              </button>
            )}
          </div>
        )}
        {panel.value && (
          <div className="hx-gpt-login-wrap" data-state={panel.state}>
            <LoginPanel
              login={panel.value}
              busy={busy === 'login'}
              error={error}
              onPaste={paste}
              onCancel={() => {
                setLogin(null);
                setError(null);
                void cancelLogin(machine);
              }}
            />
          </div>
        )}
        {error && login === null && <FormError className="hx-gpt-acc__err">{t(refusalKey(error))}</FormError>}
      </ProviderCard>
    </Group>
  );
};

export default ChatGptSettings;
