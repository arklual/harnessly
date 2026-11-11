// The machines of the whole organization for an admin: the tenants with trouble first, a filter of tenants, a search,
// and the next part loads when the end of the list comes into view.
import { useMemo, useRef, useState } from 'react';

import { useNow } from '../../../lib/hooks';
import { navigate } from '../../../lib/router';
import type { MachinesList } from '../../../org/api/machines';
import { useOrg } from '../../../org/context';
import { seatsOn } from '../../flag';
import { Btn, Icon, usePop } from '../../ui';
import {
  filterLabel,
  filterOptions,
  filterTenants,
  flatRows,
  foldGroups,
  groupLimit,
  groupSt,
  num,
  orgTiles,
  rowLimit,
  searchGroups,
  type TenantFilter,
  toggleTenant,
} from './calc';
import {
  AddCta,
  Group,
  MachineRows,
  MlLink,
  SearchBox,
  Seg,
  TableHead,
  Tiles,
  useMachineSort,
} from './parts';
import { useSentinel } from './use-sentinel';

type View = 'tenant' | 'flat';

const VIEWS = [
  { id: 'tenant', label: 'По тенантам' },
  { id: 'flat', label: 'Списком' },
] as const;

const MODES = [
  { id: 'all', label: 'Все тенанты', icon: 'layers' },
  { id: 'problems', label: 'Только с проблемами', icon: 'triangle-alert' },
  { id: 'pinned', label: 'Закреплённые', icon: 'pin' },
] as const;

const NO_FILTER: TenantFilter = { mode: 'all', pick: [] };

const TenantPicker = ({
  list,
  filter,
  onChange,
}: {
  list: MachinesList;
  filter: TenantFilter;
  onChange: (f: TenantFilter) => void;
}) => {
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const [q, setQ] = useState('');
  const options = useMemo(() => filterOptions(list.groups, q), [list.groups, q]);
  return (
    <div className="mo-tf" ref={root}>
      <button type="button" className="mo-filter" aria-expanded={pop.open} onClick={pop.toggle}>
        <Icon i="layers" size="sm" />
        <span>{filterLabel(filter)}</span>
        <Icon i="chevron-down" size="sm" />
      </button>
      {pop.mounted && (
        <div className="hb-pop mo-tf__pop" data-closing={pop.attr} role="dialog" aria-label="Тенанты">
          <label className="hb-pop__search">
            <Icon i="search" size="sm" />
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Найти тенант"
              aria-label="Найти тенант"
            />
          </label>
          {MODES.map((o) => (
            <button
              key={o.id}
              type="button"
              className="hb-pop__item"
              role="menuitemradio"
              aria-checked={filter.mode === o.id}
              onClick={() => onChange({ mode: o.id, pick: [] })}
            >
              <Icon i={o.icon} size="sm" />
              <span>{o.label}</span>
              {filter.mode === o.id && <Icon i="check" size="sm" />}
            </button>
          ))}
          <div className="hb-pop__sep" />
          <div className="hb-pop__label">Выбрать тенанты</div>
          <div className="mo-tf__list">
            {options.map((g) => {
              const on = filter.pick.includes(g.key);
              return (
                <button
                  key={g.key}
                  type="button"
                  className="hb-pop__item mo-tf__opt"
                  role="menuitemcheckbox"
                  aria-checked={on}
                  onClick={() => onChange(toggleTenant(filter, g.key))}
                >
                  <span className="mo-check" data-on={on}>
                    <Icon i="check" size="sm" />
                  </span>
                  <span className="mo-tf__name">{g.name}</span>
                  <span className="mo-dot" data-st={groupSt(g)} />
                </button>
              );
            })}
          </div>
          <div className="mo-tf__foot">
            <Btn variant="ghost" size="sm" onClick={() => onChange(NO_FILTER)}>
              Сбросить
            </Btn>
            <Btn variant="primary" size="sm" onClick={pop.close}>
              Готово
            </Btn>
          </div>
        </div>
      )}
    </div>
  );
};

const Sentinel = ({ rows, onHit, more }: { rows?: boolean; onHit: () => void; more: number }) => {
  const ref = useSentinel(true, onHit, more);
  return (
    <div ref={ref} className={rows ? 'mo-sentinel mo-sentinel--rows' : 'mo-sentinel'} aria-hidden="true">
      <span />
      <span />
      <span />
    </div>
  );
};

