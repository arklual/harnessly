import { t } from '@/lib/i18n';

const READ_TIMEOUT_MS = 20_000;
const MUTATION_TIMEOUT_MS = 120_000;

/** A non-2xx answer; the status tells a missing service (404) from a failing one. */
export class HttpStatusError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

/** Bound the entire response, including a body that never finishes downloading. */
export const jsonRequest = async <T>(url: string, init?: RequestInit): Promise<T> => {
  const controller = new AbortController();
  const abort = () => controller.abort(init?.signal?.reason);
  init?.signal?.addEventListener('abort', abort, { once: true });
  const isRead = !init?.method || ['GET', 'HEAD'].includes(init.method.toUpperCase());
  const timer = setTimeout(
    () =>
      controller.abort(
        new Error(
          isRead
            ? t('lib.conversationLoading.readTimeout')
            : t('lib.conversationLoading.mutationUnconfirmed'),
        ),
      ),
    isRead ? READ_TIMEOUT_MS : MUTATION_TIMEOUT_MS,
  );
  let onAbort: () => void = () => {};
  const interrupted = new Promise<never>((_resolve, reject) => {
    onAbort = () => reject(controller.signal.reason);
    controller.signal.addEventListener('abort', onAbort, { once: true });
  });
  if (init?.signal?.aborted) abort();
  try {
    return await Promise.race([
      interrupted,
      (async () => {
        const response = await fetch(url, { ...init, signal: controller.signal });
        if (!response.ok) {
          const body = await response.json().catch(() => ({}));
          throw new HttpStatusError(
            body.error ||
              body.detail ||
              t('lib.conversationLoading.unavailable', { status: response.status }),
            response.status,
          );
        }
        return response.json() as Promise<T>;
      })(),
    ]);
  } finally {
    clearTimeout(timer);
    init?.signal?.removeEventListener('abort', abort);
    controller.signal.removeEventListener('abort', onAbort);
  }
};

interface ConversationLoaderOptions<A, C> {
  loadAgents: (signal: AbortSignal) => Promise<A>;
  loadChats: (signal: AbortSignal) => Promise<C | null>;
  onAgents: (value: A) => void;
  onChats: (value: C) => void;
  onError: (source: 'agents' | 'chats', error: unknown) => void;
  onSettled: () => void;
}

/** Publish each service independently; deduplicate polling and invalidate old consumers. */
export const createConversationLoader = <A, C>(options: ConversationLoaderOptions<A, C>) => {
  const controller = new AbortController();
  let disposed = false;
  let inFlight: Promise<void> | undefined;
  const receive = async <T>(
    source: 'agents' | 'chats',
    request: Promise<T | null>,
    publish: (value: T) => void,
  ) => {
    try {
      const value = await request;
      if (!disposed && value !== null) {
        publish(value);
        options.onSettled();
      }
    } catch (error) {
      if (!disposed) {
        options.onError(source, error);
        options.onSettled();
      }
    }
  };
  return {
    refresh: (): Promise<void> => {
      if (disposed) return Promise.resolve();
      if (inFlight) return inFlight;
      inFlight = Promise.all([
        receive('agents', options.loadAgents(controller.signal), options.onAgents),
        receive('chats', options.loadChats(controller.signal), options.onChats),
      ])
        .then(() => {})
        .finally(() => {
          inFlight = undefined;
        });
      return inFlight;
    },
    dispose: () => {
      disposed = true;
      controller.abort();
    },
  };
};
