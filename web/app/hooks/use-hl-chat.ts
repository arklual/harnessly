import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import {
  appendRegeneration,
  appendUserTurn,
  buildCompletionRequest,
  buildNewChatBody,
  chatToTimeline,
  createChat,
  deriveChatTitle,
  describeChatError,
  errorText,
  getChat,
  getChatDefaults,
  getChatSocket,
  getMessageChain,
  isDefaultChatTitle,
  isSubscriptionModel,
  listChatTasks,
  listModels,
  normalizeReasoningEffort,
  patchMessage,
  readStreamEvent,
  reasoningOptions,
  resolveReasoningEffort,
  sendCompletion,
  stopChat,
  updateChat,
  waitForSocket,
  type IOwuiChat,
  type IOwuiHistory,
  type IOwuiMessage,
  type IOwuiModel,
  type IOwuiSocketEvent,
  type TOwuiParams,
} from '@/lib/hl-openwebui';
import { t } from '@/lib/i18n';

export interface IHlChatCallbacks {
  onChatCreated?: (chat: { id: string; title: string }) => void;
  onTitleChange?: (id: string, title: string) => void;
}

interface IChatMeta {
  id: string;
  title: string;
}

interface IStreamTarget {
  chatId: string;
  messageId: string;
}

const EMPTY_HISTORY: IOwuiHistory = { messages: {}, currentId: null };
const POLL_INTERVAL_MS = 3000;
const SOCKET_QUIET_MS = 2500;

let catalogPromise: Promise<{
  models: IOwuiModel[];
  defaults: { modelId: string | null; params: TOwuiParams };
}> | null = null;
const loadCatalog = () => {
  if (!catalogPromise) {
    catalogPromise = Promise.all([listModels(), getChatDefaults()]).then(([models, defaults]) => ({
      models,
      defaults,
    }));
    catalogPromise.catch(() => {
      catalogPromise = null;
    });
  }
  return catalogPromise;
};

const newId = () => crypto.randomUUID();

const pickModelId = (models: IOwuiModel[], ...candidates: (string | null | undefined)[]) =>
  candidates.find((id) => id && models.some((model) => model.id === id)) ??
  models.find(isSubscriptionModel)?.id ??
  models[0]?.id ??
  null;

const leafOf = (history: IOwuiHistory): IOwuiMessage | undefined =>
  history.currentId ? history.messages[history.currentId] : undefined;

