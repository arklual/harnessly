import { issueUrlOf } from '@/lib/site';
import { useMemo } from 'react';
import { useEfficiency } from '@/hooks/use-efficiency';
import type { IEffHypothesis } from '@/types/efficiency';
import Gate from '@/components/agents/efficiency/gate';
import { pct, plainValue } from '@/components/agents/efficiency/format';
import {
  bullet,
  groupHypotheses,
  progress,
  stateCounts,
} from '@/components/agents/efficiency/hypotheses-math';
import {
  Card,
  Chip,
  KeyValuesRow,
  Note,
  Pane,
  PaneHeading,
  Skeleton,
} from '@/components/agents/efficiency/primitives';
import { t } from '@/lib/i18n';

/** The tracker's own state name, as the backend sends it: data to compare with, not page text. */
const RUNNING_STATE = 'Идёт';

const W = 170;
const px = (p: number) => 4 + (p / 100) * (W - 8);

const Bullet = ({ h }: { h: IEffHypothesis }) => {
  const b = bullet(h);
  if (!b)
    return (
      <span className="text-[12px] text-(--t-text-muted)">{t('efficiency.hypothesesTab.measuredLater')}</span>
    );
  const currentColor = b.reached
    ? 'stroke-(--t-status-success) fill-(--t-status-success)'
    : 'stroke-(--t-accent-purple) fill-(--t-accent-purple)';
  return (
    <svg
      viewBox={`0 0 ${W} 26`}
      className="h-[26px] w-[170px]"
      role="img"
      aria-label={t('efficiency.hypothesesTab.bulletLabel', {
        name: h.name,
        base: plainValue(h.base),
        target: plainValue(h.target),
        current: plainValue(h.current),
      })}
    >
      <rect className="fill-(--t-surface-input)" x={4} y={11} width={W - 8} height={4} rx={2} />
      <rect
        className="fill-(--t-accent-green-bg) stroke-(--t-status-success)"
        x={px(b.zoneFrom)}
        y={8}
        width={Math.max(3, px(b.zoneTo) - px(b.zoneFrom))}
        height={10}
        rx={3}
        strokeOpacity={0.55}
      />
      <line
        className="stroke-(--t-text-secondary)"
        x1={px(b.base)}
        x2={px(b.base)}
        y1={4}
        y2={22}
        strokeWidth={2}
      />
      {b.current !== null && (
        <g className={currentColor}>
          <line x1={px(b.base)} x2={px(b.current)} y1={13} y2={13} strokeWidth={2} />
          <circle cx={px(b.current)} cy={13} r={5} />
        </g>
      )}
    </svg>
  );
};

const StateChip = ({ state }: { state: string }) => (
  <Chip tone={state === RUNNING_STATE ? 'warn' : 'flat'}>{state}</Chip>
);

const HypPane = ({ h, onClose }: { h: IEffHypothesis; onClose: () => void }) => {
  const reached = bullet(h)?.reached ?? null;
  return (
    <Pane
      chip={
        <span className="flex items-center gap-2">
          <StateChip state={h.state} />
          <span className="text-(--t-text-secondary)">{h.num}</span>
        </span>
      }
      title={h.name}
      onClose={onClose}
    >
      <p className="text-(--t-text-secondary)">{h.group}</p>
      <PaneHeading>{t('efficiency.hypothesesTab.metricAndTarget')}</PaneHeading>
      <p className="mb-3">{h.metric}</p>
      <KeyValuesRow
        rows={[
          { label: t('efficiency.hypothesesTab.september'), value: plainValue(h.base) },
          { label: t('efficiency.hypothesesTab.target'), value: plainValue(h.target) },
          {
            label: t('efficiency.hypothesesTab.now'),
            value: h.current === null ? t('efficiency.hypothesesTab.noData') : plainValue(h.current),
          },
          {
            label: t('efficiency.hypothesesTab.better'),
            value:
              h.better === 'down'
                ? t('efficiency.hypothesesTab.lower')
                : t('efficiency.hypothesesTab.higher'),
          },
        ]}
      />
      {reached !== null && (
        <p className="mt-3">
          <Chip tone={reached ? 'ok' : 'warn'}>
            {reached ? t('efficiency.hypothesesTab.reached') : t('efficiency.hypothesesTab.ahead')}
          </Chip>
        </p>
      )}
      <PaneHeading>{t('efficiency.hypothesesTab.experiment')}</PaneHeading>
      {h.exp ? (
        <a className="ts-link" href={issueUrlOf(h.exp) ?? undefined} target="_blank" rel="noreferrer">
          {h.exp}
        </a>
      ) : (
        <p className="text-(--t-text-secondary)">{t('efficiency.hypothesesTab.noExperiment')}</p>
      )}
      <Note>{t('efficiency.hypothesesTab.legend')}</Note>
    </Pane>
  );
};

