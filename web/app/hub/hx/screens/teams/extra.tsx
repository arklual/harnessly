// The two small drawers next to the teams: «Новая команда» and «Пригласить». Both use the board's drawer and fields.
import { type FormEvent, useCallback, useEffect, useState } from 'react';

import { isEmailLike } from '../../../lib/format';
import { orgErrorText } from '../../../org/api/errors';
import type { TenantInvite, TenantRole } from '../../../org/api/tenant';
import { useOrg } from '../../../org/context';
import type { TenantRef } from '../../../org/types';
import { Btn, Drawer, Icon } from '../../ui';
import { newTeam } from './ops';
import type { Op } from './use-teams';

const NAME_MAX = 60;

/** «В каком тенанте» — shown only when the viewer has more than one to pick from. */
const TenantPick = ({
  tenants,
  value,
  onPick,
}: {
  tenants: TenantRef[];
  value: string | null;
  onPick: (id: string) => void;
}) => (
  <div className="md-sec">
    <div className="md-sec__t">Тенант</div>
    <div className="hx-set__seg ps-teams" role="radiogroup" aria-label="Тенант">
      {tenants.map((t) => (
        <button
          key={t.id}
          type="button"
          className="hx-set__seg-item"
          role="radio"
          aria-checked={t.id === value}
          onClick={() => onPick(t.id)}
        >
          {t.name}
        </button>
      ))}
    </div>
  </div>
);

export const NewTeamSheet = ({
  open,
  onClose,
  apply,
  problem,
  taken,
  tenants,
}: {
  open: boolean;
  onClose: () => void;
  apply: (op: Op) => Promise<boolean>;
  problem: string | null;
  taken: string[];
  /** The tenants a team can be made in: one fixes it, several make the form ask. */
  tenants: TenantRef[];
}) => {
  const [name, setName] = useState('');
  const [picked, setPicked] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [was, setWas] = useState(open);
  if (open !== was) {
    setWas(open);
    if (open) {
      setName('');
      setPicked(null);
      setFailed(false);
    }
  }
  const tenantId = tenants.length === 1 ? (tenants[0]?.id ?? null) : picked;
  const clean = name.trim();
  const dup = taken.some((t) => t.trim().toLowerCase() === clean.toLowerCase());
  const bad = clean === '' || clean.length > NAME_MAX || dup || tenantId === null;
  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (bad || busy || tenantId === null) return;
    setBusy(true);
    setFailed(false);
    const ok = await apply(newTeam(clean, tenantId));
    setBusy(false);
    if (ok) onClose();
    else setFailed(true);
  };
  return (
    <Drawer
      open={open}
      onClose={onClose}
      className="md"
      label="Новая команда"
      title="Новая команда"
      sub="Люди, лимит и окружение — у каждой команды свои"
    >
      {tenants.length > 1 && <TenantPick tenants={tenants} value={picked} onPick={setPicked} />}
      <form className="md-sec" onSubmit={(e) => void submit(e)}>
        <div className="md-sec__t">Название</div>
        <input
          className="hx-set__input"
          value={name}
          maxLength={NAME_MAX}
          autoFocus
          placeholder="Например, Мобильная разработка"
          aria-label="Название команды"
          onChange={(e) => {
            setName(e.target.value);
            setFailed(false);
          }}
        />
        <p className="md-note" data-bad={dup || undefined}>
          {dup ? 'Команда с таким названием уже есть' : 'Лид и лимит назначаются после создания'}
        </p>
        {failed && problem && (
          <p className="md-note tm-err" role="alert">
            {problem}
          </p>
        )}
      </form>
      <div className="md-actions tm-actions">
        <span className="hx-spacer" />
        <Btn variant="ghost" size="sm" onClick={onClose}>
          Отмена
        </Btn>
        <button
          type="button"
          className="hx-btn md-save"
          data-variant="primary"
          data-size="sm"
          data-busy={busy ? '1' : undefined}
          aria-disabled={bad}
          onClick={() => void submit()}
        >
          <span className="md-spin" aria-hidden="true" />
          {busy ? 'Создаём' : 'Создать'}
        </button>
      </div>
    </Drawer>
  );
};

const until = (iso: string): string =>
  new Date(iso).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' });

