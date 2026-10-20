import { t, plural } from '@/lib/i18n';
import { useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import {
  LimitWindowRow,
  formatAgo,
  formatExactTokens,
  formatMs,
  formatShortTokens,
  formatUsd,
} from '@/components/agents/limit-forecast';
import type { IInspectorIslandProps } from '@/components/agents/inspector/island-types';
import { useConversationEconomics, type IEconomicsData } from '@/hooks/use-conversation-economics';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { appFetch } from '@/lib/app-path';
import { prettyModelName } from '@/lib/hl-models';
import type { IProviderLimits } from '@/types/server/hl-machines';
import type { ITab } from '@/types/terminal';

const PROVIDER_NAME: Record<string, string> = { claude: 'Claude', codex: 'Codex' };

const SOURCE_NAME: Record<string, string> = {
  codexbar: 'codexbar',
  statusline: t('inspector.economicsIsland.sourceStatusline'),
  'codex-jsonl': t('inspector.economicsIsland.sourceCodexLog'),
};

const economicsProvider = (tab: ITab | null): 'claude' | 'codex' | null => {
  const provider = tab?.agentState?.providerId;
  return provider === 'claude' || provider === 'codex' ? provider : null;
};

/** Visible for Claude and Codex conversations. */
export const useIslandVisible = (tab: ITab | null, _provider: string): boolean =>
  economicsProvider(tab) !== null;

const Row = ({ label, value, sub }: { label: string; value: string; sub?: string | null }) => (
  <div className="tw-econ-row">
    <span className="tw-econ-row-label">{label}</span>
    <span className="tw-econ-row-value">{value}</span>
    {sub && <span className="tw-econ-row-sub">{sub}</span>}
  </div>
);

const Skeleton = () => (
  <div className="tw-econ-skeleton" role="status" aria-label={t('inspector.economicsIsland.computing')}>
    <span className="tw-skeleton" />
    <span className="tw-skeleton" />
    <span className="tw-skeleton" />
  </div>
);

const Usage = ({ data }: { data: IEconomicsData }) => {
  const { tokens, subagents, time, apiEquivalentUsd, unpricedModels, models } = data.conversation;
  if (!tokens) return null;
  const tokenLines = [
    t('inspector.economicsIsland.tokensInOut', {
      input: formatExactTokens(tokens.input),
      output: formatExactTokens(tokens.output),
    }),
    tokens.reasoning
      ? t('inspector.economicsIsland.tokensReasoning', { reasoning: formatExactTokens(tokens.reasoning) })
      : null,
    t('inspector.economicsIsland.tokensCache', {
      read: formatExactTokens(tokens.cacheRead),
      write: formatExactTokens(tokens.cacheWrite),
    }),
  ]
    .filter(Boolean)
    .join('\n');
  let cost = t('inspector.economicsIsland.noData');
  if (apiEquivalentUsd)
    cost = `≈ ${formatUsd(apiEquivalentUsd.value)} · ${apiEquivalentUsd.source === 'claude-code' ? t('inspector.economicsIsland.byClaudeCode') : t('inspector.economicsIsland.byApiPrice')}`;
  else if (unpricedModels.length)
    cost = t('inspector.economicsIsland.noPrice', {
      models: unpricedModels.map((model) => prettyModelName(model)).join(', '),
    });
  return (
    <div className="tw-econ-group">
      <Row
        label={t('inspector.economicsIsland.tokens')}
        value={formatExactTokens(tokens.total)}
        sub={tokenLines}
      />
      {subagents && subagents.count > 0 && (
        <Row
          label={t('inspector.economicsIsland.subagents', { count: subagents.count })}
          value={formatExactTokens(subagents.tokens.total)}
        />
      )}
      {models && models.length > 1 && (
        <Row
          label={t('inspector.economicsIsland.byModel')}
          value=""
          sub={models
            .map((item) => `${prettyModelName(item.model)} ${formatShortTokens(item.tokens.total)}`)
            .join(' · ')}
        />
      )}
      {time && (
        <Row
          label={t('inspector.economicsIsland.time')}
          value={t('inspector.economicsIsland.active', { duration: formatMs(time.activeMs) })}
          sub={`${t('inspector.economicsIsland.total', { duration: formatMs(time.wallMs) })} · ${plural(time.turns, 'inspector.economicsIsland.turns')}`}
        />
      )}
      <Row label={t('inspector.economicsIsland.apiEquivalent')} value={cost} />
      <p className="tw-econ-note">{t('inspector.economicsIsland.currentSession')}</p>
    </div>
  );
};

const Limits = ({ limits, provider }: { limits: IProviderLimits | null; provider: 'claude' | 'codex' }) => {
  const providerName = PROVIDER_NAME[provider];
  if (!limits || (!limits.fiveHour && !limits.sevenDay)) {
    return (
      <div className="tw-econ-group">
        <p className="tw-econ-heading">
          {t('inspector.economicsIsland.subscription', { plan: providerName })}
        </p>
        <p className="tw-econ-note">{t('inspector.economicsIsland.noLimits')}</p>
      </div>
    );
  }
  const sampledAt = limits.fiveHour?.sampledAt ?? limits.sevenDay?.sampledAt ?? null;
  const source = limits.fiveHour?.source ?? limits.sevenDay?.source ?? null;
  return (
    <div className="tw-econ-group">
      <p className="tw-econ-heading">
        {t('inspector.economicsIsland.subscription', { plan: limits.plan ?? providerName })}
      </p>
      <LimitWindowRow
        label={t('inspector.economicsIsland.window5h')}
        window={limits.fiveHour}
        kind="five_hour"
        providerName={providerName}
      />
      <LimitWindowRow
        label={t('inspector.economicsIsland.window7d')}
        window={limits.sevenDay}
        kind="seven_day"
        providerName={providerName}
      />
      {(limits.extra ?? []).map((item) => (
        <LimitWindowRow
          key={item.id}
          label={`${t('inspector.economicsIsland.window7d')} · ${item.title}`}
          window={item}
          kind="seven_day"
          providerName={providerName}
          withForecast={false}
        />
      ))}
      {limits.error && (
        <p className="tw-econ-error" role="alert">
          {limits.error}
        </p>
      )}
      {sampledAt !== null && (
        <p className="tw-econ-note">
          {source ? `${SOURCE_NAME[source] ?? source} · ` : ''}
          {formatAgo(sampledAt)}
        </p>
      )}
    </div>
  );
};

type TSwitchState =
  { kind: 'idle' } | { kind: 'pending' } | { kind: 'done'; text: string } | { kind: 'error'; text: string };

/** Pessimistic: the button waits for switch-model; a busy agent answers 409 and the reason is shown. */
const SwitchToSonnet = ({ sessionName }: { sessionName: string }) => {
  const [state, setState] = useState<TSwitchState>({ kind: 'idle' });
  const run = async () => {
    setState({ kind: 'pending' });
    try {
      const response = await appFetch('/api/platform/switch-model', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session: sessionName, model: 'sonnet' }),
      });
      const body = (await response.json().catch(() => null)) as { error?: string } | null;
      if (!response.ok) {
        setState({
          kind: 'error',
          text: body?.error ?? t('inspector.economicsIsland.switchFailed', { status: response.status }),
        });
        return;
      }
      setState({ kind: 'done', text: t('inspector.economicsIsland.switched') });
    } catch {
      setState({ kind: 'error', text: t('inspector.economicsIsland.unreachable') });
    }
  };
  return (
    <div className="tw-econ-actions">
      <TwButton
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.secondary}
        pending={state.kind === 'pending'}
        onClick={() => void run()}
      >
        {t('inspector.economicsIsland.switchToSonnet')}
      </TwButton>
      {state.kind === 'done' && (
        <p className="tw-econ-note" role="status">
          {state.text}
        </p>
      )}
      {state.kind === 'error' && (
        <p className="tw-econ-error" role="alert">
          {state.text}
        </p>
      )}
    </div>
  );
};

