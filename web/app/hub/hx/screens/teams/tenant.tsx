// «Тенант» for its admin in one drawer: the name, the monthly limit of the whole tenant, who runs it, and the journal
// of changes. The name and the people wait for the hub; the limit goes through the screen's optimistic operations.
import { useCallback, useEffect, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type { AuditEntry, TeamsView } from '../../../org/api/teams';
import type { TenantMember, TenantRole } from '../../../org/api/tenant';
import { useOrg } from '../../../org/context';
import { ago, describe, kRub, needsText } from '../../../org/screens/teams/format';
import type { TenantRef } from '../../../org/types';
import { Btn, Drawer, Icon, initial } from '../../ui';
import { sliderOf, stopsOf, TENANT_STEPS } from './model';
import { tenantLimit } from './ops';
import { Seg } from './sheet';
import { LimitSlider } from './slider';
import type { Op } from './use-teams';

const NAME_MAX = 60;
const ROLES: { id: TenantRole; label: string }[] = [
  { id: 'member', label: 'Участник' },
  { id: 'owner', label: 'Админ' },
];

const Name = ({ tenant }: { tenant: TenantRef }) => {
  const { api, refreshShell } = useOrg();
  const [name, setName] = useState(tenant.name);
  const [saved, setSaved] = useState(tenant.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const clean = name.trim();
  const bad = clean === '' || clean.length > NAME_MAX || clean === saved;
  const save = async () => {
    if (bad || busy) return;
    setBusy(true);
    setError(null);
    try {
      const next = await api.renameTenant(tenant.id, clean);
      setName(next);
      setSaved(next);
      refreshShell();
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="md-sec"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <div className="md-sec__t">Название</div>
      <div className="tm-inline">
        <input
          className="hx-set__input"
          value={name}
          maxLength={NAME_MAX}
          aria-label="Название тенанта"
          onChange={(e) => {
            setName(e.target.value);
            setError(null);
          }}
        />
        <Btn size="sm" type="submit" aria-disabled={bad || undefined} busy={busy}>
          {busy ? 'Сохраняем' : 'Сохранить'}
        </Btn>
      </div>
      <p className="md-note">
        {saved !== tenant.name && clean === saved
          ? 'Название сохранено'
          : 'Название видят все в тенанте и в меню слева'}
      </p>
      {error && (
        <p className="md-note tm-err" role="alert">
          {error}
        </p>
      )}
    </form>
  );
};

const Limit = ({
  view,
  apply,
  problem,
}: {
  view: TeamsView;
  apply: (op: Op) => Promise<boolean>;
  problem: string | null;
}) => {
  const s = view.scope;
  const [draft, setDraft] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const steps = stopsOf(TENANT_STEPS, s.limitRub, draft);
  const now = draft ?? s.limitRub;
  const slider = sliderOf(steps, now, s.limitRub, s.spentRub, null);
  const changed = draft !== null && draft !== s.limitRub;
  const given = view.teams.reduce((n, t) => n + (t.id !== null && t.limitRub !== null ? t.limitRub : 0), 0);
  const save = async () => {
    if (!changed || busy || draft === null) return;
    setBusy(true);
    setFailed(false);
    const ok = await apply(tenantLimit(draft));
    setBusy(false);
    if (ok) setDraft(null);
    else setFailed(true);
  };
  return (
    <div className="md-sec">
      <div className="md-sec__head">
        <span className="md-sec__t">Лимит тенанта в месяц</span>
        {changed && (
          <span className="tm-inline tm-lim-save">
            <Btn variant="ghost" size="sm" onClick={() => setDraft(null)}>
              Отмена
            </Btn>
            <Btn variant="primary" size="sm" busy={busy} onClick={() => void save()}>
              {busy ? 'Сохраняем' : 'Сохранить'}
            </Btn>
          </span>
        )}
      </div>
      <LimitSlider
        slider={slider}
        value={now}
        base={s.limitRub}
        spent={s.spentRub}
        disabled={!s.can.limit}
        onPick={setDraft}
      />
      <p className="md-note">
        {!s.can.limit
          ? s.needs
            ? needsText(s.needs)
            : 'Лимит тенанта ставит админ организации'
          : `Потолок для всех команд тенанта: командам сейчас отдано ${kRub(given)}. Когда лимит кончится, агенты тенанта встанут до 1-го числа.`}
      </p>
      {failed && problem && (
        <p className="md-note tm-err" role="alert">
          {problem}
        </p>
      )}
    </div>
  );
};

const People = ({ tenant }: { tenant: TenantRef }) => {
  const { api, shell } = useOrg();
  const [members, setMembers] = useState<TenantMember[] | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [asking, setAsking] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const load = useCallback(async () => {
    try {
      setMembers(await api.tenantMembers(tenant.id));
    } catch (e) {
      setError(orgErrorText(e));
    }
  }, [api, tenant.id]);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);
  const act = async (id: string, run: () => Promise<void>) => {
    setBusy(id);
    setError(null);
    try {
      await run();
      setAsking(null);
      await load();
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(null);
    }
  };
  const admins = members?.filter((m) => m.role === 'owner').length ?? 0;
  return (
    <div className="md-sec">
      <div className="md-sec__head">
        <span className="md-sec__t">Люди тенанта</span>
        {members && (
          <small className="md-note">
            {members.length} · админов {admins}
          </small>
        )}
      </div>
      {members === null ? (
        !error && <span className="hx-sk" style={{ height: 44 }} aria-label="Загружаем" />
      ) : (
        <div className="tm-invites">
          {members.map((m) => {
            const you = m.userId === shell.viewer.id;
            const who = m.name || m.email;
            const last = m.role === 'owner' && admins === 1;
            return (
              <div key={m.userId} className="ad-row tm-member" data-busy={busy === m.userId || undefined}>
                <span className="hx-avatar tm-member__av">{initial(who)}</span>
                <span className="ad-row__t">
                  <b>
                    {who}
                    {you && ' (вы)'}
                  </b>
                  {m.name && <small>{m.email}</small>}
                </span>
                {asking === m.userId ? (
                  <span className="tm-inline tm-member__ask">
                    <Btn
                      variant="ghost"
                      size="sm"
                      className="md-danger"
                      busy={busy === m.userId}
                      onClick={() => void act(m.userId, () => api.removeTenantMember(tenant.id, m.userId))}
                    >
                      Убрать
                    </Btn>
                    <Btn variant="ghost" size="sm" onClick={() => setAsking(null)}>
                      Нет
                    </Btn>
                  </span>
                ) : (
                  <>
                    <Seg
                      items={ROLES}
                      value={m.role}
                      disabled={busy !== null || you || last}
                      onPick={(r) =>
                        r !== m.role && void act(m.userId, () => api.setTenantRole(tenant.id, m.userId, r))
                      }
                    />
                    {!you && !last && (
                      <Btn
                        variant="ghost"
                        size="sm"
                        aria-label={`Убрать ${who} из тенанта`}
                        title="Убрать из тенанта"
                        onClick={() => setAsking(m.userId)}
                      >
                        <Icon i="x" size="sm" />
                      </Btn>
                    )}
                  </>
                )}
              </div>
            );
          })}
        </div>
      )}
      <p className="md-note">
        Админ тенанта приглашает людей, собирает команды, ставит лимиты и подключает машины. Последнего админа
        не снять, себе роль не поменять.
      </p>
      {error && (
        <p className="md-note tm-err" role="alert">
          {error}
        </p>
      )}
    </div>
  );
};

const Journal = ({ slug }: { slug: string }) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const [events, setEvents] = useState<AuditEntry[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [more, setMore] = useState(false);
  const [now] = useState(() => Date.now());
  useEffect(() => {
    let alive = true;
    api.teamsAudit(org, slug, null).then(
      (p) => {
        if (!alive) return;
        setEvents(p.events);
        setNext(p.next);
      },
      (e: unknown) => alive && setError(orgErrorText(e)),
    );
    return () => {
      alive = false;
    };
  }, [api, org, slug]);
  const loadMore = async () => {
    setMore(true);
    try {
      const p = await api.teamsAudit(org, slug, next);
      setEvents((cur) => [...(cur ?? []), ...p.events]);
      setNext(p.next);
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setMore(false);
    }
  };
  return (
    <div className="md-sec">
      <div className="md-sec__t">Журнал изменений</div>
      {error ? (
        <p className="md-note tm-err" role="alert">
          {error}
        </p>
      ) : events === null ? (
        <span className="hx-sk" style={{ height: 44 }} aria-label="Загружаем" />
      ) : events.length === 0 ? (
        <p className="md-note tm-first">Пока никто ничего не менял</p>
      ) : (
        <ul className="tm-journal">
          {events.map((e) => {
            const d = describe(e);
            return (
              <li key={e.id}>
                <span>
                  <b>{d.who}</b> {d.what}
                </span>
                <time dateTime={e.at}>{ago(e.at, now)}</time>
              </li>
            );
          })}
        </ul>
      )}
      {next !== null && !error && (
        <div className="tm-inline">
          <Btn size="sm" busy={more} onClick={() => void loadMore()}>
            {more ? 'Загружаем' : 'Показать ещё'}
          </Btn>
        </div>
      )}
    </div>
  );
};

export const TenantSheet = ({
  open,
  tenant,
  view,
  apply,
  problem,
  onClose,
}: {
  open: boolean;
  tenant: TenantRef;
  view: TeamsView;
  apply: (op: Op) => Promise<boolean>;
  problem: string | null;
  onClose: () => void;
}) => (
  <Drawer
    open={open}
    onClose={onClose}
    className="md"
    label="Тенант"
    title="Тенант"
    sub={`${tenant.name} · лимит ${view.scope.limitRub === null ? 'не задан' : kRub(view.scope.limitRub)}`}
  >
    {open && (
      <>
        <Name tenant={tenant} />
        <Limit view={view} apply={apply} problem={problem} />
        <People tenant={tenant} />
        <Journal slug={tenant.slug} />
      </>
    )}
  </Drawer>
);
