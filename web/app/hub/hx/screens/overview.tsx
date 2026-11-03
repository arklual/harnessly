// The «Обзор» screen of the board «Хаб»: the money, the quotas and the efficiency for an admin and a team lead, the
// own limit and chats for a participant. Its own chunk: it loads when the person opens the section.
import { useState } from 'react';

import { navigate, useLocation } from '../../lib/router';
import type { MyOverview } from '../../org/api/member';
import type { OverviewData, OverviewPeriod } from '../../org/api/overview';
import { useOrg } from '../../org/context';
import { useLoad } from '../../org/hooks';
import { Titles } from '../shell';
import {
  Attention,
  Efficiency,
  HeadTiles,
  LoadFailed,
  PeopleSpend,
  Skeleton,
  SpendDonut,
  Subscriptions,
} from './overview/parts';
import { MemberView } from './overview/member';
import { overviewLead, PERIODS } from './overview/fmt';
import '../styles/overview.css';

/** The period lives in the address (`?period=week`), so a link and a reload keep it; the month is the default. */
const usePeriod = (): [OverviewPeriod, (p: OverviewPeriod) => void] => {
  const { path, search } = useLocation();
  const period: OverviewPeriod = new URLSearchParams(search).get('period') === 'week' ? 'week' : 'month';
  const set = (p: OverviewPeriod) => {
    const q = new URLSearchParams(search);
    if (p === 'week') q.set('period', 'week');
    else q.delete('period');
    const s = q.toString();
    navigate(s ? `${path}?${s}` : path, { replace: true });
  };
  return [period, set];
};

const PeriodSeg = ({ value, onChange }: { value: OverviewPeriod; onChange: (p: OverviewPeriod) => void }) => (
  <div className="ad-tools">
    <div className="hx-set__seg" role="radiogroup" aria-label="Период">
      {PERIODS.map((p) => (
        <button
          key={p.value}
          type="button"
          className="hx-set__seg-item"
          role="radio"
          aria-checked={p.value === value}
          onClick={() => onChange(p.value)}
        >
          {p.label}
        </button>
      ))}
    </div>
  </div>
);

/** What a load shows: the last answer stays on the screen while the next one is on its way. */
const useKept = <T,>(data: T | null): T | null => {
  const [kept, setKept] = useState<T | null>(data);
  if (data && data !== kept) setKept(data);
  return data ?? kept;
};

/** The title with its line and the period switch; the same on every state of the screen. */
const Head = ({
  role,
  tenantName,
  teamName,
  period,
  onPeriod,
}: {
  role: 'admin' | 'lead' | 'member';
  tenantName: string | null;
  teamName: string | null;
  period: OverviewPeriod;
  onPeriod: (p: OverviewPeriod) => void;
}) => (
  <div className="ad-head">
    <Titles title="Обзор" lead={overviewLead(role, { tenantName, teamName }, period)} />
    <PeriodSeg value={period} onChange={onPeriod} />
  </div>
);

interface ViewProps {
  period: OverviewPeriod;
  onPeriod: (p: OverviewPeriod) => void;
}

const StaffView = ({
  slug,
  role,
  tenantName,
  period,
  onPeriod,
}: ViewProps & { slug: string; role: 'admin' | 'lead'; tenantName: string | null }) => {
  const { base } = useOrg();
  const { data, error, reload } = useLoad((api) => api.overview(slug, period), [slug, period]);
  const shown = useKept<OverviewData>(data);
  const head = (
    <Head
      role={role}
      tenantName={tenantName}
      teamName={shown?.byPerson?.teamName ?? null}
      period={period}
      onPeriod={onPeriod}
    />
  );
  if (!shown)
    return (
      <>
        {head}
        {error ? <LoadFailed error={error} retry={reload} /> : <Skeleton member={false} />}
      </>
    );
  return (
    <>
      {head}
      <div className="dash ad-dash" data-busy={data ? undefined : '1'}>
        {error ? <LoadFailed error={error} retry={reload} /> : null}
        <div className="an-stage">
          <div className="mx">
            <HeadTiles data={shown} period={period} base={base} />
            <Attention signals={shown.signals} base={base} />
            {role === 'lead' && shown.byPerson && (
              <PeopleSpend data={shown.byPerson} base={base} period={period} />
            )}
            {role === 'admin' && shown.byTeam && (
              <div className="mx-split">
                <SpendDonut data={shown.byTeam} period={period} base={base} />
                <Efficiency rows={shown.byTeam.efficiency} base={base} period={period} />
              </div>
            )}
            <Subscriptions items={shown.subscriptions} base={base} />
          </div>
        </div>
      </div>
    </>
  );
};

const MemberScreen = ({ period, onPeriod }: ViewProps) => {
  const { data, error, reload } = useLoad((api) => api.myOverview(period), [period]);
  const shown = useKept<MyOverview>(data);
  const head = <Head role="member" tenantName={null} teamName={null} period={period} onPeriod={onPeriod} />;
  if (!shown)
    return (
      <>
        {head}
        {error ? <LoadFailed error={error} retry={reload} /> : <Skeleton member />}
      </>
    );
  return (
    <>
      {head}
      <div className="dash ad-dash" data-busy={data ? undefined : '1'}>
        {error ? <LoadFailed error={error} retry={reload} /> : null}
        <div className="an-stage">
          <div className="mx">
            <MemberView data={shown} period={period} />
          </div>
        </div>
      </div>
    </>
  );
};

const Screen = () => {
  const { shell, scope } = useOrg();
  const role = shell.viewer.role;
  const [period, setPeriod] = usePeriod();
  if (role === 'member') return <MemberScreen period={period} onPeriod={setPeriod} />;
  const slug = scope.kind === 'tenant' ? scope.tenant.slug : 'all';
  return (
    <StaffView
      key={`${role}:${slug}`}
      slug={slug}
      role={role}
      tenantName={scope.kind === 'tenant' ? scope.tenant.name : null}
      period={period}
      onPeriod={setPeriod}
    />
  );
};

export default Screen;
