import { io, type Socket } from 'socket.io-client';
import type { ITimelineAssistantMessage, ITimelineUserMessage } from '@/types/timeline';
import { reportReconnecting } from '@/lib/network-health';
import { t } from '@/lib/i18n';

export type TOwuiRole = 'user' | 'assistant' | 'system';

export interface IOwuiOutputItem {
  type: string;
  content?: { type?: string; text?: string }[];
  [key: string]: unknown;
}

export interface IOwuiMessage {
  id: string;
  parentId: string | null;
  childrenIds: string[];
  role: TOwuiRole;
  content: string;
  timestamp: number;
  models?: string[];
  model?: string;
  modelName?: string;
  modelIdx?: number;
  done?: boolean;
  error?: { content?: unknown } | null;
  output?: IOwuiOutputItem[];
  usage?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface IOwuiHistory {
  messages: Record<string, IOwuiMessage>;
  currentId: string | null;
}

export type TOwuiParams = Record<string, unknown> & { reasoning_effort?: string | null };

export interface IOwuiChatBody {
  id?: string;
  title: string;
  models: string[];
  params?: TOwuiParams;
  history: IOwuiHistory;
  messages: { role: TOwuiRole; content: string; [key: string]: unknown }[];
  files?: unknown[];
  tags?: string[];
  timestamp?: number;
  [key: string]: unknown;
}

export interface IOwuiChat {
  id: string;
  title: string;
  chat: IOwuiChatBody;
  updated_at: number;
  created_at: number;
  [key: string]: unknown;
}

export interface IOwuiModel {
  id: string;
  name?: string;
  owned_by?: string;
  openai?: { owned_by?: string; reasoning_efforts?: unknown; default_reasoning_effort?: unknown };
}

export interface IOwuiCompletionResult {
  status?: boolean;
  task_ids?: string[];
  chat_id?: string;
}

export interface IOwuiSocketEvent {
  chat_id?: string;
  message_id?: string;
  data?: { type?: string; data?: Record<string, unknown> | null };
}

export interface IChatAssistantEntry extends ITimelineAssistantMessage {
  done: boolean;
  error?: string;
  modelName?: string;
}

export type TChatTimelineEntry = ITimelineUserMessage | IChatAssistantEntry;

export const SUBSCRIPTION_OWNER = 'chatgpt-subscription';
export const DEFAULT_CHAT_TITLE = t('lib.hlOpenwebui.defaultChatTitle');
// Titles Open WebUI and earlier versions of this page gave new chats, in either language: still «no title yet».
const OWUI_DEFAULT_TITLES = new Set([
  'New Chat',
  'Новый чат',
  'Новый диалог',
  'New conversation',
  DEFAULT_CHAT_TITLE,
]);
const KNOWN_EFFORTS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'];
const TITLE_MAX_LENGTH = 60;

export const deriveChatTitle = (text: string, max = TITLE_MAX_LENGTH): string => {
  const line = text.replace(/\s+/g, ' ').trim();
  if (!line) return DEFAULT_CHAT_TITLE;
  if (line.length <= max) return line;
  const cut = line.slice(0, max);
  const boundary = cut.lastIndexOf(' ');
  return `${(boundary > max * 0.6 ? cut.slice(0, boundary) : cut).replace(/[\s,.;:!?-]+$/, '')}…`;
};

export const isDefaultChatTitle = (title: string | undefined) => !title || OWUI_DEFAULT_TITLES.has(title);

export const getOutputText = (output: unknown): string => {
  if (!Array.isArray(output)) return '';
  const texts: string[] = [];
  for (const item of output as IOwuiOutputItem[]) {
    if (!item || item.type !== 'message' || !Array.isArray(item.content)) continue;
    const text = item.content.map((part) => (part && part.text != null ? String(part.text) : '')).join('');
    if (text.trim()) texts.push(text);
  }
  return texts.join('\n');
};

export const getMessageText = (message: Pick<IOwuiMessage, 'content' | 'output'>) =>
  message.content || getOutputText(message.output);

export const getMessageChain = (
  history: IOwuiHistory,
  leafId: string | null = history.currentId,
): IOwuiMessage[] => {
  const chain: IOwuiMessage[] = [];
  const seen = new Set<string>();
  let id = leafId;
  while (id && history.messages[id] && !seen.has(id)) {
    seen.add(id);
    chain.push(history.messages[id]);
    id = history.messages[id].parentId;
  }
  return chain.reverse();
};

export const createMessagesList = (history: IOwuiHistory, leafId: string | null = history.currentId) =>
  getMessageChain(history, leafId).map((message) => ({ ...message, content: getMessageText(message) }));

const toMilliseconds = (timestamp: number) => (timestamp < 1e12 ? timestamp * 1000 : timestamp);

export const errorText = (error: unknown): string => {
  if (!error) return '';
  if (typeof error === 'string') return error;
  if (typeof error !== 'object') return String(error);
  const value = error as Record<string, unknown>;
  const nested = value.content ?? value.detail ?? value.message ?? value.error;
  if (nested !== undefined && nested !== error) return errorText(nested);
  try {
    return JSON.stringify(error);
  } catch {
    return '';
  }
};

export const describeChatError = (error: unknown): string => {
  const raw = errorText(error).trim();
  const text = raw.toLowerCase();
  if (!raw) return t('lib.hlOpenwebui.errorNoAnswer');
  if (
    /failed to fetch|networkerror|network error|load failed|econnrefused|bad gateway|502|503|504/.test(text)
  )
    return t('lib.hlOpenwebui.errorOffline');
  if (/401|unauthorized|not authenticated|invalid token|could not validate/.test(text))
    return t('lib.hlOpenwebui.errorSessionExpired');
  if (/usage limit|rate limit|quota|429|too many requests/.test(text)) return t('lib.hlOpenwebui.errorQuota');
  if (/tools are not supported/.test(text)) return t('lib.hlOpenwebui.errorNoTools');
  if (/model not found/.test(text)) return t('lib.hlOpenwebui.errorModelNotFound');
  if (/timed? ?out|timeout/.test(text)) return t('lib.hlOpenwebui.errorTimeout');
  if (/busy|concurren/.test(text)) return t('lib.hlOpenwebui.errorBusy');
  return t('lib.hlOpenwebui.errorOther', { reason: raw });
};

export const chatToTimeline = (history: IOwuiHistory, models: IOwuiModel[] = []): TChatTimelineEntry[] =>
  getMessageChain(history).flatMap<TChatTimelineEntry>((message) => {
    const timestamp = toMilliseconds(message.timestamp || 0);
    if (message.role === 'user')
      return [{ id: message.id, type: 'user-message', timestamp, text: message.content ?? '' }];
    if (message.role !== 'assistant') return [];
    const model = models.find((item) => item.id === message.model);
    const failure = message.error ? errorText(message.error) : '';
    return [
      {
        id: message.id,
        type: 'assistant-message',
        timestamp,
        markdown: getMessageText(message),
        model: message.model,
        modelName: model?.name ?? message.modelName ?? message.model,
        done: message.done !== false,
        stopReason: failure ? 'error' : null,
        ...(failure ? { error: describeChatError(failure) } : {}),
      },
    ];
  });

interface ITurnOptions {
  userId: string;
  assistantId: string;
  content: string;
  model: string;
  modelName?: string;
  timestamp?: number;
}

const assistantPlaceholder = (
  id: string,
  parentId: string,
  model: string,
  modelName: string | undefined,
  timestamp: number,
): IOwuiMessage => ({
  parentId,
  id,
  childrenIds: [],
  role: 'assistant',
  content: '',
  done: false,
  model,
  modelName: modelName ?? model,
  modelIdx: 0,
  timestamp,
});

export const appendUserTurn = (history: IOwuiHistory, options: ITurnOptions) => {
  const timestamp = options.timestamp ?? Math.floor(Date.now() / 1000);
  const parentId = history.currentId && history.messages[history.currentId] ? history.currentId : null;
  const userMessage: IOwuiMessage = {
    id: options.userId,
    parentId,
    childrenIds: [options.assistantId],
    role: 'user',
    content: options.content,
    timestamp,
    models: [options.model],
  };
  const assistantMessage = assistantPlaceholder(
    options.assistantId,
    options.userId,
    options.model,
    options.modelName,
    timestamp,
  );
  const messages = {
    ...history.messages,
    [userMessage.id]: userMessage,
    [assistantMessage.id]: assistantMessage,
  };
  if (parentId)
    messages[parentId] = {
      ...messages[parentId],
      childrenIds: [...messages[parentId].childrenIds, userMessage.id],
    };
  return { history: { messages, currentId: assistantMessage.id }, userMessage, assistantMessage };
};

export const appendRegeneration = (
  history: IOwuiHistory,
  userMessageId: string,
  options: Omit<ITurnOptions, 'userId' | 'content'>,
) => {
  const parent = history.messages[userMessageId];
  if (!parent || parent.role !== 'user') throw new Error('Regeneration needs a user message');
  const timestamp = options.timestamp ?? Math.floor(Date.now() / 1000);
  const assistantMessage = assistantPlaceholder(
    options.assistantId,
    userMessageId,
    options.model,
    options.modelName,
    timestamp,
  );
  const userMessage: IOwuiMessage = {
    ...parent,
    models: [options.model],
    childrenIds: [...parent.childrenIds, assistantMessage.id],
  };
  return {
    history: {
      messages: {
        ...history.messages,
        [userMessageId]: userMessage,
        [assistantMessage.id]: assistantMessage,
      },
      currentId: assistantMessage.id,
    },
    userMessage,
    assistantMessage,
  };
};

export const patchMessage = (
  history: IOwuiHistory,
  id: string,
  patch: Partial<IOwuiMessage>,
): IOwuiHistory =>
  history.messages[id]
    ? { ...history, messages: { ...history.messages, [id]: { ...history.messages[id], ...patch } } }
    : history;

/** Same shape the backend writes for a chat started from Open WebUI's own composer, including the legacy `messages` seed. */
export const buildNewChatBody = ({
  title,
  model,
  params,
  firstMessage,
}: {
  title: string;
  model: string;
  params?: TOwuiParams;
  firstMessage?: string;
}): IOwuiChatBody => ({
  title,
  models: [model],
  params: params ?? {},
  history: { messages: {}, currentId: null },
  messages: firstMessage ? [{ role: 'user', content: firstMessage }] : [],
  files: [],
  tags: [],
  timestamp: Date.now(),
});

// Model names from older versions of this page end in a Russian "subscription" suffix; both spellings are dropped.
export const shortModelName = (model: IOwuiModel | undefined, fallback = 'ChatGPT') =>
  (model?.name ?? model?.id ?? fallback).replace(/\s*·\s*(подписка|subscription)$/i, '');

export const isSubscriptionModel = (model: IOwuiModel | undefined) =>
  model?.openai?.owned_by === SUBSCRIPTION_OWNER;

export const reasoningOptions = (
  model: IOwuiModel | undefined,
): { efforts: string[]; defaultEffort: string | null } => {
  const provider = model?.openai;
  if (provider?.owned_by !== SUBSCRIPTION_OWNER || !Array.isArray(provider.reasoning_efforts))
    return { efforts: [], defaultEffort: null };
  const efforts = [
    ...new Set(
      provider.reasoning_efforts.filter(
        (effort): effort is string => typeof effort === 'string' && KNOWN_EFFORTS.includes(effort),
      ),
    ),
  ];
  const value = provider.default_reasoning_effort;
  return { efforts, defaultEffort: typeof value === 'string' && efforts.includes(value) ? value : null };
};

export const normalizeReasoningEffort = (model: IOwuiModel | undefined, effort: unknown): string | null =>
  typeof effort === 'string' && reasoningOptions(model).efforts.includes(effort) ? effort : null;

/** Chat params win over user defaults; `undefined` means the chat never chose, `null` means an explicit Auto. */
export const resolveReasoningEffort = (
  chatParams: TOwuiParams | undefined,
  defaultParams: TOwuiParams | undefined,
) =>
  chatParams?.reasoning_effort === undefined ? defaultParams?.reasoning_effort : chatParams.reasoning_effort;

interface ICompletionOptions {
  chatId: string;
  model: IOwuiModel;
  userMessage: IOwuiMessage;
  assistantId: string;
  chatParams?: TOwuiParams;
  defaultParams?: TOwuiParams;
  sessionId?: string;
}

/** Mirrors Open WebUI 0.11.4 `sendMessageSocket` for a persisted chat: the backend loads history and saves both messages itself. */
export const buildCompletionRequest = ({
  chatId,
  model,
  userMessage,
  assistantId,
  chatParams,
  defaultParams,
  sessionId,
}: ICompletionOptions) => ({
  stream: true,
  model: model.id,
  ...(isSubscriptionModel(model)
    ? { reasoning_effort: normalizeReasoningEffort(model, resolveReasoningEffort(chatParams, defaultParams)) }
    : {}),
  params: { ...defaultParams, ...chatParams },
  features: {},
  variables: {},
  ...(sessionId ? { session_id: sessionId } : {}),
  chat_id: chatId,
  id: assistantId,
  message_ids: [{ model_id: model.id, message_id: assistantId, modelIdx: 0 }],
  parent_id: userMessage.parentId ?? null,
  user_message: userMessage,
});

export type TStreamUpdate =
  | { kind: 'append'; text: string }
  | { kind: 'replace'; text: string }
  | { kind: 'error'; error: string; done: boolean }
  | { kind: 'done' }
  | { kind: 'inactive' };

/** Translates Open WebUI socket `events` payloads into the few transitions the chat view cares about. */
export const readStreamEvent = (event: IOwuiSocketEvent): TStreamUpdate[] => {
  const type = event.data?.type;
  const data = event.data?.data ?? {};
  const updates: TStreamUpdate[] = [];
  if (type === 'response:completion') {
    if (data.type === 'response.output_text.delta' && typeof data.delta === 'string')
      updates.push({ kind: 'append', text: data.delta });
    if (data.type === 'response.completed' && data.response && typeof data.response === 'object') {
      const text = getOutputText((data.response as { output?: unknown }).output);
      if (text) updates.push({ kind: 'replace', text });
    }
  } else if (type === 'chat:completion') {
    if (data.output) updates.push({ kind: 'replace', text: getOutputText(data.output) });
    else if (typeof data.content === 'string' && data.content)
      updates.push({ kind: 'replace', text: data.content });
    const choice = Array.isArray(data.choices)
      ? (data.choices[0] as { delta?: { content?: string } } | undefined)
      : undefined;
    if (!data.output && typeof choice?.delta?.content === 'string')
      updates.push({ kind: 'append', text: choice.delta.content });
    if (data.error) updates.push({ kind: 'error', error: errorText(data.error), done: true });
    if (data.done === true) updates.push({ kind: 'done' });
  } else if (type === 'chat:message:delta' || type === 'message') {
    if (typeof data.content === 'string') updates.push({ kind: 'append', text: data.content });
  } else if (type === 'chat:message' || type === 'replace') {
    if (typeof data.content === 'string') updates.push({ kind: 'replace', text: data.content });
  } else if (type === 'chat:message:error') {
    updates.push({ kind: 'error', error: errorText(data.error), done: data.done !== false });
  } else if (type === 'chat:tasks:cancel') {
    updates.push({ kind: 'done' });
  } else if (type === 'chat:active' && data.active === false) {
    updates.push({ kind: 'inactive' });
  }
  return updates;
};

const TOKEN_KEY = 'token';

class OwuiHttpError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

const readStoredToken = () => {
  try {
    return localStorage.getItem(TOKEN_KEY) || '';
  } catch {
    return '';
  }
};

let signInPromise: Promise<string> | null = null;

export const signIn = (): Promise<string> => {
  if (!signInPromise) {
    signInPromise = (async () => {
      const response = await fetch('/api/v1/auths/signin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: '', password: '' }),
      });
      const body = await response.json().catch(() => ({}));
      if (!response.ok || typeof body.token !== 'string')
        throw new OwuiHttpError(
          errorText(body) || t('lib.hlOpenwebui.signInFailed', { status: response.status }),
          response.status,
        );
      try {
        localStorage.setItem(TOKEN_KEY, body.token);
      } catch {}
      return body.token as string;
    })().finally(() => {
      signInPromise = null;
    });
  }
  return signInPromise;
};

