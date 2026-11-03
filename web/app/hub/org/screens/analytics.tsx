// Аналитика: twelve KPIs in three groups, the period and its comparison, the big chart of the selected KPI,
// changes and their effect, signals, wasted money, peers. Who sees what is decided by the data the API returns for
// the role (a participant gets only their own work and no money of others).
import './analytics.css';

import { useState } from 'react';

import type { AnalyticsFrame, KpiGroup, KpiId } from '../api/analytics';
import { rangeLabel } from '../dates';
import { useOrg } from '../context';
import { useLoad } from '../hooks';
import { LoadError, Loading, Page, PageHead } from '../ui/page';
import { ChartPanel } from './analytics/chart-panel';
import { KpiTile } from './analytics/kpi-tile';
import { PeriodPicker, type PeriodValue } from './analytics/period-picker';
import { defaultCompare } from './analytics/period';
import { Changes, PeersCard, Signals, WasteCard } from './analytics/sections';

const GROUPS: { id: KpiGroup; title: string }[] = [
  { id: 'result', title: 'Результат' },
  { id: 'agent', title: 'Как работает агент' },
  { id: 'env', title: 'Окружение' },
];

const LINE_NOUN = { admin: 'командам', lead: 'людям', member: 'дням' } as const;

const usd = (n: number) => `$${new Intl.NumberFormat('ru-RU').format(n)}`;

const Body = ({ frame }: { frame: AnalyticsFrame }) => {
  const { shell, base } = useOrg();
  const [value, setValue] = useState<PeriodValue>({
    period: frame.defaultPeriod,
    compare: defaultCompare(frame.defaultPeriod),
  });
  const [kpiId, setKpiId] = useState<KpiId>('moneyPerMr');
  const key = [value.period.from, value.period.to, value.compare.from, value.compare.to];
  const data = useLoad((api) => api.analytics(value), key);
  const chart = useLoad((api) => api.analyticsChart(value, kpiId), [...key, kpiId]);
  const role = shell.viewer.role;
  const periodText = rangeLabel(value.period.from, value.period.to);
  const compareText = rangeLabel(value.compare.from, value.compare.to);
  const kpi = data.data?.kpis.find((k) => k.id === kpiId) ?? null;
  const sum = data.data?.summary;

  return (
    <Page>
      <PageHead
        title="Аналитика"
        sub={
          data.data && sum
            ? `${data.data.scopeLabel} · за ${periodText}: ${usd(sum.spendUsd)} · ${sum.sessions} сессий · ${sum.mrs} MR`
            : 'Считаем'
        }
        actions={<PeriodPicker frame={frame} value={value} onChange={setValue} />}
      />
      {data.error ? (
        <LoadError error={data.error} retry={data.reload} />
      ) : !data.data ? (
        <Loading />
      ) : (
        <>
          {GROUPS.map((g) => (
            <section key={g.id} className="an-group" aria-label={g.title}>
              <h2 className="an-group__title">{g.title}</h2>
              <div className="an-kpis">
                {data.data?.kpis
                  .filter((k) => k.group === g.id)
                  .map((k) => (
                    <KpiTile key={k.id} kpi={k} selected={k.id === kpiId} onSelect={() => setKpiId(k.id)} />
                  ))}
              </div>
            </section>
          ))}
          {kpi && (
            <ChartPanel
              kpi={kpi}
              chart={chart.data}
              error={chart.error}
              retry={chart.reload}
              lineNoun={LINE_NOUN[role]}
              periodName={periodText}
              compareName={compareText}
            />
          )}
          <Changes changes={data.data.changes} />
          {data.data.signals.length > 0 && <Signals signals={data.data.signals} base={base} />}
          {data.data.waste && <WasteCard waste={data.data.waste} />}
          {data.data.peers && <PeersCard peers={data.data.peers} />}
        </>
      )}
    </Page>
  );
};

export const AnalyticsScreen = () => {
  const frame = useLoad((api) => api.analyticsFrame(), []);
  if (frame.error) {
    return (
      <Page>
        <PageHead title="Аналитика" />
        <LoadError error={frame.error} retry={frame.reload} />
      </Page>
    );
  }
  if (!frame.data) {
    return (
      <Page>
        <PageHead title="Аналитика" />
        <Loading />
      </Page>
    );
  }
  return <Body frame={frame.data} />;
};
