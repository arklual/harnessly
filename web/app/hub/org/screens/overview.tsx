// Обзор: what needs attention, where the money goes, how the shared subscriptions stand. An admin sees the
// tenant (or the whole organization); a team lead sees their team by person; the participant has their own view
// (overview/member.tsx).
import { plural } from './teams/format';
import { useState } from 'react';
import { ChartLine, KeyRound, ListChecks, Users } from 'lucide-react';

import { useOrg } from '../context';
import { inRubles } from '../money';
import { monthWord } from '../dates';
import { useLoad } from '../hooks';
import type { OverviewData, OverviewPeriod } from '../api/overview';
import { Loading, LoadError, Page, PageHead, Seg } from '../ui/page';
import { MiniBars, ProgressLine, Tile } from '../ui/tile';
import { Attention } from './overview/attention';
import { num, rub, tokens, usd, ESTIMATE_NOTE } from './overview/format';
import { MemberOverview } from './overview/member';
import { PeopleSpend } from './overview/people';
import { Subscriptions } from './overview/subscriptions';
import { TeamSpend } from './overview/team-spend';
import './overview/overview.css';

export const PERIODS = [
  { value: 'week', label: 'Неделя' },
  { value: 'month', label: 'Месяц' },
] as const;

export const periodWord = (p: OverviewPeriod) => (p === 'week' ? 'неделя' : monthWord());

const Tiles = ({ data, base }: { data: OverviewData; base: string }) => {
  const { tiles } = data;
  return (
    <div className="ho-tiles">
      <Tile
        tone="violet"
        icon={<ChartLine size={16} />}
        title="Расход"
        line={`${inRubles() ? `${rub(tiles.spend.rub)} · ` : ''}${usd(tiles.spend.usd)} · ${tokens(tiles.spend.tokens)} токенов${data.estimated ? ' · оценка' : ''}`}
        viz={<MiniBars values={tiles.spend.bars} />}
        to={`${base}/analytics`}
      />
      <Tile
        tone="teal"
        icon={<Users size={16} />}
        title="Активны"
        line={`${tiles.active.now} из ${tiles.active.total}`}
        viz={<MiniBars values={tiles.active.bars} />}
        to={`${base}/teams`}
      />
      <Tile
        tone="green"
        icon={<ListChecks size={16} />}
        title="Результат"
        line={`${tiles.tasks.closed} MR · ${tiles.tasks.inProgress} сессий`}
        viz={<MiniBars values={tiles.tasks.bars} />}
        to={`${base}/analytics`}
      />
      {tiles.subscriptions.accounts > 0 && (
        <Tile
          tone="amber"
          icon={<KeyRound size={16} />}
          title="Подписки"
          line={`${tiles.subscriptions.accounts} ${plural(tiles.subscriptions.accounts, 'общий аккаунт', 'общих аккаунта', 'общих аккаунтов')} · ближе всех к лимиту ${tiles.subscriptions.worstName} — ${num(tiles.subscriptions.worstPct, 0)}%`}
          viz={<ProgressLine value={tiles.subscriptions.worstPct / 100} />}
        />
      )}
    </div>
  );
};

const TeamOverview = () => {
  const { shell, scope, base } = useOrg();
  const [period, setPeriod] = useState<OverviewPeriod>('month');
  const tenant = scope.kind === 'tenant' ? scope.tenant.slug : 'all';
  const { data, error, reload } = useLoad((api) => api.overview(tenant, period), [tenant, period]);
  const lead = shell.viewer.role === 'lead';
  const team = shell.teams.find((t) => t.id === shell.viewer.teamId);
  const where = lead
    ? `Команда ${team?.name ?? ''}`
    : scope.kind === 'org'
      ? shell.org.name
      : `Тенант ${scope.tenant.name}`;
  return (
    <Page>
      <PageHead
        title="Обзор"
        sub={`${where} · ${periodWord(period)}`}
        actions={<Seg label="Период" value={period} options={PERIODS} onChange={setPeriod} />}
      />
      {error ? <LoadError error={error} retry={reload} /> : null}
      {!error && !data && <Loading />}
      {data && (
        <>
          <Tiles data={data} base={base} />
          {data.estimated && <p className="mo-note">{ESTIMATE_NOTE}</p>}
          <Attention signals={data.signals} base={base} />
          {data.byPerson && <PeopleSpend data={data.byPerson} base={base} />}
          {data.byTeam && <TeamSpend data={data.byTeam} base={base} />}
          {data.subscriptions.length > 0 && <Subscriptions items={data.subscriptions} />}
        </>
      )}
    </Page>
  );
};

export const OverviewScreen = () => {
  const { shell } = useOrg();
  return shell.viewer.role === 'member' ? <MemberOverview /> : <TeamOverview />;
};
