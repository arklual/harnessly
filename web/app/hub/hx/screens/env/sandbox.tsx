// The sandbox: a run of the list from scratch on one of the person's own machines, in a folder with an empty home, so
// nothing of the machine changes. «Проверить в песочнице» runs the whole list of a layer; «Проверить» in a script's form
// runs the layer's list with this one script added or put in place of the old one.
import { useEffect, useState } from 'react';

import type { CatalogItem, EnvRunRow, EnvRunStep, MeMachine } from '../../../../hub-api/env';
import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Btn, Icon } from '../../ui';
import { Help } from './bits';
import type { SheetCtx } from './ctx';
import { secs } from './format';
import { type Draft, nameOf, toSource } from './model';
import { codeOf, fixFor, MARK, moveBefore, stateOf, useRunPoll, whyOf } from './runs';
import { Foot, Title } from './sheet';
import { metaOf, type Target } from './scope';

/** The sandbox runs of this screen: ask for one, then follow it. */
export const useTry = () => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const [started, setStarted] = useState<{ runId: string; machine: string } | null>(null);
  const [asking, setAsking] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const poll = useRunPoll(started?.runId ?? null, (id) => api.envRun(org, id));
  const start = async (target: Target, draft: Draft | null, entryId: string | null, machineId?: string) => {
    setAsking(true);
    setError(null);
    setStarted(null);
    try {
      const res = await api.envTry(org, {
        layer: target.layer,
        scopeId: target.scopeId,
        machineId,
        draft: draft
          ? {
              entryId: entryId ?? undefined,
              kind: draft.k,
              name: nameOf(draft),
              source: toSource(draft),
            }
          : undefined,
      });
      setStarted({ runId: res.runId, machine: res.machine.name });
    } catch (e) {
      setError(e);
    } finally {
      setAsking(false);
    }
  };
  const reset = () => {
    setStarted(null);
    setError(null);
  };
  return {
    start,
    reset,
    asking,
    machine: started?.machine ?? '',
    run: poll.run,
    error: error ?? poll.error,
    active: started !== null,
  };
};

const StepLog = ({ log }: { log: string }) => (log ? <pre className="ev-log">{log}</pre> : null);

/** The result of one script's check, under its form. */
export const CheckResult = ({
  t,
  d,
  itemId,
  ctx,
}: {
  t: ReturnType<typeof useTry>;
  d: Draft;
  itemId: string | null;
  ctx: SheetCtx;
}) => {
  const { run, machine, asking, error } = t;
  if (!t.active && !asking && !error) return null;
  const name = nameOf(d);
  const step =
    run?.steps.find((s) => (itemId ? s.itemId === itemId : s.name === name && s.kind === 'script')) ?? null;
  const running = asking || !run || run.status === 'running';
  const failed = !running && step?.status === 'error';
  let state: 'run' | 'ok' | 'fail' = 'ok';
  if (running && !failed) state = 'run';
  if (failed || (error != null && !running)) state = 'fail';
  if (!running && !failed && run && step?.status !== 'ok' && step?.status !== 'skipped') state = 'fail';
  const fix = failed && run && step ? fixFor(run, step, ctx.catalog ?? [], ctx.layers) : null;
  const icon = { run: 'loader-circle', ok: 'circle-check', fail: 'circle-x' }[state];
  let title = `Проверяем в песочнице${machine ? ` на ${machine}` : ''}…`;
  if (state === 'ok') title = `Прошло в песочнице за ${secs(step?.ms ?? 0)} — можно добавлять`;
  if (state === 'fail') title = 'Упало в песочнице';
  return (
    <div className="ev-ft" data-state={state} data-block="Проверка в песочнице">
      <span className="ev-ft__h">
        <Icon i={icon} size="sm" />
        <b>{title}</b>
      </span>
      {state === 'fail' && error != null && <p className="ev-why">{orgErrorText(error)}</p>}
      {state === 'fail' && step && (
        <>
          <StepLog log={step.log ?? ''} />
          <p className="ev-why">
            {whyOf(step) || run?.steps.find((s) => s.status === 'error')?.message || ''}
          </p>
        </>
      )}
      {state === 'fail' && !step && run && (
        <p className="ev-why">
          {run.steps.find((s) => s.status === 'error')?.message ?? 'Список не дошёл до этого скрипта'}
        </p>
      )}
      {fix && (
        <div className="ev-acts">
          <Btn
            size="sm"
            disabled={ctx.actions.busy}
            onClick={() => {
              const l = ctx.layers.find(
                (x) => x.layer === fix.layer.layer && x.scopeId === fix.layer.scopeId,
              );
              if (l)
                void ctx.actions.reorder(
                  fix.layer,
                  moveBefore(l, fix.itemId, fix.beforeItemId),
                  `«${fix.name}» теперь запустится раньше`,
                );
            }}
          >
            {`Поставить «${fix.name}» раньше`}
          </Btn>
        </div>
      )}
    </div>
  );
};

