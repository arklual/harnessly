// «Подключить машину» as a page, the board's `pConnect()`: a strip of steps and the card of the current one. It works on the
// calls the old sheet used (code of a new machine, «эта машина», a look at the tenant's machines until the new one dials in).
// The board's step «Чья машина» is not here: the hub's code has no owner to pick, so the page does not invent one.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { hhmm } from '../../../lib/format';
import { useCopy, useNow } from '../../../lib/hooks';
import { useSession } from '../../../lib/session';
import { Sure } from '../../../ui/parts';
import { orgErrorText } from '../../../org/api/errors';
import type { OrgMachine } from '../../../org/api/machines';
import type { IssuedCode, MachineProfile } from '../../../org/api/tenant';
import { useOrg } from '../../../org/context';
import type { TenantRef } from '../../../org/types';
import { Head } from '../../shell';
import { Btn } from '../../ui';

const POLL_MS = 3_000;
const KEY = 'add';
const VAL = 'machine';

/** The address of the page: `?add=machine`. Every other key of the query stays as it was. */
export const readAdd = (search: string): boolean => new URLSearchParams(search).get(KEY) === VAL;

export const withAdd = (search: string, on: boolean): string => {
  const q = new URLSearchParams(search);
  if (on) q.set(KEY, VAL);
  else q.delete(KEY);
  const s = q.toString();
  return s ? `?${s}` : '';
};

const go = (search: string) => {
  history.pushState(null, '', `${location.pathname}${search}`);
  window.dispatchEvent(new PopStateEvent('popstate'));
};

export const useAddUrl = (add: boolean) => {
  const open = useCallback(() => go(withAdd(location.search, true)), []);
  const close = useCallback(() => {
    if (!readAdd(location.search)) return;
    go(withAdd(location.search, false));
  }, []);
  return { adding: add, open, close };
};

const PROFILES: { value: MachineProfile; title: string; sub: string }[] = [
  { value: 'dir', title: 'В отдельной папке', sub: 'Свой пользователь, одна папка. Рекомендуем.' },
  { value: 'container', title: 'В контейнере', sub: 'Каждая сессия отдельно.' },
  { value: 'full', title: 'С вашим доступом', sub: 'Агенты видят все ваши файлы.' },
];

const STEPS = ['Где машина', 'Установка', 'Проверка'];

interface Joined {
  id: string;
  name: string;
  status: 'pending' | 'online' | 'offline';
}

type Where = 'local' | 'other';

const Radio = ({
  on,
  title,
  sub,
  onPick,
}: {
  on: boolean;
  title: string;
  sub?: string;
  onPick: () => void;
}) => (
  <div
    className={`radio ${on ? 'on' : ''}`}
    role="radio"
    aria-checked={on}
    tabIndex={0}
    onClick={onPick}
    onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), onPick())}
  >
    <span className="hd-o" />
    <div>
      <b>{title}</b>
      {sub && <div className="mut sm">{sub}</div>}
    </div>
  </div>
);

const Check = ({ s, children }: { s: 'ok' | 'run' | 'err' | 'skip'; children: React.ReactNode }) => (
  <div className={`check ${s}`}>
    <span className="hd-c">{s === 'ok' ? '✓' : s === 'err' ? '!' : s === 'skip' ? '–' : ''}</span>
    <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
  </div>
);

