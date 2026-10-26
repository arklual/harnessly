// «Подписка»: the sign-in of a subscription without a terminal. The page starts the login on the machine, shows its link
// and the code to type there (or a field for the code the browser gives back), and says plainly where it stands. A code
// typed here is write-only like a key: masked, emptied the moment it is sent, never shown again.
import { useEffect, useRef, useState } from 'react';
import type { AgentAccount } from '@/next/protocol/v2';
import { FormError, SetButton } from '@/next/features/settings/rows';
import type { LoginState, LoginVia } from '@/next/features/accounts/model';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import SecretField from '@/next/ui/secret-field';

const SURE_MS = 4000;

interface ILoginPanelProps {
  via: LoginVia;
  account: AgentAccount;
  login: LoginState;
  onLogin: (via: LoginVia) => Promise<string | null>;
  onSubmitCode: (code: string) => Promise<string | null>;
  onCancel: () => Promise<void>;
  onLogout: () => Promise<string | null>;
}

const LoginPanel = ({ via, account, login, onLogin, onSubmitCode, onCancel, onLogout }: ILoginPanelProps) => {
  const [code, setCode] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sure, setSure] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const alive = useRef(true);
  const connected = account.signedIn && account.method === (via === 'console' ? 'key' : 'subscription');

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (!sure) return;
    const timer = window.setTimeout(() => setSure(false), SURE_MS);
    return () => window.clearTimeout(timer);
  }, [sure]);

  const start = async () => {
    setError(null);
    const refused = await onLogin(via);
    if (alive.current && refused !== null) setError(refused);
  };
  const submit = async () => {
    const value = code.trim();
    if (value === '') return;
    setSending(true);
    // The code leaves the page's state the moment it is sent, whatever the answer.
    setCode('');
    const refused = await onSubmitCode(value);
    if (!alive.current) return;
    setSending(false);
    setError(refused);
  };
  const leave = async () => {
    if (!sure) {
      setSure(true);
      return;
    }
    setSure(false);
    setLeaving(true);
    const refused = await onLogout();
    if (!alive.current) return;
    setLeaving(false);
    if (refused !== null) setError(refused);
  };

  const waiting = login.k === 'waiting' ? login : null;
  const shown = connected ? 'connected' : login.k;
  const failure = login.k === 'failed' ? login.reason : error;
  // The failure line stays on screen while its row closes.
  const [kept, setKept] = useState('');
  if (failure !== null && failure !== kept) setKept(failure);

  return (
    <div className="hx-preset">
      <div className="hx-reveal" data-open={shown === 'connected'}>
        <div className="hx-reveal__in" inert={shown !== 'connected' || undefined}>
          <div className="hx-preset__saved">
            <Icon name="circle-check" size="sm" />
            <span>
              {t(via === 'console' ? 'next.login.connectedConsole' : 'next.login.connected', {
                plan: account.plan ?? '',
              }).trim()}
              {account.who ? ` · ${account.who}` : ''}
            </span>
          </div>
          <div className="hx-preset__row">
            <SetButton tone="ghost" busy={leaving} onClick={() => void leave()}>
              {sure ? t('next.login.leaveSure') : t('next.login.leave')}
            </SetButton>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={shown === 'idle' || shown === 'failed'}>
        <div className="hx-reveal__in" inert={!(shown === 'idle' || shown === 'failed') || undefined}>
          <p className="hx-preset__hint">
            {t(via === 'console' ? 'next.login.hintConsole' : 'next.login.hint')}
          </p>
          <div className="hx-preset__row">
            <SetButton tone="primary" onClick={() => void start()}>
              {shown === 'failed'
                ? t('next.login.again')
                : t(via === 'console' ? 'next.login.startConsole' : 'next.login.start')}
            </SetButton>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={shown === 'starting' || shown === 'checking'}>
        <div className="hx-reveal__in" inert={!(shown === 'starting' || shown === 'checking') || undefined}>
          <div className="hx-preset__saved">
            <Icon name="loader-circle" size="sm" className="hx-spin-ic" />
            <span>{t(shown === 'checking' ? 'next.login.checking' : 'next.login.starting')}</span>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={shown === 'waiting'}>
        <div className="hx-reveal__in" inert={shown !== 'waiting' || undefined}>
          <div className="hx-preset__form">
            <p className="hx-preset__hint">
              {t(waiting?.userCode ? 'next.login.stepDevice' : 'next.login.stepOpen')}
            </p>
            {waiting && (
              <a className="hx-preset__link" href={waiting.url} target="_blank" rel="noopener noreferrer">
                <Icon name="external-link" size="sm" />
                {t('next.login.open')}
              </a>
            )}
            {waiting?.userCode && (
              <div className="hx-preset__code" aria-label={t('next.login.codeShown')}>
                {waiting.userCode}
              </div>
            )}
            {waiting?.asksCode && (
              <>
                <SecretField
                  value={code}
                  onChange={(value) => {
                    setCode(value);
                    setError(null);
                  }}
                  label={t('next.login.codeAria')}
                  placeholder={t('next.login.codePlaceholder')}
                  disabled={sending}
                  invalid={error !== null}
                  onSubmit={() => void submit()}
                />
                <p className="hx-preset__where">{t('next.login.codeNote')}</p>
              </>
            )}
            <div className="hx-preset__row">
              {waiting?.asksCode && (
                <SetButton
                  tone="primary"
                  busy={sending}
                  disabled={code.trim() === ''}
                  onClick={() => void submit()}
                >
                  {t('next.login.confirm')}
                </SetButton>
              )}
              <SetButton tone="ghost" onClick={() => void onCancel()}>
                {t('next.providers.cancel')}
              </SetButton>
            </div>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={failure !== null}>
        <div className="hx-reveal__in">
          <FormError>{kept}</FormError>
        </div>
      </div>
    </div>
  );
};

export default LoginPanel;
