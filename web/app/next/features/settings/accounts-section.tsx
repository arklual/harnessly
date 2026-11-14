// The agents' sign-in. «Провайдеры» lists the agents (a card leads to the agent's page); the agent's own page has its
// account on top (agent-account.tsx) and «Запросы»: where its requests go, a setting of the CLI's file changed here,
// and on Codex's the switch of its models in Claude Code. The page never holds a saved key, only its last digits.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { type ICredentialActions, type ISavedKey, type LoginState } from '@/next/features/accounts/model';
import {
  vendorsOf,
  type IPresetActions,
  type IPresetState,
  type PresetId,
} from '@/next/features/providers/model';
import GptSwitch from '@/next/features/providers/gpt-switch';
import SpeechKeyCards, { type SpeechCards } from '@/next/features/providers/speech-cards';
import VendorCard from '@/next/features/providers/vendor-card';
import { OWN_ACCOUNT, spendKey, type AccountSpendMap } from '@/next/features/limits/card-model';
import { profileLimitsOf, type MachineLimits } from '@/next/features/limits/model';
import ProviderCard, {
  PROVIDER_CARD_STATE,
  type IProviderCardAccount,
  type ProviderCardState,
} from '@/next/features/limits/provider-card';
import { optionValue } from '@/next/features/settings/options';
import { FormError, Group, Row, SegControl, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { AgentSettings, Provider } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
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

/** Where a card's requests go. */
const WHERE = { direct: 'direct', proxy: 'proxy' } as const;

export type AccountCard =
  | { provider: Provider; k: 'loading' }
  | { provider: Provider; k: 'failed'; text: string }
  | { provider: Provider; k: 'ready'; settings: AgentSettings };

/** What the card of an agent takes and does with its credentials. */
export interface IAgentCredentials extends ICredentialActions {
  login: LoginState;
  key: ISavedKey;
  /** The accounts the cluster saw this agent signed in with on this machine, newest first; undefined while unread. */
  seen: readonly string[] | undefined;
}

/** The vendors' cards (Kimi, GLM, Grok): the presets as the runtime lists them, or why it has none to show. */
export type VendorCards =
  | { k: 'loading' }
  | { k: 'failed'; text: string; onRetry: () => void }
  | ({
      k: 'ready';
      presets: readonly IPresetState[];
      /** Turns a switchable profile on or off (GPT in Claude Code); resolves null when the machine did it. */
      onSwitch: (preset: PresetId, on: boolean) => Promise<string | null>;
    } & IPresetActions);

export interface IAccountsSectionProps {
  /** The agents of the machine: how each is signed in. Signing in is on the agent's own page. */
  cards: readonly AccountCard[];
  /** The keys of the cloud recognition models. */
  speech: SpeechCards;
  /** Opens the agent's page, where it is signed in. */
  onOpen: (provider: Provider) => void;
  /** What is left of each account's subscription, as the machine says it; absent or null while it has not. */
  limits?: MachineLimits | null;
  /** What each account spent (the machine's report); absent or null while it has not said. */
  spend?: AccountSpendMap | null;
  /** The other sign-ins of Codex, whose limits and spend are on Codex's card. */
  codexLogins?: readonly ICodexLoginCard[];
}

/** «Запросы» on an agent's own page: where its requests go and, on Codex's, its models in Claude Code. */
export interface IAgentConnectionProps {
  settings: AgentSettings;
  /** The address of the requests is being written. */
  busy: boolean;
  /** The `gpt` profile and its switch, for Codex; null on the other pages. */
  gpt: { preset: IPresetState | undefined; onSwitch: (on: boolean) => Promise<string | null> } | null;
  /** Resolves true when the machine took the new address (null: the requests go direct). */
  onBaseUrl: (provider: Provider, url: string | null) => Promise<boolean>;
}

const statusText = (settings: AgentSettings): string => {
  const { account } = settings;
  if (!account.signedIn || !account.method) return t('next.settings.accounts.off');
  const how =
    account.method === 'subscription'
      ? t('next.settings.accounts.onSubscription', { plan: account.plan ?? '' }).trim()
      : t('next.settings.accounts.onKey');
  return account.who ? `${how} · ${account.who}` : how;
};

interface IProxyProps {
  settings: AgentSettings;
  busy: boolean;
  onBaseUrl: IAgentConnectionProps['onBaseUrl'];
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
      <Row type="seg" label={t('next.settings.accounts.where')} busy={busy}>
        <SegControl
          label={t('next.settings.accounts.where')}
          value={on ? WHERE.proxy : WHERE.direct}
          choices={[
            { value: WHERE.direct, label: t('next.settings.accounts.direct') },
            { value: WHERE.proxy, label: t('next.settings.accounts.viaProxy') },
          ]}
          onPick={(value) => {
            if (value === WHERE.direct) void direct();
            else if (!on) edit();
          }}
        />
      </Row>
      <div className="hx-reveal" data-open={on}>
        <div className="hx-reveal__in" inert={!on || undefined}>
          <div className="hx-apage__sub">
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
                <div className="hx-proxy__env">
                  <span>{t('next.settings.accounts.gets')}</span>
                  <code className="hx-trunc">
                    {BASE_URL_AT[settings.provider]}
                    {settings.provider === 'claude' ? '=' : ' = '}
                    {draft || '…'}
                  </code>
                </div>
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
          </div>
        </div>
      </div>
    </>
  );
};

