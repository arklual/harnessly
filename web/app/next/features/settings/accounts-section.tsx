// «Models and keys»: per agent and per vendor, how it is signed in on the machine and where its requests go. Everything
// is entered here: a subscription is signed in through a login the machine runs (its link and code are on the card), a
// key goes into a write-only field. The page never holds a saved key, only its last digits; the address of the requests
// is a setting of the CLI's file and is changed here.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import LoginPanel from '@/next/features/accounts/login-panel';
import {
  METHODS_OF,
  type ICredentialActions,
  type ISavedKey,
  type LoginMethod,
  type LoginVia,
  type LoginState,
} from '@/next/features/accounts/model';
import KeyPanel from '@/next/features/providers/key-panel';
import { vendorsOf, type IPresetActions, type IPresetState } from '@/next/features/providers/model';
import VendorCard from '@/next/features/providers/vendor-card';
import { optionValue } from '@/next/features/settings/options';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { AgentSettings, Provider } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { Skeleton } from '@/next/ui/skeleton';

/** Where each CLI keeps the address its requests go to. */
const BASE_URL_AT: Readonly<Record<Provider, string>> = {
  claude: 'ANTHROPIC_BASE_URL',
  codex: 'openai_base_url',
};

/** An address the CLI can send requests to: http(s) with a host. */
export const isProxyUrl = (value: string): boolean => {
  try {
    const url = new URL(value);
    return (url.protocol === 'https:' || url.protocol === 'http:') && url.host !== '' && !url.username;
  } catch {
    return false;
  }
};

export type AccountCard =
  | { provider: Provider; k: 'loading' }
  | { provider: Provider; k: 'failed'; text: string }
  | { provider: Provider; k: 'ready'; settings: AgentSettings };

/** What the card of an agent takes and does with its credentials. */
export interface IAgentCredentials extends ICredentialActions {
  login: LoginState;
  key: ISavedKey;
}

/** The vendors' cards (Kimi, GLM, Grok): the presets as the runtime lists them, or why it has none to show. */
export type VendorCards =
  | { k: 'loading' }
  | { k: 'failed'; text: string; onRetry: () => void }
  | ({ k: 'ready'; presets: readonly IPresetState[] } & IPresetActions);

export interface IAccountsSectionProps {
  cards: readonly AccountCard[];
  credentials: Readonly<Record<Provider, IAgentCredentials>>;
  vendors: VendorCards;
  /** The open card: an agent or a vendor. */
  open: string | null;
  /** The providers whose address is being written. */
  busy: ReadonlySet<Provider>;
  onToggle: (card: string) => void;
  /** Resolves true when the machine took the new address (null: the requests go direct). */
  onBaseUrl: (provider: Provider, url: string | null) => Promise<boolean>;
  onRetry: (provider: Provider) => void;
}

const statusText = (settings: AgentSettings): string => {
  const { account } = settings;
  if (!account.signedIn || !account.method) return t('next.settings.accounts.off');
  return account.method === 'subscription'
    ? t('next.settings.accounts.onSubscription', { plan: account.plan ?? '' }).trim()
    : t('next.settings.accounts.onKey');
};

