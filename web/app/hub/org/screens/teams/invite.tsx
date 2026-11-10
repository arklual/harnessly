// «Пригласить» (HRN-625): invite to a tenant by email or by link, see who has not answered yet and take an invitation
// back. The tenant is a space on the hub; its invitations are the space's own, so an invited person joins as a member.
import { useCallback, useEffect, useState } from 'react';
import { Link2, Mail, Trash2 } from 'lucide-react';

import { Button, TextField } from '@kit';

import { isEmailLike } from '../../../lib/format';
import { CopyBox, Msg, Note } from '../../../ui/parts';
import { orgErrorText } from '../../api/errors';
import type { TenantInvite } from '../../api/tenant';
import { useOrg } from '../../context';
import type { TenantRef } from '../../types';
import { Sheet } from '../../ui/sheet';

const until = (iso: string): string =>
  new Date(iso).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' });

type Busy = 'mail' | 'link' | string | null;

const Body = ({ tenant, onClose }: { tenant: TenantRef; onClose: () => void }) => {
  const { api } = useOrg();
  const [list, setList] = useState<TenantInvite[] | null>(null);
  const [email, setEmail] = useState('');
  const [fresh, setFresh] = useState<TenantInvite[]>([]);
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState<unknown>(null);
  const [badEmail, setBadEmail] = useState(false);

  const load = useCallback(async () => {
    try {
      setList(await api.tenantInvites(tenant.id));
    } catch (e) {
      setError(e);
    }
  }, [api, tenant.id]);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  const act = async (kind: Busy, run: () => Promise<void>) => {
    setBusy(kind);
    setError(null);
    try {
      await run();
      await load();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(null);
    }
  };

  const sendMail = () => {
    const address = email.trim();
    if (!isEmailLike(address)) {
      setBadEmail(true);
      return;
    }
    setBadEmail(false);
    void act('mail', async () => {
      const made = await api.inviteByEmail(tenant.id, address);
      setEmail('');
      // A letter the hub could not send: the person passes the link on by hand.
      setFresh(made && made.sent === false && made.url ? [made] : []);
    });
  };
  const makeLink = () =>
    void act('link', async () => {
      const made = await api.inviteByLink(tenant.id);
      setFresh(made.url ? [made] : []);
    });
  const revoke = (i: TenantInvite) =>
    void act(i.id, async () => {
      await api.revokeInvite(tenant.id, i.id);
      setFresh((f) => f.filter((x) => x.id !== i.id));
    });

  return (
    <div className="ht-invite">
      <section className="ht-field">
        <h3 className="ht-h">По почте</h3>
        <form
          className="ht-invite__row"
          onSubmit={(e) => {
            e.preventDefault();
            sendMail();
          }}
        >
          <TextField
            label="Почта коллеги"
            type="email"
            value={email}
            onChange={(e) => {
              setEmail(e.target.value);
              setBadEmail(false);
            }}
            autoFocus
          />
          <Button
            type="submit"
            appearance="primary"
            icon={<Mail size={14} />}
            loading={busy === 'mail'}
            disabled={email.trim() === ''}
          >
            Отправить
          </Button>
        </form>
        {badEmail && <p className="ht-err">Проверьте адрес почты коллеги.</p>}
      </section>
      <section className="ht-field">
        <h3 className="ht-h">По ссылке</h3>
        <Note>Ссылка действует 7 дней, по ней может войти любой, кому вы её передали.</Note>
        <div className="ht-invite__row">
          <Button icon={<Link2 size={14} />} loading={busy === 'link'} onClick={makeLink}>
            Создать ссылку
          </Button>
        </div>
      </section>
      {fresh.map((i) => (
        <div key={i.id} className="ht-invite__fresh">
          <Msg tone={i.kind === 'email' ? 'warn' : 'success'}>
            {i.kind === 'email'
              ? `Письмо на ${i.email ?? 'этот адрес'} не ушло: почта на хабе не настроена. Передайте ссылку сами.`
              : 'Ссылка готова, передайте её коллеге.'}
          </Msg>
          {i.url && <CopyBox text={i.url} label="Скопировать ссылку" wrap />}
        </div>
      ))}
      <section className="ht-field">
        <h3 className="ht-h">Ждут ответа</h3>
        {list === null ? (
          error == null && <p className="ht-note">Загружаем…</p>
        ) : list.length === 0 ? (
          <p className="ht-note">Открытых приглашений нет.</p>
        ) : (
          <ul className="ht-list">
            {list.map((i) => (
              <li key={i.id} className="ht-list__row">
                <span className="ht-list__who">
                  <b>{i.kind === 'email' ? (i.email ?? 'Почта') : 'Ссылка'}</b>
                  <small>
                    {i.kind === 'link' && `Входов: ${i.uses}${i.maxUses ? ` из ${i.maxUses}` : ''} · `}
                    до {until(i.expiresAt)}
                  </small>
                </span>
                <Button
                  appearance="ghost"
                  size="s"
                  icon={<Trash2 size={14} />}
                  loading={busy === i.id}
                  onClick={() => revoke(i)}
                >
                  Отозвать
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      <div className="ht-list__foot">
        <Button appearance="ghost" onClick={onClose}>
          Готово
        </Button>
      </div>
    </div>
  );
};

export const InviteSheet = ({
  open,
  tenant,
  onClose,
}: {
  open: boolean;
  tenant: TenantRef;
  onClose: () => void;
}) => (
  <Sheet
    open={open}
    onClose={onClose}
    title="Пригласить в тенант"
    sub={`${tenant.name} · приглашённый войдёт участником. Роль владельца можно выдать в настройках тенанта.`}
  >
    <Body tenant={tenant} onClose={onClose} />
  </Sheet>
);
