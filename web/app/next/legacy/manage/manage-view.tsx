// «Управление»: the person's own history. The machine's report (what this runtime counted) always works; with a hub
// the page adds what the hub accounted across machines, the personal limits and «Попросить больше».
import { useMemo, useState } from 'react';
import { mergeTeam, type TPeriod } from '@/components/agents/stats/aggregate';
import { useTeamUsage } from '@/components/agents/stats/use-team-usage';
import { manageHub, type ManageHub } from '@/next/features/manage/api';
import AskMore from '@/next/features/manage/ask-more';
import { DayBars, LimitMeter } from '@/next/features/manage/charts';
import { clockOf, dayOf, hubMoney, number, hubMoneyShort, stamp, usd } from '@/next/features/manage/format';
import type { HubMe, MeSession } from '@/next/features/manage/hub-types';
import {
  decidingLimit,
  historyRows,
  limitTone,
  localSummary,
  share,
  type HistoryRow,
} from '@/next/legacy/manage/model';
import ManageTabs from '@/next/features/manage/tabs';
import { useHub } from '@/next/features/manage/use-hub';
import { t } from '@/next/i18n';
import type { TRoute } from '@/next/routes/routes';
import { ROUTE } from '@/next/routes/routes';
import { Button } from '@/next/ui/button';
import Segmented from '@/next/ui/segmented';
import { Skeleton } from '@/next/ui/skeleton';

const PERIODS = ['7', '30', '90'] as const;
type PeriodKey = (typeof PERIODS)[number];

const OUTCOME_KEY: Record<MeSession['outcome'], string> = {
  running: 'next.manage.outcome.running',
  mr_merged: 'next.manage.outcome.merged',
  mr_open: 'next.manage.outcome.open',
  abandoned: 'next.manage.outcome.abandoned',
  done: 'next.manage.outcome.done',
};

const LimitCard = ({ me, hub, onSent }: { me: HubMe; hub: ManageHub; onSent: () => void }) => {
  const { limits, usage } = me;
  const limit = decidingLimit(limits);
  if (!limit) {
    return (
      <section className="hx-mg-card">
        <h2 className="hx-mg-card__title">{t('next.manage.limit.title')}</h2>
        <p className="hx-mg-card__sub">{t('next.manage.limit.none')}</p>
      </section>
    );
  }
  const tone = limitTone(limit);
  const left = Math.max(0, limit.amount - limit.used);
  const resets = dayOf(limit.resetsAt);
  return (
    <section className="hx-mg-card" aria-labelledby="hx-mg-limit">
      <h2 id="hx-mg-limit" className="hx-mg-card__title">
        {limit.label}
      </h2>
      <p className="hx-mg-card__sub">
        {limit.setBy
          ? t('next.manage.limit.setBy', { name: limit.setBy.name, role: limit.setBy.role })
          : t('next.manage.limit.setByHub')}
        {' · '}
        {t('next.manage.limit.resets', { date: resets })}
      </p>
      <p className="hx-mg-big" data-tone={tone}>
        {hubMoneyShort(limit.used, limits.currency)}
        <span>
          {t('next.manage.limit.of', {
            amount: hubMoneyShort(limit.amount, limits.currency),
            left: hubMoneyShort(left, limits.currency),
          })}
        </span>
      </p>
      <LimitMeter
        used={limit.used}
        amount={limit.amount}
        forecast={usage.forecast}
        warnPct={limit.warnPct}
        tone={tone}
        label={t('next.manage.limit.aria', { pct: Math.round(share(limit) * 100) })}
      />
      <ul className="hx-mg-legend">
        <li data-k="used">{t('next.manage.limit.spent')}</li>
        {usage.forecast !== null && (
          <li data-k="pace">
            {t('next.manage.limit.pace', { amount: hubMoneyShort(usage.forecast, limits.currency) })}
          </li>
        )}
        <li data-k="warn">{t('next.manage.limit.warn', { pct: limit.warnPct })}</li>
      </ul>
      {tone === 'stop' && <p className="hx-mg-note">{t('next.manage.limit.stopped')}</p>}
      {limits.askTo && limits.pendingRequest && (
        <p className="hx-mg-note">
          {t('next.manage.ask.sent', { who: limits.askTo.name, time: stamp(limits.pendingRequest.at) })}
        </p>
      )}
      {limits.askTo && !limits.pendingRequest && (
        <AskMore who={limits.askTo.name} send={(m) => hub.askMore(m)} onSent={onSent} />
      )}
    </section>
  );
};

const Recent = ({ rows, currency }: { rows: HistoryRow[]; currency: HubMe['limits']['currency'] | null }) => (
  <section className="hx-mg-card" aria-labelledby="hx-mg-recent">
    <h2 id="hx-mg-recent" className="hx-mg-card__title">
      {t('next.manage.recent.title')}
    </h2>
    <p className="hx-mg-card__sub">{t('next.manage.recent.sub')}</p>
    {rows.length === 0 ? (
      <p className="hx-mg-empty">{t('next.manage.recent.empty')}</p>
    ) : (
      <ul className="hx-mg-rows">
        {rows.slice(0, 8).map(({ session, hub }) => (
          <li key={session.id} className="hx-mg-row">
            <span className="hx-mg-row__main">
              <b>{session.title ?? t('next.manage.recent.untitled')}</b>
              <small>
                {session.project ?? t('next.manage.recent.noProject')} ·{' '}
                {clockOf(new Date(session.end * 1000).toISOString())}
              </small>
            </span>
            {hub && <span className="hx-mg-row__out">{t(OUTCOME_KEY[hub.outcome])}</span>}
            <span className="hx-mg-row__cost">
              {hub && currency ? hubMoney(hub.cost, currency) : usd(session.cost)}
            </span>
          </li>
        ))}
      </ul>
    )}
  </section>
);

