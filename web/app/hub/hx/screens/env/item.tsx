// An item opened: its own form to change (with «Убрать»), or only its lines when it belongs to a layer above.
import { useState } from 'react';

import { Btn, Icon } from '../../ui';
import type { SheetCtx } from './ctx';
import { type Draft, type Fields, fromItem, KIND, nameOf, valid, viewLinesOf, viewOf } from './model';
import { ItemForm } from './itemform';
import { useTry } from './sandbox';
import { metaOf, same } from './scope';
import { Foot, Title } from './sheet';

export const ItemBody = ({ ctx, entryId, edit }: { ctx: SheetCtx; entryId: string; edit: boolean }) => {
  const layer = ctx.layers.find((l) => l.items.some((r) => r.entryId === entryId));
  const row = layer?.items.find((r) => r.entryId === entryId);
  const item = ctx.catalog?.find((c) => c.id === row?.itemId) ?? null;
  if (!layer || !row) return <div className="ev-sh__empty">Этого пункта уже нет</div>;
  if (!item) {
    return (
      <>
        <Title text={row.name} />
        <div className="ev-sh__empty">
          {ctx.catalog === null ? 'Загружаем…' : 'Не нашли пункт в каталоге'}
        </div>
      </>
    );
  }
  return <Loaded ctx={ctx} entryId={entryId} edit={edit} />;
};

const Loaded = ({ ctx, entryId, edit }: { ctx: SheetCtx; entryId: string; edit: boolean }) => {
  const layer = ctx.layers.find((l) => l.items.some((r) => r.entryId === entryId));
  const row = layer?.items.find((r) => r.entryId === entryId);
  const item = ctx.catalog?.find((c) => c.id === row?.itemId);
  const start = item ? fromItem(item) : null;
  const [draft, setDraft] = useState<Draft | null>(start);
  const [pinned, setPinned] = useState(row?.pinned ?? false);
  const t = useTry();
  if (!layer || !row || !item) return null;
  const lm = metaOf(layer);
  const inLayer = same(layer, ctx.target);
  const itemMine =
    ctx.role === 'admin' ||
    (item.owner !== null && item.owner.layer === layer.layer && item.owner.scopeId === layer.scopeId);
  const canEdit = edit && inLayer && !layer.locked && !row.locked && itemMine && !!draft;
  const d = draft;
  const sub = { kind: lm.kind, initial: lm.initial, text: `${KIND[row.kind].one} · ${layer.title}` };
  const others = ctx.layers.filter((l) => l !== layer && l.items.some((r) => r.itemId === item.id));
  const usage = others.length
    ? `Этот же пункт есть ещё у ${others.map((l) => metaOf(l).gen).join(', ')} — правка поменяет и там`
    : '';
  const set = (p: Partial<Fields>) => {
    t.reset();
    if (d) setDraft({ ...d, f: { ...d.f, ...p } });
  };
  const remove = () => {
    void ctx.actions
      .remove({ layer: layer.layer, scopeId: layer.scopeId }, row)
      .then((ok) => ok && ctx.close());
  };

  if (canEdit && d && start) {
    const ok = valid(d);
    const dirty = JSON.stringify(d.f) !== JSON.stringify(start.f) || pinned !== row.pinned;
    return (
      <>
        <Title text={nameOf(d)} sub={sub} />
        <ItemForm
          ctx={ctx}
          d={d}
          set={set}
          pinned={pinned}
          setPinned={setPinned}
          t={t}
          itemId={item.id}
          usage={usage}
        />
        <Foot>
          <button
            type="button"
            className="ev-remove"
            onClick={remove}
            disabled={ctx.actions.busy}
            aria-label={`Убрать из ${lm.gen}`}
          >
            Убрать<span className="ev-tl">{` из ${lm.gen}`}</span>
          </button>
          <span className="ev-foot__sp" />
          {d.k === 'script' && (
            <Btn
              variant="ghost"
              size="sm"
              icon="shield-check"
              disabled={!ok || t.asking || (t.active && t.run?.status === 'running')}
              onClick={() => void t.start(ctx.target, d, row.entryId)}
            >
              Проверить
            </Btn>
          )}
          <Btn
            variant="primary"
            size="sm"
            busy={ctx.actions.busy}
            disabled={!dirty || !ok}
            onClick={() =>
              void ctx.actions.save(row, item, d, pinned, ctx.repos).then((saved) => {
                if (saved) {
                  ctx.flash([row.entryId]);
                  ctx.close();
                }
              })
            }
          >
            Сохранить
          </Btn>
        </Foot>
      </>
    );
  }

  const lines = d ? viewOf(d, ctx.repos).lines : viewLinesOf(item, ctx.repos);
  const own = inLayer && !layer.locked;
  let note = `Общее для ${lm.kind === 'org' ? 'всей организации' : lm.gen}: здесь не меняется. Поменять может админ`;
  if (ctx.role === 'admin')
    note = `Это пункт ${lm.gen}. Чтобы поменять, выберите «${lm.pick}» в «Настраиваете»`;
  if (own) note = 'Этот пункт создан выше по уровню: поменять его может админ организации';
  return (
    <>
      <Title text={d ? nameOf(d) : item.name} sub={sub} />
      <dl className="ev-dl">
        {[
          ['Вид', KIND[row.kind].one],
          ...lines,
          ['Для людей', row.pinned ? 'Обязательно — ставится всем' : 'По желанию — человек может пропустить'],
        ].map(([k, v]) => (
          <div key={k}>
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
      <p className="ev-sh__note">
        <Icon i="lock" size="sm" />
        <span>{note}</span>
      </p>
      {own && !row.locked && (
        <Foot>
          <button type="button" className="ev-remove" onClick={remove} disabled={ctx.actions.busy}>
            {`Убрать из ${lm.gen}`}
          </button>
          <span className="ev-foot__sp" />
        </Foot>
      )}
    </>
  );
};
