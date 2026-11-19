// The machines of the whole organization for an admin: the tenants with trouble first, a filter of tenants, a search,
// and the next part loads when the end of the list comes into view.
import { useMemo, useState } from 'react';

import { useNow } from '../../../lib/hooks';
import { navigate } from '../../../lib/router';
import type { MachinesList } from '../../../org/api/machines';
import { useOrg } from '../../../org/context';
import { seatsOn } from '../../flag';
import { Icon } from '../../ui';
import {
  filterByStep,
  flatRows,
  groupLimit,
  machinesTiles,
  num,
  rowLimit,
  searchGroups,
  type StepFilter,
  stepCounts,
} from './calc';
import { MachineAttention } from './attention';
import {
  AddCta,
  Group,
  MachineRows,
  MlLink,
  SearchBox,
  Seg,
  StepChips,
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
  const [step, setStep] = useState<StepFilter>('all');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [more, setMore] = useState(0);
  const uid = shell.viewer.id;
  const counts = useMemo(() => stepCounts(list.groups, uid, list.summary.total), [list, uid]);
  const tiles = useMemo(
    () =>
      machinesTiles(list.summary, counts.step).map((t, i) =>
        i === 0
          ? { ...t, onPick: () => pickStep('ready') }
          : i === 3
            ? { ...t, onPick: () => pickStep('step') }
            : t,
      ),
    [list, counts.step],
  );
  const inFilter = useMemo(() => filterByStep(list.groups, step, uid), [list.groups, step, uid]);
  const shown = useMemo(() => searchGroups(inFilter, q), [inFilter, q]);
  const ms = useMachineSort();
  const rows = ms.sort(useMemo(() => flatRows(inFilter, q), [inFilter, q]));
  const searching = q.trim() !== '';
  const first = list.groups[0]?.key;
  const gLimit = groupLimit(more);
  const rLimit = rowLimit(more);
  const empty = (searching || step !== 'all') && (view === 'tenant' ? shown.length === 0 : rows.length === 0);
  const next = () => setMore((n) => n + 1);
  const setQuery = (v: string) => {
    setQ(v);
    setMore(0);
  };
  function pickStep(f: StepFilter) {
    setStep(f);
    setMore(0);
  }
  const setViewAnd = (v: View) => {
    setView(v);
    setMore(0);
  };

  return (
    <div className="mo">
      <Tiles tiles={tiles} />
      <MachineAttention groups={list.groups} onOpen={onOpen} />
      <div className="mo-bar">
        <SearchBox value={q} onChange={setQuery} placeholder="Найти машину, тенант или человека" />
        <StepChips value={step} counts={counts} onPick={pickStep} />
        <span className="hx-spacer" />
        <MlLink url={list.mlCoreUrl} text="графики и логи всех машин" />
        <Seg options={VIEWS} value={view} onPick={setViewAnd} label="Вид списка" />
      </div>
      {empty && (
        <div className="hd-empty">
          <span className="hd-empty__ic">
            <Icon i="search" />
          </span>
          <b>Нет машин</b>
          <span>{searching ? `Ничего не нашлось по «${q}».` : 'С таким фильтром ничего не нашлось.'}</span>
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
      {!searching && step === 'all' && list.groups.length === 0 && (
        <div className="mo-empty">
          В организации пока нет машин
          <AddCta onAdd={onAdd} />
        </div>
      )}
    </div>
  );
};
