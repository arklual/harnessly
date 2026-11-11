// A tenant's machines for an admin or a lead: the tiles, the search, and the machines by team or as one list.
import { useMemo, useState } from 'react';

import type { MachinesList } from '../../../org/api/machines';
import { useNow } from '../../../lib/hooks';
import { useOrg } from '../../../org/context';
import { seatsOn } from '../../flag';
import { flatRows, foldGroups, machinesWord, searchGroups, tenantTiles } from './calc';
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

type View = 'team' | 'flat';

const VIEWS = [
  { id: 'team', label: 'По командам' },
  { id: 'flat', label: 'Списком' },
] as const;

export const TenantView = ({
  list,
  onOpen,
  onAdd,
}: {
  list: MachinesList;
  onOpen: (id: string) => void;
  onAdd?: () => void;
}) => {
  const seats = seatsOn(useOrg().shell.features);
  const now = useNow(60_000);
  const [q, setQ] = useState('');
  const [view, setView] = useState<View>('team');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const tiles = useMemo(
    () => tenantTiles(list.summary, seats ? foldGroups(list.groups) : list.groups, now),
    [list, now, seats],
  );
  const shown = useMemo(() => searchGroups(list.groups, q), [list.groups, q]);
  const ms = useMachineSort();
  const rows = ms.sort(useMemo(() => flatRows(list.groups, q), [list.groups, q]));
  const searching = q.trim() !== '';
  const empty = searching && (view === 'team' ? shown.length === 0 : rows.length === 0);

  return (
    <div className="mo">
      <Tiles tiles={tiles} />
      <div className="mo-bar">
        <SearchBox value={q} onChange={setQ} placeholder="Найти машину или человека" />
        <span className="hx-spacer" />
        <MlLink url={list.mlCoreUrl} text="графики и логи машин тенанта" />
        <Seg options={VIEWS} value={view} onPick={setView} label="Вид списка" />
      </div>
      {empty && <div className="mo-empty">Ничего не нашлось по «{q}»</div>}
      {view === 'team' && shown.length > 0 && (
        <div className="mo-list">
          {shown.map(({ group, rows: groupRows }, i) => {
            const isOpen = searching ? true : (open[group.key] ?? false);
            return (
              <Group
                key={group.key}
                group={group}
                sub={group.sub}
                index={i % 10}
                open={isOpen}
                word={machinesWord(group.total, false)}
                onToggle={() => setOpen((o) => ({ ...o, [group.key]: !isOpen }))}
              >
                <div className="mt mt--in">
                  <TableHead />
                  <MachineRows machines={groupRows} mode="team" now={now} onOpen={onOpen} seats={seats} />
                </div>
              </Group>
            );
          })}
        </div>
      )}
      {view === 'flat' && rows.length > 0 && (
        <div className="mt">
          <TableHead sorting={ms} />
          <MachineRows machines={rows} mode="flat-tenant" now={now} onOpen={onOpen} seats={seats} />
        </div>
      )}
      {!searching && list.groups.length === 0 && (
        <div className="mo-empty">
          В тенанте пока нет машин
          <AddCta onAdd={onAdd} lead />
        </div>
      )}
    </div>
  );
};
