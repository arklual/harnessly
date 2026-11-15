// The agent's own page in Settings (Codex, Claude Code), HRN-1434: the account on top as a provider card (whom it is
// signed in as, the plan and what is left of it, «⋯» to sign out or switch), the requests under it as rows of one grid
// (label on the left, control on the right, one column on a phone), and where the file is, small, at the foot. A card
// not signed in folds out the sign-in; a «⋯» action that ends the sign-in asks once in the fold before it does.
import { Menu } from '@base-ui/react/menu';
import { useState, type ReactNode } from 'react';
import LoginPanel from '@/next/features/accounts/login-panel';
import SeenAccounts from '@/next/features/accounts/seen-accounts';
import { METHODS_OF, type LoginMethod, type LoginVia } from '@/next/features/accounts/model';
import type { AccountSpend } from '@/next/features/limits/card-model';
import type { ProviderLimits } from '@/next/features/limits/model';
import ProviderCard, { PROVIDER_CARD_STATE } from '@/next/features/limits/provider-card';
import KeyPanel from '@/next/features/providers/key-panel';
import type { IAgentCredentials } from '@/next/features/settings/accounts-section';
import { FormError, Group, Row, SegControl, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { AgentSettings } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { usePresence, type TPresenceState } from '@/next/ui/presence';
import '@/next/styles/agent-page.css';

/** A fold's exit, as long as hx-apage-fold-out in agent-page.css. */
const FOLD_EXIT_MS = 200;

/** What folds out of the account card: the sign-in, or the question before signing out (or switching). */
const FOLD = { signIn: 'sign-in', leave: 'leave', change: 'change' } as const;
type TFold = (typeof FOLD)[keyof typeof FOLD];

/** What folds out of a card: it opens and closes by its height, and keeps its content while it closes. */
export const Fold = ({ state, children }: { state: TPresenceState; children: ReactNode }) => (
  <div className="hx-apage__fold" data-state={state}>
    <div className="hx-apage__fold-in">{children}</div>
  </div>
);

/** The line under the account's name: how it is signed in, never whom (that is the name). */
const subOf = (settings: AgentSettings): string => {
  const { account } = settings;
  if (!account.signedIn || !account.method) return t('next.settings.accounts.off');
  if (account.method === 'key') return t('next.settings.accounts.onKey');
  return t('next.settings.accounts.onSubscription', { plan: account.plan ?? '' }).trim();
};

interface IAccountMenuProps {
  label: string;
  onChange: () => void;
  onLeave: () => void;
}

/** The account's «⋯»: sign in as someone else, sign out. */
const AccountMenu = ({ label, onChange, onLeave }: IAccountMenuProps) => {
  const actions = useMenuActions();
  return (
    <MenuRoot actionsRef={actions}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-apage__more" aria-label={label}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" sideOffset={6}>
        <Menu.Item className="hx-cmenu__item" onClick={onChange}>
          <Icon name="refresh-cw" size="sm" />
          <span className="hx-cmenu__txt">{t('next.settings.agentPage.change')}</span>
        </Menu.Item>
        <Menu.Separator className="hx-cmenu__sep" />
        <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={onLeave}>
          <Icon name="log-out" size="sm" />
          <span className="hx-cmenu__txt">{t('next.settings.agentPage.leave')}</span>
        </Menu.Item>
      </MenuPanel>
    </MenuRoot>
  );
};

/** The sign-in that folds out of a card not signed in: the way (a row with the choice when there are two) and its panel. */
const SignInForm = ({
  settings,
  credentials,
}: {
  settings: AgentSettings;
  credentials: IAgentCredentials;
}) => {
  const { account, provider } = settings;
  const methods = METHODS_OF[provider];
  const [method, setMethod] = useState<LoginMethod>(
    account.method === 'key' && methods.some((m) => m === 'key') ? 'key' : 'subscription',
  );
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
    <div className="hx-apage__signin">
      {methods.length > 1 && (
        <Row type="seg" label={t('next.settings.accounts.how')}>
          <SegControl
            label={t('next.settings.accounts.how')}
            value={method}
            choices={methods.map((value) => ({ value, label: t(`next.settings.accounts.method.${value}`) }))}
            onPick={setMethod}
          />
        </Row>
      )}
      <div className="hx-apage__panel">
        {method === 'subscription' && panel('subscription')}
        {method === 'console' && panel('console')}
        {method === 'key' && (
          <KeyPanel
            hint={t('next.settings.accounts.keyHint.codex')}
            label={t('next.providers.keyAria', { vendor: t(`next.settings.accounts.name.${provider}`) })}
            saved={credentials.key}
            onSave={credentials.onSaveKey}
            onRemove={credentials.onLogout}
          />
        )}
      </div>
    </div>
  );
};

