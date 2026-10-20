import { useState } from 'react';
import { ArrowDownRight, ArrowUpRight, ChevronRight, Minus } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { useEfficiency } from '@/hooks/use-efficiency';
import type {
  IEffSummary,
  IEffSummaryChange,
  IEffSummaryMetric,
  TEffScope,
  TEffSummaryTab,
} from '@/types/efficiency';
import { compact, dateTime, decimal, minutes, money, n0, pct } from '@/components/agents/efficiency/format';
import { Card, Chip, Empty, Rich, Skeleton } from '@/components/agents/efficiency/primitives';
import Gate from '@/components/agents/efficiency/gate';
import PulseCard from '@/components/agents/efficiency/pulse-card';
import { plural, t } from '@/lib/i18n';

interface ISummaryProps {
  scope: TEffScope;
  onOpen: (tab: TEffSummaryTab, id?: string) => void;
}

const value = (v: number | null, unit: IEffSummaryMetric['unit']) => {
  if (unit === 'usd') return money(v);
  if (unit === 'pct') return pct(v, 1);
  if (unit === 'tokens') return compact(v);
  if (unit === 'min') return minutes(v);
  return n0(v);
};

const targetText = (metric: IEffSummaryMetric) => {
  if (metric.target === null) return t('efficiency.summaryTab.noTarget');
  if (metric.unit === 'count') return t('efficiency.summaryTab.targetAll');
  return t('efficiency.summaryTab.target', {
    sign: metric.better === 'down' ? '≤' : '≥',
    value: value(metric.target, metric.unit),
  });
};

const VERDICT: Record<IEffSummaryMetric['verdict'], { label: string; tone: 'ok' | 'bad' | 'flat' }> = {
  good: { label: t('efficiency.summaryTab.verdictGood'), tone: 'ok' },
  bad: { label: t('efficiency.summaryTab.verdictBad'), tone: 'bad' },
  flat: { label: t('efficiency.summaryTab.verdictFlat'), tone: 'flat' },
  nodata: { label: t('efficiency.summaryTab.verdictNoData'), tone: 'flat' },
};

const Trend = ({ metric }: { metric: IEffSummaryMetric }) => {
  if (metric.trendPct === null)
    return <span className="text-(--t-text-muted)">{t('efficiency.summaryTab.noTrend')}</span>;
  if (Math.abs(metric.trendPct) < 5)
    return (
      <span className="inline-flex items-center gap-1 text-(--t-text-secondary)">
        <Minus className="size-3.5" aria-hidden="true" />
        {t('efficiency.summaryTab.sameAsWeekAgo')}
      </span>
    );
  const up = metric.trendPct > 0;
  const good = up === (metric.better === 'up');
  const Icon = up ? ArrowUpRight : ArrowDownRight;
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1',
        good ? 'text-(--t-status-success)' : 'text-(--t-status-danger)',
      )}
    >
      <Icon className="size-3.5" aria-hidden="true" />
      {up ? '+' : ''}
      {t('efficiency.summaryTab.weekTrend', { value: decimal(metric.trendPct, 0) })} ·{' '}
      {good ? t('efficiency.summaryTab.better') : t('efficiency.summaryTab.worse')}
    </span>
  );
};

const MetricRow = ({ metric, onOpen }: { metric: IEffSummaryMetric; onOpen: ISummaryProps['onOpen'] }) => {
  const [open, setOpen] = useState(false);
  const verdict = VERDICT[metric.verdict];
  return (
    <li className="border-b border-(--t-border-subtle) py-3.5 last:border-b-0">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
        <div className="min-w-0 flex-1 basis-72">
          <button
            type="button"
            className="text-left text-[14px] font-semibold hover:underline"
            onClick={() => onOpen(metric.link.tab, metric.link.id)}
          >
            {metric.title}
          </button>
          <p className="mt-0.5 text-[13px] text-(--t-text-secondary)">{metric.what}</p>
          <button
            type="button"
            className="mt-1 text-[12px] text-(--t-text-muted) hover:text-(--t-text-secondary)"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            {open ? t('efficiency.summaryTab.hideHow') : t('efficiency.summaryTab.showHow')}
          </button>
          {open && <p className="mt-1 text-[12px] text-(--t-text-muted)">{metric.how}</p>}
        </div>
        <div className="flex min-w-[11rem] flex-col items-end gap-1 text-right">
          <div className="flex items-center gap-2">
            <span className="text-[22px] font-semibold tabular-nums leading-none">
              {metric.unit === 'count' && metric.target !== null && metric.value !== null
                ? t('efficiency.summaryTab.valueOfTarget', {
                    value: n0(metric.value),
                    target: n0(metric.target),
                  })
                : value(metric.value, metric.unit)}
            </span>
            <Chip tone={verdict.tone}>{verdict.label}</Chip>
          </div>
          <span className="text-[12px] tabular-nums text-(--t-text-secondary)">{targetText(metric)}</span>
          <span className="text-[12px] tabular-nums">
            <Trend metric={metric} />
          </span>
        </div>
      </div>
    </li>
  );
};

const EFFECT_LABEL = {
  better: t('efficiency.summaryTab.better'),
  worse: t('efficiency.summaryTab.worse'),
  noise: t('efficiency.summaryTab.noise'),
  early: t('efficiency.summaryTab.early'),
};
const EFFECT_TONE = { better: 'ok', worse: 'bad', noise: 'flat', early: 'flat' } as const;