const StepRow = ({
  s,
  run,
  ctx,
  catalog,
}: {
  s: EnvRunStep;
  run: EnvRunRow;
  ctx: SheetCtx;
  catalog: readonly CatalogItem[];
}) => {
  const state = stateOf(s.status);
  const failed = state === 'fail';
  const fix = failed ? fixFor(run, s, catalog, ctx.layers) : null;
  const row = ctx.layers.flatMap((l) => l.items).find((r) => r.itemId === s.itemId);
  const layer = ctx.layers.find((l) => l.items.some((r) => r.itemId === s.itemId));
  const detail = failed ? codeOf(s) : state === 'done' && s.status === 'ok' ? secs(s.ms) : '';
  return (
    <>
      <div className="ev-step" data-state={state}>
        <span className="ev-step__mark">
          <Icon i={MARK[state]} size="sm" />
        </span>
        <span className="ev-step__t">
          {s.name}
          <small>
            {[
              s.kind === 'script'
                ? 'скрипт'
                : s.kind === 'repo'
                  ? 'репозиторий'
                  : s.kind === 'access'
                    ? 'доступ'
                    : '',
              layer ? metaOf(layer).short.toLowerCase() : '',
            ]
              .filter(Boolean)
              .join(' · ')}
          </small>
        </span>
        <span className="ev-step__d">{detail}</span>
      </div>
      {failed && (
        <>
          <StepLog log={s.log ?? ''} />
          <p className="ev-why">{whyOf(s)}</p>
          <div className="ev-acts">
            {fix && (
              <Btn
                size="sm"
                disabled={ctx.actions.busy}
                onClick={() => {
                  const l = ctx.layers.find(
                    (x) => x.layer === fix.layer.layer && x.scopeId === fix.layer.scopeId,
                  );
                  if (l)
                    void ctx.actions
                      .reorder(
                        fix.layer,
                        moveBefore(l, fix.itemId, fix.beforeItemId),
                        `«${fix.name}» теперь запустится раньше`,
                      )
                      .then((ok) => ok && ctx.go({ mode: 'test' }));
                }}
              >
                {`Поставить «${fix.name}» раньше`}
              </Btn>
            )}
            {row && layer && s.kind !== 'access' && (
              <Btn variant="ghost" size="sm" onClick={() => ctx.go({ mode: 'edit', entryId: row.entryId })}>
                Изменить скрипт
              </Btn>
            )}
          </div>
        </>
      )}
    </>
  );
};