interface IHypothesesTabProps {
  openId: string | null;
  onOpen: (id: string | null) => void;
}

const HypothesesTab = ({ openId, onOpen }: IHypothesesTabProps) => {
  const resource = useEfficiency<IEffHypothesis[]>('/hypotheses');
  return (
    <Gate resource={resource} skeleton={<Skeleton className="h-[640px]" />}>
      {(list) => <Board list={list} openId={openId} onOpen={onOpen} />}
    </Gate>
  );
};

const Board = ({
  list,
  openId,
  onOpen,
}: {
  list: IEffHypothesis[];
  openId: string | null;
  onOpen: (id: string | null) => void;
}) => {
  const groups = useMemo(() => groupHypotheses(list), [list]);
  const counts = useMemo(() => stateCounts(list), [list]);
  const opened = list.find((h) => h.id === openId) ?? null;
  return (
    <Card title={t('efficiency.hypothesesTab.title')} sub={t('efficiency.hypothesesTab.sub')}>
      <div className="mb-3 flex flex-wrap gap-2">
        {counts.map((c) => (
          <Chip key={c.state} tone={c.state === RUNNING_STATE ? 'warn' : 'flat'}>
            {c.state} · {c.count}
          </Chip>
        ))}
      </div>
      {list.length === 0 ? (
        <p className="ts-muted">{t('efficiency.hypothesesTab.empty')}</p>
      ) : (
        <div className="ts-table-wrap">
          <table className="ts-table">
            <thead>
              <tr>
                <th>{t('efficiency.hypothesesTab.colNum')}</th>
                <th>{t('efficiency.hypothesesTab.colHypothesis')}</th>
                <th>{t('efficiency.hypothesesTab.metricAndTarget')}</th>
                <th>{t('efficiency.hypothesesTab.colProgress')}</th>
                <th>{t('efficiency.hypothesesTab.colState')}</th>
              </tr>
            </thead>
            <tbody>
              {groups.flatMap((g) => [
                <tr key={`g:${g.group}`}>
                  <td
                    colSpan={5}
                    className="bg-(--t-surface-input) text-[12px] font-semibold text-(--t-text-secondary)"
                  >
                    {g.group}
                  </td>
                </tr>,
                ...g.items.map((h) => {
                  const done = progress(h);
                  return (
                    <tr
                      key={h.id}
                      className="cursor-pointer"
                      tabIndex={0}
                      aria-label={`${h.num} ${h.name}`}
                      onClick={() => onOpen(h.id)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') onOpen(h.id);
                      }}
                    >
                      <td className="whitespace-nowrap text-(--t-text-secondary)">{h.num}</td>
                      <td className="font-medium">{h.name}</td>
                      <td className="text-(--t-text-secondary)">{h.metric}</td>
                      <td>
                        <Bullet h={h} />
                      </td>
                      <td className="whitespace-nowrap">
                        <StateChip state={h.state} />
                        {done !== null && (
                          <small className="ml-2 tabular-nums text-(--t-text-secondary)">
                            {t('efficiency.hypothesesTab.ofTheWay', { share: pct(done) })}
                          </small>
                        )}
                      </td>
                    </tr>
                  );
                }),
              ])}
            </tbody>
          </table>
        </div>
      )}
      {opened && <HypPane h={opened} onClose={() => onOpen(null)} />}
    </Card>
  );
};

export default HypothesesTab;