const useHlChat = (chatId: string | null, { onChatCreated, onTitleChange }: IHlChatCallbacks = {}) => {
  const [models, setModels] = useState<IOwuiModel[]>([]);
  const [defaultParams, setDefaultParams] = useState<TOwuiParams>({});
  const [chat, setChat] = useState<IChatMeta | null>(null);
  const [history, setHistory] = useState<IOwuiHistory>(EMPTY_HISTORY);
  const [params, setParams] = useState<TOwuiParams>({});
  const [modelId, setModelId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [streamingId, setStreamingId] = useState<string | null>(null);
  const [savingParams, setSavingParams] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);

  // The socket handler and the poller outlive renders, so they read the live target through refs.
  const streamRef = useRef<IStreamTarget | null>(null);
  const lastEventAtRef = useRef(0);
  const createdChatIdRef = useRef<string | null>(null);

  const beginStream = useCallback((target: IStreamTarget) => {
    streamRef.current = target;
    lastEventAtRef.current = Date.now();
    setStreamingId(target.messageId);
  }, []);

  const endStream = useCallback(() => {
    streamRef.current = null;
    setStreamingId(null);
  }, []);

  const applyServerChat = useCallback((server: IOwuiChat) => {
    setHistory(server.chat?.history?.messages ? server.chat.history : EMPTY_HISTORY);
    setParams(server.chat?.params ?? {});
    setChat({ id: server.id, title: server.title || server.chat?.title });
  }, []);

  const finishStream = useCallback(
    async (target: IStreamTarget) => {
      if (streamRef.current?.messageId !== target.messageId) return;
      try {
        const server = await getChat(target.chatId);
        if (streamRef.current?.messageId !== target.messageId) return;
        if (server.chat?.history?.messages?.[target.messageId]) setHistory(server.chat.history);
        else setHistory((current) => patchMessage(current, target.messageId, { done: true }));
      } catch {
        setHistory((current) => patchMessage(current, target.messageId, { done: true }));
      }
      if (streamRef.current?.messageId === target.messageId) endStream();
    },
    [endStream],
  );

  useEffect(() => {
    if (chatId && chatId === createdChatIdRef.current) return;
    let cancelled = false;
    createdChatIdRef.current = null;
    endStream();
    setLoading(true);
    setLoadError(null);
    (async () => {
      try {
        const [{ models: catalog, defaults }, server] = await Promise.all([
          loadCatalog(),
          chatId ? getChat(chatId) : Promise.resolve(null),
        ]);
        if (cancelled) return;
        setModels(catalog);
        setDefaultParams(defaults.params);
        if (!server) {
          setChat(null);
          setHistory(EMPTY_HISTORY);
          setParams({});
          setModelId((current) => pickModelId(catalog, current, defaults.modelId));
          return;
        }
        applyServerChat(server);
        const leaf = leafOf(server.chat?.history ?? EMPTY_HISTORY);
        setModelId(pickModelId(catalog, server.chat?.models?.[0], leaf?.model, defaults.modelId));
        if (
          leaf?.role === 'assistant' &&
          leaf.done === false &&
          (await listChatTasks(server.id).catch(() => [])).length > 0 &&
          !cancelled
        ) {
          beginStream({ chatId: server.id, messageId: leaf.id });
        }
      } catch (error) {
        if (!cancelled)
          setLoadError(
            chatId
              ? t('hooks.useHlChat.openFailed', { reason: describeChatError(error) })
              : describeChatError(error),
          );
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [chatId, reloadKey, applyServerChat, beginStream, endStream]);

  useEffect(() => {
    let disposed = false;
    let detach: (() => void) | undefined;
    const handle = (event: IOwuiSocketEvent) => {
      const target = streamRef.current;
      if (!target || event.chat_id !== target.chatId || event.message_id !== target.messageId) return;
      lastEventAtRef.current = Date.now();
      for (const update of readStreamEvent(event)) {
        if (update.kind === 'append') {
          setHistory((current) => {
            const message = current.messages[target.messageId];
            return message
              ? patchMessage(current, target.messageId, { content: (message.content ?? '') + update.text })
              : current;
          });
        } else if (update.kind === 'replace') {
          setHistory((current) => patchMessage(current, target.messageId, { content: update.text }));
        } else if (update.kind === 'error') {
          setHistory((current) =>
            patchMessage(current, target.messageId, { error: { content: update.error }, done: update.done }),
          );
          if (update.done) void finishStream(target);
        } else {
          void finishStream(target);
        }
      }
    };
    getChatSocket()
      .then((socket) => {
        if (disposed) return;
        socket.on('events', handle);
        detach = () => socket.off('events', handle);
      })
      .catch(() => {});
    return () => {
      disposed = true;
      detach?.();
    };
  }, [finishStream]);

  useEffect(() => {
    if (!streamingId) return;
    // The whole chat comes back on every poll: on a slow link one must finish before the next starts.
    let polling = false;
    const timer = setInterval(async () => {
      const target = streamRef.current;
      if (polling || document.hidden || !target || Date.now() - lastEventAtRef.current < SOCKET_QUIET_MS)
        return;
      polling = true;
      try {
        const server = await getChat(target.chatId);
        const message = server.chat?.history?.messages?.[target.messageId];
        if (!message || streamRef.current?.messageId !== target.messageId) return;
        if (message.done === false) {
          if (message.content)
            setHistory((current) => patchMessage(current, target.messageId, { content: message.content }));
          return;
        }
        setHistory(server.chat.history);
        endStream();
      } catch {
      } finally {
        polling = false;
      }
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [streamingId, endStream]);

  const model = useMemo(() => models.find((item) => item.id === modelId), [models, modelId]);
  const effortOptions = useMemo(() => reasoningOptions(model), [model]);
  const effort = normalizeReasoningEffort(model, resolveReasoningEffort(params, defaultParams));
  const entries = useMemo(() => chatToTimeline(history, models), [history, models]);

  const dispatch = useCallback(
    async (
      target: IStreamTarget,
      userMessage: IOwuiMessage,
      turnModel: IOwuiModel,
      chatParams: TOwuiParams,
    ) => {
      try {
        const socket = await getChatSocket().catch(() => null);
        const sessionId = socket ? await waitForSocket(socket) : null;
        const result = await sendCompletion(
          buildCompletionRequest({
            chatId: target.chatId,
            model: turnModel,
            userMessage,
            assistantId: target.messageId,
            chatParams,
            defaultParams,
            sessionId: sessionId ?? undefined,
          }),
        );
        if (!sessionId || !result?.task_ids?.length) await finishStream(target);
      } catch (error) {
        setHistory((current) =>
          patchMessage(current, target.messageId, {
            error: { content: errorText(error) || 'Failed to fetch' },
            done: true,
          }),
        );
        if (streamRef.current?.messageId === target.messageId) endStream();
      }
    },
    [defaultParams, finishStream, endStream],
  );

  const send = useCallback(
    async (text: string) => {
      const content = text.trim();
      if (!content || streamingId || !model) return false;
      let meta = chat;
      try {
        if (!meta) {
          const title = deriveChatTitle(content);
          const created = await createChat(
            buildNewChatBody({ title, model: model.id, params, firstMessage: content }),
          );
          meta = { id: created.id, title: created.title || title };
          createdChatIdRef.current = created.id;
          setChat(meta);
          onChatCreated?.(meta);
        } else if (isDefaultChatTitle(meta.title) && getMessageChain(history).length === 0) {
          const title = deriveChatTitle(content);
          await updateChat(meta.id, { title });
          meta = { ...meta, title };
          setChat(meta);
          onTitleChange?.(meta.id, title);
        }
      } catch (error) {
        toast.error(t('hooks.useHlChat.createFailed', { reason: describeChatError(error) }));
        return false;
      }
      const turn = appendUserTurn(history, {
        userId: newId(),
        assistantId: newId(),
        content,
        model: model.id,
        modelName: model.name,
      });
      const target = { chatId: meta.id, messageId: turn.assistantMessage.id };
      setHistory(turn.history);
      beginStream(target);
      void dispatch(target, turn.userMessage, model, params);
      return true;
    },
    [chat, history, model, params, streamingId, beginStream, dispatch, onChatCreated, onTitleChange],
  );

  const regenerate = useCallback(() => {
    const leaf = leafOf(history);
    const userMessageId = leaf?.role === 'assistant' ? leaf.parentId : leaf?.role === 'user' ? leaf.id : null;
    if (!chat || streamingId || !model || !userMessageId) return;
    const turn = appendRegeneration(history, userMessageId, {
      assistantId: newId(),
      model: model.id,
      modelName: model.name,
    });
    const target = { chatId: chat.id, messageId: turn.assistantMessage.id };
    setHistory(turn.history);
    beginStream(target);
    void dispatch(target, turn.userMessage, model, params);
  }, [chat, history, model, params, streamingId, beginStream, dispatch]);

  const stop = useCallback(async () => {
    const target = streamRef.current;
    if (!target) return;
    try {
      await stopChat(target.chatId);
    } catch (error) {
      toast.error(t('hooks.useHlChat.stopFailed', { reason: describeChatError(error) }));
      return;
    }
    setTimeout(() => void finishStream(target), 600);
  }, [finishStream]);

  const selectModel = useCallback(
    (id: string) => {
      setModelId(id);
      if (!chat) return;
      updateChat(chat.id, { models: [id] }).catch((error) =>
        toast.error(t('hooks.useHlChat.modelSaveFailed', { reason: describeChatError(error) })),
      );
    },
    [chat],
  );

  const selectEffort = useCallback(
    async (value: string | null) => {
      const next = { ...params, reasoning_effort: value };
      setParams(next);
      if (!chat) return;
      setSavingParams(true);
      try {
        await updateChat(chat.id, { params: next });
      } catch {
        toast.error(t('hooks.useHlChat.effortSaveFailed'));
      } finally {
        setSavingParams(false);
      }
    },
    [chat, params],
  );

  const reload = useCallback(() => {
    createdChatIdRef.current = null;
    setReloadKey((key) => key + 1);
  }, []);

  return {
    chat,
    entries,
    models,
    model,
    effort,
    effortOptions,
    loading,
    loadError,
    streaming: streamingId !== null,
    streamingId,
    savingParams,
    send,
    stop,
    regenerate,
    selectModel,
    selectEffort,
    reload,
  };
};

export default useHlChat;
