// The pieces both lists share: the tiles, the search, the link to ML Core, the row of a machine and the group head.
import { type CSSProperties, Fragment, type ReactNode, useState } from 'react';

import type { MachineGroup, OrgMachine } from '../../../org/api/machines';
import { Bars } from '../../charts/bars';
import { Meter } from '../../charts/meter';
import { Strip } from '../../charts/strip';
import { Count, useLive } from '../../live';
import { type SortKey, useListView } from '../../sort-bar';
import { sortRows } from '../../sort';
import { Btn, Icon, initial } from '../../ui';
import { useRowOpen } from './open';
import {
  canOpen,
  chatsText,
  groupWeek,
  liveLoad,
  machinesWord,
  num,
  rowSt,
  rowStLabel,
  rowSub,
  type RowMode,
  type ServerRow,
  serverRows,
  type Tile,
  verText,
  weekText,
} from './calc';

export const Tiles = ({ tiles }: { tiles: readonly Tile[] }) => (
  <div className="dash hb-dash mo-tiles">
    <div className="sx-wrap">
      <div className="sx-grid">
        {tiles.map((t, i) => (
          <div
            key={t.label}
            className="sx-tile mo-tile"
            style={{ '--sx-i': i, '--sx-tone': t.tone } as CSSProperties}
          >
            <span className="sx-tile__top">
              <span className="sx-tile__icon">
                <Icon i={t.icon} size="sm" />
              </span>
            </span>
            <span className="sx-tile__chart">
              {t.strip ? <Strip parts={t.strip} /> : <Bars values={t.bars ?? []} />}
            </span>
            <span className="sx-tile__label">{t.label}</span>
            <span className="sx-tile__value">
              <Count text={t.value} />
            </span>
          </div>
        ))}
      </div>
    </div>
  </div>
);

/** The skeleton of the whole screen: tiles, the bar and a list, in the boxes of the board. */
export const ListSkeleton = () => (
  <div className="mo" role="status" aria-label="Загрузка">
    <div className="mx-skel-tiles">
      {[0, 1, 2, 3].map((i) => (
        <span key={i} className="mx-skel" style={{ animationDelay: `${i * 0.1}s` }} />
      ))}
    </div>
    <div className="mx-skel-bar">
      <span className="mx-skel" />
      <span className="mx-skel" />
    </div>
    <div className="mx-skel-list">
      {[0, 1, 2, 3, 4].map((i) => (
        <span key={i} className="mx-skel" />
      ))}
    </div>
  </div>
);

/** A failed load: one row with the retry. */
export const ErrorRow = ({ text, onRetry }: { text: string; onRetry: () => void }) => (
  <div className="mx-err" role="alert">
    <span>{text}</span>
    <Btn variant="outline" size="sm" onClick={onRetry}>
      Повторить
    </Btn>
  </div>
);

/** The call to action of an empty list: the button for who may add, a hint for who may not. */
export const AddCta = ({ onAdd, lead }: { onAdd?: () => void; lead?: boolean }) =>
  onAdd ? (
    <div>
      <Btn variant="primary" size="sm" icon="plus" onClick={onAdd}>
        Добавить машину
      </Btn>
    </div>
  ) : lead ? (
    <small className="md-note">Машину добавляет админ тенанта: попросите его выдать код установки.</small>
  ) : null;

export const SearchBox = ({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
}) => (
  <label className="mo-search">
    <Icon i="search" size="sm" />
    <input
      type="search"
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onChange('');
      }}
      aria-label={placeholder}
    />
    {value !== '' && (
      <button type="button" className="mo-search__x" onClick={() => onChange('')} aria-label="Очистить">
        <Icon i="x" size="sm" />
      </button>
    )}
  </label>
);

/** «Подробнее в ML Core»: shown when the hub knows where the charts and logs of the machines are. */
export const MlLink = ({ url, text }: { url: string | null; text: string }) =>
  url ? (
    <a className="mo-ext" href={url} target="_blank" rel="noopener noreferrer">
      <span className="md-ext__ic">
        <Icon i="activity" size="sm" />
      </span>
      <span className="mo-ext__t">
        <b>Подробнее в ML Core</b>
        <small>{text}</small>
      </span>
      <Icon i="arrow-up-right" size="sm" className="md-ext__go" />
    </a>
  ) : null;

export interface SegOption<T extends string> {
  id: T;
  label: string;
}