const SignIn = ({ settings, credentials }: { settings: AgentSettings; credentials: IAgentCredentials }) => {
  const { account, provider } = settings;
  const methods = METHODS_OF[provider];
  const [method, setMethod] = useState<LoginMethod>(
    account.method === 'key' && methods.some((m) => m === 'key') ? 'key' : 'subscription',
  );
  const name = t(`next.settings.accounts.name.${provider}`);
  const open = (value: LoginMethod) => ({ 'data-open': method === value }) as const;
  const inert = (value: LoginMethod) => (method !== value ? true : undefined);
  const panel = (via: LoginVia) => (
    <LoginPanel
      via={via}
      account={account}
      login={credentials.login}
      onLogin={credentials.onLogin}
      onSubmitCode={credentials.onSubmitCode}
      onCancel={credentials.onCancelLogin}
      onLogout={credentials.onLogout}
    />
  );
  return (
    <>
      <div className="hx-acct__label">{t('next.settings.accounts.how')}</div>
      <div className="hx-acct__how">
        <Segmented
          label={t('next.settings.accounts.how')}
          value={method}
          options={methods.map((value) => ({
            value,
            label: t(`next.settings.accounts.method.${value}`),
          }))}
          onChange={setMethod}
        />
        <div className="hx-reveal" {...open('subscription')}>
          <div className="hx-reveal__in" inert={inert('subscription')}>
            {panel('subscription')}
          </div>
        </div>
        {provider === 'claude' && (
          <div className="hx-reveal" {...open('console')}>
            <div className="hx-reveal__in" inert={inert('console')}>
              {panel('console')}
            </div>
          </div>
        )}
        {provider === 'codex' && (
          <div className="hx-reveal" {...open('key')}>
            <div className="hx-reveal__in" inert={inert('key')}>
              <KeyPanel
                hint={t('next.settings.accounts.keyHint.codex')}
                label={t('next.providers.keyAria', { vendor: name })}
                saved={credentials.key}
                onSave={credentials.onSaveKey}
                onRemove={credentials.onLogout}
              />
            </div>
          </div>
        )}
      </div>
      <p className="hx-acct__note">{t('next.settings.accounts.terminal')}</p>
    </>
  );
};

interface IProxyProps {
  settings: AgentSettings;
  busy: boolean;
  onBaseUrl: IAccountsSectionProps['onBaseUrl'];
}

