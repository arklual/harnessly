// The sign-in of an agent CLI on the Settings page: the login that runs on the machine (asked again while it waits for
// the person) and Codex's API key. A code and a key live in the card's field only until the request goes out; what stays
// here is the state of the login. Who is signed in is read from the agent's settings again once a login or a sign-out is
// done.
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  cancelLogin,
  loadLogin,
  logoutAgent,
  putAgentKey,
  sendLoginCode,
  startLogin,
} from '@/next/features/accounts/api';
import type { LoginState, LoginVia } from '@/next/features/accounts/model';
import type { IAgentCredentials } from '@/next/features/settings/accounts-section';
import { t } from '@/next/i18n';
import { noteAgentAccount, refreshAgentAccounts, useMachineAccounts } from '@/next/stores/agent-accounts';
import type { LoginFlow, LoginStatus, MachineId, Provider } from '@/next/protocol/v2';
import type { AgentSettings } from '@/next/protocol/v2';

const POLL_MS = 1500;
const IDLE: LoginState = { k: 'idle' };

/** What a refused request says, in the words of what the person does about it. */
const refusalText = (res: { r?: string; code?: string }, fallback: string): string => {
  if (res.r !== 'refused') return fallback;
  if (res.code === 'conflict') return t('next.login.busy');
  if (res.code === 'rate-limited') return t('next.login.rateLimited');
  if (res.code === 'unsupported') return t('next.login.unsupported');
  return fallback;
};

const failureOf = (flow: LoginFlow): string =>
  flow.reason ? t(`next.login.reason.${flow.reason}`) : t('next.login.failed');

/** The state the card shows for a flow; a finished one is `idle` (who is signed in comes from the settings). */
const loginOf = (flow: LoginFlow | null): LoginState => {
  if (!flow) return IDLE;
  if (flow.state === 'starting') return { k: 'starting' };
  if (flow.state === 'checking') return { k: 'checking' };
  if (flow.state === 'awaiting' && flow.url)
    return { k: 'waiting', url: flow.url, userCode: flow.userCode, asksCode: flow.input === 'code' };
  if (flow.state === 'failed') return { k: 'failed', reason: failureOf(flow) };
  if (flow.state === 'expired') return { k: 'failed', reason: t('next.login.expired') };
  return IDLE;
};
const isFinished = (flow: LoginFlow | null): boolean =>
  flow === null || ['done', 'failed', 'cancelled', 'expired'].includes(flow.state);

/** What the card of one agent needs; `reload` re-reads the agent's settings once the sign-in changed. */
export const useAgentCredentials = (
  machine: MachineId,
  provider: Provider,
  settings: AgentSettings | null,
  reload: () => void,
): IAgentCredentials => {
  const [login, setLogin] = useState<LoginState>(IDLE);
  const seen = useMachineAccounts(machine, provider);
  const who = settings?.account.who ?? null;
  // The cluster keeps which account this machine was signed in with: the one the CLI holds now goes to it.
  useEffect(() => {
    void (who === null ? refreshAgentAccounts() : noteAgentAccount(machine, provider, who));
  }, [machine, provider, who]);
  const flowId = useRef<string | null>(null);
  const running = login.k === 'starting' || login.k === 'waiting' || login.k === 'checking';

  const take = useCallback(
    (status: LoginStatus) => {
      const { flow } = status;
      flowId.current = flow && !isFinished(flow) ? flow.id : null;
      setLogin(loginOf(flow));
      if (flow?.state === 'done') reload();
    },
    [reload],
  );

  useEffect(() => {
    if (!running) return;
    let live = true;
    const timer = setInterval(() => {
      void loadLogin(machine, provider).then((res) => {
        if (live && 'ok' in res) take(res.value);
      });
    }, POLL_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [running, machine, provider, take]);

  const onLogin = useCallback(
    async (via: LoginVia) => {
      setLogin({ k: 'starting' });
      const res = await startLogin(machine, provider, via);
      if (!('ok' in res)) {
        setLogin(IDLE);
        return refusalText(res, t('next.login.startFailed'));
      }
      take(res.value);
      return null;
    },
    [machine, provider, take],
  );
  const onSubmitCode = useCallback(
    async (code: string) => {
      const id = flowId.current;
      if (id === null) return t('next.login.codeFailed');
      const res = await sendLoginCode(machine, provider, id, code);
      if (!('ok' in res)) return refusalText(res, t('next.login.codeFailed'));
      take(res.value);
      return null;
    },
    [machine, provider, take],
  );
  const onCancelLogin = useCallback(async () => {
    await cancelLogin(machine, provider);
    flowId.current = null;
    setLogin(IDLE);
  }, [machine, provider]);
  const onLogout = useCallback(async () => {
    const res = await logoutAgent(machine, provider);
    if (!('ok' in res)) return t('next.login.logoutFailed');
    setLogin(IDLE);
    reload();
    return null;
  }, [machine, provider, reload]);
  const onSaveKey = useCallback(
    async (value: string) => {
      const res = await putAgentKey(machine, provider, value);
      if (!('ok' in res)) return refusalText(res, t('next.providers.saveFailed'));
      reload();
      return null;
    },
    [machine, provider, reload],
  );

  return {
    login,
    seen,
    // The machine does not give a key back, not even its digits: only that the agent signed in with one.
    key: { configured: settings?.account.signedIn === true && settings.account.method === 'key', tail: null },
    onLogin,
    onSubmitCode,
    onCancelLogin,
    onLogout,
    onSaveKey,
  };
};