export const Seg = <T extends string>({
  options,
  value,
  onPick,
  label,
}: {
  options: readonly SegOption<T>[];
  value: T;
  onPick: (id: T) => void;
  label: string;
}) => (
  <div className="hx-set__seg" role="radiogroup" aria-label={label}>
    {options.map((o) => (
      <button
        key={o.id}
        type="button"
        className="hx-set__seg-item"
        role="radio"
        aria-checked={value === o.id}
        onClick={() => onPick(o.id)}
      >
        {o.label}
      </button>
    ))}
  </div>
);

const MACHINE_SORTS: SortKey[] = [
  { id: 'name', label: 'Машина', first: 'asc' },
  { id: 'chats', label: 'Чаты сейчас' },
  { id: 'cpu', label: 'Процессор' },
  { id: 'ram', label: 'Память' },
  { id: 'disk', label: 'Диск' },
  { id: 'week', label: 'За неделю' },
];

/** The sort of the flat list of machines (live hub only): the address keeps it, no key keeps the usual order. */
export const useMachineSort = () => {
  const live = useLive();
  const view = useListView(MACHINE_SORTS, { key: null, dir: 'desc' });
  const sort = (rows: OrgMachine[]): OrgMachine[] =>
    live && view.sort.key
      ? sortRows(rows, view.sort, {
          name: (m) => m.name,
          chats: (m) => m.chats ?? -1,
          cpu: (m) => m.cpu ?? -1,
          ram: (m) => m.ram ?? -1,
          disk: (m) => m.disk ?? -1,
          week: (m) => m.weekCostUsd ?? -1,
        })
      : rows;
  return { view, sort };
};

const HEAD: [string | null, string][] = [
  ['name', 'Машина'],
  [null, 'Состояние'],
  ['chats', 'Чаты сейчас'],
  ['cpu', 'Процессор'],
  ['ram', 'Память'],
  ['disk', 'Диск'],
  [null, 'Раннер'],
  ['week', 'За неделю'],
];

export const TableHead = ({ sorting }: { sorting?: ReturnType<typeof useMachineSort> }) => {
  const live = useLive();
  const view = live ? sorting?.view : undefined;
  return (
    <div className="mt-row mt-head">
      {HEAD.map(([id, label]) =>
        view && id ? (
          <span key={label}>
            <button
              type="button"
              className="live-sort"
              aria-sort={
                view.sort.key === id ? (view.sort.dir === 'asc' ? 'ascending' : 'descending') : undefined
              }
              onClick={() => view.pick(id)}
            >
              {label}
              <i aria-hidden="true">↓</i>
            </button>
          </span>
        ) : (
          <span key={label}>{label}</span>
        ),
      )}
    </div>
  );
};

export const MachineRow = ({
  m,
  mode,
  now,
  onOpen,
  seat,
}: {
  m: OrgMachine;
  mode: RowMode;
  now: number;
  onOpen: (id: string) => void;
  /** A person's seat under its server: the server's load is told once, in the server's row. */
  seat?: boolean;
}) => {
  const st = rowSt(m);
  const off = st === 'off';
  const ws = useRowOpen();
  const shortcut = ws && m.canManage && canOpen(m);
  return (
    <div
      role="button"
      tabIndex={0}
      className="mt-row"
      data-st={st}
      data-seat={seat ? '1' : undefined}
      onClick={() => onOpen(m.id)}
      onKeyDown={(e) => {
        if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault();
          onOpen(m.id);
        }
      }}
    >
      <span className="mt-name">
        <span className="mo-dot" data-st={st} />
        <span className="mt-name__t">
          <b>{m.name}</b>
          <small>{rowSub(m, mode)}</small>
        </span>
      </span>
      <span className="mt-st" data-st={st}>
        {rowStLabel(m, now)}
      </span>
      <span className="mt-num">{chatsText(m)}</span>
      {seat ? (
        <>
          <span />
          <span />
          <span />
        </>
      ) : (
        <>
          <Meter pct={liveLoad(m, m.cpu)} off={off} />
          <Meter pct={liveLoad(m, m.ram)} off={off} />
          <Meter pct={m.disk} off={off} />
        </>
      )}
      <span className="mt-ver" data-old={m.runnerOutdated ? '1' : '0'}>
        {verText(m)}
      </span>
      <span className="mt-week">{weekText(m)}</span>
      {shortcut && (
        <button
          type="button"
          className="mt-open"
          disabled={ws.busyId === m.id}
          aria-label={`Открыть рабочее пространство ${m.name}`}
          onClick={(e) => {
            e.stopPropagation();
            void ws.open(m.id);
          }}
        >
          <Icon i="external-link" size="sm" />
          Открыть
        </button>
      )}
    </div>
  );
};