export const ensureToken = async () => readStoredToken() || signIn();

const request = async <T>(path: string, init: RequestInit = {}, retry = true): Promise<T> => {
  const token = await ensureToken();
  const response = await fetch(path, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...init.headers,
      authorization: `Bearer ${token}`,
    },
  });
  if (response.status === 401 && retry) {
    try {
      localStorage.removeItem(TOKEN_KEY);
    } catch {}
    await signIn();
    return request<T>(path, init, false);
  }
  const body = await response.json().catch(() => null);
  if (!response.ok)
    throw new OwuiHttpError(
      errorText(body) || t('lib.hlOpenwebui.serviceError', { status: response.status }),
      response.status,
    );
  return body as T;
};

export const listModels = async (): Promise<IOwuiModel[]> => {
  const body = await request<{ data?: IOwuiModel[] }>('/api/models');
  return (body?.data ?? []).filter((model) => model.owned_by !== 'arena');
};

export const getChatDefaults = async (): Promise<{ modelId: string | null; params: TOwuiParams }> => {
  const [settings, config] = await Promise.all([
    request<{ ui?: { models?: string[]; params?: TOwuiParams } } | null>('/api/v1/users/user/settings').catch(
      () => null,
    ),
    request<{ default_models?: string | null }>('/api/config').catch(() => null),
  ]);
  const configured =
    settings?.ui?.models?.find(Boolean) ??
    config?.default_models
      ?.split(',')
      .map((id) => id.trim())
      .find(Boolean) ??
    null;
  return { modelId: configured, params: settings?.ui?.params ?? {} };
};

