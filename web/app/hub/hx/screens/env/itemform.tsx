// An item's form with what stands around it on the board: «Обязательно», «Что сделает», the check of a script and, when the
// item is in other layers too, the note that a change reaches them.
import type { SheetCtx } from './ctx';
import { DraftForm, type Patch } from './form';
import { type Draft, need, valid, viewOf } from './model';
import { CheckResult, type useTry } from './sandbox';

export const PIN_HINT = (on: boolean): string =>
  on
    ? 'Поставится всем. Выключите — человек сможет пропустить'
    : 'Человек может пропустить. Включите — поставится всем';

export const ItemForm = ({
  ctx,
  d,
  set,
  pinned,
  setPinned,
  t,
  itemId,
  usage,
}: {
  ctx: SheetCtx;
  d: Draft;
  set: Patch;
  pinned: boolean;
  setPinned: (v: boolean) => void;
  t: ReturnType<typeof useTry>;
  itemId: string | null;
  usage: string;
}) => {
  const ok = valid(d);
  const v = viewOf(d, ctx.repos);
  return (
    <>
      <div className="ev-form" data-block="Форма">
        <DraftForm
          d={d}
          set={set}
          repos={ctx.repos}
          secretNames={ctx.secretNames}
          canSecrets={ctx.orgAdmin}
        />
        <div className="ev-f ev-f--sw">
          <div>
            <span className="ev-f__l">Обязательно</span>
            <span className="ev-f__h">{PIN_HINT(pinned)}</span>
          </div>
          <button
            type="button"
            className="hx-switch"
            role="switch"
            aria-checked={pinned ? 'true' : 'false'}
            aria-label="Обязательно"
            onClick={() => setPinned(!pinned)}
          >
            <i />
          </button>
        </div>
      </div>
      <p className="ev-does" data-ok={ok ? 'true' : 'false'}>
        {ok && <b>Что сделает: </b>}
        {ok ? v.does : need(d)}
      </p>
      {d.k === 'script' && <CheckResult t={t} d={d} itemId={itemId} ctx={ctx} />}
      {usage !== '' && (
        <p className="ev-sh__note">
          <i className="hx-icon" data-size="sm" data-i="info" aria-hidden="true" />
          <span>{usage}</span>
        </p>
      )}
    </>
  );
};