interface ILeaveFormProps {
  who: string;
  /** Switching: after the sign-out the sign-in folds out. */
  change: boolean;
  onLogout: () => Promise<string | null>;
  onDone: (next: TFold | null) => void;
}

/** The one question before the sign-in ends: the running chats lose it. */
const LeaveForm = ({ who, change, onLogout, onDone }: ILeaveFormProps) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const go = async () => {
    setBusy(true);
    setError(null);
    const refused = await onLogout();
    setBusy(false);
    if (refused !== null) return setError(refused);
    onDone(change ? FOLD.signIn : null);
  };
  return (
    <div className="hx-apage__ask">
      <p className="hx-apage__ask-text">
        {t(change ? 'next.settings.agentPage.changeAsk' : 'next.settings.agentPage.leaveAsk', { who })}
      </p>
      {error !== null && <FormError>{error}</FormError>}
      <div className="hx-apage__actions">
        <SetButton tone="ghost" disabled={busy} onClick={() => onDone(null)}>
          {t('next.settings.cancel')}
        </SetButton>
        <SetButton tone={change ? 'primary' : 'danger'} busy={busy} onClick={() => void go()}>
          {t(change ? 'next.settings.agentPage.changeGo' : 'next.settings.agentPage.leaveGo')}
        </SetButton>
      </div>
    </div>
  );
};

export interface IAgentAccountProps {
  settings: AgentSettings;
  credentials: IAgentCredentials;
  /** What is left of the plan; null while the machine has not said. */
  limits: ProviderLimits | null;
  spend: AccountSpend | null;
}

/** «Аккаунт»: the CLI's own sign-in on the machine as a provider card. */
export const AgentAccount = ({ settings, credentials, limits, spend }: IAgentAccountProps) => {
  const { account, provider } = settings;
  const [fold, setFold] = useState<TFold | null>(null);
  const shown = usePresence(fold, FOLD_EXIT_MS);
  const agent = t(`next.settings.section.${provider}`);
  const name = account.signedIn && account.who ? account.who : agent;
  // A sign-in that has just gone through closes its form.
  const [was, setWas] = useState(account.signedIn);
  if (was !== account.signedIn) {
    setWas(account.signedIn);
    if (account.signedIn && fold === FOLD.signIn) setFold(null);
  }
  const open = shown.value;
  return (
    <>
      <SeenAccounts seen={credentials.seen} />
      <Group title={t('next.settings.agentPage.account')} card="hx-pcards hx-apage__account">
        <ProviderCard
          logo={<i className="hx-agent" data-agent={provider} aria-hidden="true" />}
          name={name}
          sub={subOf(settings)}
          state={account.signedIn ? PROVIDER_CARD_STATE.on : PROVIDER_CARD_STATE.off}
          accounts={
            account.signedIn
              ? [{ key: 'main', name, plan: account.plan ?? limits?.plan ?? null, limits, spend }]
              : []
          }
          expanded={fold === FOLD.signIn}
          onOpen={account.signedIn ? undefined : () => setFold(fold === FOLD.signIn ? null : FOLD.signIn)}
          side={
            account.signedIn ? (
              <AccountMenu
                label={t('next.settings.agentPage.menu', { name: agent })}
                onChange={() => setFold(FOLD.change)}
                onLeave={() => setFold(FOLD.leave)}
              />
            ) : (
              <span className="hx-apage__connect" data-hidden={fold === FOLD.signIn || undefined}>
                <SetButton tone="ghost" icon="plus" onClick={() => setFold(FOLD.signIn)}>
                  {t('next.settings.accounts.connect')}
                </SetButton>
              </span>
            )
          }
        >
          {open !== null && (
            <Fold state={shown.state}>
              {open === FOLD.signIn ? (
                <SignInForm settings={settings} credentials={credentials} />
              ) : (
                <LeaveForm
                  who={name}
                  change={open === FOLD.change}
                  onLogout={credentials.onLogout}
                  onDone={setFold}
                />
              )}
            </Fold>
          )}
        </ProviderCard>
      </Group>
    </>
  );
};

/** Where the agent's settings are written, on which machine: small, at the foot of its page. */
export const AgentFileNote = ({ path, machine }: { path: string; machine: string }) => (
  <p className="hx-apage__file">
    {t('next.settings.agentPage.file', { machine })} <code>{path}</code>
  </p>
);
