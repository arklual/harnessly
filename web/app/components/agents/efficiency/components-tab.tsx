import { useMemo, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { useEfficiency } from '@/hooks/use-efficiency';
import type {
  IEffComponents,
  IEffFamily,
  IEffFamilyDetail,
  IEffPanel,
  IEffSkill,
  IEffVerb,
  TEffScope,
} from '@/types/efficiency';
import Gate from '@/components/agents/efficiency/gate';
import useWidth from '@/components/agents/efficiency/use-width';
import {
  FAMILY_SORTS,
  barWidths,
  errorTone,
  sortFamilies,
  sparkBars,
  type TFamilySort,
} from '@/components/agents/efficiency/families-math';
import { millis, n0, pct, shortDay, tokens } from '@/components/agents/efficiency/format';
import {
  Card,
  Chip,
  Empty,
  Kpi,
  KpiRow,
  Segmented,
  Skeleton,
} from '@/components/agents/efficiency/primitives';
import { plural, t } from '@/lib/i18n';

const ROW_GRID = 'md:grid-cols-[minmax(0,2.2fr)_130px_minmax(70px,0.7fr)_minmax(90px,0.9fr)_72px]';

const Spark = ({ series }: { series: number[] }) => (
  <svg viewBox="0 0 120 26" className="h-[26px] w-[120px]" aria-hidden="true">
    {sparkBars(series, 120, 26).map((b) => (
      <rect
        key={b.x}
        className="fill-(--t-accent-purple) opacity-80"
        x={b.x}
        y={b.y}
        width={3}
        height={b.height}
      />
    ))}
  </svg>
);

const Bars = ({
  rows,
}: {
  rows: { label: string; value: number; text: string; tone: IEffPanel['rows'][number]['tone'] }[];
}) => {
  const widths = barWidths(rows.map((r) => r.value));
  return (
    <ul className="flex flex-col gap-2" role="list">
      {rows.map((r, i) => (
        <li
          key={r.label}
          className="grid grid-cols-[minmax(0,1fr)] gap-x-3 gap-y-1 text-[12px] sm:grid-cols-[minmax(120px,0.9fr)_minmax(0,1.4fr)_auto] sm:items-center"
        >
          <span className="truncate" title={r.label}>
            {r.label}
          </span>
          <svg
            viewBox="0 0 100 8"
            preserveAspectRatio="none"
            className="h-2 w-full"
            role="img"
            aria-label={`${r.label}: ${r.text}`}
          >
            <rect className="fill-(--t-surface-input)" x={0} y={0} width={100} height={8} rx={4} />
            <rect
              className={cx(
                r.tone === 'bad' && 'fill-(--t-status-danger)',
                r.tone === 'warn' && 'fill-(--t-status-warning)',
                r.tone === 'ok' && 'fill-(--t-status-success)',
                r.tone === null && 'fill-(--t-accent-purple)',
              )}
              x={0}
              y={0}
              width={widths[i]}
              height={8}
              rx={4}
            />
          </svg>
          <span className="tabular-nums text-(--t-text-secondary)">{r.text}</span>
        </li>
      ))}
    </ul>
  );
};

const FamilyList = ({ data, onOpen }: { data: IEffComponents; onOpen: (id: string) => void }) => {
  const [sort, setSort] = useState<TFamilySort>('carry');
  const families = useMemo(() => sortFamilies(data.families, sort), [data.families, sort]);
  return (
    <Card
      title={t('efficiency.componentsTab.title')}
      sub={t('efficiency.componentsTab.sub', { from: shortDay(data.from), to: shortDay(data.to) })}
      actions={
        <Segmented
          label={t('efficiency.componentsTab.sort')}
          value={sort}
          options={FAMILY_SORTS}
          onChange={setSort}
        />
      }
    >
      {families.length === 0 ? (
        <Empty text={t('efficiency.componentsTab.empty')} />
      ) : (
        <div role="list">
          <div
            className={cx(
              'hidden gap-3 border-b border-(--t-border-subtle) px-2 pb-2 text-[12px] text-(--t-text-secondary) md:grid',
              ROW_GRID,
            )}
          >
            <span>{t('efficiency.componentsTab.colFamily')}</span>
            <span>{t('efficiency.componentsTab.callsPerDay')}</span>
            <span className="text-right">{t('efficiency.componentsTab.colCalls')}</span>
            <span className="text-right">{t('efficiency.componentsTab.colCarry')}</span>
            <span className="text-right">{t('efficiency.componentsTab.colErrors')}</span>
          </div>
          {families.map((f) => (
            <button
              key={f.id}
              type="button"
              role="listitem"
              onClick={() => onOpen(f.id)}
              className={cx(
                'grid w-full cursor-pointer items-center gap-x-3 gap-y-1 border-b border-(--t-border-subtle) px-2 py-3 text-left last:border-b-0 hover:bg-(--t-surface-hover)',
                ROW_GRID,
              )}
            >
              <span className="min-w-0">
                <b className="block text-[13px]">{f.name}</b>
                <small className="block truncate text-[11px] text-(--t-text-secondary)">{f.desc}</small>
              </span>
              <Spark series={f.perDay} />
              <span className="text-[13px] tabular-nums md:text-right">
                {n0(f.calls)}
                {f.loads > 0 && (
                  <small className="block text-[11px] text-(--t-text-secondary)">
                    {plural(f.loads, 'efficiency.componentsTab.bodyLoads', { count: n0(f.loads) })}
                  </small>
                )}
              </span>
              <span className="text-[13px] tabular-nums md:text-right">
                {tokens(f.carryTokens)}
                <small className="block text-[11px] text-(--t-text-secondary)">
                  {t('efficiency.componentsTab.ofTotal', { share: pct(f.carryShare) })}
                </small>
              </span>
              <span className="md:text-right">
                <Chip tone={errorTone(f)}>{pct(f.errRate, 1)}</Chip>
              </span>
            </button>
          ))}
        </div>
      )}
      <p className="mt-3 text-[12px] text-(--t-text-secondary)">
        {t('efficiency.componentsTab.carryNote', { total: tokens(data.totalCarry) })}
      </p>
    </Card>
  );
};

const CallsPerDay = ({ days, series }: { days: string[]; series: number[] }) => {
  const [widthRef, width] = useWidth();
  const H = 96;
  const max = Math.max(...series, 1);
  const step = (width - 40) / Math.max(series.length, 1);
  const bar = Math.max(2, step - 2);
  return (
    <div ref={widthRef}>
      <svg
        viewBox={`0 0 ${width} ${H}`}
        className="block w-full"
        height={H}
        role="img"
        aria-label={t('efficiency.componentsTab.callsPerDay')}
      >
        {[0, 0.5, 1].map((f) => {
          const y = H - 18 - f * (H - 30);
          return (
            <g key={f}>
              <line className="stroke-(--t-border-subtle)" x1={34} x2={width - 4} y1={y} y2={y} />
              <text className="fill-(--t-text-muted)" x={30} y={y + 3} fontSize={9} textAnchor="end">
                {n0(max * f)}
              </text>
            </g>
          );
        })}
        {series.map(
          (v, i) =>
            v > 0 && (
              <rect
                key={days[i] ?? i}
                className="fill-(--t-accent-purple) opacity-85"
                x={36 + i * step}
                y={H - 18 - (v / max) * (H - 30)}
                width={bar}
                height={(v / max) * (H - 30)}
                rx={1.5}
              >
                <title>{`${shortDay(days[i] ?? '')}: ${n0(v)}`}</title>
              </rect>
            ),
        )}
        {[0, series.length - 1].map((i) => (
          <text
            key={i}
            className="fill-(--t-text-muted)"
            x={i === 0 ? 36 : width - 4}
            y={H - 4}
            fontSize={9}
            textAnchor={i === 0 ? 'start' : 'end'}
          >
            {shortDay(days[i] ?? '')}
          </text>
        ))}
      </svg>
    </div>
  );
};

const VerbsTable = ({ verbs }: { verbs: IEffVerb[] }) => (
  <div className="ts-table-wrap">
    <table className="ts-table">
      <thead>
        <tr>
          <th>{t('efficiency.componentsTab.colSubcomponent')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colCalls')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colResult')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colCarry')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colErrors')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colRetries')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colLatency')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colSessions')}</th>
        </tr>
      </thead>
      <tbody>
        {verbs.map((v) => {
          const rate = v.calls > 0 ? v.errors / v.calls : null;
          return (
            <tr key={v.verb}>
              <td>{v.verb}</td>
              <td className="ts-num">{n0(v.calls)}</td>
              <td className="ts-num">{tokens(v.resultTokens)}</td>
              <td className="ts-num">{tokens(v.carryTokens)}</td>
              <td className="ts-num">
                {n0(v.errors)} · {pct(rate, 1)}
              </td>
              <td className="ts-num">{n0(v.retries)}</td>
              <td className="ts-num">
                {millis(v.latAvgMs)}
                {v.slowShare !== null && v.slowShare >= 0.05 && (
                  <small className="block text-[11px] text-(--t-text-secondary)">
                    {t('efficiency.componentsTab.slow', { share: pct(v.slowShare) })}
                  </small>
                )}
              </td>
              <td className="ts-num">{n0(v.sessions)}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  </div>
);

const SkillsTable = ({ skills }: { skills: IEffSkill[] }) => (
  <div className="ts-table-wrap">
    <table className="ts-table">
      <thead>
        <tr>
          <th>{t('efficiency.componentsTab.colSkill')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colLoads')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colBody')}</th>
          <th className="ts-num">{t('efficiency.componentsTab.colCarry')}</th>
          <th>{t('efficiency.componentsTab.colFollowups')}</th>
          <th>{t('efficiency.componentsTab.colErrVsBase')}</th>
        </tr>
      </thead>
      <tbody>
        {skills.map((s) => (
          <tr key={s.skill}>
            <td>
              <b>{s.skill}</b>
            </td>
            <td className="ts-num">{n0(s.loads)}</td>
            <td className="ts-num">{tokens(s.bodyTokens)}</td>
            <td className="ts-num">{tokens(s.carryTokens)}</td>
            <td>
              {s.followups.length === 0
                ? '—'
                : s.followups.slice(0, 4).map(([what, calls]) => (
                    <span key={what} className="block text-[12px]">
                      {what} <b className="tabular-nums">{n0(calls)}</b>
                    </span>
                  ))}
            </td>
            <td>
              {s.errVsBase.length === 0
                ? '—'
                : s.errVsBase.map(([verb, calls, after, elsewhere]) => (
                    <span key={verb} className="block text-[12px]">
                      {verb}:{' '}
                      <b
                        className={cx(
                          'tabular-nums',
                          elsewhere !== null && after > elsewhere * 1.25 && 'text-(--t-status-danger)',
                        )}
                      >
                        {pct(after, 1)}
                      </b>{' '}
                      {t('efficiency.componentsTab.versus', { value: pct(elsewhere, 1) })}{' '}
                      <span className="text-(--t-text-secondary)">({n0(calls)})</span>
                    </span>
                  ))}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

interface IFamilyPageProps {
  id: string;
  scope: TEffScope;
  onBack: () => void;
  onOpenHyp: (id: string) => void;
}

const FamilyPage = ({ id, scope, onBack, onOpenHyp }: IFamilyPageProps) => {
  const resource = useEfficiency<IEffFamilyDetail>(
    `/components/${encodeURIComponent(id)}?scope=${scope}&days=30`,
  );
  return (
    <>
      <div className="mb-4 flex items-center gap-3 text-[13px] text-(--t-text-secondary)">
        <button type="button" className="ts-btn inline-flex items-center gap-1.5" onClick={onBack}>
          <ArrowLeft className="size-4" aria-hidden="true" /> {t('efficiency.componentsTab.allComponents')}
        </button>
        <span>
          {t('efficiency.componentsTab.title')} /{' '}
          <b className="text-(--t-text)">{resource.data?.family.name ?? id}</b>
        </span>
      </div>
      <Gate
        resource={resource}
        skeleton={
          <>
            <Skeleton className="mb-4 h-28" />
            <Skeleton className="mb-4 h-40" />
            <Skeleton className="h-60" />
          </>
        }
      >
        {(detail) => <FamilyBody detail={detail} onOpenHyp={onOpenHyp} />}
      </Gate>
    </>
  );
};

const FamilyBody = ({ detail, onOpenHyp }: { detail: IEffFamilyDetail; onOpenHyp: (id: string) => void }) => {
  const f: IEffFamily = detail.family;
  return (
    <>
      <p className="mb-3 text-[13px] text-(--t-text-secondary)">{f.desc}</p>
      <KpiRow>
        <Kpi
          label={t('efficiency.componentsTab.callsInPeriod')}
          value={n0(f.calls)}
          sub={
            f.loads > 0
              ? plural(f.loads, 'efficiency.componentsTab.skillBodyLoads', { count: n0(f.loads) })
              : t('efficiency.componentsTab.sessionsCount', { count: n0(f.sessions) })
          }
        />
        <Kpi
          label={t('efficiency.componentsTab.colCarry')}
          value={tokens(f.carryTokens)}
          sub={t('efficiency.componentsTab.ofAllCarry', { share: pct(f.carryShare) })}
          hint={t('efficiency.componentsTab.carryHint')}
        />
        <Kpi
          label={t('efficiency.componentsTab.colErrors')}
          value={pct(f.errRate, 1)}
          sub={plural(f.errors, 'efficiency.componentsTab.failedCalls', { count: n0(f.errors) })}
        />
        <Kpi
          label={t('efficiency.componentsTab.toolResults')}
          value={tokens(f.resultTokens)}
          sub={plural(f.sessions, 'efficiency.componentsTab.inSessions', { count: n0(f.sessions) })}
        />
      </KpiRow>
      <Card title={t('efficiency.componentsTab.callsPerDay')}>
        <CallsPerDay days={detail.days} series={f.perDay} />
      </Card>
      {detail.panels.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          {detail.panels.map((p) => (
            <Card key={p.title} title={p.title}>
              <Bars rows={p.rows} />
              {p.note && <p className="mt-3 text-[12px] text-(--t-text-secondary)">{p.note}</p>}
            </Card>
          ))}
        </div>
      )}
      <Card
        title={t('efficiency.componentsTab.compositionTitle')}
        sub={t('efficiency.componentsTab.compositionSub')}
      >
        {detail.verbs.length > 0 ? (
          <VerbsTable verbs={detail.verbs} />
        ) : (
          <Empty text={t('efficiency.componentsTab.noSubcomponents')} />
        )}
      </Card>
      {detail.skills.length > 0 && (
        <Card title={t('efficiency.componentsTab.skillsTitle')} sub={t('efficiency.componentsTab.skillsSub')}>
          <SkillsTable skills={detail.skills} />
        </Card>
      )}
      {f.hyps.length > 0 && (
        <Card title={t('efficiency.componentsTab.experimentsTitle')}>
          <div className="flex flex-wrap gap-2">
            {f.hyps.map((h) => (
              <button key={h} type="button" className="ts-chip" onClick={() => onOpenHyp(h)}>
                {t('efficiency.componentsTab.hypothesisChip', { id: h.replace('h', '') })}
              </button>
            ))}
          </div>
        </Card>
      )}
    </>
  );
};

interface IComponentsTabProps {
  scope: TEffScope;
  onOpenHyp: (id: string) => void;
}

const ComponentsTab = ({ scope, onOpenHyp }: IComponentsTabProps) => {
  const resource = useEfficiency<IEffComponents>(`/components?scope=${scope}&days=30`);
  const [family, setFamily] = useState<string | null>(null);
  if (family)
    return <FamilyPage id={family} scope={scope} onBack={() => setFamily(null)} onOpenHyp={onOpenHyp} />;
  return (
    <Gate resource={resource} skeleton={<Skeleton className="h-[620px]" />}>
      {(data) => <FamilyList data={data} onOpen={setFamily} />}
    </Gate>
  );
};

export default ComponentsTab;
