import { appFetch, scopedAppPath } from '@/lib/app-path';
import {
  SIDE_CHAT_ENDPOINT,
  parseSideChatStream,
  type ISideChatRequest,
  type TSideChatErrorCode,
  type TSideChatEvent,
} from '@/lib/hl-side-chat';
import { t } from '@/lib/i18n';

export type TSideChatBody = Omit<ISideChatRequest, 'model'> & { model?: string };

const failure = (message: string, code: TSideChatErrorCode = 'failed'): TSideChatEvent => ({
  type: 'error',
  code,
  message,
});

const refusal = async (response: Response): Promise<TSideChatEvent> => {
  const body: unknown = await response.json().catch(() => null);
  if (typeof body === 'object' && body !== null && typeof (body as { error?: unknown }).error === 'string') {
    const { error, code } = body as { error: string; code?: TSideChatErrorCode };
    return failure(error, code);
  }
  return failure(t('lib.hlSideChatClient.serverError', { status: response.status }));
};

/**
 * Asks the question and reports events as they arrive. Every outcome except an abort by the caller ends with an `error`
 * or a `done` event; an abort is silent, the caller knows it asked for it. `machineId` names the machine the tab lives
 * on when the caller is the shell, which is not inside that machine's frame.
 */
export const streamSideChat = async (
  body: TSideChatBody,
  signal: AbortSignal,
  onEvent: (event: TSideChatEvent) => void,
  machineId?: string,
): Promise<void> => {
  let response: Response;
  try {
    response = await appFetch(scopedAppPath(SIDE_CHAT_ENDPOINT, machineId), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal,
    });
  } catch {
    if (!signal.aborted) onEvent(failure(t('lib.hlSideChatClient.offline')));
    return;
  }
  if (!response.ok || !response.body) {
    onEvent(await refusal(response));
    return;
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let rest = '';
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const parsed = parseSideChatStream(rest + decoder.decode(value, { stream: true }));
      rest = parsed.rest;
      for (const event of parsed.events) onEvent(event);
    }
  } catch {
    if (!signal.aborted) onEvent(failure(t('lib.hlSideChatClient.interrupted')));
  }
};
