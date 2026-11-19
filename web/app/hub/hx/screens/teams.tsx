// The «Команды» screen of the board «Хаб» (admin): the tenant's teams with their people, limits and the four tiles.
// Its own chunk: it loads when the person opens the section.
import './teams/teams.css';

import { Fragment, type CSSProperties, useCallback, useEffect, useMemo, useState } from 'react';

import { useNow } from '../../lib/hooks';
import { Redirect } from '../../lib/router';
import { useOrg } from '../../org/context';
import type { TenantInvite } from '../../org/api/tenant';
import { orgErrorText } from '../../org/api/errors';
import { people as peopleText, kRub } from '../../org/screens/teams/format';
import { Btn, Icon, initial } from '../ui';
import { Head } from '../shell';
import { InviteSheet, NewTeamSheet } from './teams/extra';
import { flatOf, kNum, rowOf, seenOf, toneOf } from './teams/model';
import { Empty, LoadFailed, Problem, TeamsSkeleton } from './teams/parts';
import { PersonOrTeamSheet, type Subject } from './teams/sheet';
import { TenantSheet } from './teams/tenant';
import { useTeams } from './teams/use-teams';
import { useUrl } from './teams/url';

const CARDS = ['p', 't', 'dlg'] as const;
type Tab = 'teams' | 'people' | 'invites' | 'roles';
const TABS: Tab[] = ['teams', 'people', 'invites', 'roles'];

const ROLES_ROWS = [
  ['Видит', 'всю организацию', 'свою команду и вложенные', 'себя и свою команду'],
  [
    'Люди и команды',
    'создаёт команды, назначает лидов и роли',
    'приглашает в свою команду, ставит лимиты',
    'смотрит состав команды',
  ],
  [
    'Машины',
    'подтверждает любые, переносит, отключает',
    'подтверждает машины команды',
    'подключает и подтверждает свои',
  ],
  ['Окружение', 'слой организации', 'слой своей команды', 'своё, поверх команды'],
  ['Подписки', 'добавляет, раздаёт командам', 'видит подписки команды', 'видит свою'],
  ['Настройки организации', 'да', 'нет', 'нет'],
];

const Meter = ({ spent, limit }: { spent: number; limit: number | null }) => {
  const tone = toneOf(spent, limit);
  const w = limit && limit > 0 ? Math.min(100, Math.round((spent / limit) * 100)) : 0;
  return (
    <span
      className="hd-meter"
      data-t={tone === 'risk' ? 'bad' : tone === 'warn' ? 'warn' : undefined}
      style={{ '--w': `${w}%` } as CSSProperties}
    >
      <i />
    </span>
  );
};

const Spent = ({ spent, limit }: { spent: number; limit: number | null }) => (
  <td className="num">
    {kRub(spent)}
    {limit !== null && <span className="mut2"> / {kNum(limit)}</span>}
  </td>
);

/** The invitations of the open tenants; they count in the tab and fill it. */
const useInvites = (tenants: { id: string; name: string }[], tick: number) => {
  const { api } = useOrg();
  const [list, setList] = useState<(TenantInvite & { tenant: string; tenantId: string })[] | null>(null);
  const ids = tenants.map((t) => t.id).join(',');
  useEffect(() => {
    let alive = true;
    Promise.all(
      tenants.map((t) =>
        api.tenantInvites(t.id).then(
          (l) => l.map((i) => ({ ...i, tenant: t.name, tenantId: t.id })),
          () => [],
        ),
      ),
    ).then((all) => alive && setList(all.flat()));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, ids, tick]);
  return [list, setList] as const;
};

