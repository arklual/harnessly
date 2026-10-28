// The login card of a chat whose last turn failed because the agent's sign-in ran out: the login runs on the chat's
// machine for the chat's agent; once it went through, the card sends the turn's message again.
import { useCallback, useEffect, useState } from 'react';
import { useAgentCredentials } from '@/next/bindings/settings-credentials';
import LoginCard, { isLoginLost } from '@/next/features/chat/login-card';
import type { FeedItem, TurnItem } from '@/next/features/chat/types';
import { loadAgentSettings } from '@/next/features/settings/api';
import type { AgentSettings, MachineId, Provider } from '@/next/protocol/v2';

/** The last turn of the feed when it ended on a lost sign-in; null otherwise (a newer message hides the card). */
export const loginLostTurn = (items: readonly FeedItem[] | null): TurnItem | null => {
  const last = items?.at(-1);
  if (last?.kind !== 'turn' || last.turn.status === 'live') return null;
  if (isLoginLost(last.turn.failure?.detail)) return last;
  // The agent's own error notice as the turn's last step: «Login expired · Please run /login».
  const end = last.steps?.at(-1);
  return end?.t === 'notice' && end.level === 'error' && isLoginLost(end.detail) ? last : null;
};

interface IChatLoginProps {
  machine: MachineId;
  provider: Provider;
  onRetry: (() => Promise<void>) | null;
}

/** Keyed by the failed turn: a later failure starts a card of its own. The accounts this machine was signed in with
 *  come from the cluster; the agent's settings are read when the card opens and again once the login went through,
 *  so the account it signed in with reaches the cluster too. */
const ChatLogin = ({ machine, provider, onRetry }: IChatLoginProps) => {
  const [done, setDone] = useState(false);
  const [settings, setSettings] = useState<AgentSettings | null>(null);
  const onDone = useCallback(() => setDone(true), []);
  const credentials = useAgentCredentials(machine, provider, settings, onDone);
  useEffect(() => {
    let live = true;
    void loadAgentSettings(machine, provider).then((res) => {
      if (live && 'ok' in res) setSettings(res.value);
    });
    return () => {
      live = false;
    };
  }, [machine, provider, done]);
  return (
    <LoginCard
      provider={provider}
      login={credentials.login}
      actions={credentials}
      done={done}
      seen={credentials.seen}
      onRetry={onRetry}
    />
  );
};

export default ChatLogin;
