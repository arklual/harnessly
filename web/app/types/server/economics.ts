// Shapes of what the runtime answers; the runtime itself is Go.
import type { IProviderLimits } from '@/types/server/hl-machines';

export type TEconomicsProvider = 'claude' | 'codex';

export interface ITokenBreakdown {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  /** Part of `output`; null when the transcript never reports it. */
  reasoning: number | null;
  total: number;
}

/** A conversation's tokens, time and API-equivalent cost as the runtime counts them from its transcript. */
export interface IConversationEconomics {
  provider: TEconomicsProvider;
  sessionId: string | null;
  status: 'ready' | 'computing' | 'no-transcript';
  tokens: ITokenBreakdown | null;
  subagents: { count: number; tokens: ITokenBreakdown } | null;
  models: { model: string; tokens: ITokenBreakdown }[] | null;
  apiEquivalentUsd: { value: number; source: 'claude-code' | 'price-table' } | null;
  unpricedModels: string[];
  time: { firstAt: number; lastAt: number; wallMs: number; activeMs: number; turns: number } | null;
  limitShare: null;
  computedAt: number;
}

export interface IEconomicsData {
  conversation: IConversationEconomics & { tabId: string };
  /** Subscription limits of the conversation's provider on this machine. */
  limits: IProviderLimits | null;
}

/** No live tab here (closed, or not an agent tab) or an agent without economics: nothing to show, not an error. */
export interface IEconomicsNone {
  conversation: null;
  limits: null;
}

export type IEconomicsResponse = IEconomicsData | IEconomicsNone;