export const getChat = (id: string) => request<IOwuiChat>(`/api/v1/chats/${encodeURIComponent(id)}`);

export const createChat = (chat: IOwuiChatBody) =>
  request<IOwuiChat>('/api/v1/chats/new', {
    method: 'POST',
    body: JSON.stringify({ chat, folder_id: null }),
  });

export const updateChat = (id: string, chat: Partial<IOwuiChatBody>) =>
  request<IOwuiChat>(`/api/v1/chats/${encodeURIComponent(id)}`, {
    method: 'POST',
    body: JSON.stringify({ chat }),
  });

export const sendCompletion = (payload: ReturnType<typeof buildCompletionRequest>) =>
  request<IOwuiCompletionResult | null>('/api/chat/completions', {
    method: 'POST',
    body: JSON.stringify(payload),
  });

export const stopChat = (id: string) =>
  request<unknown>(`/api/tasks/chat/${encodeURIComponent(id)}/stop`, { method: 'POST' });

export const listChatTasks = async (id: string) =>
  (await request<{ task_ids?: string[] }>(`/api/tasks/chat/${encodeURIComponent(id)}`)).task_ids ?? [];

let sharedSocket: Promise<Socket> | null = null;

export const getChatSocket = (): Promise<Socket> => {
  if (!sharedSocket) {
    sharedSocket = ensureToken().then((token) => {
      const socket = io({
        path: '/ws/socket.io',
        transports: ['websocket', 'polling'],
        tryAllTransports: true,
        reconnection: true,
        reconnectionDelay: 1000,
        reconnectionDelayMax: 5000,
        auth: (cb) => cb({ token: readStoredToken() || token }),
      });
      socket.on('connect', () => reportReconnecting('chat', false));
      // `active` stays true while the manager keeps retrying on its own.
      socket.on('disconnect', () => reportReconnecting('chat', socket.active));
      return socket;
    });
    sharedSocket.catch(() => {
      sharedSocket = null;
    });
  }
  return sharedSocket;
};

export const waitForSocket = (socket: Socket, timeoutMs = 4000) =>
  new Promise<string | null>((resolve) => {
    if (socket.connected && socket.id) return resolve(socket.id);
    const timer = setTimeout(() => {
      socket.off('connect', onConnect);
      resolve(null);
    }, timeoutMs);
    const onConnect = () => {
      clearTimeout(timer);
      resolve(socket.id ?? null);
    };
    socket.once('connect', onConnect);
  });
