// «У кого есть доступ»: the owner, then everyone with a role of their own — a round initial, the address, «до 12 ноя»
// under it when the role ends, and the role's own menu (the role, «Доступ до…», «Убрать доступ»). «Вы» marks the
// person themself.
import { putPerson, removePerson, type IPerson } from '@/next/features/designly/access/api';
import {
  canLabel,
  grantRoles,
  initialOf,
  roleLabel,
  untilLabel,
} from '@/next/features/designly/access/labels';
import RoleMenu from '@/next/features/designly/access/role-menu';
import { change, isManager, type ISectionProps } from '@/next/features/designly/access/section';
import { useLeavingList } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const OUT_MS = 200;
const NONE: IPerson[] = [];

interface IWhoProps {
  name: string;
  sub: string | null;
  you: boolean;
}

/** The left of a row: the initial, the name with «Вы», the line under it. */
export const Who = ({ name, sub, you }: IWhoProps) => (
  <>
    <span className="hx-acc-av" aria-hidden="true">
      {initialOf(name)}
    </span>
    <span className="hx-acc-row__who">
      <span className="hx-acc-row__name">
        <span className="hx-acc-row__nm">{name}</span>
        {you && <span className="hx-acc-tag">{t('next.access.you')}</span>}
      </span>
      {sub && <span className="hx-acc-row__sub">{sub}</span>}
    </span>
  </>
);

const AccessPeople = (props: ISectionProps & { me: string | null }) => {
  const { project, access, action, me } = props;
  const mayManage = isManager(access);
  const rows = useLeavingList(access.people ?? NONE, (p: IPerson) => p.email, OUT_MS);
  const idle = action.busy === null;
  const ownerName = access.ownerName || access.owner;

  return (
    <section className="hx-acc-sec" data-section="people">
      <h3 className="hx-acc-sec__title">{t('next.access.whoHas')}</h3>
      <ul className="hx-acc-rows">
        {ownerName && (
          <li className="hx-acc-row">
            <Who
              name={ownerName}
              sub={access.ownerName && access.owner ? access.owner : null}
              you={access.role === 'owner'}
            />
            <span className="hx-acc-row__fixed">{roleLabel('owner')}</span>
          </li>
        )}
        {rows.map(({ item: person, leaving }) => {
          // Only a manager touches a manager.
          const fixed = person.role === 'manage' && !mayManage;
          return (
            <li key={person.email} className="hx-acc-row" data-leaving={leaving || undefined}>
              <Who name={person.email} sub={untilLabel(person.expiresAt)} you={person.email === me} />
              {fixed && <span className="hx-acc-row__fixed">{canLabel(person.role)}</span>}
              {!fixed && (
                <RoleMenu
                  label={t('next.access.roleOf', { who: person.email })}
                  role={person.role}
                  roles={grantRoles(mayManage)}
                  disabled={!idle || leaving}
                  onRole={(next) =>
                    change(props, `role:${person.email}`, () =>
                      putPerson(project, person.email, next, person.expiresAt),
                    )
                  }
                  term={{
                    value: person.expiresAt,
                    onChange: (next) =>
                      change(props, `ttl:${person.email}`, () =>
                        putPerson(project, person.email, person.role, next),
                      ),
                  }}
                  onRemove={() =>
                    change(props, `rm:${person.email}`, () => removePerson(project, person.email))
                  }
                />
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
};

export default AccessPeople;