/** «В какую команду» — optional: the invited person joins the team together with the tenant. */
const TeamPick = ({
  teams,
  value,
  onPick,
}: {
  teams: { id: string; name: string }[];
  value: string | null;
  onPick: (id: string | null) => void;
}) => (
  <div className="md-sec">
    <div className="md-sec__t">Команда</div>
    <div className="hx-set__seg ps-teams" role="radiogroup" aria-label="Команда">
      {[{ id: null, name: 'Без команды' }, ...teams].map((t) => (
        <button
          key={t.id ?? 'none'}
          type="button"
          className="hx-set__seg-item"
          role="radio"
          aria-checked={t.id === value}
          onClick={() => onPick(t.id)}
        >
          {t.name}
        </button>
      ))}
    </div>
  </div>
);

export const InviteSheet = ({
  open,
  tenants,
  teams = [],
  presetTeam = null,
  onClose,
  onChanged,
}: {
  open: boolean;
  /** A team the person is put in unless the form says otherwise (the invitation started from that team). */
  presetTeam?: string | null;
  /** The tenants a person can be invited to: one fixes it, several make the form ask. */
  tenants: TenantRef[];
  /** The teams the person may be put in, each named with the tenant it belongs to. */
  teams?: { id: string; name: string; tenantId?: string }[];
  onClose: () => void;
  onChanged: () => void;
}) => {
  const { api } = useOrg();
  const [picked, setPicked] = useState<string | null>(null);
  const tenant = tenants.length === 1 ? (tenants[0] ?? null) : (tenants.find((t) => t.id === picked) ?? null);
  const [list, setList] = useState<TenantInvite[] | null>(null);
  const [teamId, setTeamId] = useState<string | null>(null);
  const tenantTeams = tenant ? teams.filter((t) => t.tenantId === tenant.id) : [];
  const team = tenantTeams.find((t) => t.id === teamId) ?? null;
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<TenantRole>('member');
  const [fresh, setFresh] = useState<TenantInvite | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [badMail, setBadMail] = useState(false);
  const [copied, setCopied] = useState(false);
  const [was, setWas] = useState(open);
  if (open !== was) {
    setWas(open);
    if (open) {
      setEmail('');
      setRole('member');
      setFresh(null);
      setError(null);
      setBadMail(false);
      setList(null);
      const pre = presetTeam ? teams.find((t) => t.id === presetTeam) : undefined;
      setPicked(pre?.tenantId ?? null);
      setTeamId(pre?.id ?? null);
    }
  }

  const tenantId = tenant?.id ?? null;
  const load = useCallback(async () => {
    if (tenantId === null) return;
    try {
      setList(await api.tenantInvites(tenantId));
    } catch (e) {
      setError(orgErrorText(e));
      setList((l) => l ?? []);
    }
  }, [api, tenantId]);
  useEffect(() => {
    if (open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      void load();
    }
  }, [open, load]);

  const act = async (what: string, run: () => Promise<void>) => {
    setBusy(what);
    setError(null);
    try {
      await run();
      await load();
      onChanged();
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(null);
    }
  };
  const sendMail = () => {
    const address = email.trim();
    if (!tenant) return;
    if (!isEmailLike(address)) {
      setBadMail(true);
      return;
    }
    setBadMail(false);
    void act('mail', async () => {
      const made = await api.inviteByEmail(tenant!.id, address, role, team?.id);
      setEmail('');
      setFresh(made.sent === false && made.url ? made : null);
    });
  };
  const makeLink = () =>
    void act('link', async () => {
      const made = await api.inviteByLink(tenant!.id, team?.id);
      setFresh(made.url ? made : null);
      setCopied(false);
    });
  const revoke = (i: TenantInvite) => {
    // The invitation leaves the list at once; when the hub refuses, it comes back.
    const before = list;
    setList((l) => l && l.filter((x) => x.id !== i.id));
    setError(null);
    api.revokeInvite(tenant!.id, i.id).then(
      () => {
        setFresh((f) => (f?.id === i.id ? null : f));
        onChanged();
      },
      (e: unknown) => {
        setList(before);
        setError(orgErrorText(e));
      },
    );
  };
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      className="md"
      label="Пригласить в тенант"
      title="Пригласить в тенант"
      sub={tenant ? tenant.name : 'Выберите тенант, куда пригласить'}
    >
      {tenants.length > 1 && (
        <TenantPick
          tenants={tenants}
          value={picked}
          onPick={(id) => {
            setPicked(id);
            setTeamId(null);
            setList(null);
            setFresh(null);
            setError(null);
          }}
        />
      )}
      {tenant && tenantTeams.length > 0 && <TeamPick teams={tenantTeams} value={teamId} onPick={setTeamId} />}
      {tenant && (
        <>
          <form
            className="md-sec"
            onSubmit={(e) => {
              e.preventDefault();
              sendMail();
            }}
          >
            <div className="md-sec__t">По почте</div>
            <div className="tm-inline">
              <input
                className="hx-set__input"
                type="email"
                value={email}
                autoFocus
                placeholder="Почта коллеги"
                aria-label="Почта коллеги"
                onChange={(e) => {
                  setEmail(e.target.value);
                  setBadMail(false);
                }}
              />
              <Btn
                variant="primary"
                size="sm"
                type="submit"
                icon="mail"
                busy={busy === 'mail'}
                disabled={email.trim() === ''}
              >
                Отправить
              </Btn>
            </div>
            <div className="hx-set__seg tm-role" role="radiogroup" aria-label="Роль приглашённого">
              {(
                [
                  ['member', 'Участник', 'Работает с агентами на своих машинах'],
                  ['owner', 'Админ тенанта', 'Приглашает людей, управляет машинами, командами и лимитами'],
                ] as const
              ).map(([value, label, hint]) => (
                <button
                  key={value}
                  type="button"
                  className="hx-set__seg-item"
                  role="radio"
                  aria-checked={role === value}
                  title={hint}
                  onClick={() => setRole(value)}
                >
                  {label}
                </button>
              ))}
            </div>
            {badMail && (
              <p className="md-note tm-err" role="alert">
                Проверьте адрес почты коллеги
              </p>
            )}
          </form>

          <div className="md-sec">
            <div className="md-sec__t">По ссылке</div>
            <p className="md-note tm-first">
              {`Ссылка действует 7 дней, по ней может войти любой, кому вы её передали. Входит участником${team ? ` команды «${team.name}»` : ''}.`}
            </p>
            <div className="tm-inline">
              <Btn size="sm" icon="link" busy={busy === 'link'} onClick={makeLink}>
                Создать ссылку
              </Btn>
            </div>
            {fresh?.url && (
              <div className="tm-copy">
                <p className="md-note tm-first">
                  {fresh.kind === 'email'
                    ? `Письмо на ${fresh.email ?? 'этот адрес'} не ушло: почта на хабе не настроена. Передайте ссылку сами.`
                    : 'Ссылка готова, передайте её коллеге.'}
                </p>
                <div className="tm-inline">
                  <input
                    className="hx-set__input hx-set__input--mono"
                    readOnly
                    value={fresh.url}
                    aria-label="Ссылка"
                  />
                  <Btn size="sm" icon={copied ? 'check' : 'copy'} onClick={() => void copy(fresh.url ?? '')}>
                    {copied ? 'Скопировано' : 'Копировать'}
                  </Btn>
                </div>
              </div>
            )}
          </div>

          <div className="md-sec">
            <div className="md-sec__t">Ждут ответа</div>
            {list === null ? (
              <span className="hx-sk" style={{ height: 44 }} />
            ) : list.length === 0 ? (
              <p className="md-note tm-first">Открытых приглашений нет</p>
            ) : (
              <div className="tm-invites">
                {list.map((i) => (
                  <div key={i.id} className="hb-row">
                    <span className="hb-ic">
                      <Icon i={i.kind === 'email' ? 'mail' : 'link'} size="sm" />
                    </span>
                    <span className="hb-row__t">
                      <b>{i.kind === 'email' ? (i.email ?? 'Почта') : 'Ссылка'}</b>
                      <small>
                        {i.role === 'owner' && 'админ тенанта · '}
                        {i.kind === 'link' &&
                          `Входов: ${i.uses}${i.maxUses ? ` из ${i.maxUses}` : ''} · `}до {until(i.expiresAt)}
                      </small>
                    </span>
                    <Btn variant="ghost" size="sm" onClick={() => revoke(i)}>
                      Отозвать
                    </Btn>
                  </div>
                ))}
              </div>
            )}
            {error && (
              <p className="md-note tm-err" role="alert">
                {error}
              </p>
            )}
          </div>
        </>
      )}
    </Drawer>
  );
};
