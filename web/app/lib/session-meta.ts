// The session's numbers the composer and the header show, from the runtime's session stats (the agent's status line,
// sent with the timeline) and the transcript's own meta.
import type { ISessionMeta } from '@/hooks/use-tab-store';
import type { ITimelineEntry } from '@/types/timeline';

const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const str = (v: unknown): string | null => (typeof v === 'string' && v ? v : null);

export const sessionMetaFrom = (
  stats: Record<string, unknown> | null,
  initMeta: Record<string, unknown> | null,
  entries: readonly ITimelineEntry[],
): ISessionMeta => {
  const firstUser = entries.find((e) => e.type === 'user-message');
  return {
    title:
      str(initMeta?.customTitle) ??
      (firstUser?.type === 'user-message' ? firstUser.text.slice(0, 120) : null),
    model: str(stats?.model),
    effort: str(stats?.effort),
    inputTokens: num(stats?.inputTokens),
    cachedInputTokens: num(stats?.cachedInputTokens),
    outputTokens: num(stats?.outputTokens),
    reasoningOutputTokens: num(stats?.reasoningOutputTokens),
    totalCost: num(stats?.cost),
    currentContextTokens: num(stats?.currentContextTokens) || null,
    contextWindowSize: num(stats?.contextWindowSize) || null,
    usedPercentage: num(stats?.usedPercentage),
    exceeds200k: stats?.exceeds200k === true,
    createdAt: str(initMeta?.createdAt),
    updatedAt: str(initMeta?.updatedAt),
  };
};