const Proxy = ({ settings, busy, onBaseUrl }: IProxyProps) => {
  const saved = optionValue(settings, 'baseUrl');
  const url = typeof saved === 'string' ? saved : null;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const on = url !== null || editing;
  const field = useRef<HTMLInputElement>(null);

  // The address field takes the cursor when it opens.
  useEffect(() => {
    if (editing) field.current?.focus();
  }, [editing]);

  const edit = () => {
    setDraft(url ?? '');
    setError(null);
    setEditing(true);
  };
  const direct = async () => {
    setEditing(false);
    setError(null);
    if (url !== null) await onBaseUrl(settings.provider, null);
  };
  const save = async () => {
    const next = draft.trim();
    if (!isProxyUrl(next)) {
      setError(t('next.settings.accounts.proxyBad'));
      return;
    }
    setError(null);
    if (await onBaseUrl(settings.provider, next)) setEditing(false);
  };
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') void save();
    if (event.key === 'Escape') setEditing(false);
  };

  return (
    <>
      <div className="hx-acct__label">{t('next.settings.accounts.where')}</div>
      <div className="hx-proxy" data-on={on}>
        <div className="hx-proxy__seg" role="radiogroup" aria-label={t('next.settings.accounts.where')}>
          <button type="button" role="radio" aria-checked={!on} onClick={() => void direct()}>
            {t('next.settings.accounts.direct')}
          </button>
          <button type="button" role="radio" aria-checked={on} onClick={on ? undefined : edit}>
            {t('next.settings.accounts.viaProxy')}
          </button>
        </div>
        <div className="hx-proxy__body">
          <div>
            {editing && (
              <>
                <div className="hx-proxy__edit">
                  <input
                    className="hx-set__input hx-set__input--mono"
                    type="url"
                    value={draft}
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="https://llm.example.ru"
                    aria-label={t('next.settings.accounts.proxyAria')}
                    ref={field}
                    onChange={(event) => setDraft(event.target.value)}
                    onKeyDown={onKey}
                  />
                  <SetButton tone="ghost" onClick={() => setEditing(false)}>
                    {t('next.settings.cancel')}
                  </SetButton>
                  <SetButton tone="primary" busy={busy} onClick={() => void save()}>
                    {t('next.settings.save')}
                  </SetButton>
                </div>
                {error && <FormError>{error}</FormError>}
              </>
            )}
            {!editing && url !== null && (
              <div className="hx-proxy__url">
                <Icon name="globe" size="sm" />
                <code className="hx-trunc">{url}</code>
                <button type="button" className="hx-src__act" onClick={edit}>
                  {t('next.settings.accounts.change')}
                </button>
              </div>
            )}
            <div className="hx-proxy__env">
              <span>{t('next.settings.accounts.gets')}</span>
              <code className="hx-trunc">
                {BASE_URL_AT[settings.provider]}
                {settings.provider === 'claude' ? '=' : ' = '}
                {url ?? (draft || '…')}
              </code>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

const Card = ({
  card,
  open,
  busy,
  credentials,
  onToggle,
  onBaseUrl,
  onRetry,
}: { card: AccountCard; open: boolean; busy: boolean; credentials: IAgentCredentials } & Pick<
  IAccountsSectionProps,
  'onToggle' | 'onBaseUrl' | 'onRetry'
>) => {
  const settings = card.k === 'ready' ? card.settings : null;
  const connected = settings?.account.signedIn ?? false;
  let status = t('next.settings.loading');
  if (card.k === 'failed') status = card.text;
  if (settings) status = statusText(settings);
  return (
    <div className="hx-acct" data-open={open} data-on={connected}>
      <button
        type="button"
        className="hx-acct__head"
        aria-expanded={open}
        onClick={() => onToggle(card.provider)}
      >
        <span className="hx-acct__logo">
          <i className="hx-agent" data-agent={card.provider} aria-hidden="true" />
        </span>
        <span className="hx-acct__text">
          <span className="hx-acct__name">{t(`next.settings.accounts.name.${card.provider}`)}</span>
          <span className="hx-acct__status">{status}</span>
        </span>
        <span className="hx-acct__dot" data-on={connected} />
        <Icon name="chevron-down" size="sm" className="hx-acct__chev" />
      </button>
      <div className="hx-acct__body">
        <div className="hx-acct__inner" inert={!open || undefined}>
          {card.k === 'loading' && <Skeleton className="hx-set__sk" />}
          {card.k === 'failed' && (
            <LoadFailed
              size={LOAD_FAILED_SIZE.inline}
              title={t('next.load.failed')}
              reason={card.text}
              onRetry={() => onRetry(card.provider)}
            />
          )}
          {settings && (
            <>
              <SignIn settings={settings} credentials={credentials} />
              <Proxy key={settings.provider} settings={settings} busy={busy} onBaseUrl={onBaseUrl} />
            </>
          )}
        </div>
      </div>
    </div>
  );
};

const AccountsSection = ({
  cards,
  credentials,
  vendors,
  open,
  busy,
  onToggle,
  onBaseUrl,
  onRetry,
}: IAccountsSectionProps) => (
  <Group foot={t('next.settings.accounts.foot')}>
    {cards.map((card) => (
      <Card
        key={card.provider}
        card={card}
        open={open === card.provider}
        busy={busy.has(card.provider)}
        credentials={credentials[card.provider]}
        onToggle={onToggle}
        onBaseUrl={onBaseUrl}
        onRetry={onRetry}
      />
    ))}
    {vendors.k === 'ready' &&
      vendorsOf(vendors.presets).map((vendor) => (
        <VendorCard
          key={vendor}
          vendor={vendor}
          states={vendors.presets}
          open={open === vendor}
          onToggle={onToggle}
          onSave={vendors.onSave}
          onRemove={vendors.onRemove}
          onTest={vendors.onTest}
        />
      ))}
    {vendors.k === 'loading' && <Skeleton className="hx-set__sk" />}
    {vendors.k === 'failed' && (
      <div className="hx-acct hx-acct--solo">
        <LoadFailed
          size={LOAD_FAILED_SIZE.inline}
          title={t('next.providers.loadFailed')}
          reason={vendors.text}
          onRetry={vendors.onRetry}
        />
      </div>
    )}
  </Group>
);

export default AccountsSection;