export const OrgView = ({
  list,
  onOpen,
  onAdd,
}: {
  list: MachinesList;
  onOpen: (id: string) => void;
  /** Opens «Добавить машину»; absent for a role that cannot add. */
  onAdd?: () => void;
}) => {
  const { shell } = useOrg();
  const seats = seatsOn(shell.features);
  const now = useNow(60_000);
  const [q, setQ] = useState('');
  const [view, setView] = useState<View>('tenant');
  const [filter, setFilter] = useState<TenantFilter>(NO_FILTER);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [more, setMore] = useState(0);
  const tiles = useMemo(
    () => orgTiles(list.summary, seats ? foldGroups(list.groups) : list.groups),
    [list, seats],
  );
  const inFilter = useMemo(() => filterTenants(list.groups, filter), [list.groups, filter]);
  const shown = useMemo(() => searchGroups(inFilter, q), [inFilter, q]);
  const ms = useMachineSort();
  const rows = ms.sort(useMemo(() => flatRows(inFilter, q), [inFilter, q]));
  const searching = q.trim() !== '';
  const first = list.groups[0]?.key;
  const gLimit = groupLimit(more);
  const rLimit = rowLimit(more);
  const empty =
    (searching || filter.mode !== 'all') && (view === 'tenant' ? shown.length === 0 : rows.length === 0);
  const next = () => setMore((n) => n + 1);
  const setQuery = (v: string) => {
    setQ(v);
    setMore(0);
  };
  const setFilterAnd = (f: TenantFilter) => {
    setFilter(f);
    setMore(0);
  };
  const setViewAnd = (v: View) => {
    setView(v);
    setMore(0);
  };

  return (
    <div className="mo">
      <Tiles tiles={tiles} />
      <div className="mo-bar">
        <SearchBox value={q} onChange={setQuery} placeholder="Найти машину, тенант или человека" />
        <TenantPicker list={list} filter={filter} onChange={setFilterAnd} />
        <span className="hx-spacer" />
        <MlLink url={list.mlCoreUrl} text="графики и логи всех машин" />
        <Seg options={VIEWS} value={view} onPick={setViewAnd} label="Вид списка" />
      </div>
      {empty && (
        <div className="mo-empty">
          {searching ? `Ничего не нашлось по «${q}»` : 'В выбранных тенантах нет машин'}
        </div>
      )}
      {view === 'tenant' && shown.length > 0 && (
        <div className="mo-list">
          {shown.slice(0, gLimit).map(({ group, rows: groupRows }, i) => {
            const isOpen = searching ? true : (open[group.key] ?? group.key === first);
            return (
              <Group
                key={group.key}
                group={group}
                sub={group.sub}
                index={i % 10}
                open={isOpen}
                word="машин"
                onToggle={() => setOpen((o) => ({ ...o, [group.key]: !isOpen }))}
              >
                <div className="mt mt--in">
                  <TableHead />
                  <MachineRows machines={groupRows} mode="group" now={now} onOpen={onOpen} seats={seats} />
                </div>
                <button
                  type="button"
                  className="mo-more"
                  data-show={group.total > groupRows.length && group.tenantSlug != null}
                  onClick={() =>
                    group.tenantSlug && navigate(`/o/${shell.org.slug}/${group.tenantSlug}/machines`)
                  }
                >
                  Все {num(group.total)} машин тенанта
                  <Icon i="arrow-right" size="sm" />
                </button>
              </Group>
            );
          })}
          {shown.length > gLimit && <Sentinel onHit={next} more={more} />}
        </div>
      )}
      {view === 'flat' && rows.length > 0 && (
        <div className="mt">
          <TableHead sorting={ms} />
          <MachineRows
            machines={rows.slice(0, rLimit)}
            mode="flat-org"
            now={now}
            onOpen={onOpen}
            seats={seats}
          />
          {rows.length > rLimit && <Sentinel rows onHit={next} more={more} />}
        </div>
      )}
      {!searching && filter.mode === 'all' && list.groups.length === 0 && (
        <div className="mo-empty">
          В организации пока нет машин
          <AddCta onAdd={onAdd} />
        </div>
      )}
    </div>
  );
};