const List = () => {
  const { scope, shell, api } = useOrg();
  const slug = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const { view, error, reload, refresh, problem, clearProblem, apply } = useTeams(slug);
  const url = useUrl(CARDS);
  const now = useNow(60_000);
  const [q, setQ] = useState('');
  const [tick, setTick] = useState(0);
  const [inviteError, setInviteError] = useState<string | null>(null);

  const raw = url.get('tab');
  const tab: Tab = TABS.find((t) => t === raw) ?? 'teams';

  const pId = url.get('p');
  const tId = url.get('t');
  const dlg = url.get('dlg');
  const subject: Subject | null = pId
    ? { kind: 'person', id: pId }
    : tId && dlg === 'limit'
      ? { kind: 'team', id: tId }
      : null;

  const needle = q.trim();
  const flat = useMemo(() => (view ? flatOf(view, needle) : []), [view, needle]);
  const teamCount = view ? view.teams.filter((t) => t.id !== null).length : 0;
  // Where an invitation or a team goes: the open tenant; in «all» every tenant, and the form asks when there are several.
  const tenants = useMemo(
    () => (scope.kind === 'tenant' ? [scope.tenant] : shell.tenants),
    [scope, shell.tenants],
  );
  const [invites, setInvites] = useInvites(tenants, tick);
  const onChanged = useCallback(() => {
    refresh();
    setTick((n) => n + 1);
  }, [refresh]);

  const revoke = (i: TenantInvite & { tenantId: string }) => {
    const before = invites;
    setInvites((l) => l && l.filter((x) => !(x.id === i.id && x.tenantId === i.tenantId)));
    setInviteError(null);
    api.revokeInvite(i.tenantId, i.id).then(refresh, (e: unknown) => {
      setInvites(before);
      setInviteError(orgErrorText(e));
    });
  };

  const people = view ? view.teams.reduce((n, t) => n + t.people.length, 0) : 0;
  const nobody = view !== null && view.total === 0 && teamCount === 0;
  const orgName = shell.org.name;

  const tabs: [Tab, string][] = [
    ['teams', 'Команды'],
    ['people', `Люди · ${view ? people : ''}`.replace(/ · $/, '')],
    ['invites', `Приглашения · ${invites ? invites.length : (view?.notAccepted ?? 0)}`],
    ['roles', 'Роли и доступ'],
  ];

  const acts = view ? (
    <>
      {scope.kind === 'tenant' && (
        <Btn size="sm" icon="settings" data-variant="outline" onClick={() => url.open({ dlg: 'tenant' })}>
          Тенант
        </Btn>
      )}
      {view.can.createTeam && tenants.length > 0 && (
        <Btn icon="plus" data-variant="outline" onClick={() => url.open({ dlg: 'new' })}>
          Новая команда
        </Btn>
      )}
      {view.can.invite && tenants.length > 0 && (
        <Btn variant="primary" icon="plus" onClick={() => url.open({ dlg: 'invite' })}>
          Пригласить
        </Btn>
      )}
    </>
  ) : undefined;

  const teamsTable = () => {
    if (!view) return null;
    const blocks = view.teams.filter((t) => t.id !== null);
    const none = view.teams.find((t) => t.id === null);
    const groups = tenants
      .map((t) => ({
        tenant: t,
        teams: blocks.filter(
          (b) => (b.tenantId ?? (scope.kind === 'tenant' ? scope.tenant.id : undefined)) === t.id,
        ),
      }))
      .filter((g) => g.teams.length > 0 || scope.kind === 'tenant');
    const known = new Set(groups.flatMap((g) => g.teams.map((b) => b.id)));
    const orphans = blocks.filter((b) => !known.has(b.id));
    const machinesOf = (b: (typeof blocks)[number]) => b.people.reduce((n, p) => n + (p.machines ?? 0), 0);
    const teamRow = (b: (typeof blocks)[number], nested: boolean) => (
      <tr key={b.id} className="click" onClick={() => url.open({ t: b.id, dlg: 'limit', p: null })}>
        <td>
          <div className="row" style={{ paddingLeft: nested ? 26 : 0 }}>
            {nested && <span className="mut2">└</span>}
            <span className="hd-gmark">{initial(b.name)}</span>
            <span className="nm">{b.name}</span>
          </div>
        </td>
        <td>{b.leadName ?? '—'}</td>
        <td className="num">{b.people.length}</td>
        <td className="num">{b.people.some((p) => p.machines != null) ? machinesOf(b) : '—'}</td>
        <td style={{ width: '22%' }}>
          <Meter spent={b.spentRub} limit={b.limitRub} />
        </td>
        <Spent spent={b.spentRub} limit={b.limitRub} />
      </tr>
    );
    const lone = none && none.people.length > 0 ? none.people.length : 0;
    return (
      <div className="t-card viz-card dash-tile hd-card flat">
        <table className="tbl">
          <thead>
            <tr>
              <th>Команда</th>
              <th>Лид</th>
              <th className="num">Люди</th>
              <th className="num">Машины</th>
              <th>Бюджет на месяц</th>
              <th className="num">Потрачено</th>
            </tr>
          </thead>
          <tbody>
            {groups.map(({ tenant, teams }) => {
              const spent =
                scope.kind === 'tenant' ? view.scope.spentRub : teams.reduce((n, b) => n + b.spentRub, 0);
              const limit =
                scope.kind === 'tenant' && view.scope.limitRub !== null
                  ? view.scope.limitRub
                  : teams.some((b) => b.limitRub !== null)
                    ? teams.reduce((n, b) => n + (b.limitRub ?? 0), 0)
                    : null;
              const ppl =
                scope.kind === 'tenant' ? view.total : teams.reduce((n, b) => n + b.people.length, 0);
              const mach = teams.reduce((n, b) => n + machinesOf(b), 0);
              const known = teams.some((b) => b.people.some((p) => p.machines != null));
              const own = scope.kind === 'tenant';
              return (
                <Fragment key={tenant.id}>
                  <tr
                    className={own ? 'click' : undefined}
                    onClick={own ? () => url.open({ dlg: 'tenant' }) : undefined}
                  >
                    <td>
                      <div className="row">
                        <span className="hd-gmark">{initial(tenant.name)}</span>
                        <span className="nm">{tenant.name}</span>
                        {teams.length > 0 && <span className="pill">{teams.length} внутри</span>}
                      </div>
                    </td>
                    <td>—</td>
                    <td className="num">{ppl}</td>
                    <td className="num">{known ? mach : '—'}</td>
                    <td style={{ width: '22%' }}>
                      <Meter spent={spent} limit={limit} />
                    </td>
                    <Spent spent={spent} limit={limit} />
                  </tr>
                  {teams.map((b) => teamRow(b, true))}
                </Fragment>
              );
            })}
            {orphans.map((b) => teamRow(b, false))}
            <tr>
              <td colSpan={6} className="mut sm">
                {lone > 0
                  ? `${peopleText(lone)} без команды: действуют лимиты тенанта. Они видны во вкладке «Люди».`
                  : `Людей без команды нет. Новые люди без команды попадают в «${orgName}» напрямую и видны здесь.`}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    );
  };

  const peopleTable = () => (
    <>
      <div className="row" style={{ marginBottom: 14 }}>
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
            <button type="button" className="mo-search__x" aria-label="Очистить" onClick={() => setQ('')}>
              <Icon i="x" size="sm" />
            </button>
          )}
        </label>
      </div>
      <div className="t-card viz-card dash-tile hd-card flat">
        <table className="tbl">
          <thead>
            <tr>
              <th>Человек</th>
              <th>Команда</th>
              <th>Роль</th>
              <th>Активность</th>
              <th>Лимит на месяц</th>
              <th className="num">Потрачено</th>
              <th className="num">Машины</th>
            </tr>
          </thead>
          <tbody>
            {flat.length === 0 && (
              <tr>
                <td colSpan={7} className="hd-empty">
                  Никого не нашли
                </td>
              </tr>
            )}
            {flat.map(({ person: p, team }) => {
              const r = rowOf(p, now, null);
              const limit = p.effective.source === 'tenant' ? null : p.effective.amountRub;
              return (
                <tr key={p.id} className="click" onClick={() => url.open({ p: p.id })}>
                  <td>
                    <div className="row">
                      <span className="hd-av">{r.ini}</span>
                      <div className="nm">{p.name}</div>
                    </div>
                  </td>
                  <td>{team}</td>
                  <td>
                    <span className={`pill${p.role === 'lead' ? ' info' : ''}`}>{r.role}</span>
                  </td>
                  <td className="mut">
                    {p.invited ? <span className="pill warn">не входил(а)</span> : seenOf(p, now).text}
                  </td>
                  <td style={{ width: '16%' }}>
                    <Meter spent={p.spentRub} limit={limit} />
                  </td>
                  {p.invited ? <td className="num">—</td> : <Spent spent={p.spentRub} limit={limit} />}
                  <td className="num">{p.machines ?? '—'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );

  const invitesTable = () => (
    <>
      {inviteError && <Problem text={inviteError} onClose={() => setInviteError(null)} />}
      <div className="t-card viz-card dash-tile hd-card flat">
        <table className="tbl">
          <thead>
            <tr>
              <th>Почта</th>
              <th>Тенант</th>
              <th>Роль</th>
              <th>Действует до</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {(invites ?? []).length === 0 && (
              <tr>
                <td colSpan={5} className="hd-empty">
                  {invites ? 'Все приглашённые уже вошли' : 'Загружаем'}
                </td>
              </tr>
            )}
            {(invites ?? []).map((i) => (
              <tr key={`${i.tenantId}/${i.id}`}>
                <td className="nm">{i.email ?? 'Ссылка-приглашение'}</td>
                <td>{i.tenant}</td>
                <td>{i.role === 'owner' ? 'админ' : 'участник'}</td>
                <td className="mut">{new Date(i.expiresAt).toLocaleDateString('ru-RU')}</td>
                <td className="num">
                  <Btn size="sm" data-variant="ghost" onClick={() => revoke(i)}>
                    Отозвать
                  </Btn>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );

  const rolesTable = () => (
    <>
      <div className="t-card viz-card dash-tile hd-card flat">
        <table className="tbl">
          <thead>
            <tr>
              <th />
              <th>Админ организации</th>
              <th>Лид команды</th>
              <th>Участник</th>
            </tr>
          </thead>
          <tbody>
            {ROLES_ROWS.map((r) => (
              <tr key={r[0]}>
                <td className="nm">{r[0]}</td>
                <td>{r[1]}</td>
                <td>{r[2]}</td>
                <td>{r[3]}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mut sm" style={{ marginTop: 12 }}>
        Роль даётся на организацию (админ) или на команду (лид). Лид вложенной команды — не лид родительской.
      </div>
    </>
  );

  return (
    <>
      <Head
        title="Люди и команды"
        lead="Команды вложены друг в друга: отдел → команда. Других контейнеров нет."
        acts={acts}
      />
      <nav className="hx-set__nav hb-tabs hd-tabs">
        {tabs.map(([id, label]) => (
          <button
            key={id}
            type="button"
            className="hx-set__nav-item"
            aria-current={tab === id}
            onClick={() => url.put({ tab: id === 'teams' ? null : id })}
          >
            <span>{label}</span>
          </button>
        ))}
      </nav>

      {error != null ? (
        <LoadFailed error={error} onRetry={reload} />
      ) : !view ? (
        <TeamsSkeleton />
      ) : (
        <div>
          {problem && <Problem text={problem} onClose={clearProblem} />}
          {tab === 'teams' &&
            (nobody ? (
              <Empty>Здесь пока никого нет. Пригласите коллег или создайте команду</Empty>
            ) : (
              teamsTable()
            ))}
          {tab === 'people' && peopleTable()}
          {tab === 'invites' && invitesTable()}
          {tab === 'roles' && rolesTable()}
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
              onChanged={onChanged}
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