/** A server several people work on: one row with its load, the people's seats unfold under it. */
const ServerLine = ({
  s,
  now,
  open,
  onToggle,
}: {
  s: ServerRow;
  now: number;
  open: boolean;
  onToggle: () => void;
}) => {
  const m = s.server;
  const st = rowSt(m);
  const off = st === 'off';
  return (
    <div
      role="button"
      tabIndex={0}
      className="mt-row"
      data-st={st}
      data-server="1"
      aria-expanded={open}
      onClick={onToggle}
      onKeyDown={(e) => {
        if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault();
          onToggle();
        }
      }}
    >
      <span className="mt-name">
        <span className="mo-dot" data-st={st} />
        <span className="mt-name__t">
          <b>{m.name}</b>
          <small>{m.purpose}</small>
        </span>
        <Icon i="chevron-right" size="sm" className="mt-chev" />
      </span>
      <span className="mt-st" data-st={st}>
        {rowStLabel(m, now)}
      </span>
      <span className="mt-num">{chatsText(m)}</span>
      <Meter pct={liveLoad(m, m.cpu)} off={off} />
      <Meter pct={liveLoad(m, m.ram)} off={off} />
      <Meter pct={m.disk} off={off} />
      <span className="mt-ver" data-old={m.runnerOutdated ? '1' : '0'}>
        {verText(m)}
      </span>
      <span className="mt-week">{weekText(m)}</span>
    </div>
  );
};

/** The rows of a table. With `seats` the machines of one server fold into the server's row (machine.seats). */
export const MachineRows = ({
  machines,
  mode,
  now,
  onOpen,
  seats,
}: {
  machines: readonly OrgMachine[];
  mode: RowMode;
  now: number;
  onOpen: (id: string) => void;
  seats: boolean;
}) => {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  if (!seats) {
    return (
      <>
        {machines.map((m) => (
          <MachineRow key={m.id} m={m} mode={mode} now={now} onOpen={onOpen} />
        ))}
      </>
    );
  }
  return (
    <>
      {serverRows(machines).map((r) =>
        r.kind === 'machine' ? (
          <MachineRow key={r.m.id} m={r.m} mode={mode} now={now} onOpen={onOpen} />
        ) : (
          <Fragment key={r.s.key}>
            <ServerLine
              s={r.s}
              now={now}
              open={!!open[r.s.key]}
              onToggle={() => setOpen((o) => ({ ...o, [r.s.key]: !o[r.s.key] }))}
            />
            {open[r.s.key] &&
              r.s.seats.map((m) => (
                <MachineRow key={m.id} m={m} mode={mode} now={now} onOpen={onOpen} seat />
              ))}
          </Fragment>
        ),
      )}
    </>
  );
};

const share = (n: number, total: number): string => `${total > 0 ? ((n / total) * 100).toFixed(1) : '0.0'}%`;

/** One group of a list: the head with the bar and the numbers, and the table that unfolds under it. */
export const Group = ({
  group,
  sub,
  open,
  index,
  onToggle,
  word,
  children,
}: {
  group: MachineGroup;
  sub: string;
  open: boolean;
  index: number;
  onToggle: () => void;
  word?: string;
  children: ReactNode;
}) => {
  const { online, idle, offline, total } = group;
  const spend = groupWeek(group);
  return (
    <div className="mo-g" aria-expanded={open} style={{ '--i': index } as CSSProperties}>
      <button type="button" className="mo-g__head" onClick={onToggle}>
        <Icon i="chevron-right" size="sm" className="mo-g__chev" />
        <span className="hb-dot">{initial(group.name)}</span>
        <span className="mo-g__t">
          <b>{group.name}</b>
          <small>{sub}</small>
        </span>
        <span className="mo-g__flag" data-show={offline > 0}>
          {offline > 0 ? `${offline} не на связи` : ''}
        </span>
        <span
          className="mo-g__bar"
          title={`${online} на связи · ${idle} простаивает · ${offline} не на связи`}
        >
          <i data-st="on" style={{ '--w': share(online, total) } as CSSProperties} />
          <i data-st="idle" style={{ '--w': share(idle, total) } as CSSProperties} />
          <i data-st="off" style={{ '--w': share(offline, total) } as CSSProperties} />
        </span>
        <span className="mo-g__n">
          {num(total)} <small>{word ?? machinesWord(total, false)}</small>
        </span>
        <span className="mo-g__s">
          {spend ?? '—'}
          <small> за неделю</small>
        </span>
      </button>
      <div className="mo-g__body">
        <div>{children}</div>
      </div>
    </div>
  );
};