export const AgentConnection = ({ settings, busy, gpt, onBaseUrl }: IAgentConnectionProps) => (
  <Group
    title={t('next.settings.agentPage.requests')}
    foot={
      settings.provider === 'claude'
        ? t('next.settings.accounts.connectionFootClaude')
        : t('next.settings.accounts.connectionFoot')
    }
  >
    <Proxy key={settings.provider} settings={settings} busy={busy} onBaseUrl={onBaseUrl} />
    {gpt && (
      <div className="hx-apage__row">
        <GptSwitch
          quiet
          preset={gpt.preset}
          ready={settings.account.signedIn && settings.account.method === 'subscription'}
          onSwitch={gpt.onSwitch}
        />
      </div>
    )}
  </Group>
);

/** The `gpt` profile of the runtime's list and its switch, for the Codex page. */
export const gptOf = (vendors: Extract<VendorCards, { k: 'ready' }>) => {
  const preset = vendors.presets.find((p) => p.vendor === 'gpt');
  return {
    preset,
    onSwitch: (on: boolean) => (preset ? vendors.onSwitch(preset.id, on) : Promise.resolve(null)),
  };
};

/** The vendor profiles the auto profile routes between: it is offered once one of them has a key. */
const AUTO_TARGETS: readonly string[] = ['kimi-code', 'glm', 'glm-cn'];

/** «Авто» under the vendor cards: the switch of the `auto` profile; nothing from a runtime that has none. */
const AutoCard = ({ vendors }: { vendors: Extract<VendorCards, { k: 'ready' }> }) => {
  const preset = vendors.presets.find((p) => p.vendor === 'auto');
  if (!preset) return null;
  const ready = vendors.presets.some((p) => AUTO_TARGETS.includes(p.id) && p.key.configured);
  return (
    <div className="hx-acct hx-acct--solo hx-acct--switch">
      <GptSwitch
        kind="auto"
        preset={preset}
        ready={ready}
        onSwitch={(on) => vendors.onSwitch(preset.id, on)}
      />
    </div>
  );
};

