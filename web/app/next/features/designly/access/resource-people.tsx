// «У кого есть доступ» of the one dialog (HRN-675): the owner; the home team first, with its role and «Убрать доступ
// команды» (a removed one reads «Команда X — нет доступа · Вернуть»); then the people and teams with a role of their
// own, each with its menu: the role, «Может делиться» at edit (for a manager), the term, «Убрать доступ». Above them the
// requests, when someone asks. Every change waits for the gateway.
import { useMemo, useState } from 'react';
import type { TTtlValue } from '@/next/features/designly/access/api';
import { canLabel, roleLabel, untilLabel } from '@/next/features/designly/access/labels';
import { Who } from '@/next/features/designly/access/access-people';
import { Mark } from '@/next/features/designly/access/resource-add';
import {
  decide,
  putGrant,
  putTeamRole,
  removeGrant,
  type IGrant,
  type IResourceRef,
  type IResourceRequest,
  type IResourceView,
  type TShareRole,
} from '@/next/features/designly/access/resource-api';
import RoleMenu from '@/next/features/designly/access/role-menu';
import type { IAction } from '@/next/features/designly/access/use-action';
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';
import Tooltip from '@/next/ui/tooltip';
import { t } from '@/next/i18n';

const OUT_MS = 200;
const ROLES = ['view', 'comment', 'edit'] as const satisfies readonly TShareRole[];
const NO_GRANTS: IGrant[] = [];

export interface IResourceSection {
  target: IResourceRef;
  view: IResourceView;
  action: IAction;
  onNext: (next: IResourceView) => void;
}

const run = ({ action, onNext }: IResourceSection, key: string, call: () => Promise<IResourceView>) =>
  void action.run(key, async () => onNext(await call()));

const GrantRow = ({
  props,
  grant,
  me,
  leaving,
}: {
  props: IResourceSection;
  grant: IGrant;
  me: string | null;
  leaving: boolean;
}) => {
  const { target, view, action } = props;
  const idle = action.busy === null && !leaving;
  const key = `${grant.type}:${grant.id}`;
  // Only a manager touches one who may share; nobody but the owner touches their own grant.
  const fixed =
    (grant.canShare && !view.manager) || (grant.type === 'user' && grant.id === me && view.role !== 'owner');
  const sub = [
    grant.sub,
    fixed ? '' : grant.canShare && t('next.access.mayShareShort'),
    untilLabel(grant.expiresAt),
  ]
    .filter(Boolean)
    .join(' · ');
  const put = (role: TShareRole, canShare: boolean, ttl: TTtlValue) =>
    run(props, `grant:${key}`, () =>
      putGrant(target, grant.type, grant.id, { role, canShare: role === 'edit' && canShare, ttl }),
    );
  return (
    <li className="hx-acc-row" data-leaving={leaving || undefined}>
      {grant.type === 'team' ? (
        <>
          <Mark type="team" name={grant.name} />
          <span className="hx-acc-row__who">
            <span className="hx-acc-row__name">
              <span className="hx-acc-row__nm">{grant.name}</span>
            </span>
            {sub && <span className="hx-acc-row__sub">{sub}</span>}
          </span>
        </>
      ) : (
        <Who name={grant.name} sub={sub || null} you={grant.id === me} />
      )}
      {fixed ? (
        <span className="hx-acc-row__fixed">
          {canLabel(grant.role)}
          {grant.canShare ? ` · ${t('next.access.mayShareShort')}` : ''}
        </span>
      ) : (
        <RoleMenu
          label={t('next.access.roleOf', { who: grant.name })}
          role={grant.role}
          roles={ROLES}
          disabled={!idle}
          onRole={(next) => put(next, grant.canShare, grant.expiresAt)}
          share={
            view.manager
              ? { on: grant.canShare, onToggle: () => put(grant.role, !grant.canShare, grant.expiresAt) }
              : undefined
          }
          term={{ value: grant.expiresAt, onChange: (next) => put(grant.role, grant.canShare, next) }}
          onRemove={() => run(props, `rm:${key}`, () => removeGrant(target, grant.type, grant.id))}
        />
      )}
    </li>
  );
};