export const ConnectPage = ({
  tenants,
  machines,
  refresh,
  onClose,
  onOpen,
}: {
  tenants: TenantRef[];
  /** The machines the screen loaded: the new one shows up here with what the hub knows of it. */
  machines: OrgMachine[];
  refresh: () => void;
  onClose: () => void;
  onOpen: (id: string) => void;
}) => {
  const { api } = useOrg();
  const { config, me } = useSession();
  const localOffered = config.connect.local.available && me?.user.isAdmin === true;
  const [step, setStep] = useState(1);
  const [where, setWhere] = useState<Where>('other');
  const [tenantId, setTenantId] = useState(tenants[0]?.id ?? '');
  const [profile, setProfile] = useState<MachineProfile>('dir');
  const [sure, setSure] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [issued, setIssued] = useState<IssuedCode | null>(null);
  const [joined, setJoined] = useState<Joined | null>(null);
  // The machines the tenant had when the code was issued: a new id among them later is the one that used the code.
  const known = useRef<Set<string>>(new Set());
  const waiting = step === 2 && issued !== null && joined === null;
  const now = useNow(1000, waiting);
  const expired = issued !== null && new Date(issued.expiresAt).getTime() <= now;
  const { copied, copy } = useCopy();
  const canNext = tenantId !== '' && (where === 'local' || profile !== 'full' || sure);

  const issue = async () => {
    setPending(true);
    setError(null);
    try {
      known.current = new Set((await api.spaceMachines(tenantId)).map((m) => m.id));
      setIssued(await api.issueMachineCode(tenantId, profile, profile === 'full'));
      setStep(2);
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };

  const claimLocal = async () => {
    setPending(true);
    setError(null);
    try {
      const m = await api.claimLocalMachine(tenantId);
      setJoined({ id: m.id, name: m.name, status: m.status });
      setStep(3);
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
          if (alive && fresh) {
            setJoined({ id: fresh.id, name: fresh.name, status: fresh.status });
            setStep(3);
          }
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

  const fresh = joined ? (machines.find((m) => m.id === joined.id) ?? null) : null;
  const live = fresh ? fresh.state === 'online' || fresh.state === 'idle' : joined?.status === 'online';
  const ready = joined !== null && live;

  // The list is asked again until the machine is in it and answers.
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  });
  useEffect(() => {
    if (step !== 3) return;
    refreshRef.current();
    if (ready && fresh) return;
    const timer = window.setInterval(() => refreshRef.current(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [step, ready, fresh]);

  const reset = () => {
    setStep(1);
    setIssued(null);
    setJoined(null);
    setSure(false);
    setError(null);
  };

  const bar = useMemo(
    () => (
      <div className="life" style={{ marginBottom: 22, gridTemplateColumns: `repeat(${STEPS.length}, 1fr)` }}>
        {STEPS.map((s, i) => (
          <div key={s} className={`st ${i + 1 < step ? 'done' : i + 1 === step ? 'cur' : ''}`}>
            <b>
              {i + 1}. {s}
            </b>
            {i + 1 < step ? 'готово' : i + 1 === step ? 'сейчас' : ''}
          </div>
        ))}
      </div>
    ),
    [step],
  );

  let body: React.ReactNode = null;
  if (step === 1) {
    body = (
      <>
        {tenants.length > 1 && (
          <div className="stack" style={{ marginBottom: 18 }}>
            <label className="mut sm" htmlFor="cn-tenant">
              Тенант
            </label>
            <select
              id="cn-tenant"
              className="inp"
              style={{ maxWidth: 280 }}
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
            >
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
        )}
        <div className="stack" role="radiogroup" aria-label="Где машина">
          {localOffered && (
            <Radio
              on={where === 'local'}
              title="Эта машина"
              sub="Машина, которая запущена рядом с хабом. Подключается одним действием, без команды."
              onPick={() => setWhere('local')}
            />
          )}
          {localOffered && (
            <Radio
              on={where === 'other'}
              title="Другая машина"
              sub="Компьютер или сервер: одна команда в терминале."
              onPick={() => setWhere('other')}
            />
          )}
          {where === 'other' && (
            <>
              <div className="mut sm" style={{ marginTop: 6 }}>
                Доступ агентов к машине
              </div>
              {PROFILES.filter((p) => p.value !== 'container' || config.profiles.container).map((p) => (
                <Radio
                  key={p.value}
                  on={profile === p.value}
                  title={p.title}
                  sub={p.sub}
                  onPick={() => setProfile(p.value)}
                />
              ))}
            </>
          )}
        </div>
        {where === 'other' && profile === 'full' && (
          <div className="fixbox" style={{ marginTop: 14 }}>
            <b>Агенты увидят ваши файлы</b>
            <div className="mut sm" style={{ margin: '6px 0 10px' }}>
              Ключи SSH и переменные окружения тоже. Включайте на машине, которая нужна только для них.
            </div>
            <Sure on={sure} onToggle={() => setSure(!sure)}>
              Понимаю и подтверждаю
            </Sure>
          </div>
        )}
        <div className="mut2 sm" style={{ marginTop: 12 }}>
          Доступ можно изменить позже в настройках машины.
        </div>
      </>
    );
  }
  if (step === 2 && where === 'local') {
    body = (
      <div className="fixbox ok">
        <b>Команд не нужно</b>
        <div className="mut sm" style={{ marginTop: 6 }}>
          Нажмите «Подключить эту машину»: хаб возьмёт машину, которая запущена рядом с ним, в выбранный
          тенант.
        </div>
      </div>
    );
  }
  if (step === 2 && where === 'other' && issued) {
    body = (
      <>
        <div className="mut" style={{ marginBottom: 10 }}>
          Выполните на машине одну команду. Открывать порты не нужно, работает и за NAT.
        </div>
        <div className="code">
          <span>{issued.command}</span>
          <Btn
            variant="ghost"
            size="sm"
            icon={copied ? 'check' : 'copy'}
            aria-label="Скопировать команду"
            title="Скопировать команду"
            onClick={() => copy(issued.command)}
          />
        </div>
        <div className="mut2 sm" style={{ marginTop: 8 }}>
          {expired
            ? 'Код устарел. Получите новую команду.'
            : `Код в команде одноразовый, действует до ${hhmm(issued.expiresAt)}. Машина появится здесь сама, как только подключится.`}
        </div>
        {!expired && (
          <div className="check run" style={{ marginTop: 10 }} role="status">
            <span className="hd-c" />
            <div>Ждём, пока машина подключится</div>
          </div>
        )}
      </>
    );
  }
  if (step === 3 && joined) {
    const state = fresh?.state ?? (joined.status === 'online' ? 'online' : 'offline');
    body = (
      <>
        <div>
          <Check s="ok">Машина «{joined.name}» связалась с хабом</Check>
          <Check s={live ? 'ok' : 'run'}>
            {live
              ? state === 'idle'
                ? 'Агент на связи, чатов пока нет'
                : 'Агент запущен и на связи'
              : 'Ждём, пока агент ответит'}
          </Check>
          {fresh && fresh.runnerVersion && (
            <Check s={fresh.runnerOutdated ? 'err' : 'ok'}>
              Версия агента {fresh.runnerVersion}
              {fresh.runnerOutdated && (
                <div className="mut sm">Есть новее — обновить можно из карточки машины.</div>
              )}
            </Check>
          )}
          <Check s={fresh ? 'ok' : 'run'}>
            {fresh ? 'Видна в списке машин' : 'Появляется в списке машин'}
          </Check>
        </div>
        {ready && (
          <div className="fixbox ok" style={{ marginTop: 14 }}>
            <div className="row sp">
              <div>
                <b>Машина подключена</b>
                <div className="mut sm">Она уже в списке машин: там её карточка, доступ и ресурсы.</div>
              </div>
              <div className="row">
                <Btn size="sm" onClick={reset}>
                  Подключить ещё
                </Btn>
                <Btn variant="primary" size="sm" onClick={() => onOpen(joined.id)}>
                  Открыть машину
                </Btn>
              </div>
            </div>
          </div>
        )}
      </>
    );
  }

  const next =
    step === 1 ? (
      <Btn
        variant="primary"
        busy={pending && where === 'other'}
        disabled={!canNext}
        onClick={() => (where === 'local' ? (setError(null), setStep(2)) : void issue())}
      >
        {where === 'local' ? 'Дальше' : 'Показать команду'}
      </Btn>
    ) : step === 2 && where === 'local' ? (
      <Btn variant="primary" busy={pending} onClick={() => void claimLocal()}>
        Подключить эту машину
      </Btn>
    ) : step === 2 && expired ? (
      <Btn variant="primary" busy={pending} onClick={() => void issue()}>
        Новая команда
      </Btn>
    ) : null;

  return (
    <>
      <Head
        title="Подключить машину"
        lead="Мастер заканчивается, только когда машина готова к чатам. Если что-то не так — он скажет, что сделать."
        acts={
          <Btn variant="ghost" size="sm" onClick={onClose}>
            К машинам
          </Btn>
        }
      />
      <div style={{ maxWidth: 860 }}>
        {bar}
        <div className="t-card viz-card dash-tile hd-card">
          {body}
          {error != null && (
            <div className="mut sm" role="alert" style={{ marginTop: 12, color: 'var(--hx-danger)' }}>
              {orgErrorText(error)}
            </div>
          )}
          <div className="row" style={{ marginTop: 22 }}>
            {step === 2 && !joined && (
              <Btn variant="ghost" onClick={() => setStep(1)} disabled={pending}>
                Назад
              </Btn>
            )}
            <span style={{ marginLeft: 'auto' }} />
            {next}
          </div>
        </div>
      </div>
    </>
  );
};
