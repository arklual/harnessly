// «Ваша машина» of a member: the environment on a machine is current, out of date, or waits for a token; one button brings
// it up to date («Обновить») or looks («Проверить»). One machine is a single line, several are a row each. A run is followed
// until it ends.
import { useState } from 'react';

import type { MeMachine } from '../../../../hub-api/env';
import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Btn, Icon } from '../../ui';
import { useRunPoll } from './runs';

type State = 'ok' | 'stale' | 'token' | 'busy' | 'off' | 'none';

/** The state of a machine: a machine without the setup feature is not «устарело», it is just not set up by the hub. */
export const stateOfMachine = (m: MeMachine, busy: boolean): State => {
  if (busy) return 'busy';
  if (!m.setup) return 'none';
  if (!m.online) return 'off';
  if (!m.current) return 'stale';
  if (m.failed > 0) return 'token';
  return 'ok';
};

const WORD: Record<State, string> = {
  ok: 'Готова',
  stale: 'Устарела',
  token: 'Ждёт токен',
  busy: 'Обновляется…',
  off: 'Не в сети',
  none: 'Без настройки окружения',
};

const ICON: Record<State, string> = {
  ok: 'circle-check',
  stale: 'triangle-alert',
  token: 'key-round',
  busy: 'loader-circle',
  off: 'server',
  none: 'server',
};

export const MachineStrip = ({
  machines,
  refresh,
  say,
  fail,
}: {
  machines: MeMachine[];
  refresh: () => Promise<void>;
  say: (text: string) => void;
  fail: (text: string) => void;
}) => {
  const { api, shell, scope } = useOrg();
  const org = shell.org.slug;
  const space = scope.kind === 'tenant' ? scope.tenant.slug : undefined;
  const [active, setActive] = useState<{
    runId: string | null;
    machineId: string;
    mode: 'update' | 'check';
  } | null>(null);
  const poll = useRunPoll(
    active?.runId ?? null,
    (id) => api.envRun(org, id),
    (run) => {
      const mode = active?.mode ?? 'update';
      setActive(null);
      void refresh().then(() => {
        if (run.status === 'ok')
          say(
            mode === 'update'
              ? `${run.machine.name} обновили — окружение актуально`
              : `${run.machine.name}: всё стоит и работает`,
          );
        else fail(`${run.machine.name}: не всё встало. Подробности — в «Логах»`);
      });
    },
  );
  if (machines.length === 0) return null;
  const go = (m: MeMachine, mode: 'update' | 'check') => {
    setActive({ runId: null, machineId: m.id, mode });
    api.meRun({ mode, machineId: m.id }, space).then(
      (res) => setActive({ runId: res.runs[0]?.runId ?? null, machineId: m.id, mode }),
      (e: unknown) => {
        setActive(null);
        fail(orgErrorText(e));
      },
    );
  };
  const many = machines.length > 1;
  const rows = machines.map((m) => {
    const mine = active?.machineId === m.id;
    const st = stateOfMachine(m, mine && poll.run?.status !== 'ok');
    const mode = active?.mode ?? 'update';
    let title = `Окружение на ${m.name} актуально`;
    let sub = 'Всё из списка стоит и работает';
    if (st === 'stale') {
      title = `Окружение на ${m.name} устарело`;
      sub =
        m.failed > 0
          ? `Не встало: ${m.failed}. Обновите — машина попробует ещё раз`
          : 'Список поменялся, машина ещё не подтянула его';
    }
    if (st === 'token') {
      title = `${m.name} ждёт токен`;
      sub = 'Часть окружения встанет, когда админ организации положит токен в сейф';
    }
    if (st === 'busy') {
      title = mode === 'update' ? `Обновляем ${m.name}…` : `Проверяем ${m.name}…`;
      sub = mode === 'update' ? 'Ставим новое, запускаем скрипты' : 'Сверяем, что всё стоит и работает';
    }
    if (st === 'off') {
      title = `${m.name} сейчас не в сети`;
      sub = 'Включите машину — окружение подтянется само';
    }
    if (st === 'none') {
      title = m.name;
      sub = 'Окружение на этой машине хаб не настраивает';
    }
    const can = st === 'ok' || st === 'stale' || st === 'token';
    const update = st === 'stale';
    const block = (
      <>
        <span className="hb-ic">
          <Icon i={ICON[st]} size="sm" />
        </span>
        <span className="ev-me__t">
          <b>{many ? m.name : title}</b>
          <small>{many ? `${WORD[st]}${m.team.name ? ` · ${m.team.name}` : ''}` : sub}</small>
        </span>
        {can && (
          <Btn
            variant={update ? 'primary' : undefined}
            size="sm"
            onClick={() => go(m, update ? 'update' : 'check')}
          >
            {update ? 'Обновить' : 'Проверить'}
          </Btn>
        )}
      </>
    );
    return { m, st, block };
  });
  if (!many) {
    const r = rows[0];
    return (
      <div className="ev-me" data-state={r.st} data-block="Ваша машина">
        {r.block}
      </div>
    );
  }
  return (
    <div className="ev-mes" data-block="Ваша машина">
      {rows.map((r) => (
        <div className="ev-me" data-state={r.st} key={r.m.id}>
          {r.block}
        </div>
      ))}
    </div>
  );
};