export const TestBody = ({ ctx }: { ctx: SheetCtx }) => {
  const { api, scope } = useOrg();
  const space = scope.kind === 'tenant' ? scope.tenant.slug : undefined;
  const [machines, setMachines] = useState<MeMachine[] | null>(null);
  const [mErr, setMErr] = useState<unknown>(null);
  const [pick, setPick] = useState<string | null>(null);
  const t = useTry();
  useEffect(() => {
    let live = true;
    api.meMachines(space).then(
      (m) => live && setMachines(m),
      (e: unknown) => live && setMErr(e),
    );
    return () => {
      live = false;
    };
  }, [api, space]);
  const usable = (machines ?? []).filter((m) => m.online && m.setup);
  const chosen = usable.find((m) => m.id === pick) ?? usable[0] ?? null;
  const run = t.run;
  const inRun = t.active && (!run || run.status === 'running');
  const done = t.active && !!run && run.status !== 'running';
  const setup = !t.active;
  const fails = run ? run.steps.filter((s) => s.status === 'error') : [];
  const lead = `Поставим с нуля всё, что получит новичок ${ctx.meta.gen}: от организации, тенанта и команды. Ставим в отдельной песочнице на вашей машине — ваши файлы, чаты и настройки не тронем, потом песочницу удалим.`;
  const vmTip =
    'Одна из ваших машин. Песочница — отдельная папка и отдельный пользователь на ней: ставим как на чистую машину, ваше рабочее не видно и не меняется';
  const start = () => void t.start(ctx.target, null, null, chosen?.id);
  return (
    <div data-block="Проверка в песочнице">
      <Title
        text="Проверить в песочнице"
        sub={{ kind: ctx.meta.kind, initial: ctx.meta.initial, text: ctx.meta.pick }}
      />
      {setup && (
        <>
          <p className="ev-sh__lead">{lead}</p>
          <div className="ev-form">
            <div className="ev-f">
              <div className="ev-f__l">
                <span>Машина</span>
                <Help text={vmTip} />
              </div>
              {machines === null && mErr == null && <div className="ev-f__h">Ищем ваши машины…</div>}
              {mErr != null && (
                <div className="ev-f__h" data-warn="true">
                  {orgErrorText(mErr)}
                </div>
              )}
              {machines !== null && usable.length === 0 && (
                <div className="ev-f__h" data-warn="true">
                  Нет вашей машины в сети, которая умеет настраиваться. Включите машину и вернитесь.
                </div>
              )}
              {usable.length > 0 && (
                <div className="ev-opts" role="radiogroup" aria-label="Машина">
                  {usable.map((m) => (
                    <button
                      type="button"
                      key={m.id}
                      className="ev-opt"
                      aria-pressed={chosen?.id === m.id ? 'true' : 'false'}
                      onClick={() => setPick(m.id)}
                    >
                      {m.name}
                    </button>
                  ))}
                </div>
              )}
              {chosen && <div className="ev-f__h">Linux · в сети</div>}
            </div>
          </div>
          {t.error != null && <p className="ev-sh__note">{orgErrorText(t.error)}</p>}
          <p className="ev-sh__note">
            <Icon i="lock" size="sm" />
            <span>
              Доступы войдут вашим аккаунтом, ключи возьмём из сейфа. В логах значения ключей скрыты.
            </span>
          </p>
        </>
      )}
      {t.active && (
        <>
          {done && (
            <p className="ev-does" data-ok="true">
              <b>{fails.length ? 'Не поставилось: ' : 'Всё поставилось с нуля. '}</b>
              {fails.length
                ? `${fails.map((s) => `«${s.name}»`).join(', ')}. Остальное в порядке. Песочницу удалили, лог — в «Логах»`
                : 'Можно спокойно раскатывать. Песочницу удалили'}
            </p>
          )}
          {inRun && (
            <p className="ev-sh__lead">{`Ставим на ${t.machine} в песочнице — по порядку, как на новой машине`}</p>
          )}
          {t.error != null && <p className="ev-sh__note">{orgErrorText(t.error)}</p>}
          <div className="ev-steps">
            {run?.steps.map((s) => (
              <StepRow key={s.itemId} s={s} run={run} ctx={ctx} catalog={ctx.catalog ?? []} />
            ))}
          </div>
        </>
      )}
      <Foot>
        <span className="ev-foot__sp" />
        {done && (
          <Btn variant="ghost" size="sm" onClick={start}>
            Запустить снова
          </Btn>
        )}
        {setup && (
          <Btn variant="primary" size="sm" busy={t.asking} disabled={!chosen} onClick={start}>
            Запустить проверку
          </Btn>
        )}
        {inRun && (
          <Btn variant="primary" size="sm" disabled>
            Проверяем…
          </Btn>
        )}
        {done && (
          <Btn variant="primary" size="sm" onClick={ctx.close}>
            Готово
          </Btn>
        )}
      </Foot>
    </div>
  );
};
