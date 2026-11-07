// «Логи»: what the machines did with the list in the last 14 days, step by step. Errors first; the log of a step opens
// on a click (it is asked for then: the list of runs carries none).
import { useEffect, useMemo, useState } from 'react';

import type { EnvRunRow, EnvRunStep } from '../../../../hub-api/env';
import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Icon } from '../../ui';
import { Search, Seg } from './bits';
import type { SheetCtx } from './ctx';
import { whenText } from './format';
import { codeOf, whyOf } from './runs';
import { Title } from './sheet';

interface Line {
  key: string;
  run: EnvRunRow;
  step: EnvRunStep;
}

export const LogsBody = ({ ctx }: { ctx: SheetCtx }) => {
  const { api, shell, scope } = useOrg();
  const org = shell.org.slug;
  const member = ctx.role === 'member';
  const space = scope.kind === 'tenant' ? scope.tenant.slug : undefined;
  const [runs, setRuns] = useState<EnvRunRow[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [tab, setTab] = useState<'err' | 'all'>('err');
  const [q, setQ] = useState('');
  const [open, setOpen] = useState<string | null>(null);
  const [full, setFull] = useState<Record<string, EnvRunRow>>({});
  const [now] = useState(() => Date.now());
  const layer = ctx.target.layer;
  const scopeId = ctx.target.scopeId;

  useEffect(() => {
    let live = true;
    const ask = member ? api.meRuns(space) : api.envRuns(org, { layer, scopeId }, false);
    ask.then(
      (r) => live && setRuns(r),
      (e: unknown) => live && setError(e),
    );
    return () => {
      live = false;
    };
  }, [api, org, member, space, layer, scopeId]);

  const lines: Line[] | null = useMemo(
    () =>
      runs === null
        ? null
        : runs.flatMap((run) =>
            run.steps
              .filter(
                (s) =>
                  s.status === 'error' || (tab === 'all' && (s.status === 'ok' || s.status === 'pending')),
              )
              .map((step) => ({ key: `${run.id}:${step.itemId}`, run, step })),
          ),
    [runs, tab],
  );
  const needle = q.trim().toLowerCase();
  const shown = (lines ?? []).filter(
    (l) =>
      needle === '' ||
      `${l.step.name} ${l.run.person.name} ${l.run.machine.name}`.toLowerCase().includes(needle),
  );

  const toggle = (l: Line) => {
    if (open === l.key) return setOpen(null);
    setOpen(l.key);
    if (!full[l.run.id])
      api.envRun(org, l.run.id).then(
        (r) => setFull((f) => ({ ...f, [r.id]: r })),
        (e: unknown) => setError(e),
      );
  };
  const who = (r: EnvRunRow): string => {
    if (r.mode === 'sandbox') return 'Песочница';
    return r.person.id === shell.viewer.id ? 'Вы' : r.person.name;
  };
  const note = member
    ? 'Здесь только ваша машина. Храним 14 дней, ключи из сейфа в логах скрыты'
    : 'Логи с машин команды и проверок в песочнице. Храним 14 дней, ключи из сейфа в логах скрыты';
  const machine = runs?.[0]?.machine.name;
  return (
    <div data-block="Логи">
      <Title
        text={member ? 'Логи вашей машины' : 'Логи окружения'}
        sub={{
          kind: ctx.meta.kind,
          initial: ctx.meta.initial,
          text: member && machine ? `${machine} · ${ctx.meta.pick}` : ctx.meta.pick,
        }}
      />
      <div className="ev-logbar">
        <Seg
          items={[
            ['err', 'Ошибки'],
            ['all', 'Все'],
          ]}
          value={tab}
          onChange={setTab}
          label="Какие"
        />
        {!member && (
          <Search value={q} onChange={setQ} placeholder="Пункт, человек или машина" label="Найти в логах" />
        )}
      </div>
      {runs === null && error == null && <div className="ev-sh__empty">Загружаем…</div>}
      {error != null && <div className="ev-sh__empty">{orgErrorText(error)}</div>}
      {shown.map((l) => {
        const isOpen = open === l.key;
        const detail = full[l.run.id]?.steps.find((s) => s.itemId === l.step.itemId) ?? l.step;
        const bad = l.step.status === 'error';
        return (
          <div className="ev-logi" key={l.key}>
            <button
              type="button"
              className="ev-logrow"
              aria-expanded={isOpen ? 'true' : 'false'}
              data-st={bad ? 'fail' : 'ok'}
              onClick={() => toggle(l)}
            >
              <Icon i={bad ? 'circle-x' : 'circle-check'} size="sm" />
              <span className="hb-row__t">
                <b>{l.step.name}</b>
                <small>{`${who(l.run)} · ${l.run.machine.name} · ${whenText(l.run.startedAt, now)}`}</small>
              </span>
              <span className="ev-logrow__d">{codeOf(l.step)}</span>
            </button>
            {isOpen && (
              <>
                <pre className="ev-log">{detail.log ?? (full[l.run.id] ? 'Лог пуст' : 'Загружаем лог…')}</pre>
                {bad && whyOf(detail) && <p className="ev-why">{whyOf(detail)}</p>}
              </>
            )}
          </div>
        );
      })}
      {runs !== null && shown.length === 0 && (
        <div className="ev-sh__empty">
          {needle
            ? 'Ничего не нашлось'
            : tab === 'all'
              ? 'За 14 дней запусков не было'
              : 'Ошибок нет — всё ставится'}
        </div>
      )}
      <p className="ev-sh__note">
        <Icon i="lock" size="sm" />
        <span>{note}</span>
      </p>
    </div>
  );
};
