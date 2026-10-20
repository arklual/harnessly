/**
 * What a queued message's bubble shows between the click on «Send now» and the transcript taking the
 * message: sending, handed over (the server is done, the transcript is not yet), or told to wait for the next step.
 */
import { t } from '@/lib/i18n';
export type TSendNowPhase = 'sending' | 'handed' | 'waits';

export interface ISendNowPhaseEntry {
  phase: TSendNowPhase;
  /** When the phase stops showing; null while the request runs. */
  until: number | null;
}

export const HANDED_SHOWN_MS = 20_000;
export const WAITS_SHOWN_MS = 8_000;

export const sendNowKey = (tabId: string, text: string): string =>
  `${tabId}\u0000${text.replace(/\s+/g, ' ').trim()}`;

export interface ISendNowReply {
  status: number;
  body: { outcome?: string; error?: string; waits?: boolean } | null;
}

export type TSendNowVerdict =
  | { kind: 'handed'; outcome: string | null }
  | { kind: 'waits'; message: string }
  | { kind: 'failed'; message: string };

export const interpretSendNow = ({ status, body }: ISendNowReply): TSendNowVerdict => {
  if (status >= 200 && status < 300) return { kind: 'handed', outcome: body?.outcome ?? null };
  const message = body?.error ?? t('lib.hlSendNowClient.failed', { status });
  return body?.waits ? { kind: 'waits', message } : { kind: 'failed', message };
};

/** A phase counts while its time has not run out; a request in flight has none. */
export const livePhase = (entry: ISendNowPhaseEntry | undefined, now: number): TSendNowPhase | null =>
  entry && (entry.until === null || entry.until > now) ? entry.phase : null;
