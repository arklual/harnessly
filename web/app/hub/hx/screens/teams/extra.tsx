// The two small drawers next to the teams: «Новая команда» and «Пригласить». Both use the board's drawer and fields.
import { type FormEvent, useCallback, useEffect, useState } from 'react';

import { isEmailLike } from '../../../lib/format';
import { orgErrorText } from '../../../org/api/errors';
import type { TenantInvite } from '../../../org/api/tenant';
import { useOrg } from '../../../org/context';
import type { TenantRef } from '../../../org/types';
import { Btn, Drawer, Icon } from '../../ui';
import { newTeam } from './ops';
import type { Op } from './use-teams';

const NAME_MAX = 60;

export const NewTeamSheet = ({
  open,
  onClose,
  apply,
  problem,
  taken,
}: {
  open: boolean;
  onClose: () => void;
  apply: (op: Op) => Promise<boolean>;
  problem: string | null;
  taken: string[];
}) => {
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [was, setWas] = useState(open);
  if (open !== was) {
    setWas(open);
    if (open) {
      setName('');
      setFailed(false);
    }
  }
  const clean = name.trim();
  const dup = taken.some((t) => t.trim().toLowerCase() === clean.toLowerCase());
  const bad = clean === '' || clean.length > NAME_MAX || dup;
  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (bad || busy) return;
    setBusy(true);
    setFailed(false);
    const ok = await apply(newTeam(clean));
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

export const InviteSheet = ({
  open,
  tenant,
  onClose,
  onChanged,
}: {
  open: boolean;
  tenant: TenantRef;
  onClose: () => void;
  onChanged: () => void;
}) => {
  const { api } = useOrg();
  const [list, setList] = useState<TenantInvite[] | null>(null);
  const [email, setEmail] = useState('');
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
      setFresh(null);
      setError(null);
      setBadMail(false);
      setList(null);
    }
  }

  const load = useCallback(async () => {
    try {
      setList(await api.tenantInvites(tenant.id));
    } catch (e) {
      setError(orgErrorText(e));
      setList((l) => l ?? []);
    }
  }, [api, tenant.id]);
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
    if (!isEmailLike(address)) {
      setBadMail(true);
      return;
    }
    setBadMail(false);
    void act('mail', async () => {
      const made = await api.inviteByEmail(tenant.id, address);
      setEmail('');
      setFresh(made.sent === false && made.url ? made : null);
    });
  };
  const makeLink = () =>
    void act('link', async () => {
      const made = await api.inviteByLink(tenant.id);
      setFresh(made.url ? made : null);
      setCopied(false);
    });
  const revoke = (i: TenantInvite) => {
    // The invitation leaves the list at once; when the hub refuses, it comes back.
    const before = list;
    setList((l) => l && l.filter((x) => x.id !== i.id));
    setError(null);
    api.revokeInvite(tenant.id, i.id).then(
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
      sub={`${tenant.name} · приглашённый войдёт участником`}
    >
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
        {badMail && (
          <p className="md-note tm-err" role="alert">
            Проверьте адрес почты коллеги
          </p>
        )}
      </form>

      <div className="md-sec">
        <div className="md-sec__t">По ссылке</div>
        <p className="md-note tm-first">
          Ссылка действует 7 дней, по ней может войти любой, кому вы её передали.
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
              <div key={i.id} className="ad-row">
                <span className="ad-ic">
                  <Icon i={i.kind === 'email' ? 'mail' : 'link'} size="sm" />
                </span>
                <span className="ad-row__t">
                  <b>{i.kind === 'email' ? (i.email ?? 'Почта') : 'Ссылка'}</b>
                  <small>
                    {i.kind === 'link' && `Входов: ${i.uses}${i.maxUses ? ` из ${i.maxUses}` : ''} · `}до{' '}
                    {until(i.expiresAt)}
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
    </Drawer>
  );
};
