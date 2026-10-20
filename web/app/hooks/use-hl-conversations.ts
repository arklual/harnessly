import { appConfig } from '@/platform/config';
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { HttpStatusError, jsonRequest } from '@/lib/conversation-loading';
export { jsonRequest } from '@/lib/conversation-loading';
import {
  mergeConversations,
  agentConversationKey,
  type AgentConversation,
  type ChatConversation,
  type Conversation,
} from '@/lib/hl-conversations';
import { createSharedResource, type ISharedResource } from '@/lib/shared-resource';
import { startLatencySample } from '@/lib/network-health';
import { agentActivityKey } from '@/lib/hl-activity-signals';
import useTabStore from '@/hooks/use-tab-store';
import type { ILaunchChoice } from '@/types/hl-launch';
import { keepUnansweredMachines } from '@/lib/hl-fleet-list';
import useSharedResource from '@/hooks/use-shared-resource';
import { t, locale } from '@/lib/i18n';

const SIGN_IN_RETRY_MS = [3_000, 10_000, 30_000, 60_000];
const SIGN_IN_FAILURES_SHOWN = 3;

/** A runtime workspace (a directory) on one runtime; not an organizer project. */
export interface IRuntimeWorkspace {
  id: string;
  name: string;
  machineId?: string;
}

interface IAgentsResponse {
  conversations: AgentConversation[];
  workspaces: IRuntimeWorkspace[];
  /** Fleet gateway only: the machine new conversations go to when none is picked. */
  defaultMachineId?: string;
  /** Fleet gateway only: machines that did not answer this round. */
  errors?: { machineId: string; error: string }[];
  /** Fleet gateway only: slow machines shown from their previous answer, `ageMs` old. */
  stale?: { machineId: string; ageMs: number }[];
}

const CHAT_PAGE_SIZE = 20;
const NO_WORKSPACES: IRuntimeWorkspace[] = [];
const NO_AGENTS: AgentConversation[] = [];
const NO_FLEET_ERRORS: string[] = [];

const POLL = {
  /** Status-socket messages announce agent changes on the default machine; the timer only covers other machines. */
  agentsMs: 60_000,
  /** Chats change from this page (upsertChat) or from another device. */
  chatsMs: 60_000,
  maxBackoffMs: 5 * 60_000,
  /** Merges a burst of status messages into one list refresh. */
  signalDelayMs: 1_500,
  /** A fleet list that lacked a machine is followed up once this soon: the gateway keeps the late answer for it. */
  partialFollowUpMs: 4_000,
  /** The gateway may hold a list up to its runtime deadline (8 s) while no machine has answered yet. */
  agentsStallMs: 10_000,
} as const;

/** Signal-driven loads follow a change the gateway's short list cache has not seen. */
const FRESH_LIST_HEADERS = { 'Cache-Control': 'no-cache' } as const;

export const chatHeaders = (): Record<string, string> => ({
  authorization: `Bearer ${localStorage.getItem('token') || ''}`,
});

const errorText = (error: unknown) => (error instanceof Error ? error.message : '');

let signInFlight: Promise<void> | null = null;

const signInToChats = (): Promise<void> => {
  if (!appConfig().features.chats) return Promise.resolve();
  signInFlight ??= (async () => {
    const sample = startLatencySample({ via: 'gateway' });
    try {
      const user = await jsonRequest<{ token: string }>('/api/v1/auths/signin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: '', password: '' }),
      });
      sample.finish();
      localStorage.setItem('token', user.token);
    } finally {
      sample.cancel();
    }
  })().finally(() => {
    signInFlight = null;
  });
  return signInFlight;
};

const isPartial = (body: IAgentsResponse) => Boolean(body.errors?.length || body.stale?.length);

/** One early load after a partial list, then the regular pace again even if the machine is still slow. */
let followUpSoon = false;

/** Module-level: every consumer shares one request stream instead of each polling on its own. */
const agentsSource: ISharedResource<IAgentsResponse> = createSharedResource<IAgentsResponse>({
  load: async (signal, cause): Promise<IAgentsResponse> => {
    const sample = startLatencySample({ stallAfterMs: POLL.agentsStallMs });
    try {
      const body = await jsonRequest<IAgentsResponse>('/agents/api/platform/conversations', {
        signal,
        headers: cause === 'signal' ? FRESH_LIST_HEADERS : undefined,
      });
      const partial = isPartial(body);
      // A partial fleet answer waited for a slow machine: its time says nothing about this network.
      if (!partial) sample.finish();
      followUpSoon = partial && !followUpSoon;
      return keepUnansweredMachines(agentsSource.getSnapshot().data, body);
    } finally {
      sample.cancel();
    }
  },
  describeError: (error) => t('hooks.useHlConversations.sessionsFailed', { reason: errorText(error) }),
  intervalMs: () => (followUpSoon ? POLL.partialFollowUpMs : POLL.agentsMs),
  maxBackoffMs: POLL.maxBackoffMs,
});

