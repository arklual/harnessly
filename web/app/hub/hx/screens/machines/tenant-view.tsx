// A tenant's machines for an admin or a lead: the tiles, the search, and the machines by team or as one list.
import { useMemo, useState } from 'react';

import type { MachinesList } from '../../../org/api/machines';
import { useNow } from '../../../lib/hooks';
import { useOrg } from '../../../org/context';
import { seatsOn } from '../../flag';
import { Icon } from '../../ui';
import {
  filterByStep,
  flatRows,
  foldGroups,
  machinesWord,
  searchGroups,
  type StepFilter,
  stepCounts,
  tenantTiles,
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
  const { shell } = useOrg();
  const seats = seatsOn(shell.features);
  const uid = shell.viewer.id;
  const now = useNow(60_000);
  const [q, setQ] = useState('');
  const [view, setView] = useState<View>('team');
  const [step, setStep] = useState<StepFilter>('all');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const tiles = useMemo(
    () =>
      tenantTiles(list.summary, seats ? foldGroups(list.groups) : list.groups, now).map((t, i) =>
        i === 0
          ? { ...t, onPick: () => setStep('ready') }
          : i === 3
            ? { ...t, onPick: () => setStep('step') }
            : t,
      ),
    [list, now, seats],
  );
  const counts = useMemo(() => stepCounts(list.groups, uid, list.summary.total), [list, uid]);
  const inStep = useMemo(() => filterByStep(list.groups, step, uid), [list.groups, step, uid]);
  const shown = useMemo(() => searchGroups(inStep, q), [inStep, q]);
  const ms = useMachineSort();
  const rows = ms.sort(useMemo(() => flatRows(inStep, q), [inStep, q]));
  const searching = q.trim() !== '';
  const empty = (searching || step !== 'all') && (view === 'team' ? shown.length === 0 : rows.length === 0);

  return (
    <div className="mo">
      <Tiles tiles={tiles} />
      <MachineAttention groups={list.groups} onOpen={onOpen} />
      <div className="mo-bar">
        <SearchBox value={q} onChange={setQ} placeholder="Найти машину или человека" />
        <StepChips value={step} counts={counts} onPick={setStep} />
        <span className="hx-spacer" />
        <MlLink url={list.mlCoreUrl} text="графики и логи машин тенанта" />
        <Seg options={VIEWS} value={view} onPick={setView} label="Вид списка" />
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
