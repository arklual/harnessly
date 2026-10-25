// The side chat of a conversation (POST /api/v2/conversations/{id}/side-chat): a fork of its Claude session answers a
// question about it, streamed as server-sent events — delta pieces, then done with the whole answer, or an error. The
// conversation's own feed gets nothing. The page keeps the history and sends it with each question.
import { deviceId } from '@/next/core/device';
import { machineApi } from '@/next/core/net/machine';
import { DEVICE_HEADER, type ConvId, type MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

export type TSideEvent =
  | { type: 'delta'; text: string }
  | { type: 'done'; text: string }
  | { type: 'error'; code: string; message: string };

export interface ISideTurn {
  question: string;
  answer: string;
}

/** Splits what came so far into whole frames (`data: {json}` and a blank line) and what is left of the next one. */
export const readFrames = (buffer: string): { events: TSideEvent[]; rest: string } => {
  const events: TSideEvent[] = [];
  const blocks = buffer.split('\n\n');
  const rest = blocks.pop() ?? '';
  for (const block of blocks) {
    const data = block
      .split('\n')
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trimStart())
      .join('\n');
    if (!data) continue;
    try {
      const doc = JSON.parse(data) as Partial<TSideEvent> & {
        text?: unknown;
        code?: unknown;
        message?: unknown;
      };
      if (doc.type === 'delta' && typeof doc.text === 'string')
        events.push({ type: 'delta', text: doc.text });
      else if (doc.type === 'done' && typeof doc.text === 'string')
        events.push({ type: 'done', text: doc.text });
      else if (doc.type === 'error')
        events.push({
          type: 'error',
          code: String(doc.code ?? 'failed'),
          message: String(doc.message ?? ''),
        });
    } catch {
      // a frame that is not JSON is not ours
    }
  }
  return { events, rest };
};

const failed = (message: string, code = 'failed'): TSideEvent => ({ type: 'error', code, message });

/** Asks and reports the frames as they come; every outcome but the caller's own abort ends in done or error. */
export const askSideChat = async (
  m: MachineId,
  conv: ConvId,
  body: { question: string; history: ISideTurn[] },
  signal: AbortSignal,
  onEvent: (event: TSideEvent) => void,
): Promise<void> => {
  let res: Response;
  try {
    res = await fetch(machineApi(m).http(`/conversations/${encodeURIComponent(conv)}/side-chat`), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { [DEVICE_HEADER]: deviceId(), 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal,
    });
  } catch {
    if (!signal.aborted) onEvent(failed(t('next.failure.network'), 'network'));
    return;
  }
  if (!res.ok || !res.body) {
    const doc: unknown = await res.json().catch(() => null);
    const code =
      typeof doc === 'object' && doc !== null && 'code' in doc
        ? String((doc as { code: unknown }).code)
        : 'failed';
    onEvent(failed(t(`next.side.error.${code}`, { status: res.status }), code));
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let rest = '';
  let ended = false;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const parsed = readFrames(rest + decoder.decode(value, { stream: true }));
      rest = parsed.rest;
      for (const event of parsed.events) {
        if (event.type !== 'delta') ended = true;
        onEvent(event);
      }
    }
  } catch {
    if (!signal.aborted) onEvent(failed(t('next.side.error.interrupted'), 'interrupted'));
    return;
  }
  if (!ended && !signal.aborted) onEvent(failed(t('next.side.error.interrupted'), 'interrupted'));
};
