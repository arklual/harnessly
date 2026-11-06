// «Скопировать из другой команды»: pick a team, then take all it has or only what is needed. An item of the other team is
// copied into this one (an item belongs to its layer), the organization's items are put in as they are, and the
// repository a copied script needs comes along.
import { useState } from 'react';

import type { EnvLayer } from '../../../../hub-api/env';
import { Btn, Icon } from '../../ui';
import { Search } from './bits';
import type { SheetCtx } from './ctx';
import { plural } from './format';
import { KIND } from './model';
import { metaOf, same } from './scope';
import { Foot, Slot, Title } from './sheet';
import { ScopePicker } from './picker';

const MANY = 6;

const peek = (l: EnvLayer): string => {
  const names = l.items.filter((r) => r.enabled).map((r) => r.name);
  if (names.length === 0) return 'Пока пусто';
  return names.length > 3 ? `${names.slice(0, 3).join(', ')} и ещё ${names.length - 3}` : names.join(', ');
};

export const CopyBody = ({ ctx }: { ctx: SheetCtx }) => {
  const [srcKey, setSrcKey] = useState<string | null>(null);
  const [off, setOff] = useState<ReadonlySet<string>>(new Set());
  const [q, setQ] = useState('');
  const sources = ctx.layers.filter((l) => l.layer === 'team' && !same(l, ctx.target));
  const src = sources.find((l) => `${l.layer}:${l.scopeId}` === srcKey) ?? null;
  const to = (
    <Slot name="to">
      <div className="ev-to">
        <span>Куда</span>
        <ScopePicker
          view={ctx.view}
          role={ctx.role}
          target={ctx.target}
          onPick={ctx.setTarget}
          variant="sheet"
          label="Куда добавить"
        />
      </div>
    </Slot>
  );

  if (!src) {
    const needle = q.trim().toLowerCase();
    const hits = sources.filter((l) => needle === '' || metaOf(l).short.toLowerCase().includes(needle));
    const many = sources.length > MANY;
    const shown = many && needle === '' ? hits.slice(0, MANY - 1) : hits;
    const rest = hits.length - shown.length;
    return (
      <>
        <Title text="Скопировать из другой команды" onBack={() => ctx.go({ mode: 'add', step: 'home' })} />
        {to}
        <p className="ev-sh__lead">{`Выберите команду — покажем, что у неё настроено. Возьмёте всё или только нужное в ${ctx.meta.acc}`}</p>
        {many && <Search value={q} onChange={setQ} placeholder="Найти команду" label="Найти команду" />}
        <div className="ev-pick">
          {shown.map((l) => {
            const m = metaOf(l);
            return (
              <button
                type="button"
                className="ev-pick__i"
                key={l.scopeId}
                disabled={l.items.every((r) => !r.enabled)}
                onClick={() => {
                  setSrcKey(`${l.layer}:${l.scopeId}`);
                  setOff(new Set());
                }}
              >
                <span className="ad-dot" data-l="team">
                  {m.initial}
                </span>
                <span className="ad-row__t">
                  <b>{m.short}</b>
                  <small>{peek(l)}</small>
                </span>
                <Icon i="chevron-right" size="sm" />
              </button>
            );
          })}
        </div>
        {rest > 0 && (
          <div className="ev-pop-more">{`и ещё ${rest} ${plural(rest, 'команда', 'команды', 'команд')} — начните вводить название`}</div>
        )}
        {hits.length === 0 && <div className="ev-pop-more">Ничего не нашлось</div>}
      </>
    );
  }

  const have = new Set(ctx.chain.flatMap((l) => l.items.map((r) => r.itemId)));
  const rows = src.items
    .filter((r) => r.enabled)
    .map((r) => ({
      r,
      has: have.has(r.itemId),
      sel: !have.has(r.itemId) && !off.has(r.itemId),
      lock: have.has(r.itemId),
      hint: '',
    }));
  for (const x of rows) {
    x.hint = x.has
      ? 'Уже есть в окружении'
      : `${KIND[x.r.kind].one} · ${x.r.pinned ? 'обязательно' : 'по желанию'}`;
  }
  for (const x of rows) {
    if (!x.sel || x.r.kind !== 'script') continue;
    const need = ctx.catalog?.find((c) => c.id === x.r.itemId)?.source.repo;
    const repo = rows.find((o) => o.r.itemId === need && !o.has);
    if (!repo) continue;
    repo.sel = true;
    repo.lock = true;
    repo.hint = `Нужен скрипту «${x.r.name}» — скопируем вместе`;
  }
  const free = rows.filter((x) => !x.lock);
  const allOn = free.length > 0 && free.every((x) => x.sel);
  const picked = rows.filter((x) => x.sel);
  const srcMeta = metaOf(src);
  return (
    <>
      <Title text={`Из ${srcMeta.gen}`} onBack={() => setSrcKey(null)} />
      {to}
      <div className="ev-cp__h" data-top="true">
        <span>Что скопировать</span>
        {free.length > 1 && (
          <button
            type="button"
            className="ev-cp__all"
            onClick={() => setOff(allOn ? new Set(free.map((x) => x.r.itemId)) : new Set())}
          >
            {allOn ? 'Снять всё' : 'Выбрать всё'}
          </button>
        )}
      </div>
      {rows.map((x) => (
        <button
          type="button"
          className="ev-check"
          key={x.r.entryId}
          aria-pressed={x.sel || x.has ? 'true' : 'false'}
          data-has={x.has ? 'true' : undefined}
          disabled={x.lock}
          onClick={() =>
            setOff(new Set(x.sel ? [...off, x.r.itemId] : [...off].filter((i) => i !== x.r.itemId)))
          }
        >
          <span className="ev-check__box">
            <Icon i="check" />
          </span>
          <span className="ad-ic">
            <Icon i={KIND[x.r.kind].icon} size="sm" />
          </span>
          <span className="ad-row__t">
            <b>{x.r.name}</b>
            <small>{x.hint}</small>
          </span>
        </button>
      ))}
      <p className="ev-sh__note">
        <Icon i="info" size="sm" />
        <span>
          Перенесём вместе с «обязательно» и «по желанию». Свои пункты команды копируются: правка у одной
          команды другую не тронет. Пункты организации остаются общими
        </span>
      </p>
      <Foot>
        <span className="ev-foot__sp" />
        <Btn
          variant="primary"
          size="sm"
          busy={ctx.actions.busy}
          disabled={picked.length === 0}
          onClick={() =>
            void ctx.actions
              .copy(
                src,
                ctx.target,
                picked.map((x) => x.r.itemId),
                ctx.catalog ?? [],
              )
              .then((ok) => ok && ctx.close())
          }
        >
          {`Скопировать${picked.length ? ` ${picked.length}` : ''}`}
        </Btn>
      </Foot>
    </>
  );
};
