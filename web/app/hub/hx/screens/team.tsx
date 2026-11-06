// The «Команда» screen of the board «Хаб»: the lead's own team, or one team the admin opened from «Команды».
// People, the budget and the limits of the team, and what a new person gets. Its own chunk.
import './teams/teams.css';

import { type CSSProperties, useRef, useState } from 'react';

import { useNow } from '../../lib/hooks';
import { navigate, Redirect } from '../../lib/router';
import { useOrg } from '../../org/context';
import type { MemberRole, Person, TeamBlock, TeamsView } from '../../org/api/teams';
import { kRub, needsText } from '../../org/screens/teams/format';
import { Btn, Icon, initial, usePop } from '../ui';
import { Titles } from '../shell';
import { InviteSheet } from './teams/extra';
import { TeamEditSheet } from './teams/team-edit';
import {
  budgetOf,
  envSummary,
  machinesHint,
  personMeta,
  ROLE_HINT,
  secretsText,
  teamLead,
} from './teams/model';
import { machinesPerPerson, setRole } from './teams/ops';
import { LoadFailed, Problem, TeamsSkeleton } from './teams/parts';
import { PersonOrTeamSheet, type Subject } from './teams/sheet';
import { useUrl } from './teams/url';
import { type Op, useTeams } from './teams/use-teams';

const CARDS = ['p', 'dlg'] as const;

const ROLES: MemberRole[] = ['lead', 'member'];
const ROLE_LABEL: Record<MemberRole, string> = { lead: 'Руководитель', member: 'Участник' };

/** The role of a person as a button with the menu under it. */
const RoleMenu = ({ p, apply }: { p: Person; apply: (op: Op) => Promise<boolean> }) => {
  const root = useRef<HTMLSpanElement>(null);
  const pop = usePop(root);
  const locked = !p.can.role || p.self;
  if (locked) {
    return (
      <button
        type="button"
        className="ad-role"
        disabled
        title={
          p.self
            ? 'Себе менять нельзя'
            : p.needs.role
              ? `Может поменять: ${needsText(p.needs.role)}`
              : undefined
        }
      >
        {ROLE_LABEL[p.role]}
      </button>
    );
  }
  return (
    <span className="hx-set__pick" ref={root}>
      <button
        type="button"
        className="ad-role hx-set__pick-btn"
        aria-haspopup="listbox"
        aria-expanded={pop.open}
        aria-label={`Роль: ${p.name}`}
        onClick={pop.toggle}
      >
        {ROLE_LABEL[p.role]}
        <Icon i="chevron-down" size="sm" />
      </button>
      {pop.mounted && (
        <div className="hx-set-menu" role="listbox" data-state={pop.closing ? 'closing' : undefined}>
          {ROLES.map((r) => (
            <button
              key={r}
              type="button"
              role="option"
              className="hx-set-menu__item"
              aria-selected={r === p.role}
              onClick={() => {
                pop.close();
                if (r !== p.role) void apply(setRole(p.id, r));
              }}
            >
              <span className="hx-set-menu__text">
                <span>{ROLE_LABEL[r]}</span>
                <span className="hx-set-menu__sub">{ROLE_HINT[r]}</span>
              </span>
              <Icon i="check" size="sm" />
            </button>
          ))}
        </div>
      )}
    </span>
  );
};

const PeopleGroup = ({
  b,
  now,
  apply,
  onOpen,
}: {
  b: TeamBlock;
  now: number;
  apply: (op: Op) => Promise<boolean>;
  onOpen: (id: string) => void;
}) => (
  <div className="hx-set__group">
    <div className="hx-set__group-title">Люди</div>
    <div className="hx-set__card">
      {b.people.length === 0 && (
        <div className="ad-row tm-row">
          <span className="ad-row__t">
            <small>В команде пока никого</small>
          </span>
        </div>
      )}
      {b.people.map((p) => (
        <div className="ad-row tm-row" key={p.id}>
          <button
            type="button"
            className="tm-hit"
            onClick={() => onOpen(p.id)}
            aria-label={`Открыть: ${p.name}`}
          >
            <span className="hx-avatar">{initial(p.name)}</span>
            <span className="ad-row__t">
              <b>{p.name}</b>
              <small>{personMeta(p, now)}</small>
            </span>
            <span className="ad-row__v">{p.invited ? '—' : kRub(p.spentRub)}</span>
          </button>
          <RoleMenu p={p} apply={apply} />
        </div>
      ))}
    </div>
  </div>
);