/** First page only; older pages are fetched on demand and merged under it. */
const chatsSource = createSharedResource<ChatConversation[]>({
  load: async (signal) => {
    // Plain chats exist only where an OpenWebUI is configured (features.chats).
    if (!appConfig().features.chats) return [];
    // Chats live in the cluster next to the gateway: their time is the network's, not a machine's.
    const sample = startLatencySample({ via: 'gateway' });
    try {
      const chats = await jsonRequest<ChatConversation[]>(
        '/api/v1/chats/?page=1&include_pinned=true&include_folders=true',
        { signal, headers: chatHeaders() },
      );
      sample.finish();
      return chats;
    } finally {
      sample.cancel();
    }
  },
  describeError: (error) => t('hooks.useHlConversations.chatsFailed', { reason: errorText(error) }),
  intervalMs: POLL.chatsMs,
  maxBackoffMs: POLL.maxBackoffMs,
});

/** Another machine's frame reported its agents changing: its list rows (titles, new tabs) follow shortly. */
export const pokeAgentList = () => agentsSource.poke(POLL.signalDelayMs);

const subscribeNothing = () => () => undefined;

export interface ICreateAgentOptions {
  /** Sent as `model` and `effort`; omitted means "Auto" for both. */
  launch?: ILaunchChoice | null;
}

