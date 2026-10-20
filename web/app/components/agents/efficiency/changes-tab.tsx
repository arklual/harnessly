import { useMemo, useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import { useEfficiency } from '@/hooks/use-efficiency';
import type { IEffChannel, IEffMarker, IEffOverview, IEvalAlert, TEffScope } from '@/types/efficiency';
import Gate from '@/components/agents/efficiency/gate';
import Monitor, { MarkerShape, VERDICT_TONE } from '@/components/agents/efficiency/monitor';
import { clock, money, n0, pct, shortDay, tokens } from '@/components/agents/efficiency/format';
import {
  MARKER_LABEL,
  VERDICT_LABEL,
  changeEffects,
  deltaText,
  markerDays,
  nearbyMarkers,
  type IMarkerDay,
  type IMetric,
  type IEffect,
} from '@/components/agents/efficiency/monitor-math';
import {
  Card,
  Chip,
  Note,
  Notice,
  Pane,
  PaneHeading,
  Rich,
  Skeleton,
} from '@/components/agents/efficiency/primitives';
import { plural, t } from '@/lib/i18n';

export const CHANNEL_LABEL: Record<IEffChannel['channel'], string> = {
  'remote-control': t('efficiency.changesTab.channelRemote'),
  tab: t('efficiency.changesTab.channelTab'),
  sdk: t('efficiency.changesTab.channelSdk'),
  eval: t('efficiency.changesTab.channelEval'),
  automation: t('efficiency.changesTab.channelAutomation'),
};

const MAX_MOVES_IN_FEED = 3;

const Slope = ({ effect }: { effect: IEffect }) => {
  if (effect.before === null || effect.after === null) return null;
  const hi = Math.max(effect.before, effect.after) || 1;
  const y1 = 12 - (effect.before / hi) * 10;
  const y2 = 12 - (effect.after / hi) * 10;
  const tone = VERDICT_TONE[effect.verdict];
  const stroke = {
    ok: 'stroke-(--t-status-success)',
    bad: 'stroke-(--t-status-danger)',
    flat: 'stroke-(--t-text-secondary)',
  }[tone];
  const dot = {
    ok: 'fill-(--t-status-success)',
    bad: 'fill-(--t-status-danger)',
    flat: 'fill-(--t-text-secondary)',
  }[tone];
  return (
    <svg viewBox="0 0 30 14" className="h-3.5 w-[30px] flex-none" aria-hidden="true">
      <line className={stroke} x1={3} y1={y1} x2={27} y2={y2} strokeWidth={2} strokeLinecap="round" />
      <circle className="fill-(--t-text-secondary)" cx={3} cy={y1} r={2} />
      <circle className={dot} cx={27} cy={y2} r={2.4} />
    </svg>
  );
};

const Move = ({ metric, effect }: { metric: IMetric; effect: IEffect }) => (
  <span className="inline-flex flex-wrap items-center gap-1.5 text-[12px] text-(--t-text-secondary)">
    <Slope effect={effect} />
    {metric.name}:{' '}
    <b className="tabular-nums text-(--t-text)">
      {metric.format(effect.before)} → {metric.format(effect.after)}
    </b>
    <Chip tone={VERDICT_TONE[effect.verdict]}>{VERDICT_LABEL[effect.verdict]}</Chip>
  </span>
);

const ChannelStrip = ({ overview }: { overview: IEffOverview }) => (
  <div className="mb-4 flex flex-wrap gap-2" aria-label={t('efficiency.changesTab.channels')}>
    {overview.channels.map((c) => (
      <span
        key={c.channel}
        className="inline-flex flex-wrap items-center gap-x-1.5 rounded-full border border-(--t-border-subtle) bg-(--t-surface) px-3 py-1.5 text-[12px] text-(--t-text-secondary)"
      >
        <Rich
          text={t('efficiency.changesTab.channelLine', {
            label: CHANNEL_LABEL[c.channel],
            sessions: plural(c.sessions, 'efficiency.changesTab.channelSessions'),
          })}
          parts={{
            sessionCount: <b className="tabular-nums text-(--t-text)">{n0(c.sessions)}</b>,
            cost: <b className="tabular-nums text-(--t-text)">{money(c.costUsd)}</b>,
            mrs: <b className="tabular-nums text-(--t-text)">{n0(c.mrs)}</b>,
          }}
        />
      </span>
    ))}
  </div>
);

const AlertBanner = ({ alerts, onOpenRuns }: { alerts: IEvalAlert[]; onOpenRuns: () => void }) => (
  <Notice tone="alert">
    <TriangleAlert className="mt-0.5 size-4 flex-none text-(--t-status-danger)" aria-hidden="true" />
    <span className="min-w-0 flex-1">
      <b>{t('efficiency.changesTab.alertTitle')}</b> {alerts[0].metric}.{' '}
      <button type="button" className="ts-link" onClick={onOpenRuns}>
        {t('efficiency.changesTab.openRuns')}
      </button>
    </span>
  </Notice>
);

interface IDayPaneProps {
  day: IMarkerDay;
  all: IMarkerDay[];
  overview: IEffOverview;
  onClose: () => void;
}

const DayPane = ({ day, all, overview, onClose }: IDayPaneProps) => {
  const effects = changeEffects(overview.days, day.index).slice(0, 6);
  const near = nearbyMarkers(all, day);
  return (
    <Pane
      chip={
        <Chip tone="accent">
          {MARKER_LABEL[day.lead.kind]} · {shortDay(day.day)}
        </Chip>
      }
      title={day.markers.map((m) => m.title).join('; ')}
      onClose={onClose}
    >
      <p className="text-(--t-text-secondary)">{t('efficiency.changesTab.dayPaneIntro')}</p>
      <ul className="mt-3 flex flex-col gap-2">
        {effects.map(({ metric, effect }) => (
          <li key={metric.key} className="rounded-xl border border-(--t-border-subtle) px-3 py-2.5">
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">{metric.name}</span>
              <Chip tone={VERDICT_TONE[effect.verdict]}>{VERDICT_LABEL[effect.verdict]}</Chip>
            </div>
            <div className="mt-1 flex items-center gap-2 text-[12px] text-(--t-text-secondary)">
              <Slope effect={effect} />
              <span className="tabular-nums">
                {metric.format(effect.before)} →{' '}
                <b className="text-(--t-text)">{metric.format(effect.after)}</b> ·{' '}
                {deltaText(metric, effect.before, effect.after)}
              </span>
            </div>
          </li>
        ))}
      </ul>
      <PaneHeading>{t('efficiency.changesTab.nearby')}</PaneHeading>
      {near.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {near.map((m) => (
            <li key={`${m.at}:${m.title}`} className="flex flex-wrap items-center gap-2">
              <Chip>{MARKER_LABEL[m.kind]}</Chip>
              <span className="text-(--t-text-secondary)">
                {shortDay(m.at.slice(0, 10))} · {m.title}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-(--t-text-secondary)">{t('efficiency.changesTab.nearbyNone')}</p>
      )}
      <Note>{t('efficiency.changesTab.notExperiment')}</Note>
    </Pane>
  );
};

const Feed = ({
  marks,
  overview,
  onOpenDay,
}: {
  marks: IMarkerDay[];
  overview: IEffOverview;
  onOpenDay: (day: IMarkerDay) => void;
}) => (
  <ul className="flex flex-col">
    {[...marks].reverse().map((day) => {
      const moves = changeEffects(overview.days, day.index)
        .filter((m) => m.effect.verdict !== 'wait')
        .slice(0, MAX_MOVES_IN_FEED);
      return (
        <li key={day.day} className="border-t border-(--t-border-subtle) first:border-t-0">
          <button
            type="button"
            onClick={() => onOpenDay(day)}
            className="grid w-full cursor-pointer gap-x-4 gap-y-1 rounded-lg px-1 py-3 text-left hover:bg-(--t-surface-hover) md:grid-cols-[72px_minmax(0,1fr)]"
          >
            <b className="text-[13px] tabular-nums">{shortDay(day.day)}</b>
            <span className="flex min-w-0 flex-col gap-1.5">
              {day.markers.map((m: IEffMarker) => (
                <span key={m.title} className="flex flex-wrap items-center gap-2">
                  <svg viewBox="0 0 16 20" className="h-4 w-3.5 flex-none" aria-hidden="true">
                    <MarkerShape kind={m.kind} x={8} y={10} />
                  </svg>
                  <Chip>{MARKER_LABEL[m.kind]}</Chip>
                  <span className="text-[13px] font-medium">{m.title}</span>
                  {m.detail && <span className="text-[12px] text-(--t-text-secondary)">{m.detail}</span>}
                </span>
              ))}
              {moves.map(({ metric, effect }) => (
                <Move key={metric.key} metric={metric} effect={effect} />
              ))}
            </span>
          </button>
        </li>
      );
    })}
  </ul>
);

const Loading = () => (
  <>
    <Skeleton className="mb-4 h-9" />
    <Skeleton className="mb-4 h-[640px]" />
    <Skeleton className="h-60" />
  </>
);

interface IChangesTabProps {
  scope: TEffScope;
  alerts: IEvalAlert[];
  onOpenRuns: () => void;
}

const ChangesTab = ({ scope, alerts, onOpenRuns }: IChangesTabProps) => {
  const resource = useEfficiency<IEffOverview>(`/overview?scope=${scope}&days=30`);
  const [openDay, setOpenDay] = useState<string | null>(null);

  return (
    <Gate resource={resource} skeleton={<Loading />}>
      {(overview) => (
        <ChangesBody
          overview={overview}
          alerts={alerts}
          onOpenRuns={onOpenRuns}
          openDay={openDay}
          setOpenDay={setOpenDay}
        />
      )}
    </Gate>
  );
};

interface IBodyProps {
  overview: IEffOverview;
  alerts: IEvalAlert[];
  onOpenRuns: () => void;
  openDay: string | null;
  setOpenDay: (day: string | null) => void;
}

const ChangesBody = ({ overview, alerts, onOpenRuns, openDay, setOpenDay }: IBodyProps) => {
  const days = useMemo(() => overview.days.map((d) => d.day), [overview.days]);
  const marks = useMemo(() => markerDays(days, overview.markers), [days, overview.markers]);
  const opened = marks.find((m) => m.day === openDay) ?? null;
  const totals = overview.totals;

  if (overview.days.length === 0)
    return (
      <Card title={t('efficiency.changesTab.monitor')}>
        <p className="ts-muted">{t('efficiency.changesTab.noSessions')}</p>
      </Card>
    );

  return (
    <>
      {alerts.length > 0 && <AlertBanner alerts={alerts} onOpenRuns={onOpenRuns} />}
      <ChannelStrip overview={overview} />
      <Card
        title={t('efficiency.changesTab.monitor')}
        sub={[
          t('efficiency.changesTab.monitorSub', {
            from: shortDay(overview.from),
            to: shortDay(overview.to),
            cost: money(totals.costUsd),
            sessions: plural(totals.sessions, 'efficiency.changesTab.sessions', {
              count: n0(totals.sessions),
            }),
            mrs: n0(totals.mrs),
          }),
          totals.readingShare === null
            ? t('efficiency.changesTab.readingUnknown')
            : t('efficiency.changesTab.readingShare', { share: pct(totals.readingShare) }),
          t('efficiency.changesTab.carried', { tokens: tokens(totals.carryTokens) }),
          ...(overview.scannedAt
            ? [t('efficiency.changesTab.updatedAt', { time: clock(overview.scannedAt) })]
            : []),
        ].join(' · ')}
        hint={t('efficiency.changesTab.monitorHint')}
      >
        <Monitor days={overview.days} marks={marks} onOpenDay={(m) => setOpenDay(m.day)} />
      </Card>
      <Card title={t('efficiency.changesTab.feedTitle')} sub={t('efficiency.changesTab.feedSub')}>
        {marks.length > 0 ? (
          <Feed marks={marks} overview={overview} onOpenDay={(m) => setOpenDay(m.day)} />
        ) : (
          <p className="ts-muted">{t('efficiency.changesTab.feedEmpty')}</p>
        )}
      </Card>
      {opened && <DayPane day={opened} all={marks} overview={overview} onClose={() => setOpenDay(null)} />}
    </>
  );
};

export default ChangesTab;