const Limits = ({
  b,
  base,
  apply,
  onLimit,
  canMachines,
  extras,
}: {
  b: TeamBlock;
  base: string;
  apply: (op: Op) => Promise<boolean>;
  onLimit: () => void;
  canMachines: boolean;
  /** The hub keeps machines per person, shared accounts and secrets; when it does not, the rows are not drawn. */
  extras: boolean;
}) => {
  const bud = budgetOf(b);
  const x = b.extras;
  const per = x?.machinesPerPerson ?? null;
  const goEnv = () => navigate(`${base}/env`);
  return (
    <div className="hx-set__group">
      <div className="hx-set__group-title">Лимиты</div>
      <div className="hx-set__card">
        <div className="hx-set__row">
          <div className="hx-set__row-text">
            <span className="hx-set__row-label">Бюджет в месяц</span>
            <span className="hx-set__row-hint">{bud.hint}</span>
            <div className="ad-meter" data-tone={bud.warn ? 'warn' : undefined}>
              <i style={{ '--w': `${Math.round(bud.w * 100)}%` } as CSSProperties} />
            </div>
          </div>
          {b.can.limit && b.id ? (
            <button type="button" className="hx-set__pick-btn tm-lim-btn" onClick={onLimit}>
              <span>{bud.value}</span>
              <Icon i="chevron-down" size="sm" />
            </button>
          ) : (
            <span
              className="ad-row__v"
              title={b.needs.limit ? `Может поменять: ${needsText(b.needs.limit)}` : undefined}
            >
              {bud.value}
            </span>
          )}
        </div>
        {extras && (
          <>
            <div className="hx-set__row">
              <div className="hx-set__row-text">
                <span className="hx-set__row-label">Машин на человека</span>
                <span className="hx-set__row-hint">{machinesHint(b)}</span>
              </div>
              <div className="hx-set__seg" role="radiogroup" data-off={!canMachines || undefined}>
                {[1, 2, 3].map((n) => (
                  <button
                    key={n}
                    type="button"
                    className="hx-set__seg-item"
                    role="radio"
                    aria-checked={per === n}
                    disabled={!canMachines}
                    onClick={() => {
                      if (b.id && per !== n) void apply(machinesPerPerson(b.id, n));
                    }}
                  >
                    {n}
                  </button>
                ))}
              </div>
            </div>
            <div className="hx-set__row">
              <div className="hx-set__row-text">
                <span className="hx-set__row-label">Общие аккаунты</span>
                <span className="hx-set__row-hint">Агенты людей команды входят через общие подписки</span>
              </div>
              <button type="button" className="hx-set__link" onClick={goEnv}>
                {x?.accounts ?? '—'}
                <Icon i="chevron-right" size="sm" />
              </button>
            </div>
            <div className="hx-set__row">
              <div className="hx-set__row-text">
                <span className="hx-set__row-label">Секреты</span>
                <span className="hx-set__row-hint">Приходят на машины команды, значения не видны никому</span>
              </div>
              <button type="button" className="hx-set__link" onClick={goEnv}>
                {secretsText(x?.secrets)}
                <Icon i="chevron-right" size="sm" />
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
};

const Body = ({
  view,
  b,
  apply,
  problem,
  clearProblem,
  now,
  base,
  url,
}: {
  view: TeamsView;
  b: TeamBlock;
  apply: (op: Op) => Promise<boolean>;
  problem: string | null;
  clearProblem: () => void;
  now: number;
  base: string;
  url: ReturnType<typeof useUrl>;
}) => (
  <>
    {problem && <Problem text={problem} onClose={clearProblem} />}
    <PeopleGroup b={b} now={now} apply={apply} onOpen={(id) => url.open({ p: id })} />
    <Limits
      b={b}
      base={base}
      apply={apply}
      extras={view.supports.teamExtras}
      canMachines={b.can.limit && !!b.id}
      onLimit={() => url.open({ dlg: 'limit', p: null })}
    />
    <div className="hx-set__group">
      <div className="hx-set__group-title">Окружение</div>
      <div className="hx-set__card">
        <button type="button" className="ad-row ad-row--btn" onClick={() => navigate(`${base}/env`)}>
          <span className="ad-ic">
            <Icon i="layers" size="sm" />
          </span>
          <span className="ad-row__t">
            <b>Новый человек получит</b>
            <small>{envSummary(b)}</small>
          </span>
          <Icon i="chevron-right" size="sm" />
        </button>
      </div>
    </div>
    <PersonOrTeamSheet
      subject={
        url.get('p')
          ? ({ kind: 'person', id: url.get('p') as string } satisfies Subject)
          : url.get('dlg') === 'limit' && b.id
            ? ({ kind: 'team', id: b.id } satisfies Subject)
            : null
      }
      view={view}
      apply={apply}
      problem={problem}
      onClose={() => url.close()}
    />
  </>
);

const Page = ({ teamId }: { teamId: string | null }) => {
  const { scope, shell, base } = useOrg();
  const url = useUrl(CARDS);
  const now = useNow(60_000);
  const [tn, setTn] = useState<string | null>(null);
  const [seen, setSeen] = useState<{ slug: string; name: string }[]>([]);
  const own = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const slug = tn ?? own;
  const { view, error, reload, refresh, apply, problem, clearProblem } = useTeams(slug);

  const b = view ? (view.teams.find((t) => (teamId ? t.id === teamId : t.id !== null)) ?? null) : null;
  const tenants = b?.extras?.tenants;
  if (
    tenants &&
    tenants.length >= 2 &&
    (seen.length !== tenants.length || seen.some((s, i) => s.slug !== tenants[i].slug))
  ) {
    setSeen(tenants);
  }
  const tabs = seen.length >= 2 ? seen : [];
  const current = tabs.find((t) => t.slug === slug);
  const tenantName =
    b?.extras?.tenantName ?? current?.name ?? (scope.kind === 'tenant' ? scope.tenant.name : null);
  // The tenant to invite to: the open one, else the tenant of the team (in the «all» view the slug names none).
  const invitee =
    shell.tenants.find((t) => t.slug === slug) ?? shell.tenants.find((t) => t.id === b?.tenantId) ?? null;
  const admin = shell.viewer.role === 'admin';

  const lead = b ? teamLead(b) : '';
  return (
    <>
      <div className="ad-head">
        <Titles title={b?.name ?? 'Команда'} lead={lead} />
        {b && view && (
          <div className="ad-tools">
            {admin && b.id && (
              <Btn size="sm" icon="pencil" data-variant="outline" onClick={() => url.open({ dlg: 'edit' })}>
                Изменить
              </Btn>
            )}
            {view.can.invite && invitee && (
              <Btn size="sm" icon="mail" data-variant="outline" onClick={() => url.open({ dlg: 'invite' })}>
                Пригласить
              </Btn>
            )}
          </div>
        )}
      </div>
      {tabs.length > 0 && (
        <div className="ad-tenants" role="group" aria-label="Тенант">
          {tabs.map((t) => (
            <button key={t.slug} type="button" aria-pressed={t.slug === slug} onClick={() => setTn(t.slug)}>
              {t.name}
            </button>
          ))}
        </div>
      )}
      {tabs.length > 0 && tenantName && (
        <div className="ad-note">Лимиты и окружение ниже — для тенанта {tenantName}</div>
      )}
      {error != null ? (
        <LoadFailed error={error} onRetry={reload} />
      ) : !view ? (
        <TeamsSkeleton rows={4} />
      ) : !b ? (
        <div className="mo-empty">Такой команды нет</div>
      ) : (
        <Body
          view={view}
          b={b}
          apply={apply}
          problem={problem}
          clearProblem={clearProblem}
          now={now}
          base={base}
          url={url}
        />
      )}
      {b && view && admin && b.id && (
        <TeamEditSheet
          open={url.get('dlg') === 'edit'}
          teamId={b.id}
          name={b.name}
          peopleCount={b.people.length}
          taken={view.teams.filter((t) => t.id !== b.id).map((t) => t.name)}
          canDelete
          apply={apply}
          problem={problem}
          onClose={() => url.close()}
          onDeleted={() => navigate(`${base}/teams`)}
        />
      )}
      {invitee && view && (
        <InviteSheet
          open={url.get('dlg') === 'invite'}
          tenants={[invitee]}
          teams={view.teams.flatMap((t) =>
            t.id === null ? [] : [{ id: t.id, name: t.name, tenantId: t.tenantId ?? invitee.id }],
          )}
          onClose={() => url.close()}
          onChanged={refresh}
        />
      )}
    </>
  );
};

const Screen = () => {
  const { shell, base } = useOrg();
  const t = new URLSearchParams(typeof window === 'undefined' ? '' : window.location.search).get('t');
  if (shell.viewer.role === 'admin') {
    // The admin opens a team from the list; without one there is nothing to show.
    return t ? <Page key={t} teamId={t} /> : <Redirect to={`${base}/teams${window.location.search}`} />;
  }
  if (shell.viewer.role === 'member') return <Redirect to={`${base}/overview${window.location.search}`} />;
  return <Page teamId={shell.viewer.teamId ?? null} />;
};

export default Screen;
