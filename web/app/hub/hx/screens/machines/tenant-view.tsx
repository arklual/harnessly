// A tenant's machines for an admin or a lead: the tiles, the search, and the machines by team or as one list.
import { useMemo, useState } from 'react';

import type { MachinesList, OrgMachine } from '../../../org/api/machines';
import { useNow } from '../../../lib/hooks';
import { flatRows, machinesWord, searchGroups, tenantTiles } from './calc';
import { Group, MachineRow, MlLink, SearchBox, Seg, TableHead, Tiles } from './parts';

type View = 'team' | 'flat';

const VIEWS = [
  { id: 'team', label: 'По командам' },
  { id: 'flat', label: 'Списком' },
] as const;

export const TenantView = ({ list, onOpen }: { list: MachinesList; onOpen: (id: string) => void }) => {
  const now = useNow(60_000);
  const [q, setQ] = useState('');
  const [view, setView] = useState<View>('team');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const tiles = useMemo(() => tenantTiles(list.summary, list.groups, now), [list, now]);
  const shown = useMemo(() => searchGroups(list.groups, q), [list.groups, q]);
  const rows = useMemo(() => flatRows(list.groups, q), [list.groups, q]);
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
                  {groupRows.map((m: OrgMachine) => (
                    <MachineRow key={m.id} m={m} mode="team" now={now} onOpen={onOpen} />
                  ))}
                </div>
              </Group>
            );
          })}
        </div>
      )}
      {view === 'flat' && rows.length > 0 && (
        <div className="mt">
          <TableHead />
          {rows.map((m) => (
            <MachineRow key={m.id} m={m} mode="flat-tenant" now={now} onOpen={onOpen} />
          ))}
        </div>
      )}
      {!searching && list.groups.length === 0 && <div className="mo-empty">В тенанте пока нет машин</div>}
    </div>
  );
};
