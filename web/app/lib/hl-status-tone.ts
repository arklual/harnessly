import type { TProviderStatus } from '@/components/agents/provider-icon';

/** One visual language for a conversation's state, in the collapsed rail, the list, toasts and the header. */
export const STATUS_TONE = {
  working: 'working',
  waiting: 'waiting',
  done: 'done',
  failed: 'failed',
  offline: 'offline',
} as const;

export type TStatusTone = (typeof STATUS_TONE)[keyof typeof STATUS_TONE];

const TONES: Record<TProviderStatus, TStatusTone | null> = {
  idle: null,
  working: STATUS_TONE.working,
  'needs-input': STATUS_TONE.waiting,
  ready: STATUS_TONE.done,
  done: STATUS_TONE.done,
  failed: STATUS_TONE.failed,
  gone: STATUS_TONE.offline,
};

/** null: nothing to say, the conversation just sits there. */
export const statusTone = (status: TProviderStatus): TStatusTone | null => TONES[status];