/** The home team's row: its role and menu for a manager; a removed one with «Вернуть». */
const TeamRow = ({ props }: { props: IResourceSection }) => {
  const { target, view, action } = props;
  const team = view.homeTeam;
  if (!team) return null;
  const name = t('next.access.teamName', { name: team.name });
  const idle = action.busy === null;
  if (team.role === null)
    return (
      <li className="hx-acc-row hx-acc-gone">
        <span className="hx-acc-av hx-acc-av--team" data-tone="quiet" aria-hidden="true">
          <Icon name="users" size="sm" />
        </span>
        <span className="hx-acc-row__who">
          <span className="hx-acc-row__sub">{t('next.access.teamGone', { name })}</span>
        </span>
        {view.manager && (
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-busy={action.busy === 'team'}
            disabled={!idle}
            onClick={() => run(props, 'team', () => putTeamRole(target, 'edit'))}
          >
            <span>{t('next.access.teamBack')}</span>
            <span className="hx-gate__spin" aria-hidden="true" />
          </button>
        )}
      </li>
    );
  return (
    <li className="hx-acc-row">
      <Mark type="team" name={name} />
      <span className="hx-acc-row__who">
        <span className="hx-acc-row__name">
          <span className="hx-acc-row__nm">{name}</span>
        </span>
        <span className="hx-acc-row__sub">{t('next.access.teamAll')}</span>
      </span>
      {view.manager ? (
        <RoleMenu
          label={t('next.access.roleOf', { who: name })}
          role={team.role}
          roles={ROLES}
          disabled={!idle}
          onRole={(next) => run(props, 'team', () => putTeamRole(target, next))}
          onRemove={() => run(props, 'team', () => putTeamRole(target, null))}
          removeLabel={t('next.access.teamRemove')}
        />
      ) : (
        <span className="hx-acc-row__fixed">{canLabel(team.role)}</span>
      )}
    </li>
  );
};

const RequestRow = ({
  props,
  request,
  leaving,
}: {
  props: IResourceSection;
  request: IResourceRequest;
  leaving: boolean;
}) => {
  const { target, action } = props;
  const [role, setRole] = useState<TShareRole>(request.level);
  const [ttl, setTtl] = useState<TTtlValue>('7d');
  const idle = action.busy === null && !leaving;
  const asks = t(`next.access.asks.${request.level}`);
  return (
    <li className="hx-acc-row hx-acc-req" data-leaving={leaving || undefined}>
      <Who
        name={request.name}
        sub={[request.email, asks, request.message].filter(Boolean).join(' · ')}
        you={false}
      />
      <div className="hx-acc-req__do">
        <RoleMenu
          label={t('next.access.roleOf', { who: request.name })}
          role={role}
          roles={ROLES}
          onRole={setRole}
          term={{ value: ttl, onChange: setTtl }}
          disabled={!idle}
        />
        <button
          type="button"
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          disabled={!idle}
          onClick={() =>
            run(props, `deny:${request.id}`, () => decide(target, request.id, { decision: 'deny' }))
          }
        >
          {t('next.access.deny')}
        </button>
        <button
          type="button"
          className="hx-btn"
          data-variant="primary"
          data-size="sm"
          data-busy={action.busy === `allow:${request.id}`}
          disabled={!idle}
          onClick={() =>
            run(props, `allow:${request.id}`, () =>
              decide(target, request.id, { decision: 'allow', role, canShare: false, ttl }),
            )
          }
        >
          <span>{t('next.access.allow')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </div>
    </li>
  );
};

export const ResourceRequests = (props: IResourceSection) => {
  const requests = props.view.requests;
  const open = useMemo(() => requests ?? [], [requests]);
  const rows = useLeavingList(open, (r: IResourceRequest) => r.id, OUT_MS);
  if (rows.length === 0) return null;
  return (
    <section className="hx-acc-sec" data-section="requests">
      <h3 className="hx-acc-sec__title">{t('next.access.requests')}</h3>
      <ul className="hx-acc-rows">
        {rows.map(({ item, leaving }) => (
          <RequestRow key={item.id} props={props} request={item} leaving={leaving} />
        ))}
      </ul>
    </section>
  );
};

const ResourcePeople = (props: IResourceSection & { me: string | null }) => {
  const { view, me } = props;
  const rows = useLeavingList(view.grants ?? NO_GRANTS, (g: IGrant) => `${g.type}:${g.id}`, OUT_MS);
  const owner = view.owner;
  return (
    <section className="hx-acc-sec" data-section="people">
      <h3 className="hx-acc-sec__title hx-acc-sec__title--tip">
        <span>{t('next.access.whoHas')}</span>
        <Tooltip label={t('next.access.whoHasTip')}>
          <button type="button" className="hx-acc-help" aria-label={t('next.access.whoHasTip')}>
            <Icon name="info" size="sm" />
          </button>
        </Tooltip>
      </h3>
      <ul className="hx-acc-rows">
        {owner && (
          <li className="hx-acc-row">
            <Who name={owner.name} sub={owner.email} you={view.role === 'owner'} />
            <span className="hx-acc-row__fixed">{roleLabel('owner')}</span>
          </li>
        )}
        <TeamRow props={props} />
        {rows.map(({ item, leaving }) => (
          <GrantRow key={`${item.type}:${item.id}`} props={props} grant={item} me={me} leaving={leaving} />
        ))}
      </ul>
    </section>
  );
};

export default ResourcePeople;
