import { useMemo, useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { EFFICIENCY_ALERTS_EVENT, postEfficiency, type IEfficiencyResource } from '@/hooks/use-efficiency';
import type {
  IEvalAlert,
  IEvalArmRow,
  IEvalBatch,
  IEvalCompareArm,
  IEvalDanger,
  IEvalRuns,
  IEvalSchedule,
} from '@/types/efficiency';
import Gate from '@/components/agents/efficiency/gate';
import useWidth from '@/components/agents/efficiency/use-width';
import {
  compact,
  dateTime,
  dayLabel,
  decimal,
  money,
  n0,
  pct,
  seconds,
} from '@/components/agents/efficiency/format';
import {
  ALERT_STATES,
  RUN_STATE_LABEL,
  batchStateLabel,
  isOpenAlert,
  runTone,
  scheduleTotals,
  smokeMedian,
  smokePoints,
  type ISmokePoint,
} from '@/components/agents/efficiency/runs-math';
import {
  Card,
  Chip,
  Empty,
  Kpi,
  KpiRow,
  Notice,
  Pane,
  PaneHeading,
  Segmented,
  Skeleton,
} from '@/components/agents/efficiency/primitives';
import { plural, t } from '@/lib/i18n';

const CHART_H = 190;
const PAD = { left: 34, right: 12, top: 22, bottom: 30 };

const SmokeChart = ({ runs }: { runs: IEvalRuns }) => {
  const [widthRef, width] = useWidth();
  const points = useMemo(() => smokePoints(runs), [runs]);
  if (points.length === 0) return <Empty text={t('efficiency.runsTab.noSmoke')} />;
  const total = Math.max(...points.map((p) => p.total), 1);
  const x = (i: number) => PAD.left + (i + 0.5) * ((width - PAD.left - PAD.right) / points.length);
  const y = (v: number) => PAD.top + (1 - v / total) * (CHART_H - PAD.top - PAD.bottom);
  const band = runs.band;
  return (
    <div ref={widthRef}>
      <svg
        viewBox={`0 0 ${width} ${CHART_H}`}
        className="block w-full"
        height={CHART_H}
        role="img"
        aria-label={t('efficiency.runsTab.smokeChartLabel', { total })}
      >
        {band && (
          <rect
            className="fill-(--t-accent-purple) opacity-10"
            x={PAD.left}
            y={y(band[1])}
            width={width - PAD.left - PAD.right}
            height={Math.max(2, y(band[0]) - y(band[1]))}
          />
        )}
        {[0, Math.round(total / 2), total].map((v) => (
          <g key={v}>
            <line
              className="stroke-(--t-border-subtle)"
              x1={PAD.left}
              x2={width - PAD.right}
              y1={y(v)}
              y2={y(v)}
            />
            <text
              className="fill-(--t-text-muted)"
              x={PAD.left - 6}
              y={y(v) + 3}
              fontSize={10}
              textAnchor="end"
            >
              {v}
            </text>
          </g>
        ))}
        {band && (
          <text className="fill-(--t-text-muted)" x={PAD.left + 6} y={y(band[0]) + 13} fontSize={10}>
            {t('efficiency.runsTab.aaBand', { low: band[0], high: band[1] })}
          </text>
        )}
        {points
          .filter((p) => p.changed)
          .map((p) => (
            <g key={`m:${p.at}`}>
              <line
                className="stroke-(--t-status-warning)"
                x1={x(p.index)}
                x2={x(p.index)}
                y1={PAD.top - 8}
                y2={CHART_H - PAD.bottom}
                strokeDasharray="3 2"
              />
              <rect
                className="fill-(--t-status-warning)"
                x={x(p.index) - 4}
                y={PAD.top - 12}
                width={8}
                height={8}
                rx={1.5}
              >
                <title>
                  {t('efficiency.runsTab.fingerprintChanged', {
                    day: dayLabel(p.at.slice(0, 10)),
                    fingerprint: p.fingerprint,
                  })}
                </title>
              </rect>
            </g>
          ))}
        <polyline
          className="fill-none stroke-(--t-accent-purple)"
          strokeWidth={2}
          points={points.map((p) => `${x(p.index)},${y(p.passed)}`).join(' ')}
        />
        {points.map((p) => (
          <circle
            key={p.at}
            className={cx(
              'stroke-(--t-surface)',
              p.outside ? 'fill-(--t-status-danger)' : 'fill-(--t-accent-purple)',
            )}
            cx={x(p.index)}
            cy={y(p.passed)}
            r={4.5}
            strokeWidth={2}
          >
            <title>
              {p.usdPerTask === null
                ? t('efficiency.runsTab.pointTitle', {
                    day: dayLabel(p.at.slice(0, 10)),
                    passed: p.passed,
                    total: p.total,
                    fingerprint: p.fingerprint,
                  })
                : t('efficiency.runsTab.pointTitleCost', {
                    day: dayLabel(p.at.slice(0, 10)),
                    passed: p.passed,
                    total: p.total,
                    cost: money(p.usdPerTask),
                    fingerprint: p.fingerprint,
                  })}
            </title>
          </circle>
        ))}
        {[0, Math.floor((points.length - 1) / 2), points.length - 1].map((i) => (
          <text
            key={i}
            className="fill-(--t-text-muted)"
            x={x(i)}
            y={CHART_H - 10}
            fontSize={10}
            textAnchor="middle"
          >
            {dayLabel(points[i].at.slice(0, 10))}
          </text>
        ))}
      </svg>
    </div>
  );
};

interface IActionProps {
  refresh: () => void;
}

const Switch = ({
  on,
  busy,
  label,
  onToggle,
}: {
  on: boolean;
  busy: boolean;
  label: string;
  onToggle: () => void;
}) => (
  <button
    type="button"
    role="switch"
    aria-checked={on}
    aria-label={label}
    disabled={busy}
    onClick={onToggle}
    className={cx(
      'relative h-5 w-9 flex-none cursor-pointer rounded-full border border-(--t-border-default) transition-colors disabled:cursor-wait disabled:opacity-60',
      on ? 'bg-(--t-accent-purple)' : 'bg-(--t-surface-input)',
    )}
  >
    <span
      className={cx(
        'absolute top-0.5 size-3.5 rounded-full bg-white shadow transition-all',
        on ? 'left-[18px]' : 'left-0.5',
      )}
    />
  </button>
);

const ScheduleTable = ({ runs, refresh }: { runs: IEvalRuns } & IActionProps) => {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState('');
  const totals = scheduleTotals(runs.schedules);
  const toggle = async (s: IEvalSchedule) => {
    setBusy(s.id);
    setError('');
    const failed = await postEfficiency(`/schedules/${encodeURIComponent(s.id)}`, { enabled: !s.enabled });
    setBusy(null);
    if (failed) setError(failed);
    else refresh();
  };
  return (
    <Card
      title={t('efficiency.runsTab.scheduleTitle')}
      sub={[
        runs.weekLimitPct === null
          ? t('efficiency.runsTab.budget', { cap: n0(runs.weekCapPct), used: decimal(runs.weekUsedPct) })
          : t('efficiency.runsTab.budgetWithWindow', {
              cap: n0(runs.weekCapPct),
              used: decimal(runs.weekUsedPct),
              window: n0(runs.weekLimitPct),
            }),
        plural(totals.runsPerWeek, 'efficiency.runsTab.scheduleTotals', {
          count: n0(totals.runsPerWeek),
          share: decimal(totals.weekPct),
        }),
      ].join(' ')}
    >
      {error && (
        <p className="mb-2 text-[13px] text-(--t-status-danger)" role="alert">
          {error}
        </p>
      )}
      {runs.schedules.length === 0 ? (
        <Empty text={t('efficiency.runsTab.scheduleEmpty')} />
      ) : (
        <div className="ts-table-wrap">
          <table className="ts-table">
            <thead>
              <tr>
                <th>{t('efficiency.runsTab.colSuite')}</th>
                <th>{t('efficiency.runsTab.colWhat')}</th>
                <th>{t('efficiency.runsTab.colWhen')}</th>
                <th className="ts-num">{t('efficiency.runsTab.colPerWeek')}</th>
                <th className="ts-num">{t('efficiency.runsTab.colWeekShare')}</th>
                <th>{t('efficiency.runsTab.colNext')}</th>
                <th>{t('efficiency.runsTab.colEnabled')}</th>
              </tr>
            </thead>
            <tbody>
              {runs.schedules.map((s) => (
                <tr key={s.id}>
                  <td>
                    <b>{s.suite}</b>
                  </td>
                  <td>{s.title}</td>
                  <td>{s.when}</td>
                  <td className="ts-num">{n0(s.runsPerWeek)}</td>
                  <td className="ts-num">{decimal(s.weekPct)}%</td>
                  <td className="whitespace-nowrap">{s.nextAt && runs.enabled ? dateTime(s.nextAt) : '—'}</td>
                  <td>
                    <Switch
                      on={s.enabled}
                      busy={busy === s.id}
                      label={t('efficiency.runsTab.suiteLabel', { suite: s.suite })}
                      onToggle={() => void toggle(s)}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
};

const AlertCard = ({ alert, refresh }: { alert: IEvalAlert } & IActionProps) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const resolve = async (state: (typeof ALERT_STATES)[number]['value']) => {
    setBusy(true);
    setError('');
    const failed = await postEfficiency(`/alerts/${encodeURIComponent(alert.id)}`, { state });
    setBusy(false);
    if (failed) {
      setError(failed);
      return;
    }
    window.dispatchEvent(new Event(EFFICIENCY_ALERTS_EVENT));
    refresh();
  };
  const open = isOpenAlert(alert);
  return (
    <Card
      title={
        alert.mine
          ? t('efficiency.runsTab.alertMine', { at: dateTime(alert.createdAt) })
          : t('efficiency.runsTab.alertOther', { at: dateTime(alert.createdAt) })
      }
      tone={alert.mine && open ? 'alert' : 'plain'}
      sub={
        alert.mine
          ? t('efficiency.runsTab.alertMineSub')
          : alert.addressee
            ? t('efficiency.runsTab.alertAddresseeSub')
            : undefined
      }
    >
      <p className="text-[13px]">{alert.metric}.</p>
      {alert.diff.length > 0 && (
        <>
          <PaneHeading>{t('efficiency.runsTab.diffHeading')}</PaneHeading>
          <ul className="list-disc pl-5 text-[13px] text-(--t-text-secondary)">
            {alert.diff.map((d) => (
              <li key={d}>{d}</li>
            ))}
          </ul>
        </>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-2">
        {open ? (
          ALERT_STATES.map((s) => (
            <button
              key={s.value}
              type="button"
              className="ts-btn disabled:opacity-60"
              disabled={busy}
              onClick={() => void resolve(s.value)}
            >
              {s.label}
            </button>
          ))
        ) : (
          <Chip tone="flat">{ALERT_STATES.find((s) => s.value === alert.state)?.label ?? alert.state}</Chip>
        )}
        {error && (
          <span className="text-[13px] text-(--t-status-danger)" role="alert">
            {error}
          </span>
        )}
      </div>
    </Card>
  );
};

const BatchesTable = ({ batches, onOpen }: { batches: IEvalBatch[]; onOpen: (id: string) => void }) => (
  <div className="ts-table-wrap">
    <table className="ts-table">
      <thead>
        <tr>
          <th>{t('efficiency.runsTab.colWhen')}</th>
          <th>{t('efficiency.runsTab.colSuite')}</th>
          <th>{t('efficiency.runsTab.colReason')}</th>
          <th>{t('efficiency.runsTab.colArms')}</th>
          <th className="ts-num">{t('efficiency.runsTab.colPassed')}</th>
          <th className="ts-num">$</th>
          <th className="ts-num">{t('efficiency.runsTab.colMin')}</th>
          <th>{t('efficiency.runsTab.colResult')}</th>
        </tr>
      </thead>
      <tbody>
        {batches.map((b) => (
          <tr
            key={b.id}
            className="cursor-pointer"
            tabIndex={0}
            aria-label={t('efficiency.runsTab.batchLabel', { suite: b.suite, at: dateTime(b.createdAt) })}
            onClick={() => onOpen(b.id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onOpen(b.id);
            }}
          >
            <td className="whitespace-nowrap">{dateTime(b.createdAt)}</td>
            <td>
              <b>{b.suite}</b>
            </td>
            <td>{b.reason}</td>
            <td className="text-(--t-text-secondary)">{b.arms.join(' · ')}</td>
            <td className="ts-num">
              {t('efficiency.runsTab.passedOf', { passed: b.passed, total: b.total })}
            </td>
            <td className="ts-num">{money(b.costUsd)}</td>
            <td className="ts-num">{b.wallMin === null ? '—' : n0(b.wallMin)}</td>
            <td>
              <Chip tone={runTone(b.state)}>{batchStateLabel(b.state)}</Chip>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

const BatchPane = ({ batch, onClose }: { batch: IEvalBatch; onClose: () => void }) => (
  <Pane
    chip={<Chip tone={runTone(batch.state)}>{batchStateLabel(batch.state)}</Chip>}
    title={`${batch.suite} · ${dateTime(batch.createdAt)}`}
    onClose={onClose}
  >
    <p className="text-(--t-text-secondary)">
      {batch.reason} · {t('efficiency.runsTab.batchPassed', { passed: batch.passed, total: batch.total })} ·{' '}
      {money(batch.costUsd)}
      {batch.wallMin !== null && ` · ${t('efficiency.runsTab.minutes', { value: n0(batch.wallMin) })}`}
    </p>
    <PaneHeading>{t('efficiency.runsTab.tasks')}</PaneHeading>
    {batch.runs.length === 0 ? (
      <p className="text-(--t-text-secondary)">{t('efficiency.runsTab.summaryOnly')}</p>
    ) : (
      <ul className="flex flex-col gap-2">
        {batch.runs.map((r) => (
          <li key={r.id} className="rounded-xl border border-(--t-border-subtle) px-3 py-2.5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <b className="text-[13px]">{r.task}</b>
              <Chip tone={runTone(r.state)}>
                {r.passed === null
                  ? RUN_STATE_LABEL[r.state]
                  : r.passed
                    ? t('efficiency.runsTab.taskPassed')
                    : t('efficiency.runsTab.taskFailed')}
              </Chip>
            </div>
            <div className="mt-1 text-[12px] tabular-nums text-(--t-text-secondary)">
              {t('efficiency.runsTab.runLine', {
                arm: r.arm,
                runNo: r.runNo,
                cost: money(r.costUsd),
                wall: seconds(r.wallS),
                first: compact(r.firstCtx),
                peak: compact(r.peakCtx),
              })}
            </div>
            {r.checks.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {r.checks.map((c) => (
                  <Chip key={c.name} tone={c.passed ? 'ok' : 'bad'}>
                    {t('efficiency.runsTab.checkCode', { name: c.name, code: c.exitCode ?? '—' })}
                  </Chip>
                ))}
              </div>
            )}
            {r.error && (
              <p className="mt-2 flex items-start gap-1.5 text-[12px] text-(--t-status-danger)">
                <TriangleAlert className="mt-0.5 size-3.5 flex-none" aria-hidden="true" />
                {r.error}
              </p>
            )}
          </li>
        ))}
      </ul>
    )}
  </Pane>
);

const ArmCard = ({ arm, other }: { arm: IEvalCompareArm; other: IEvalCompareArm | undefined }) => {
  const cheaper =
    other && arm.usdPerTask !== null && other.usdPerTask !== null && arm.usdPerTask < other.usdPerTask;
  return (
    <div className="rounded-xl border border-(--t-border-subtle) p-4">
      <p className="mb-2 text-[13px]">
        <b>{arm.arm}</b>{' '}
        <span className="text-(--t-text-secondary)">
          · {t('efficiency.runsTab.fingerprint', { fingerprint: arm.fingerprint })}
        </span>
      </p>
      <dl className="ts-kv">
        <div>
          <dt>{t('efficiency.runsTab.tasksPassed')}</dt>
          <dd>{pct(arm.passRate)}</dd>
        </div>
        <div>
          <dt>{t('efficiency.runsTab.usdPerTask')}</dt>
          <dd>
            {money(arm.usdPerTask)}
            {cheaper && (
              <span className="ml-1.5 text-(--t-status-success)">{t('efficiency.runsTab.cheaper')}</span>
            )}
          </dd>
        </div>
        <div>
          <dt>{t('efficiency.runsTab.sessionStart')}</dt>
          <dd>{compact(arm.firstCtx)}</dd>
        </div>
        <div>
          <dt>{t('efficiency.runsTab.runsCount')}</dt>
          <dd>{n0(arm.runs)}</dd>
        </div>
      </dl>
    </div>
  );
};

const DANGER_TONE = { held: 'ok', broken: 'bad', pending: 'flat' } as const;
const DANGER_LABEL = {
  held: t('efficiency.runsTab.dangerHeld'),
  broken: t('efficiency.runsTab.dangerBroken'),
  pending: t('efficiency.runsTab.dangerPending'),
};

const DangerCard = ({ danger }: { danger: IEvalDanger }) => (
  <Card
    title={t('efficiency.runsTab.dangerTitle', {
      held: danger.held,
      total: danger.checked || danger.rows.length,
    })}
    sub={[
      t('efficiency.runsTab.dangerSub'),
      danger.at
        ? t('efficiency.runsTab.lastRun', { at: dateTime(danger.at) })
        : t('efficiency.runsTab.noRunsYet'),
      t('efficiency.runsTab.dangerTwelfth'),
    ].join(' ')}
  >
    <div className="ts-table-wrap">
      <table className="ts-table">
        <thead>
          <tr>
            <th className="ts-num">{t('efficiency.runsTab.colNum')}</th>
            <th>{t('efficiency.runsTab.colScenario')}</th>
            <th className="ts-num">{t('efficiency.runsTab.colHeld')}</th>
            <th>{t('efficiency.runsTab.colResult')}</th>
          </tr>
        </thead>
        <tbody>
          {danger.rows.map((r) => (
            <tr key={r.key}>
              <td className="ts-num">{r.scenario}</td>
              <td>{r.title}</td>
              <td className="ts-num">
                {r.runs ? t('efficiency.runsTab.passedOf', { passed: r.held, total: r.runs }) : '—'}
              </td>
              <td>
                <Chip tone={DANGER_TONE[r.state]}>{DANGER_LABEL[r.state]}</Chip>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  </Card>
);

const signedPct = (v: number | null) => (v === null ? '—' : `${v > 0 ? '+' : ''}${decimal(v)}%`);
const signedPp = (v: number | null) =>
  v === null ? '—' : t('efficiency.runsTab.points', { sign: v > 0 ? '+' : '', value: decimal(v) });

const ArmsCard = ({ arms }: { arms: IEvalRuns['arms'] }) => (
  <Card
    title={t('efficiency.runsTab.armsTitle')}
    sub={[
      t('efficiency.runsTab.armsSub'),
      arms.at
        ? t('efficiency.runsTab.armsRun', { at: dateTime(arms.at) })
        : t('efficiency.runsTab.noRunsYet'),
    ].join(' ')}
  >
    {arms.rows.length === 0 ? (
      <Empty text={t('efficiency.runsTab.armsEmpty')} />
    ) : (
      <div className="ts-table-wrap">
        <table className="ts-table">
          <thead>
            <tr>
              <th>{t('efficiency.runsTab.colArm')}</th>
              <th>{t('efficiency.runsTab.colBase')}</th>
              <th className="ts-num">pass³</th>
              <th className="ts-num">Δ pass³</th>
              <th className="ts-num">{t('efficiency.runsTab.usdPerTask')}</th>
              <th className="ts-num">Δ $</th>
              <th className="ts-num">{t('efficiency.runsTab.colTokens')}</th>
              <th className="ts-num">{t('efficiency.runsTab.colDeltaTokens')}</th>
              <th className="ts-num">{t('efficiency.runsTab.colDeltaTime')}</th>
            </tr>
          </thead>
          <tbody>
            {arms.rows.map((r: IEvalArmRow) => (
              <tr key={r.arm}>
                <td>
                  <b>{r.title}</b>
                  {r.hyp && (
                    <span className="ml-1.5 text-(--t-text-secondary)">
                      {t('efficiency.runsTab.hypothesisChip', { id: r.hyp.replace('h', '') })}
                    </span>
                  )}
                </td>
                <td className="text-(--t-text-secondary)">{r.base ?? t('efficiency.runsTab.base')}</td>
                <td className="ts-num">{pct(r.pass3)}</td>
                <td className="ts-num">{signedPp(r.deltaPass3Pp)}</td>
                <td className="ts-num">
                  {r.usdPerTask === null && r.runs > 0
                    ? t('efficiency.runsTab.chatgptSubscription')
                    : money(r.usdPerTask)}
                </td>
                <td className="ts-num">{signedPct(r.deltaUsdPct)}</td>
                <td className="ts-num">{compact(r.tokensPerTask)}</td>
                <td className="ts-num">{signedPct(r.deltaTokensPct)}</td>
                <td className="ts-num">{signedPct(r.deltaWallPct)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )}
  </Card>
);

const ManualRun = ({ runs, refresh }: { runs: IEvalRuns } & IActionProps) => {
  const suites = [...new Set(runs.schedules.map((s) => s.suite))];
  const [suite, setSuite] = useState(suites[0] ?? '');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  if (suites.length === 0) return null;
  const enqueue = async () => {
    setBusy(true);
    setMessage('');
    const failed = await postEfficiency('/runs', { suite });
    setBusy(false);
    setMessage(failed ?? t('efficiency.runsTab.queued'));
    if (!failed) refresh();
  };
  return (
    <Card
      title={t('efficiency.runsTab.manualTitle')}
      sub={runs.enabled ? t('efficiency.runsTab.manualSub') : t('efficiency.runsTab.manualSubDisabled')}
    >
      <div className="flex flex-wrap items-center gap-3">
        <Segmented
          label={t('efficiency.runsTab.colSuite')}
          value={suite}
          options={suites.map((s) => ({ value: s, label: s }))}
          onChange={setSuite}
        />
        <button
          type="button"
          className="ts-btn disabled:opacity-60"
          disabled={busy || !suite}
          onClick={() => void enqueue()}
        >
          {t('efficiency.runsTab.enqueue')}
        </button>
        {message && (
          <span className="text-[13px] text-(--t-text-secondary)" role="status">
            {message}
          </span>
        )}
      </div>
    </Card>
  );
};

const Body = ({ runs, refresh }: { runs: IEvalRuns } & IActionProps) => {
  const [openBatch, setOpenBatch] = useState<string | null>(null);
  const points: ISmokePoint[] = useMemo(() => smokePoints(runs), [runs]);
  const last = points[points.length - 1];
  const median = smokeMedian(points);
  const batch = runs.batches.find((b) => b.id === openBatch) ?? null;
  const alerts = [...runs.alerts].sort(
    (a, b) => Number(isOpenAlert(b)) - Number(isOpenAlert(a)) || Number(b.mine) - Number(a.mine),
  );
  return (
    <>
      {!runs.enabled && (
        <Notice tone="warn">
          <span>{t('efficiency.runsTab.runnerOff')}</span>
        </Notice>
      )}
      {alerts.map((a) => (
        <AlertCard key={a.id} alert={a} refresh={refresh} />
      ))}
      <KpiRow>
        <Kpi
          label={t('efficiency.runsTab.smokeLast')}
          value={last ? t('efficiency.runsTab.passedOf', { passed: last.passed, total: last.total }) : '—'}
          sub={
            median === null
              ? undefined
              : [
                  t('efficiency.runsTab.median7', { value: decimal(median, 0) }),
                  ...(runs.band
                    ? [t('efficiency.runsTab.aaBand', { low: runs.band[0], high: runs.band[1] })]
                    : []),
                ].join('; ')
          }
          hint={t('efficiency.runsTab.smokeHint')}
        />
        <Kpi
          label={t('efficiency.runsTab.usdPerTask')}
          value={money(last?.usdPerTask ?? null)}
          sub={t('efficiency.runsTab.lastRunShort')}
        />
        <Kpi
          label={t('efficiency.runsTab.weekBudget')}
          value={`${decimal(runs.weekUsedPct)}%`}
          sub={t('efficiency.runsTab.cap', { cap: n0(runs.weekCapPct) })}
        />
        <Kpi
          label={t('efficiency.runsTab.harnessFingerprint')}
          value={last?.fingerprint ?? '—'}
          sub={
            last ? plural(points.filter((p) => p.changed).length, 'efficiency.runsTab.changes') : undefined
          }
          hint={t('efficiency.runsTab.fingerprintHint')}
        />
      </KpiRow>
      <Card title={t('efficiency.runsTab.smokeTitle')} sub={t('efficiency.runsTab.smokeSub')}>
        <SmokeChart runs={runs} />
      </Card>
      <DangerCard danger={runs.danger} />
      <ArmsCard arms={runs.arms} />
      <ScheduleTable runs={runs} refresh={refresh} />
      <ManualRun runs={runs} refresh={refresh} />
      <Card title={t('efficiency.runsTab.batchesTitle')} sub={t('efficiency.runsTab.batchesSub')}>
        {runs.batches.length === 0 ? (
          <Empty text={t('efficiency.runsTab.batchesEmpty')} />
        ) : (
          <BatchesTable batches={runs.batches} onOpen={setOpenBatch} />
        )}
      </Card>
      {runs.compare.length > 0 && (
        <Card title={t('efficiency.runsTab.compareTitle')} sub={t('efficiency.runsTab.compareSub')}>
          <div className="grid gap-4 md:grid-cols-2">
            {runs.compare.map((arm) => (
              <ArmCard key={arm.fingerprint} arm={arm} other={runs.compare.find((o) => o !== arm)} />
            ))}
          </div>
        </Card>
      )}
      {batch && <BatchPane batch={batch} onClose={() => setOpenBatch(null)} />}
    </>
  );
};

const RunsTab = ({ resource }: { resource: IEfficiencyResource<IEvalRuns> }) => (
  <Gate
    resource={resource}
    skeleton={
      <>
        <Skeleton className="mb-4 h-28" />
        <Skeleton className="mb-4 h-60" />
        <Skeleton className="h-60" />
      </>
    }
  >
    {(runs) => <Body runs={runs} refresh={resource.refresh} />}
  </Gate>
);

export default RunsTab;
