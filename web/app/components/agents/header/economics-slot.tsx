import { TriangleAlert } from 'lucide-react';
import { CHIP_TONE, TwChip, TwTooltip } from '@/components/agents/ui';
import {
  formatExactTokens,
  formatMs,
  formatShortTokens,
  formatUsd,
  useClock,
} from '@/components/agents/limit-forecast';
import type { IHeaderSlotProps } from '@/components/agents/header/header-slot';
import { useConversationEconomics, type IEconomicsData } from '@/hooks/use-conversation-economics';
import { limitWarning } from '@/hooks/use-machine-limits';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { openPaneTab } from '@/lib/hl-pane-store';
import { t } from '@/lib/i18n';

const PROVIDER_NAME: Record<string, string> = { claude: 'Claude', codex: 'Codex' };

export const ECONOMICS_STATE = {
  hidden: 'hidden',
  loading: 'loading',
  error: 'error',
  ready: 'ready',
} as const;

export type TEconomicsSummary =
  | { state: typeof ECONOMICS_STATE.hidden }
  | { state: typeof ECONOMICS_STATE.loading }
  | { state: typeof ECONOMICS_STATE.error; error: string }
  | { state: typeof ECONOMICS_STATE.ready; label: string; detail: string; warning: string | null };

const detailText = (data: IEconomicsData): string => {
  const { tokens, subagents, apiEquivalentUsd } = data.conversation;
  if (!tokens) return t('header.economicsSlot.title');
  const parts = [
    t('header.economicsSlot.inputOutput', {
      input: formatExactTokens(tokens.input),
      output: formatExactTokens(tokens.output),
    }),
    t('header.economicsSlot.cache', {
      read: formatShortTokens(tokens.cacheRead),
      write: formatShortTokens(tokens.cacheWrite),
    }),
  ];
  if (subagents && subagents.count > 0)
    parts.push(
      t('header.economicsSlot.subagents', {
        count: subagents.count,
        tokens: formatShortTokens(subagents.tokens.total),
      }),
    );
  if (apiEquivalentUsd)
    parts.push(t('header.economicsSlot.apiEquivalent', { usd: formatUsd(apiEquivalentUsd.value) }));
  return parts.join(' · ');
};

/** «1.8M tok · 42 min · 5h 46%» of the agent conversation on screen, and a sure limit forecast «Claude until 16:50». */
export const useEconomicsSummary = ({ conversation, sessionName }: IHeaderSlotProps): TEconomicsSummary => {
  const agent =
    conversation?.kind === 'agent' &&
    (conversation.provider === 'claude' || conversation.provider === 'codex');
  const activity = useTimelineSnapshotStore((s) =>
    sessionName ? s.bySession[sessionName]?.entries.length : undefined,
  );
  const { data, error } = useConversationEconomics(
    agent ? conversation.id : null,
    conversation?.machineId,
    activity,
  );
  const clock = useClock();
  if (!agent) return { state: ECONOMICS_STATE.hidden };
  if (!data) return error ? { state: ECONOMICS_STATE.error, error } : { state: ECONOMICS_STATE.loading };
  if (!data.conversation || data.conversation.status === 'no-transcript')
    return { state: ECONOMICS_STATE.hidden };
  const { status, tokens, time } = data.conversation;
  if (status === 'computing' || !tokens) return { state: ECONOMICS_STATE.loading };

  const fiveHour = data.limits?.fiveHour ?? null;
  const warning = limitWarning(data.limits, conversation.provider === 'codex' ? 'codex' : 'claude');
  const label = [
    t('header.economicsSlot.tokens', { tokens: formatShortTokens(tokens.total) }),
    time ? formatMs(time.activeMs) : null,
    fiveHour ? t('header.economicsSlot.fiveHour', { pct: Math.round(fiveHour.usedPct) }) : null,
  ]
    .filter(Boolean)
    .join(' · ');
  return {
    state: ECONOMICS_STATE.ready,
    label,
    detail: detailText(data),
    warning: warning
      ? t('header.economicsSlot.until', {
          provider: PROVIDER_NAME[conversation.provider],
          time: clock(warning.at, warning.kind === 'seven_day'),
        })
      : null,
  };
};

export const openEconomicsIsland = () => openPaneTab('economics');

/**
 * The header keeps economics quiet: the full numbers live in the conversation menu and the island;
 * only a sure limit forecast «⚠ Claude until 16:50» earns a chip of its own.
 */
const EconomicsSlot = (props: IHeaderSlotProps) => {
  const summary = useEconomicsSummary(props);
  if (summary.state !== ECONOMICS_STATE.ready || !summary.warning) return null;
  return (
    <TwTooltip content={`${summary.label} · ${summary.detail}`}>
      <TwChip
        tone={CHIP_TONE.warning}
        icon={<TriangleAlert />}
        onClick={openEconomicsIsland}
        aria-label={t('header.economicsSlot.chipLabel', { warning: summary.warning, label: summary.label })}
        className="tw-econ-chip"
      >
        <span className="tw-header-slot-label">{summary.warning}</span>
      </TwChip>
    </TwTooltip>
  );
};

export default EconomicsSlot;