const Kpis = ({ me }: { me: HubMe }) => {
  const label = {
    moneyPerMr: t('next.manage.kpi.moneyPerMr'),
    yield: t('next.manage.kpi.yield'),
    timeToMr: t('next.manage.kpi.timeToMr'),
    mrPerMonth: t('next.manage.kpi.mrPerMonth'),
  } as const;
  const show = (id: keyof typeof label, v: number) => {
    if (id === 'moneyPerMr') return hubMoney(v, me.limits.currency);
    if (id === 'yield') return `${Math.round(v * 100)}%`;
    if (id === 'timeToMr') return t('next.manage.kpi.minutes', { n: Math.round(v) });
    return number(Math.round(v * 10) / 10);
  };
  return (
    <section className="hx-mg-kpis" aria-label={t('next.manage.kpi.title')}>
      {me.kpis.kpis.map((k) => (
        <div key={k.id} className="hx-mg-kpi">
          <span className="hx-mg-kpi__label">{label[k.id]}</span>
          <b className="hx-mg-kpi__value">{show(k.id, k.value)}</b>
          {k.previous !== null && (
            <small className="hx-mg-kpi__prev">
              {t('next.manage.kpi.was', { v: show(k.id, k.previous) })}
            </small>
          )}
        </div>
      ))}
    </section>
  );
};

const ManageView = ({ onOpen }: { onOpen: (route: TRoute) => void }) => {
  const hub = useMemo(() => manageHub(), []);
  const { state, reload } = useHub(hub);
  const { team, error, loading, reload: reloadLocal } = useTeamUsage();
  const [period, setPeriod] = useState<PeriodKey>('30');
  const data = useMemo(() => (team ? mergeTeam(team) : null), [team]);
  const local = useMemo(() => (data ? localSummary(data, Number(period) as TPeriod) : null), [data, period]);
  const me = state.kind === 'ready' ? state.me : null;
  const rows = useMemo(
    () => (local ? historyRows(local.sessions, me ? me.sessions.sessions : null) : []),
    [local, me],
  );

  return (
    <div className="hx-mg">
      <ManageTabs current={ROUTE.manage} onOpen={onOpen} />
      <div className="hx-mg__page">
        <header className="hx-mg-head">
          <div>
            <h1 className="hx-mg-head__title">{t('next.manage.title')}</h1>
            <p className="hx-mg-head__sub">
              {me
                ? t('next.manage.measured', { time: stamp(me.limits.measuredUntil) })
                : t('next.manage.localOnly')}
            </p>
          </div>
          <Segmented
            label={t('next.manage.period')}
            value={period}
            options={PERIODS.map((p) => ({ value: p, label: t(`next.manage.period.${p}`) }))}
            onChange={setPeriod}
          />
        </header>

        {state.kind === 'failed' && (
          <p className="hx-mg-banner" role="status">
            {t('next.manage.hubFailed')}
            <Button size="sm" variant="ghost" onClick={reload}>
              {t('next.manage.retry')}
            </Button>
          </p>
        )}

        {error && !team && (
          <p className="hx-mg-banner" role="alert">
            {error}
            <Button size="sm" variant="ghost" onClick={reloadLocal}>
              {t('next.manage.retry')}
            </Button>
          </p>
        )}

        {loading && !local && !error && (
          <div className="hx-mg-tiles" aria-busy="true">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="hx-mg-tile__sk" />
            ))}
          </div>
        )}

        {local && (
          <div className="hx-mg-tiles">
            <div className="hx-mg-tile">
              <b>{usd(local.totals.cost)}</b>
              <span>{t('next.manage.tile.cost')}</span>
            </div>
            <div className="hx-mg-tile">
              <b>{local.totals.sessions}</b>
              <span>{t('next.manage.tile.sessions')}</span>
            </div>
            <div className="hx-mg-tile">
              <b>{local.totals.prs}</b>
              <span>{t('next.manage.tile.prs')}</span>
            </div>
            <div className="hx-mg-tile">
              <b>
                {t('next.manage.tile.hours', { n: number(Math.round(local.totals.turnHours * 10) / 10) })}
              </b>
              <span>{t('next.manage.tile.work')}</span>
            </div>
          </div>
        )}

        {hub && state.kind === 'loading' && <Skeleton className="hx-mg-card__sk" />}
        {hub && me && <Kpis me={me} />}

        <div className="hx-mg-cols">
          {hub && me && <LimitCard me={me} hub={hub} onSent={reload} />}
          {local && <Recent rows={rows} currency={me ? me.limits.currency : null} />}
        </div>

        {local && (
          <section className="hx-mg-card" aria-labelledby="hx-mg-spend">
            <h2 id="hx-mg-spend" className="hx-mg-card__title">
              {t('next.manage.spend.title')}
            </h2>
            <p className="hx-mg-card__sub">
              {me ? t('next.manage.spend.sub.hub') : t('next.manage.spend.sub.local')}
            </p>
            {local.totals.cost === 0 && !me ? (
              <p className="hx-mg-empty">{t('next.manage.spend.empty')}</p>
            ) : (
              <DayBars
                days={me ? me.usage.days.slice(-Number(period)) : local.daily}
                label={t('next.manage.spend.aria')}
              />
            )}
            <ul className="hx-mg-models">
              {(me ? me.usage.byModel : local.models).map((m) => (
                <li key={m.model}>
                  <span>{m.model}</span>
                  <b>{me ? hubMoney(m.cost, me.usage.currency) : usd(m.cost)}</b>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </div>
  );
};

export default ManageView;
