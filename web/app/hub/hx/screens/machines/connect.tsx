// «Подключить машину» as a page, the board's `pConnect()`: a strip of steps and the card of the current one. It works on the
// calls the old sheet used (code of a new machine, «эта машина», a look at the tenant's machines until the new one dials in).
// The hub's code carries no owner: the code is the creator's, so «Чья машина» is applied once the machine is in (a name, and for
// a shared one its team and the team's access) with the calls the card already uses.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { hhmm } from '../../../lib/format';
import { useCopy, useNow } from '../../../lib/hooks';
import { useSession } from '../../../lib/session';
import { Sure } from '../../../ui/parts';
import { orgErrorText } from '../../../org/api/errors';
import type { MachineSetup, OrgMachine } from '../../../org/api/machines';
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

const STEPS = ['Где машина', 'Чья машина', 'Установка', 'Проверка'];

type Owner = 'me' | 'team';
const AGENT_NAME = { claude: 'Claude Code', codex: 'Codex' } as const;

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
  const { api, scope, shell } = useOrg();
  const { config, me } = useSession();
  const localOffered = config.connect.local.available && me?.user.isAdmin === true;
  const [step, setStep] = useState(1);
  const [where, setWhere] = useState<Where>('other');
  const [tenantId, setTenantId] = useState(tenants[0]?.id ?? '');
  const [profile, setProfile] = useState<MachineProfile>('dir');
  const [sure, setSure] = useState(false);
  const [owner, setOwner] = useState<Owner>('me');
  const [teamId, setTeamId] = useState('');
  const [name, setName] = useState('');
  const [setup, setSetup] = useState<MachineSetup | null>(null);
  const [trusting, setTrusting] = useState(false);
  const [applied, setApplied] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [issued, setIssued] = useState<IssuedCode | null>(null);
  const [joined, setJoined] = useState<Joined | null>(null);
  // The machines the tenant had when the code was issued: a new id among them later is the one that used the code.
  const known = useRef<Set<string>>(new Set());
  const waiting = step === 3 && issued !== null && joined === null;
  const now = useNow(1000, waiting);
  const expired = issued !== null && new Date(issued.expiresAt).getTime() <= now;
  const { copied, copy } = useCopy();
  // The hub lists the teams of the tenant the person is in now; for another tenant there is nothing to pick from.
  // A member connects only a machine of their own: a shared one, and trusting it, are for those who manage.
  const manages = shell.viewer.role !== 'member';
  const teams = manages && scope.kind === 'tenant' && scope.tenant.id === tenantId ? shell.teams : [];
  const team = teams.find((t) => t.id === teamId) ?? teams[0] ?? null;
  const canOwn = owner === 'me' || team !== null;
  const canNext = tenantId !== '' && (where === 'local' || profile !== 'full' || sure);

  const issue = async () => {
    setPending(true);
    setError(null);
    try {
      known.current = new Set((await api.spaceMachines(tenantId)).map((m) => m.id));
      setIssued(await api.issueMachineCode(tenantId, profile, profile === 'full'));
      setStep(3);
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
      setStep(4);
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
            setStep(4);
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
  const trusted = setup === null || setup.runtime === 'ready';
  const ready = joined !== null && live && trusted;

  // The list is asked again until the machine is in it and answers.
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  });
  useEffect(() => {
    if (step !== 4) return;
    refreshRef.current();
    if (ready && fresh) return;
    const timer = window.setInterval(() => refreshRef.current(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [step, ready, fresh]);

  // What the hub knows of the machine after it dialed in: confirmation, the agents and their sign-in.
  const joinedId = joined?.id ?? null;
  const settled =
    setup !== null &&
    setup.runtime === 'ready' &&
    setup.agents.every((a) => !a.installed || a.login !== 'unknown');
  useEffect(() => {
    if (step !== 4 || joinedId === null || settled) return;
    let alive = true;
    const look = () =>
      api.machineDetail(joinedId).then(
        (d) => alive && setSetup(d.setup ?? null),
        () => undefined,
      );
    void look();
    const timer = window.setInterval(() => void look(), POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [api, step, joinedId, settled]);

  // «Чья машина» is applied once, when the machine is in: the name and, for a shared one, its team and the team's access.
  const applying = useRef<string | null>(null);
  useEffect(() => {
    if (step !== 4 || joinedId === null || applying.current === joinedId) return;
    applying.current = joinedId;
    const todo = async () => {
      const wanted = name.trim();
      if (owner === 'team' && team) {
        await api.moveMachine(joinedId, tenantId, team.id);
        await api.setMachineAccess(joinedId, { mode: 'team', personIds: [] });
      }
      if (wanted !== '' && wanted !== joined?.name) await api.renameMachine(joinedId, wanted);
    };
    todo().then(
      () => setApplied('ok'),
      (e: unknown) => setApplied(orgErrorText(e)),
    );
  }, [api, step, joinedId, owner, team, tenantId, name, joined?.name]);

  const trust = async () => {
    if (joinedId === null) return;
    setTrusting(true);
    try {
      await api.trustMachine(joinedId);
      setSetup((x) => (x ? { ...x, runtime: 'ready' } : x));
    } catch (e) {
      setError(e);
    } finally {
      setTrusting(false);
    }
  };

  const reset = () => {
    setSetup(null);
    setApplied(null);
    applying.current = null;
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
  if (step === 2) {
    body = (
      <div className="stack">
        <Radio
          on={owner === 'me'}
          title="Моя"
          sub={`Владелец — ${me?.user.name || me?.user.email || 'вы'}. Чаты на ней открываете вы, ${manages ? 'подтверждаете её тоже вы' : 'подтверждает администратор организации'}.`}
          onPick={() => setOwner('me')}
        />
        {teams.length > 0 && (
          <div
            className={`radio ${owner === 'team' ? 'on' : ''}`}
            role="radio"
            aria-checked={owner === 'team'}
            tabIndex={0}
            onClick={() => setOwner('team')}
            onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), setOwner('team'))}
          >
            <span className="hd-o" />
            <div style={{ flex: 1 }}>
              <b>Общая для команды</b>
              <div className="mut sm">Сборочная или общая машина: открывать чаты может любой в команде.</div>
              {owner === 'team' && (
                <select
                  className="inp"
                  aria-label="Команда"
                  style={{ marginTop: 8, maxWidth: 280 }}
                  value={team?.id ?? ''}
                  onClick={(e) => e.stopPropagation()}
                  onChange={(e) => setTeamId(e.target.value)}
                >
                  {teams.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>
        )}
        <label className="mut sm" htmlFor="cn-name" style={{ marginTop: 6 }}>
          Название машины
        </label>
        <input
          id="cn-name"
          className="inp"
          style={{ maxWidth: 360 }}
          maxLength={60}
          placeholder="Если не задать, возьмём имя, с которым она назовётся сама"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <div className="mut2 sm">
          Название видно в списке машин. Поменять его и передать машину можно потом в её карточке.
        </div>
      </div>
    );
  }
  if (step === 3 && where === 'local') {
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
  if (step === 3 && where === 'other' && issued) {
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
  if (step === 4 && joined) {
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
          {live && setup && (
            <Check s={setup.runtime === 'ready' ? 'ok' : 'err'}>
              {setup.runtime === 'ready' ? 'Машина подтверждена' : 'Машина ждёт подтверждения'}
              {setup.runtime === 'needs_trust' && !manages && (
                <div className="mut sm" style={{ marginTop: 4 }}>
                  Подтверждает администратор организации: пока этого нет, чаты на ней не откроются. Машина уже
                  видна ему в разделе «Машины».
                </div>
              )}
              {setup.runtime === 'needs_trust' && manages && (
                <>
                  <div className="mut sm" style={{ margin: '4px 0 8px' }}>
                    Подтвердите, что это ваша машина: пока этого нет, чаты на ней не откроются.
                  </div>
                  <Btn variant="primary" size="sm" icon="check" busy={trusting} onClick={() => void trust()}>
                    Это моя машина — подтвердить
                  </Btn>
                </>
              )}
              {setup.runtime === 'blocked' && (
                <div className="mut sm">
                  Подтверждать машины на этом хабе выключено: открыть её сможет только администратор хаба.
                </div>
              )}
            </Check>
          )}
          {live &&
            setup?.agents.map((a) => (
              <Check key={a.provider} s={a.installed ? 'ok' : 'skip'}>
                {AGENT_NAME[a.provider]} {a.installed ? 'поставлен' : 'не поставлен'}
                {a.installed && a.version && <span className="mut2 sm"> · {a.version}</span>}
                {!a.installed && <span className="mut2 sm"> · поставить можно позже из карточки машины</span>}
              </Check>
            ))}
          {live && setup && setup.agents.some((a) => a.installed) && (
            <Check
              s={
                setup.agents.some((a) => a.installed && a.login === 'out')
                  ? 'err'
                  : setup.agents.some((a) => a.installed && a.login === 'unknown')
                    ? 'run'
                    : 'ok'
              }
            >
              Агенты вошли в аккаунты
              {setup.agents
                .filter((a) => a.installed && a.login === 'out')
                .map((a) => (
                  <div key={a.provider} className="mut sm">
                    {AGENT_NAME[a.provider]}: вход не выполнен — чаты на нём не стартуют, войдите на машине.
                  </div>
                ))}
              {setup.accounts.length > 0 && (
                <div className="mut2 sm">{setup.accounts.map((c) => `«${c.label}»`).join(' · ')}</div>
              )}
            </Check>
          )}
          {applied !== null && applied !== 'ok' && (
            <Check s="err">
              Название или команду применить не удалось
              <div className="mut sm">{applied}. Поменять их можно в карточке машины.</div>
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
      <Btn variant="primary" disabled={!canNext} onClick={() => (setError(null), setStep(2))}>
        Дальше
      </Btn>
    ) : step === 2 ? (
      <Btn
        variant="primary"
        busy={pending && where === 'other'}
        disabled={!canOwn}
        onClick={() => (where === 'local' ? (setError(null), setStep(3)) : void issue())}
      >
        {where === 'local' ? 'Дальше' : 'Показать команду'}
      </Btn>
    ) : step === 3 && where === 'local' ? (
      <Btn variant="primary" busy={pending} onClick={() => void claimLocal()}>
        Подключить эту машину
      </Btn>
    ) : step === 3 && expired ? (
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
            {(step === 2 || (step === 3 && !joined)) && (
              <Btn variant="ghost" onClick={() => setStep(step - 1)} disabled={pending}>
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