/** Tokens, time, API-equivalent cost and the subscription limits with their forecast. */
export const Island = ({ tab, expanded }: IInspectorIslandProps) => {
  const provider = economicsProvider(tab);
  const sessionName = tab?.sessionName ?? null;
  const activity = useTimelineSnapshotStore((s) =>
    sessionName ? s.bySession[sessionName]?.entries.length : undefined,
  );
  const { data, error } = useConversationEconomics(provider ? (tab?.id ?? null) : null, undefined, activity);
  if (!provider || !tab) return null;

  if (!data) {
    if (!error) return <Skeleton />;
    return (
      <div className="tw-econ" data-expanded={expanded || undefined}>
        <p className="tw-econ-error" role="alert">
          {error}
        </p>
      </div>
    );
  }
  if (!data.conversation) {
    return (
      <div className="tw-econ" data-expanded={expanded || undefined}>
        <p className="tw-inspector-muted">{t('inspector.economicsIsland.notOpenHere')}</p>
      </div>
    );
  }
  const { status } = data.conversation;
  const usedModels = data.conversation.models ?? [];
  const onSonnet = usedModels.length > 0 && usedModels.every((item) => /sonnet/i.test(item.model));
  return (
    <div className="tw-econ" data-expanded={expanded || undefined}>
      {error && (
        <p className="tw-econ-error" role="alert">
          <RefreshCw aria-hidden="true" /> {t('inspector.economicsIsland.stale', { error })}
        </p>
      )}
      {status === 'computing' && <Skeleton />}
      {status === 'no-transcript' && (
        <p className="tw-econ-note">{t('inspector.economicsIsland.noTranscript')}</p>
      )}
      {status === 'ready' && <Usage data={data} />}
      <Limits limits={data.limits} provider={provider} />
      {provider === 'claude' && sessionName && !onSonnet && <SwitchToSonnet sessionName={sessionName} />}
    </div>
  );
};
