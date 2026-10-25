// «Запросы»: only when someone asks. Who, what for and with which words; the role to give (what was asked by default,
// 7 days under «Доступ до…»), «Разрешить» and «Отклонить». The request leaves the list when the gateway has the decision.
import { useMemo, useState } from 'react';
import {
  decideRequest,
  type IAccessRequest,
  type TGrantRole,
  type TTtlValue,
} from '@/next/features/designly/access/api';
import { grantRoles } from '@/next/features/designly/access/labels';
import { Who } from '@/next/features/designly/access/access-people';
import RoleMenu from '@/next/features/designly/access/role-menu';
import { change, isManager, type ISectionProps } from '@/next/features/designly/access/section';
import { useLeavingList } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const OUT_MS = 200;

const RequestRow = ({
  props,
  request,
  leaving,
}: {
  props: ISectionProps;
  request: IAccessRequest;
  leaving: boolean;
}) => {
  const { project, access, action } = props;
  const [role, setRole] = useState<TGrantRole>(request.level);
  const [ttl, setTtl] = useState<TTtlValue>('7d');
  const idle = action.busy === null && !leaving;
  const asks = t(`next.access.asks.${request.level}`);
  return (
    <li className="hx-acc-row hx-acc-req" data-leaving={leaving || undefined}>
      <Who name={request.email} sub={request.message ? `${asks} · ${request.message}` : asks} you={false} />
      <div className="hx-acc-req__do">
        <RoleMenu
          label={t('next.access.roleOf', { who: request.email })}
          role={role}
          roles={grantRoles(isManager(access))}
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
            change(props, `deny:${request.id}`, () =>
              decideRequest(project, request.id, { decision: 'deny' }),
            )
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
            change(props, `allow:${request.id}`, () =>
              decideRequest(project, request.id, { decision: 'allow', role, ttl }),
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

const AccessRequests = (props: ISectionProps) => {
  const { requests } = props.access;
  // The list keeps its identity between renders: the leaving list compares it by reference.
  const open = useMemo(() => (requests ?? []).filter((r) => r.status === 'open'), [requests]);
  const rows = useLeavingList(open, (r: IAccessRequest) => r.id, OUT_MS);
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

export default AccessRequests;
