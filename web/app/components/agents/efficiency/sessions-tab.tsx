import { useMemo, useState } from 'react';
import { useEfficiency } from '@/hooks/use-efficiency';
import type { IEffMe, IEffSession } from '@/types/efficiency';
import Gate from '@/components/agents/efficiency/gate';
import {
  compact,
  dateTime,
  decimal,
  minutes,
  money,
  n0,
  pct,
  shortDay,
} from '@/components/agents/efficiency/format';
import {
  cardioPath,
  sessionDuration,
  sortSessions,
  type TSessionSort,
} from '@/components/agents/efficiency/sessions-math';
import {
  Card,
  Chip,
  Empty,
  Kpi,
  KpiRow,
  Segmented,
  Skeleton,
} from '@/components/agents/efficiency/primitives';
import { CHANNEL_LABEL } from '@/components/agents/efficiency/changes-tab';
import { plural, t } from '@/lib/i18n';

const SORTS: { value: TSessionSort; label: string }[] = [
  { value: 'cost', label: t('efficiency.sessionsTab.sortCost') },
  { value: 'mrs', label: t('efficiency.sessionsTab.sortMrs') },
  { value: 'date', label: t('efficiency.sessionsTab.sortDate') },
];
const CARDIO_W = 600;
const CARDIO_H = 44;

const Cardiogram = ({ session }: { session: IEffSession }) => {
  const path = cardioPath(session.cardio, CARDIO_W, CARDIO_H);
  if (!path)
    return (
      <span className="text-[12px] text-(--t-text-muted)">{t('efficiency.sessionsTab.tooFewCalls')}</span>
    );
  return (
    <svg
      viewBox={`0 0 ${CARDIO_W} ${CARDIO_H}`}
      preserveAspectRatio="none"
      className="h-11 w-full"
      role="img"
      aria-label={t('efficiency.sessionsTab.cardioLabel', {
        peak: compact(session.peakCtx),
        share: pct(session.over250kShare),
      })}
    >
      <path className="fill-(--t-status-danger) opacity-30" d={path.over} />
      <line
        className="stroke-(--t-status-danger)"
        x1={0}
        x2={CARDIO_W}
        y1={path.limitY}
        y2={path.limitY}
        strokeDasharray="3 3"
        vectorEffect="non-scaling-stroke"
      />
      <path
        className="fill-none stroke-(--t-accent-purple)"
        d={path.line}
        strokeWidth={1.5}
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
};

const SessionRow = ({ s }: { s: IEffSession }) => {
  const hours = sessionDuration(s);
  return (
    <li className="grid gap-x-4 gap-y-1 border-t border-(--t-border-subtle) py-3 first:border-t-0 md:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_150px] md:items-center">
      <div className="min-w-0">
        <span className="block truncate text-[13px] font-medium" title={s.title ?? undefined}>
          {s.title ?? t('efficiency.sessionsTab.untitled')}
        </span>
        <small className="block text-[11px] text-(--t-text-secondary)">
          {dateTime(s.endedAt ?? s.startedAt)} · {CHANNEL_LABEL[s.channel]}
          {hours !== null && ` · ${t('efficiency.sessionsTab.hours', { value: decimal(hours) })}`}
        </small>
      </div>
      <Cardiogram session={s} />
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] tabular-nums md:justify-end">
        <b className="text-[14px]">{money(s.costUsd)}</b>
        <span className="text-(--t-text-secondary)">{n0(s.mrs)} MR</span>
        <span className="text-(--t-text-secondary)">
          {plural(s.modelCalls, 'efficiency.sessionsTab.calls', { count: n0(s.modelCalls) })}
        </span>
        {s.over250kShare !== null && s.over250kShare >= 0.2 && (
          <Chip tone="bad">{t('efficiency.sessionsTab.over250k', { share: pct(s.over250kShare) })}</Chip>
        )}
      </div>
    </li>
  );
};

const Body = ({ me }: { me: IEffMe }) => {
  const [sort, setSort] = useState<TSessionSort>('cost');
  const sessions = useMemo(() => sortSessions(me.sessions, sort), [me.sessions, sort]);
  const a = me.attention;
  const limit = me.limit;
  return (
    <>
      <Card
        title={t('efficiency.sessionsTab.attentionTitle')}
        sub={t('efficiency.sessionsTab.attentionSub', { from: shortDay(me.from), to: shortDay(me.to) })}
      >
        <KpiRow>
          <Kpi
            label={t('efficiency.sessionsTab.replyLabel')}
            value={minutes(a.replyP50Min)}
            sub={t('efficiency.sessionsTab.replySub', { value: minutes(a.replyP90Min) })}
            hint={t('efficiency.sessionsTab.replyHint')}
          />
          <Kpi
            label={t('efficiency.sessionsTab.autonomyLabel')}
            value={minutes(a.autonomyP90Min)}
            sub={t('efficiency.sessionsTab.autonomySub')}
            hint={t('efficiency.sessionsTab.autonomyHint')}
          />
          <Kpi
            label={t('efficiency.sessionsTab.waitLabel')}
            value={minutes(a.askWaitP90Min)}
            sub={plural(a.questions, 'efficiency.sessionsTab.questions', { count: n0(a.questions) })}
            hint={t('efficiency.sessionsTab.waitHint')}
          />
          <Kpi
            label={t('efficiency.sessionsTab.parallelLabel')}
            value={decimal(a.parallelP90, 0)}
            sub={plural(a.interrupts, 'efficiency.sessionsTab.interrupts', { count: n0(a.interrupts) })}
            hint={t('efficiency.sessionsTab.parallelHint')}
          />
        </KpiRow>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-[13px] text-(--t-text-secondary)">
          <span>
            {t('efficiency.sessionsTab.weekLimit')}{' '}
            <b className="tabular-nums text-(--t-text)">
              {limit.weekUsedPct === null ? t('efficiency.sessionsTab.noData') : `${n0(limit.weekUsedPct)}%`}
            </b>
          </span>
          <span>
            {t('efficiency.sessionsTab.evalShare')}{' '}
            <b className="tabular-nums text-(--t-text)">{decimal(limit.evalWeekPct)}%</b>
          </span>
          {limit.resetsAt && (
            <span>{t('efficiency.sessionsTab.resets', { at: dateTime(limit.resetsAt) })}</span>
          )}
        </div>
      </Card>
      <Card
        title={t('efficiency.sessionsTab.sessionsTitle')}
        sub={t('efficiency.sessionsTab.sessionsSub')}
        actions={
          <Segmented
            label={t('efficiency.sessionsTab.sort')}
            value={sort}
            options={SORTS}
            onChange={setSort}
          />
        }
      >
        {sessions.length === 0 ? (
          <Empty text={t('efficiency.sessionsTab.empty')} />
        ) : (
          <ul>
            {sessions.map((s) => (
              <SessionRow key={s.fileId} s={s} />
            ))}
          </ul>
        )}
      </Card>
    </>
  );
};

const SessionsTab = () => {
  const resource = useEfficiency<IEffMe>('/me?days=30');
  return (
    <Gate
      resource={resource}
      skeleton={
        <>
          <Skeleton className="mb-4 h-48" />
          <Skeleton className="h-[480px]" />
        </>
      }
    >
      {(me) => <Body me={me} />}
    </Gate>
  );
};

export default SessionsTab;
