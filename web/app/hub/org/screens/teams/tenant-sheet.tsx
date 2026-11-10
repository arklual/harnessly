// Настройки тенанта (HRN-625): the name, and the people of the tenant with their tenant role. A tenant is a space on the
// hub, so the roles here are the space's: owner (runs the tenant) or member. Every change is pessimistic.
import { useCallback, useEffect, useState } from 'react';

import { Button, TextField } from '@kit';

import { orgErrorText } from '../../api/errors';
import type { TenantDeletePlan, TenantMember, TenantRole } from '../../api/tenant';
import { useOrg } from '../../context';
import type { TenantRef } from '../../types';
import { Seg } from '../../ui/page';
import { Sheet } from '../../ui/sheet';
import { BLOCKER, REMOVES } from './format';

const ROLES = [
  { value: 'owner', label: 'Владелец' },
  { value: 'member', label: 'Участник' },
] as const;

const Name = ({ tenant }: { tenant: TenantRef }) => {
  const { api, refreshShell } = useOrg();
  const [name, setName] = useState(tenant.name);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  const changed = name.trim() !== '' && name.trim() !== tenant.name;
  const save = async () => {
    setPending(true);
    setError(null);
    setSaved(false);
    try {
      setName(await api.renameTenant(tenant.id, name.trim()));
      setSaved(true);
      refreshShell();
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  return (
    <section className="ht-field">
      <h3 className="ht-h">Название</h3>
      <form
        className="ht-invite__row"
        onSubmit={(e) => {
          e.preventDefault();
          if (changed) void save();
        }}
      >
        <TextField
          label="Название тенанта"
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setSaved(false);
          }}
          maxLength={60}
        />
        <Button type="submit" appearance="primary" loading={pending} disabled={!changed}>
          Сохранить
        </Button>
      </form>
      {saved && <p className="ht-note">Название сохранено.</p>}
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
    </section>
  );
};

const People = ({ tenant }: { tenant: TenantRef }) => {
  const { api, shell } = useOrg();
  const [members, setMembers] = useState<TenantMember[] | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [asking, setAsking] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const load = useCallback(async () => {
    try {
      setMembers(await api.tenantMembers(tenant.id));
    } catch (e) {
      setError(e);
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
      setError(e);
    } finally {
      setBusy(null);
    }
  };
  const setRole = (m: TenantMember, role: TenantRole) =>
    role === m.role ? undefined : void act(m.userId, () => api.setTenantRole(tenant.id, m.userId, role));

  return (
    <section className="ht-field">
      <h3 className="ht-h">Люди тенанта</h3>
      {members === null ? (
        error == null && <p className="ht-note">Загружаем…</p>
      ) : (
        <ul className="ht-list">
          {members.map((m) => {
            const you = m.userId === shell.viewer.id;
            return (
              <li key={m.userId} className="ht-list__row" data-busy={busy === m.userId || undefined}>
                <span className="ht-list__who">
                  <b>
                    {m.name || m.email}
                    {you && ' (вы)'}
                  </b>
                  {m.name && <small>{m.email}</small>}
                </span>
                <Seg
                  label={`Роль: ${m.name || m.email}`}
                  value={m.role}
                  options={ROLES}
                  onChange={(r) => setRole(m, r)}
                />
                {asking === m.userId ? (
                  <span className="ht-confirm">
                    Убрать из тенанта?
                    <Button
                      size="s"
                      appearance="outline"
                      loading={busy === m.userId}
                      onClick={() => void act(m.userId, () => api.removeTenantMember(tenant.id, m.userId))}
                    >
                      Убрать
                    </Button>
                    <Button size="s" appearance="ghost" onClick={() => setAsking(null)}>
                      Нет
                    </Button>
                  </span>
                ) : (
                  !you && (
                    <Button
                      size="s"
                      appearance="ghost"
                      className="ht-danger"
                      onClick={() => setAsking(m.userId)}
                    >
                      Убрать
                    </Button>
                  )
                )}
              </li>
            );
          })}
        </ul>
      )}
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
    </section>
  );
};

const Delete = ({ tenant, onClose }: { tenant: TenantRef; onClose: () => void }) => {
  const { api, shell, refreshShell } = useOrg();
  const [plan, setPlan] = useState<TenantDeletePlan | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const load = async () => {
    setPending(true);
    setError(null);
    try {
      setPlan(await api.planDeleteTenant(shell.org.slug, tenant.id));
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  const remove = async () => {
    setPending(true);
    setError(null);
    try {
      await api.deleteTenant(shell.org.slug, tenant.id);
      refreshShell();
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  return (
    <section className="ht-field">
      <h3 className="ht-h">Удаление тенанта</h3>
      <p className="ht-note">Удалить можно только пустой тенант: без машин, людей, команд и сайтов.</p>
      {plan && plan.blockers.length > 0 && (
        <ul className="ht-note">
          {plan.blockers.map((b) => (
            <li key={b.kind}>
              Мешает: {b.count} {BLOCKER[b.kind] ?? b.kind}
            </li>
          ))}
        </ul>
      )}
      {plan && plan.canDelete && plan.removes.length > 0 && (
        <ul className="ht-note">
          {plan.removes.map((r) => (
            <li key={r.kind}>
              Уйдёт вместе с ним: {r.count} {REMOVES[r.kind] ?? r.kind}
            </li>
          ))}
        </ul>
      )}
      {plan?.canDelete && <p className="ht-note">Тенант пуст. Удаление нельзя отменить.</p>}
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      {plan?.canDelete ? (
        <Button appearance="danger" loading={pending} onClick={() => void remove()}>
          Удалить тенант
        </Button>
      ) : (
        <Button appearance="ghost" loading={pending} onClick={() => void load()}>
          Проверить, можно ли удалить
        </Button>
      )}
    </section>
  );
};

export const TenantSheet = ({
  open,
  tenant,
  onClose,
}: {
  open: boolean;
  tenant: TenantRef;
  onClose: () => void;
}) => (
  <Sheet open={open} onClose={onClose} title="Настройки тенанта" sub={tenant.name}>
    <Name tenant={tenant} />
    <People tenant={tenant} />
    <Delete tenant={tenant} onClose={onClose} />
  </Sheet>
);
