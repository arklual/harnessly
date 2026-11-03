// «Добавить машину»: the old setup step as a sheet over the machines of a tenant. The person picks the tenant
// (when the screen shows the whole organization), the access of the agents, gets the one-time code and the command the
// hub built, and the sheet waits until that machine dials in, then closes and the list refreshes.
import { useEffect, useRef, useState } from 'react';
import { Terminal } from 'lucide-react';

import { Button, Spinner } from '@kit';

import { hhmm } from '../../../lib/format';
import { useNow } from '../../../lib/hooks';
import { useSession } from '../../../lib/session';
import { CopyBox, Msg, Note, Sure } from '../../../ui/parts';
import { orgErrorText } from '../../api/errors';
import type { IssuedCode, MachineProfile } from '../../api/tenant';
import { useOrg } from '../../context';
import type { TenantRef } from '../../types';
import { Sheet } from '../../ui/sheet';

const POLL_MS = 3_000;
const CLOSE_AFTER_MS = 1_500;

interface Joined {
  id: string;
  name: string;
}

const PROFILES: { value: MachineProfile; title: string; sub: string; tag?: string }[] = [
  { value: 'dir', title: 'В отдельной папке', sub: 'Свой пользователь, одна папка. Рекомендуем.' },
  { value: 'container', title: 'В контейнере', sub: 'Каждая сессия отдельно' },
  { value: 'full', title: 'С вашим доступом', sub: 'Агенты видят все ваши файлы' },
];

const Body = ({
  tenants,
  onClose,
  onAdded,
}: {
  tenants: TenantRef[];
  onClose: () => void;
  onAdded: () => void;
}) => {
  const { api } = useOrg();
  const { config } = useSession();
  const [tenantId, setTenantId] = useState(tenants[0]?.id ?? '');
  const [profile, setProfile] = useState<MachineProfile>('dir');
  const [sure, setSure] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [issued, setIssued] = useState<IssuedCode | null>(null);
  const [joined, setJoined] = useState<Joined | null>(null);
  // The machines the tenant had when the code was issued: a new id among them later is the one that used the code.
  const known = useRef<Set<string>>(new Set());
  const waiting = issued !== null && joined === null;
  const now = useNow(1000, waiting);
  const expired = issued !== null && new Date(issued.expiresAt).getTime() <= now;
  const canIssue = tenantId !== '' && (profile !== 'full' || sure);

  const issue = async () => {
    setPending(true);
    setError(null);
    try {
      known.current = new Set((await api.spaceMachines(tenantId)).map((m) => m.id));
      setIssued(await api.issueMachineCode(tenantId, profile, profile === 'full'));
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };

  useEffect(() => {
    if (!waiting) return;
    let alive = true;
    const timer = window.setInterval(() => {
      api.spaceMachines(tenantId).then(
        (list) => {
          const fresh = list.find((m) => !known.current.has(m.id));
          if (alive && fresh) setJoined({ id: fresh.id, name: fresh.name });
        },
        // A failed look is not a reason to stop waiting: the next one may pass.
        () => undefined,
      );
    }, POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [api, tenantId, waiting]);

  // The parent passes fresh closures on every render; the effect below must run once per arrival.
  const done = useRef({ onAdded, onClose });
  useEffect(() => {
    done.current = { onAdded, onClose };
  });
  useEffect(() => {
    if (!joined) return;
    done.current.onAdded();
    const timer = window.setTimeout(() => done.current.onClose(), CLOSE_AFTER_MS);
    return () => window.clearTimeout(timer);
  }, [joined]);

  if (issued) {
    return (
      <div className="hm-add">
        {joined ? (
          <Msg tone="success">Машина {joined.name} подключена.</Msg>
        ) : expired ? (
          <Msg
            tone="warn"
            action={
              <button type="button" className="hub-link" onClick={() => void issue()}>
                Новая команда
              </button>
            }
          >
            Код устарел.
          </Msg>
        ) : (
          <>
            <p className="hm-add__step">
              Выполните на машине. Открывать порты не нужно, работает и за NAT. Код действует до{' '}
              {hhmm(issued.expiresAt)}.
            </p>
            <CopyBox text={issued.command} label="Скопировать команду" />
            <div className="hm-add__wait" role="status">
              <Spinner size="s" />
              <span>Ждём, пока машина подключится. Она появится здесь сама.</span>
            </div>
          </>
        )}
        {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
        {!joined && (
          <div className="hm-add__foot">
            <Button appearance="ghost" onClick={onClose}>
              Закрыть
            </Button>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="hm-add">
      {tenants.length > 1 && (
        <section className="ht-field">
          <h3 className="ht-h">Тенант</h3>
          <div className="hm-radios" role="radiogroup" aria-label="Тенант">
            {tenants.map((t) => (
              <button
                key={t.id}
                type="button"
                role="radio"
                aria-checked={tenantId === t.id}
                className="hm-radio"
                onClick={() => setTenantId(t.id)}
              >
                <i className="hm-radio__dot" aria-hidden="true" />
                <span>
                  <b>{t.name}</b>
                </span>
              </button>
            ))}
          </div>
        </section>
      )}
      <section className="ht-field">
        <h3 className="ht-h">Доступ агентов к машине</h3>
        <div className="hm-radios" role="radiogroup" aria-label="Доступ агентов к машине">
          {PROFILES.filter((p) => p.value !== 'container' || config.profiles.container).map((p) => (
            <button
              key={p.value}
              type="button"
              role="radio"
              aria-checked={profile === p.value}
              className="hm-radio"
              onClick={() => setProfile(p.value)}
            >
              <i className="hm-radio__dot" aria-hidden="true" />
              <span>
                <b>{p.title}</b>
                <small>{p.sub}</small>
              </span>
            </button>
          ))}
        </div>
        {profile === 'full' && (
          <div className="hm-add__sure">
            <Note>
              Агенты увидят ваши файлы, ключи SSH и переменные окружения. Включайте на машине, которая нужна
              только для них.
            </Note>
            <Sure on={sure} onToggle={() => setSure(!sure)}>
              Понимаю и подтверждаю
            </Sure>
          </div>
        )}
        <Note>Можно изменить позже в настройках машины.</Note>
      </section>
      {error != null && <p className="ht-err">{orgErrorText(error)}</p>}
      <div className="hm-add__foot">
        <Button appearance="ghost" onClick={onClose} disabled={pending}>
          Отмена
        </Button>
        <Button
          appearance="primary"
          icon={<Terminal size={14} />}
          loading={pending}
          aria-disabled={!canIssue || undefined}
          onClick={() => canIssue && void issue()}
        >
          Показать команду
        </Button>
      </div>
    </div>
  );
};

export const AddMachineSheet = ({
  open,
  tenants,
  onClose,
  onAdded,
}: {
  open: boolean;
  tenants: TenantRef[];
  onClose: () => void;
  onAdded: () => void;
}) => (
  <Sheet
    open={open}
    onClose={onClose}
    title="Добавить машину"
    sub="Одна команда на машине, и она подключится к тенанту."
  >
    <Body tenants={tenants} onClose={onClose} onAdded={onAdded} />
  </Sheet>
);
