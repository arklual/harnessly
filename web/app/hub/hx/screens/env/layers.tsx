// The layers of the environment as the board draws them: a group per layer (organization, tenant, team), each opening
// onto a table of rows; or one table of every row.
import type { CSSProperties, KeyboardEvent } from 'react';

import { Icon } from '../../ui';
import { type Group, LOCK_TITLE, type RowVM } from './helpers';

type Vars = CSSProperties & Record<`--${string}`, string | number>;

const onKey = (fn: () => void) => (e: KeyboardEvent) => {
  if (e.target !== e.currentTarget) return;
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    fn();
  }
};

export const Row = ({
  r,
  onOpen,
  onToggle,
}: {
  r: RowVM;
  onOpen: (entryId: string) => void;
  onToggle: (entryId: string, on: boolean) => void;
}) => (
  <div
    className="pt-row en-row"
    role="button"
    tabIndex={0}
    data-off={r.off ? '1' : '0'}
    onClick={() => onOpen(r.entryId)}
    onKeyDown={onKey(() => onOpen(r.entryId))}
  >
    <span className="pt-name">
      <span className="ad-ic">
        <Icon i={r.icon} size="sm" />
      </span>
      <span className="mt-name__t">
        <b>{r.name}</b>
        {r.issue ? (
          <small data-warn="1" title={r.why ?? undefined}>
            {r.issue}
          </small>
        ) : (
          <small>{r.sub}</small>
        )}
      </span>
    </span>
    <span className="en-who">{r.who}</span>
    <span className="en-use">
      <span className="en-spark" data-zero={r.zero ? '1' : '0'} aria-hidden="true">
        {r.spark.map((h, i) => (
          <i key={i} style={{ '--h': h } as Vars} />
        ))}
      </span>
      <b>{r.wk}</b>
    </span>
    <span className="pt-last" data-warn={r.stale ? '1' : '0'}>
      {r.last}
    </span>
    <span className="en-ctl">
      {r.locked && r.lockedReason ? (
        <span
          className="en-lock"
          title={LOCK_TITLE[r.lockedReason]}
          role="img"
          aria-label={LOCK_TITLE[r.lockedReason]}
        >
          <Icon i="lock" size="sm" />
        </span>
      ) : (
        <button
          type="button"
          className="hx-switch"
          role="switch"
          aria-checked={r.off ? 'false' : 'true'}
          aria-label={`${r.off ? 'Включить' : 'Выключить'} «${r.name}»`}
          onClick={(e) => {
            e.stopPropagation();
            onToggle(r.entryId, r.off);
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <i />
        </button>
      )}
    </span>
  </div>
);

const Head = () => (
  <div className="pt-row en-row mt-head">
    <span>Название</span>
    <span>Кому</span>
    <span>Вызовов за неделю</span>
    <span>Последний раз</span>
    <span />
  </div>
);

export const GroupCard = ({
  g,
  onToggleOpen,
  onOpen,
  onToggle,
}: {
  g: Group;
  onToggleOpen: (key: string) => void;
  onOpen: (entryId: string) => void;
  onToggle: (entryId: string, on: boolean) => void;
}) => (
  <div className="mo-g tg" aria-expanded={g.open ? 'true' : 'false'} style={{ '--i': g.i } as Vars}>
    <div
      className="mo-g__head en-head"
      role="button"
      tabIndex={0}
      aria-expanded={g.open ? 'true' : 'false'}
      onClick={() => onToggleOpen(g.key)}
      onKeyDown={onKey(() => onToggleOpen(g.key))}
    >
      <Icon i="chevron-right" className="mo-g__chev" />
      <span className="ad-dot tg-dot" style={{ '--c': g.color } as Vars}>
        {g.initial}
      </span>
      <span className="mo-g__t">
        <b>{g.layer.title}</b>
        <small>{g.layer.subtitle}</small>
      </span>
      <span className="en-chips">{g.kinds}</span>
      <span className="mo-g__s">
        {g.calls}
        <small> вызовов</small>
      </span>
    </div>
    <div className="mo-g__body">
      <div>
        <div className="mt mt--in">
          <Head />
          {g.rows.map((r) => (
            <Row key={r.entryId} r={r} onOpen={onOpen} onToggle={onToggle} />
          ))}
        </div>
      </div>
    </div>
  </div>
);

export const FlatList = ({
  rows,
  onOpen,
  onToggle,
}: {
  rows: RowVM[];
  onOpen: (entryId: string) => void;
  onToggle: (entryId: string, on: boolean) => void;
}) => (
  <div className="mt">
    <Head />
    {rows.map((r) => (
      <Row key={r.entryId} r={r} onOpen={onOpen} onToggle={onToggle} />
    ))}
  </div>
);
