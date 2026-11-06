// The layers of the board: a card per layer (organization, tenant, team), each opening onto its items grouped by kind.
// The layer being set up is editable: a switch «обязательно», the pencil, «Добавить», and the scripts' dots to pull.
import { type KeyboardEvent, useState } from 'react';

import type { EnvEntryRow, EnvLayer } from '../../../../hub-api/env';
import { Icon } from '../../ui';
import { Help } from './bits';
import { type DragLock, keyMove, startDrag } from './drag';
import { KIND } from './model';
import { type Block, blocksOf, type LayerMeta } from './scope';

const onKey = (fn: () => void) => (e: KeyboardEvent) => {
  if (e.target !== e.currentTarget) return;
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    fn();
  }
};

export interface LayerVM {
  layer: EnvLayer;
  meta: LayerMeta;
  index: number;
  open: boolean;
  /** This is the layer being set up and the person may change it. */
  edit: boolean;
  /** The person's own team of a member: the line says editors change it. */
  sub: string;
}

export interface LayerActions {
  onToggle: () => void;
  onAdd: () => void;
  /** null: the row opens nothing (a member's). */
  onOpen: ((row: EnvEntryRow) => void) | null;
  onPin: (row: EnvEntryRow) => void;
  /** Asks the hub for a new order of the layer's scripts; true: kept. */
  onReorder: (entryIds: string[]) => Promise<boolean>;
}

const SCRIPT_TIP = (edit: boolean): string =>
  `Запускаются сверху вниз${edit ? ' — тяните за точки слева, чтобы поменять порядок' : ''}. Сначала ставим доступы и репозитории, потом скрипты организации, тенанта и команды, потом скиллы и MCP`;

const Row = ({
  r,
  vm,
  a,
  fresh,
  drag,
  scriptIds,
}: {
  r: EnvEntryRow;
  vm: LayerVM;
  a: LayerActions;
  fresh: boolean;
  drag: DragLock;
  scriptIds: string[];
}) => {
  const canEdit = vm.edit && !r.locked;
  const canDrag = canEdit && r.kind === 'script' && scriptIds.length > 1;
  const group = `${vm.layer.layer}:${vm.layer.scopeId}`;
  const open = () => a.onOpen?.(r);
  const opts = { ids: scriptIds, commit: a.onReorder, group };
  return (
    <div
      className="ev-row"
      data-fresh={fresh ? 'true' : undefined}
      data-dg={canDrag ? group : undefined}
      role={a.onOpen ? 'button' : undefined}
      tabIndex={a.onOpen ? 0 : undefined}
      onClick={a.onOpen ? open : undefined}
      onKeyDown={a.onOpen ? onKey(open) : undefined}
      aria-label={a.onOpen ? undefined : r.name}
    >
      {canDrag && (
        <span
          className="ev-grip"
          role="button"
          tabIndex={0}
          aria-label={`Порядок: «${r.name}». Тяните или жмите стрелки`}
          onPointerDown={(e) => startDrag(e, r.entryId, opts, drag)}
          onKeyDown={(e) => void keyMove(e, r.entryId, opts, drag)}
          onClick={(e) => e.stopPropagation()}
        />
      )}
      <span className="ad-ic">
        <Icon i={KIND[r.kind].icon} size="sm" />
      </span>
      <span className="ev-row__t">
        <b>{r.name}</b>
        <small>{r.hint}</small>
      </span>
      {canEdit ? (
        <>
          <span className="ev-req hx-tipped" onClick={(e) => e.stopPropagation()}>
            <span className="ev-req__l">{r.pinned ? 'обязательно' : 'по желанию'}</span>
            <button
              type="button"
              className="hx-switch"
              role="switch"
              aria-checked={r.pinned ? 'true' : 'false'}
              aria-label="Обязательно"
              onClick={() => a.onPin(r)}
            >
              <i />
            </button>
            <span className="hx-tip pz-tip" data-align="end">
              {r.pinned
                ? 'Поставится всем. Выключите — человек сможет пропустить'
                : 'Человек может пропустить. Включите — поставится всем'}
            </span>
          </span>
          <span className="ev-pen-w hx-tipped">
            <button
              type="button"
              className="hx-btn ev-pen"
              data-variant="ghost"
              data-size="sm"
              data-shape="icon"
              aria-label="Изменить"
              onClick={(e) => {
                e.stopPropagation();
                open();
              }}
            >
              <Icon i="pencil" size="sm" />
            </button>
            <span className="hx-tip pz-tip" data-align="end">
              Изменить
            </span>
          </span>
        </>
      ) : (
        <>
          <span className="ev-req__ro">{r.pinned ? 'обязательно' : 'по желанию'}</span>
          {a.onOpen && (
            <span className="ev-go">
              <Icon i="chevron-right" size="sm" />
            </span>
          )}
        </>
      )}
    </div>
  );
};

export const LayerCard = ({
  vm,
  rows,
  q,
  a,
  fresh,
}: {
  vm: LayerVM;
  rows: Block[];
  q: string;
  a: LayerActions;
  fresh: ReadonlySet<string>;
}) => {
  const [drag] = useState<DragLock>(() => ({ busy: false }));
  const { layer, meta, edit } = vm;
  const scriptIds = layer.items.filter((r) => r.enabled && r.kind === 'script').map((r) => r.entryId);
  const empty = rows.length === 0 && q.trim() === '';
  return (
    <section className="ev-layer" aria-expanded={vm.open ? 'true' : 'false'} data-i={vm.index}>
      <div className="ev-layer__hd">
        <button type="button" className="ev-layer__head" onClick={a.onToggle}>
          <Icon i="chevron-right" size="sm" className="ev-chev" />
          <span className="ad-dot" data-l={meta.kind}>
            {meta.initial}
          </span>
          <span className="ev-layer__t">
            <b>{layer.layer === 'org' ? `${meta.short} — вся организация` : layer.title}</b>
            <small>{vm.sub}</small>
          </span>
          {!edit && <Icon i="lock" size="sm" className="ev-lock" />}
        </button>
        {edit && (
          <button
            type="button"
            className="hx-btn ev-layer__add"
            data-size="sm"
            onClick={a.onAdd}
            aria-label="Добавить в окружение"
          >
            <Icon i="plus" size="sm" />
            <span className="ev-tl">Добавить в окружение</span>
          </button>
        )}
      </div>
      <div className="ev-layer__body">
        <div>
          {rows.map((b) => (
            <div key={b.kind}>
              <div className="ev-kindh">
                <span>{KIND[b.kind].many}</span>
                {b.kind === 'script' && <Help text={SCRIPT_TIP(edit && q.trim() === '')} />}
              </div>
              {b.rows.map((r) => (
                <Row
                  key={r.entryId}
                  r={r}
                  vm={vm}
                  a={a}
                  fresh={fresh.has(r.entryId)}
                  drag={drag}
                  scriptIds={q.trim() === '' ? scriptIds : []}
                />
              ))}
            </div>
          ))}
          {empty && (
            <div className="ev-layer__empty">
              {edit ? 'Пока пусто — добавьте то, что нужно именно здесь' : 'Пусто'}
            </div>
          )}
          {edit && q.trim() === '' && (
            <button type="button" className="ev-addrow" onClick={a.onAdd}>
              <Icon i="plus" size="sm" />
              {`Добавить в ${meta.acc}`}
            </button>
          )}
        </div>
      </div>
    </section>
  );
};

export { blocksOf };