const useHlConversations = () => {
  const [chatReady, setChatReady] = useState(false);
  const [signInError, setSignInError] = useState('');
  /** This origin serves no chats (a runtime opened directly, not through the fleet gateway). */
  const [chatsAbsent, setChatsAbsent] = useState(false);
  const [actionError, setError] = useState('');
  const [older, setOlder] = useState<ChatConversation[]>([]);
  const [page, setPage] = useState(1);
  const [olderHasMore, setOlderHasMore] = useState(false);

  const agentsState = useSharedResource(agentsSource);
  const chatsState = useSyncExternalStore(
    chatReady ? chatsSource.subscribe : subscribeNothing,
    chatsSource.getSnapshot,
    chatsSource.getServerSnapshot,
  );

  // A gateway restarting under the page (a release) makes one sign-in fail: it is tried again on its own, and
  // the red strip shows only when chats stay unreachable, not for a blink nobody can act on.
  const signInFailures = useRef(0);
  const signInRetry = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [signInAttempt, setSignInAttempt] = useState(0);
  const signIn = useCallback(() => {
    if (signInRetry.current) {
      clearTimeout(signInRetry.current);
      signInRetry.current = null;
    }
    signInToChats()
      .then(() => {
        signInFailures.current = 0;
        setSignInError('');
        setChatReady(true);
      })
      .catch((e: unknown) => {
        // A runtime opened by its own address has no Open WebUI behind it: chats are simply not here, nothing failed.
        if (e instanceof HttpStatusError && e.status === 404) {
          setChatsAbsent(true);
          return;
        }
        signInFailures.current += 1;
        if (signInFailures.current >= SIGN_IN_FAILURES_SHOWN)
          setSignInError(
            t('hooks.useHlConversations.signInFailed', {
              reason: e instanceof Error ? e.message : t('hooks.useHlConversations.serviceUnavailable'),
            }),
          );
        const delay = SIGN_IN_RETRY_MS[Math.min(signInFailures.current - 1, SIGN_IN_RETRY_MS.length - 1)];
        signInRetry.current = setTimeout(() => setSignInAttempt((n) => n + 1), delay);
      });
  }, []);
  useEffect(
    () => () => {
      if (signInRetry.current) clearTimeout(signInRetry.current);
    },
    [],
  );

  useEffect(() => {
    localStorage.setItem('locale', locale() === 'ru' ? 'ru-RU' : 'en-US');
    signIn();
  }, [signIn, signInAttempt]);

  // The status socket already reports new prompts, titles and tabs on the default machine: refresh on that, not on a timer.
  useEffect(() => {
    const initial = useTabStore.getState().tabs;
    let last = Object.keys(initial).length ? agentActivityKey(initial) : null;
    return useTabStore.subscribe((state, previous) => {
      if (state.tabs === previous.tabs) return;
      const next = agentActivityKey(state.tabs);
      if (last !== null && next !== last) agentsSource.poke(POLL.signalDelayMs);
      last = next;
    });
  }, []);

  const refresh = useCallback(async () => {
    if (!chatReady) signIn();
    await Promise.all([agentsSource.refresh(), chatReady ? chatsSource.refresh() : undefined]);
  }, [chatReady, signIn]);

  const loadMore = useCallback(async () => {
    try {
      const next = await jsonRequest<ChatConversation[]>(
        `/api/v1/chats/?page=${page + 1}&include_pinned=true&include_folders=true`,
        { headers: chatHeaders() },
      );
      setOlder((current) => [...current, ...next.filter((n) => !current.some((c) => c.id === n.id))]);
      setPage(page + 1);
      setOlderHasMore(next.length >= CHAT_PAGE_SIZE);
    } catch (e) {
      setError(e instanceof Error ? e.message : t('hooks.useHlConversations.historyFailed'));
    }
  }, [page]);

  const createAgent = useCallback(
    async (
      provider: string,
      workspaceId?: string,
      prompt?: string,
      machineId?: string,
      options: ICreateAgentOptions = {},
    ) => {
      const launch = options.launch ? { model: options.launch.model, effort: options.launch.effort } : {};
      const item = await jsonRequest<AgentConversation>('/agents/api/platform/conversations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider,
          machineId,
          workspaceId: workspaceId || undefined,
          prompt: prompt || undefined,
          ...launch,
        }),
      });
      const owner = item.machineId || machineId;
      // The new conversation opens from this answer and takes its row at once; the list catches up in the background.
      agentsSource.update((current) =>
        current &&
        !current.conversations.some((c) => c.id === item.id && (c.machineId || undefined) === owner)
          ? {
              ...current,
              conversations: [
                { ...item, machineId: owner, updatedAt: item.updatedAt || Date.now() },
                ...current.conversations,
              ],
            }
          : current,
      );
      void agentsSource.refresh();
      return {
        ...item,
        machineId: owner,
        key: agentConversationKey(item.id, owner),
        kind: 'agent' as const,
      } satisfies Conversation;
    },
    [],
  );

  /** A conversation started outside createAgent joins the list without waiting for the next poll. */
  const refreshAgents = useCallback(() => agentsSource.refresh(), []);

  const upsertChat = useCallback((chat: { id: string; title: string }) => {
    chatsSource.update((current) => [
      { id: chat.id, title: chat.title, updated_at: Math.floor(Date.now() / 1000) },
      ...(current ?? []).filter((c) => c.id !== chat.id),
    ]);
    // A poll already in flight may predate this chat; the trailing run brings the server's version.
    void chatsSource.refresh();
  }, []);

  const fresh = chatsState.data;
  const chats = useMemo(() => {
    if (!fresh) return older;
    return [...fresh, ...older.filter((c) => !fresh.some((n) => n.id === c.id))];
  }, [fresh, older]);
  const agents = agentsState.data?.conversations ?? NO_AGENTS;
  const conversations = useMemo(() => mergeConversations(chats, agents), [chats, agents]);
  const hasMore = page === 1 ? (fresh?.length ?? 0) >= CHAT_PAGE_SIZE : olderHasMore;
  // Rows show up as soon as either list answers; the other one joins when it arrives.
  const loading = agentsState.data === null && agentsState.error === null && fresh === null;
  const error = actionError || signInError || agentsState.error || chatsState.error || '';
  const defaultMachineId = agentsState.data?.defaultMachineId;
  // Stale entries change the answer on every poll; the joined text keeps the array stable while the failures stay the same.
  const fleetErrorsText = (agentsState.data?.errors ?? [])
    .map((e) => `${e.machineId}: ${e.error}`)
    .join('\n');
  const fleetErrors = useMemo(
    () => (fleetErrorsText ? fleetErrorsText.split('\n') : NO_FLEET_ERRORS),
    [fleetErrorsText],
  );

  return {
    conversations,
    defaultMachineId,
    fleetErrors,
    chats,
    workspaces: agentsState.data?.workspaces ?? NO_WORKSPACES,
    /** Agent lists answered at least once: a pinned agent missing from them is closed, not loading. */
    agentsLoaded: agentsState.data !== null,
    chatReady,
    chatsAbsent,
    hasMore,
    loading,
    error,
    setError,
    refresh,
    loadMore,
    createAgent,
    refreshAgents,
    upsertChat,
  };
};

export default useHlConversations;