const ChangeRow = ({ change }: { change: IEffSummaryChange }) => (
  <li className="border-b border-(--t-border-subtle) py-3 last:border-b-0">
    <p className="text-[13px]">
      <span className="tabular-nums text-(--t-text-secondary)">{dateTime(change.at)}</span> ·{' '}
      <b>{change.title}</b>
    </p>
    <div className="mt-1.5 flex flex-wrap gap-x-5 gap-y-1 text-[12px] tabular-nums text-(--t-text-secondary)">
      {change.effects.map((e) => (
        <span key={e.title} className="inline-flex items-center gap-1.5">
          {e.title}: {value(e.before, e.unit)} → {value(e.after, e.unit)}
          <Chip tone={EFFECT_TONE[e.verdict]}>{EFFECT_LABEL[e.verdict]}</Chip>
        </span>
      ))}
    </div>
  </li>
);

const Body = ({
  data,
  onOpen,
  scope,
}: {
  data: IEffSummary;
  onOpen: ISummaryProps['onOpen'];
  scope: TEffScope;
}) => {
  const bad = data.metrics.filter((m) => m.verdict === 'bad').length;
  return (
    <>
      <section className="ts-panel mb-4">
        <p className="text-[15px] leading-relaxed">
          <Rich
            text={t('efficiency.summaryTab.lead', {
              days: plural(
                Math.round((Date.parse(data.to) - Date.parse(data.from)) / 86_400_000) + 1,
                'efficiency.summaryTab.days',
              ),
              sessions: plural(data.totals.sessions, 'efficiency.summaryTab.sessions'),
              mrs: plural(data.totals.mrs, 'efficiency.summaryTab.mergedMrs'),
            })}
            parts={{
              cost: <b className="tabular-nums">{money(data.totals.costUsd)}</b>,
              sessionCount: <b className="tabular-nums">{n0(data.totals.sessions)}</b>,
              mrCount: <b className="tabular-nums">{n0(data.totals.mrs)}</b>,
              perMr: <b className="tabular-nums">{money(data.totals.usdPerMr)}</b>,
            }}
          />
          {data.totals.readingShare !== null && (
            <>
              {' '}
              <Rich
                text={t('efficiency.summaryTab.readingShare')}
                parts={{ share: <b className="tabular-nums">{pct(data.totals.readingShare)}</b> }}
              />
            </>
          )}
        </p>
        <p className="mt-1.5 text-[13px] text-(--t-text-secondary)">
          {bad
            ? t('efficiency.summaryTab.badMetrics', { bad, total: data.metrics.length })
            : t('efficiency.summaryTab.allMetricsOk')}
          {' · '}
          {t('efficiency.summaryTab.periodNote', { at: dateTime(data.scannedAt) })}
        </p>
      </section>

      {scope === 'me' && <PulseCard />}

      <Card title={t('efficiency.summaryTab.fixesTitle')} sub={t('efficiency.summaryTab.fixesSub')}>
        {data.fixes.length === 0 ? (
          <Empty text={t('efficiency.summaryTab.fixesEmpty')} />
        ) : (
          <ol className="flex flex-col">
            {data.fixes.map((f, i) => (
              <li key={f.id} className="flex gap-3 border-b border-(--t-border-subtle) py-3 last:border-b-0">
                <span className="mt-0.5 w-5 flex-none text-right text-[13px] tabular-nums text-(--t-text-muted)">
                  {i + 1}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-[14px] font-semibold">{f.title}</p>
                  <p className="mt-0.5 text-[13px] text-(--t-text-secondary)">{f.evidence}</p>
                  <p className="mt-1 text-[13px]">
                    <span className="text-(--t-text-secondary)">{t('efficiency.summaryTab.doThis')} </span>
                    {f.action}
                  </p>
                </div>
                <button
                  type="button"
                  className="flex-none self-center text-(--t-text-muted) hover:text-(--t-text)"
                  aria-label={t('efficiency.summaryTab.fixDetails', { title: f.title })}
                  onClick={() => onOpen(f.link.tab, f.link.id)}
                >
                  <ChevronRight className="size-4" aria-hidden="true" />
                </button>
              </li>
            ))}
          </ol>
        )}
      </Card>

      <Card title={t('efficiency.summaryTab.stateTitle')} sub={t('efficiency.summaryTab.stateSub')}>
        <ul>
          {data.metrics.map((m) => (
            <MetricRow key={m.id} metric={m} onOpen={onOpen} />
          ))}
        </ul>
      </Card>

      <Card title={t('efficiency.summaryTab.changesTitle')} sub={t('efficiency.summaryTab.changesSub')}>
        {data.changes.length === 0 ? (
          <Empty text={t('efficiency.summaryTab.changesEmpty')} />
        ) : (
          <ul>
            {data.changes.map((c) => (
              <ChangeRow key={`${c.at}-${c.title}`} change={c} />
            ))}
          </ul>
        )}
      </Card>
    </>
  );
};

const SummaryTab = ({ scope, onOpen }: ISummaryProps) => {
  const summary = useEfficiency<IEffSummary>(`/summary?scope=${scope}`);
  return (
    <Gate
      resource={summary}
      skeleton={
        <div className="flex flex-col gap-4">
          <Skeleton className="h-24" />
          <Skeleton className="h-72" />
          <Skeleton className="h-96" />
        </div>
      }
    >
      {(data) => <Body data={data} onOpen={onOpen} scope={scope} />}
    </Gate>
  );
};

export default SummaryTab;