/** The vendors whose models run in Claude Code in chats of their own (Kimi, GLM, Grok), on the Claude Code page. */
export const VendorKeys = ({ vendors }: { vendors: VendorCards }) => {
  const [open, setOpen] = useState<string | null>(null);
  return (
    <Group title={t('next.settings.accounts.vendors')} foot={t('next.settings.accounts.vendorsFoot')}>
      {vendors.k === 'ready' &&
        vendorsOf(vendors.presets).map((vendor) => (
          <VendorCard
            key={vendor}
            vendor={vendor}
            states={vendors.presets}
            open={open === vendor}
            onToggle={(card) => setOpen((now) => (now === card ? null : card))}
            onSave={vendors.onSave}
            onRemove={vendors.onRemove}
            onTest={vendors.onTest}
          />
        ))}
      {vendors.k === 'ready' && <AutoCard vendors={vendors} />}
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
};

const agentStatus = (card: AccountCard): string => {
  if (card.k === 'failed') return card.text;
  if (card.k === 'loading') return t('next.settings.loading');
  return statusText(card.settings);
};

/** One more sign-in of Codex on the machine: its launch profile carries its limits, its name its spend. */
export interface ICodexLoginCard {
  name: string;
  profile: string;
  /** Whose plan it is signed in with; '' when it is not signed in. */
  account: string;
}

const agentStateOf = (card: AccountCard): ProviderCardState =>
  card.k === 'ready' && !card.settings.account.signedIn ? PROVIDER_CARD_STATE.off : PROVIDER_CARD_STATE.on;

/** The accounts of an agent's card: the CLI's own sign-in, and for Codex each other sign-in that is signed in. */
const agentAccountsOf = (
  card: AccountCard,
  limits: MachineLimits | null | undefined,
  spend: AccountSpendMap | null | undefined,
  logins: readonly ICodexLoginCard[],
): IProviderCardAccount[] => {
  const { provider } = card;
  const own = card.k === 'ready' ? card.settings.account : null;
  const main: IProviderCardAccount = {
    key: 'main',
    name: own?.who ?? t(`next.settings.section.${provider}`),
    plan: own?.plan ?? limits?.[provider]?.plan ?? null,
    limits: limits?.[provider] ?? null,
    spend: spend?.get(spendKey(provider, OWN_ACCOUNT)) ?? null,
  };
  if (provider !== 'codex') return [main];
  const others = logins
    .filter((login) => login.account !== '')
    .map((login): IProviderCardAccount => ({
      key: login.profile,
      name: login.name,
      plan: null,
      limits: limits ? profileLimitsOf(limits, login.profile) : null,
      spend: spend?.get(spendKey(provider, login.name)) ?? null,
    }));
  return own?.signedIn === false && others.length > 0 ? others : [main, ...others];
};

/** «Провайдеры», its agents: a card each with what is left of its subscription and what it spent; the card
 *  leads to the agent's page, where it is signed in. */
const AccountsSection = ({
  cards,
  speech,
  limits,
  spend,
  codexLogins = [],
  onOpen,
}: IAccountsSectionProps) => (
  <>
    <Group
      title={t('next.settings.accounts.agents')}
      foot={t('next.settings.accounts.agentsFoot')}
      card="hx-pcards"
    >
      {cards.map((card) => {
        const logins = card.provider === 'codex' ? codexLogins : [];
        // Another sign-in of Codex is signed in: the agent is connected though the CLI's own home is not.
        const state = logins.some((login) => login.account !== '')
          ? PROVIDER_CARD_STATE.on
          : agentStateOf(card);
        return (
          <ProviderCard
            key={card.provider}
            logo={<i className="hx-agent" data-agent={card.provider} aria-hidden="true" />}
            name={t(`next.settings.section.${card.provider}`)}
            sub={agentStatus(card)}
            state={state}
            accounts={agentAccountsOf(card, limits, spend, logins)}
            onOpen={() => onOpen(card.provider)}
            onAction={() => onOpen(card.provider)}
          />
        );
      })}
    </Group>
    <Group title={t('next.settings.accounts.speech')}>
      <SpeechKeyCards cards={speech} />
    </Group>
  </>
);

export default AccountsSection;
