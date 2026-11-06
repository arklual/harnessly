// The «Команды» screen of the board «Хаб» (admin): the tenant's teams with their people, limits and the four tiles.
// Its own chunk: it loads when the person opens the section.
import './teams/teams.css';

import { useMemo, useState } from 'react';

import { useNow } from '../../lib/hooks';
import { Redirect } from '../../lib/router';
import { useOrg } from '../../org/context';
import { useLive } from '../live';
import { people as peopleText, plural, kRub } from '../../org/screens/teams/format';
import { Btn, Icon } from '../ui';
import { Titles } from '../shell';
import { InviteSheet, NewTeamSheet } from './teams/extra';
import type { Manage } from './teams/manage';
import { flatOf, groupsOf } from './teams/model';
import { moveTo, setRole } from './teams/ops';
import { Empty, GroupCard, LoadFailed, PeopleTable, Problem, Tiles, TeamsSkeleton } from './teams/parts';
import { PersonOrTeamSheet, type Subject } from './teams/sheet';
import { TenantSheet } from './teams/tenant';
import { useTeams } from './teams/use-teams';
import { useUrl } from './teams/url';

const CARDS = ['p', 't', 'dlg'] as const;
type Mode = 'team' | 'flat';

const List = () => {
  const { scope, shell, base } = useOrg();
  const live = useLive();
  const slug = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const { view, error, reload, refresh, apply, problem, clearProblem } = useTeams(slug);
  const url = useUrl(CARDS);
  const now = useNow(60_000);
  const [q, setQ] = useState('');
  const [mode, setMode] = useState<Mode>('team');
  const [opened, setOpened] = useState<Record<string, boolean>>({});

  const pId = url.get('p');
  const tId = url.get('t');
  const dlg = url.get('dlg');
  const subject: Subject | null = pId
    ? { kind: 'person', id: pId }
    : tId && dlg === 'limit'
      ? { kind: 'team', id: tId }
      : null;

  const needle = q.trim();
  const groups = useMemo(() => (view ? groupsOf(view, needle) : []), [view, needle]);
  const flat = useMemo(() => (view ? flatOf(view, needle) : []), [view, needle]);
  const teamCount = view ? view.teams.filter((t) => t.id !== null).length : 0;

  const lead = view
    ? [
        scope.kind === 'tenant' ? `Тенант ${scope.tenant.name}` : 'Все тенанты организации',
        `${teamCount} ${plural(teamCount, 'команда', 'команды', 'команд')} и ${peopleText(view.total)}`,
        view.scope.limitRub !== null ? `лимит тенанта ${kRub(view.scope.limitRub)} в месяц` : null,
      ]
        .filter(Boolean)
        .join(' · ')
    : scope.kind === 'tenant'
      ? `Тенант ${scope.tenant.name}`
      : '';

  // Moving, roles and an invitation from the list itself: only for the one who may change teams, and with live effects.
  const manage: Manage | undefined =
    live && view && view.teams.some((t) => t.people.some((p) => p.can.team || p.can.role))
      ? {
          teams: view.teams.map((t) => ({ id: t.id, name: t.name })),
          move: (id, teamId) => void apply(moveTo(id, teamId)),
          role: (id, role) => void apply(setRole(id, role)),
          invite: (teamId) => url.open({ dlg: 'invite', t: teamId }),
          base,
          block: (teamId) => view.teams.find((t) => t.id === teamId),
        }
      : undefined;

  const noResult = needle !== '' && (mode === 'team' ? groups.length === 0 : flat.length === 0);
  const nobody = view !== null && view.total === 0 && teamCount === 0;
  // Where an invitation or a team goes: the open tenant; in «all» every tenant, and the form asks when there are several.
  const tenants = scope.kind === 'tenant' ? [scope.tenant] : shell.tenants;

  return (
    <>
      <div className="ad-head">
        <Titles title="Команды" lead={lead} />
        {view && (
          <div className="ad-tools">
            {scope.kind === 'tenant' && (
              <Btn
                size="sm"
                icon="settings"
                data-variant="outline"
                onClick={() => url.open({ dlg: 'tenant' })}
              >
                Тенант
              </Btn>
            )}
            {view.can.invite && tenants.length > 0 && (
              <Btn size="sm" icon="mail" data-variant="outline" onClick={() => url.open({ dlg: 'invite' })}>
                Пригласить
              </Btn>
            )}
            {view.can.createTeam && tenants.length > 0 && (
              <Btn variant="primary" size="sm" icon="plus" onClick={() => url.open({ dlg: 'new' })}>
                Новая команда
              </Btn>
            )}
          </div>
        )}
      </div>

      {error != null ? (
        <LoadFailed error={error} onRetry={reload} />
      ) : !view ? (
        <TeamsSkeleton />
      ) : (
        <div className="mo">
          {problem && <Problem text={problem} onClose={clearProblem} />}
          <Tiles view={view} lead={shell.viewer.role === 'lead'} />
          {nobody ? (
            <Empty>Здесь пока никого нет. Пригласите коллег или создайте команду</Empty>
          ) : (
            <>
              <div className="mo-bar">
                <label className="mo-search">
                  <Icon i="search" size="sm" />
                  <input
                    type="search"
                    placeholder="Найти человека или команду"
                    aria-label="Найти человека или команду"
                    value={q}
                    onChange={(e) => setQ(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') setQ('');
                    }}
                  />
                  {q !== '' && (
                    <button
                      type="button"
                      className="mo-search__x"
                      aria-label="Очистить"
                      onClick={() => setQ('')}
                    >
                      <Icon i="x" size="sm" />
                    </button>
                  )}
                </label>
                <span className="hx-spacer" />
                <div className="hx-set__seg" role="radiogroup">
                  {(
                    [
                      ['team', 'По командам'],
                      ['flat', 'Списком'],
                    ] as [Mode, string][]
                  ).map(([id, label]) => (
                    <button
                      key={id}
                      type="button"
                      className="hx-set__seg-item"
                      role="radio"
                      aria-checked={mode === id}
                      onClick={() => setMode(id)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>
              {noResult && <Empty>Ничего не нашлось по «{q}»</Empty>}
              {mode === 'team' && !noResult && (
                <div className="mo-list">
                  {groups.map((g, i) => (
                    <GroupCard
                      key={g.key}
                      g={g}
                      i={i}
                      now={now}
                      open={needle !== '' ? true : (opened[g.key] ?? false)}
                      onToggle={() =>
                        setOpened((o) => ({ ...o, [g.key]: !(needle !== '' ? true : (o[g.key] ?? false)) }))
                      }
                      onGear={g.teamId ? () => url.open({ t: g.teamId, dlg: 'limit', p: null }) : undefined}
                      onOpenPerson={(id) => url.open({ p: id })}
                      manage={manage}
                    />
                  ))}
                </div>
              )}
              {mode === 'flat' && !noResult && (
                <PeopleTable
                  items={flat.map((x) => ({ person: x.person, team: x.team }))}
                  now={now}
                  onOpen={(id) => url.open({ p: id })}
                  manage={manage}
                />
              )}
            </>
          )}
        </div>
      )}

      {view && (
        <>
          <PersonOrTeamSheet
            subject={subject}
            view={view}
            apply={apply}
            problem={problem}
            onClose={() => url.close()}
          />
          <NewTeamSheet
            open={dlg === 'new'}
            onClose={() => url.close()}
            apply={apply}
            problem={problem}
            taken={view.teams.map((t) => t.name)}
            tenants={tenants}
          />
          {scope.kind === 'tenant' && (
            <TenantSheet
              open={dlg === 'tenant'}
              tenant={scope.tenant}
              view={view}
              apply={apply}
              problem={problem}
              onClose={() => url.close()}
            />
          )}
          {tenants.length > 0 && (
            <InviteSheet
              open={dlg === 'invite'}
              presetTeam={dlg === 'invite' ? tId : null}
              tenants={tenants}
              teams={view.teams.flatMap((t) =>
                t.id === null
                  ? []
                  : [
                      {
                        id: t.id,
                        name: t.name,
                        tenantId: t.tenantId ?? (scope.kind === 'tenant' ? scope.tenant.id : undefined),
                      },
                    ],
              )}
              onClose={() => url.close()}
              onChanged={refresh}
            />
          )}
        </>
      )}
    </>
  );
};

const Screen = () => {
  const { shell, base } = useOrg();
  // A lead has one team, and the section for it is «Команда».
  if (shell.viewer.role !== 'admin') return <Redirect to={`${base}/team`} />;
  return <List />;
};

export default Screen;
