// The «Машины» screen of the board «Хаб». Its own chunk: it loads when the person opens the section.
import { useState } from 'react';

import { useOrg } from '../../org/context';
import { AddMachineSheet } from '../../org/screens/machines/add';
import { orgErrorText } from '../../org/api/errors';
import { Btn } from '../ui';
import { Titles } from '../shell';
import { tenantLead } from './machines/calc';
import { MachineDrawer } from './machines/drawer';
import { OpenCtx, useOpenMachine } from './machines/open';
import { MemberSkeleton, MemberView } from './machines/member-view';
import { OrgView } from './machines/org-view';
import { ErrorRow, ListSkeleton } from './machines/parts';
import { TenantView } from './machines/tenant-view';
import { useDrawerUrl } from './machines/url';
import { useMachineList } from './machines/use-machines';

import '../styles/machines.css';

const MEMBER_LEAD = 'Открыть — перейти в рантайм машины';

const Screen = () => {
  const { scope, shell } = useOrg();
  const url = useDrawerUrl();
  const list = useMachineList();
  const [adding, setAdding] = useState(false);
  const ws = useOpenMachine();

  // The card stays mounted while it slides away, and a fresh one is mounted every time a machine is opened.
  const [card, setCard] = useState<{ m: string | null; shown: string | null; n: number }>({
    m: null,
    shown: null,
    n: 0,
  });
  if (card.m !== url.m) setCard({ m: url.m, shown: url.m ?? card.shown, n: url.m ? card.n + 1 : card.n });

  const role = shell.viewer.role;
  const org = scope.kind === 'org';
  const member = role === 'member';
  const data = list.data;
  const all = data ? data.groups.flatMap((g) => g.machines) : [];
  const title = org ? 'Машины организации' : 'Машины';
  const lead = member
    ? MEMBER_LEAD
    : org
      ? `${shell.org.name} · по тенантам, сначала те, где что-то не так`
      : tenantLead(scope.tenant.name, all);
  const addable = org ? shell.tenants : [scope.tenant];
  const canAdd = role === 'admin';
  const hint = card.shown ? (all.find((m) => m.id === card.shown) ?? null) : null;

  const end = canAdd ? (
    <div className="ad-tools">
      <Btn variant="outline" size="sm" icon="plus" onClick={() => setAdding(true)}>
        Добавить машину
      </Btn>
    </div>
  ) : null;

  const content = () => {
    if (list.error && !data) {
      return (
        <ErrorRow text={`Не удалось загрузить машины: ${orgErrorText(list.error)}`} onRetry={list.reload} />
      );
    }
    if (!data) return member ? <MemberSkeleton /> : <ListSkeleton />;
    if (member) return <MemberView list={data} />;
    const onAdd = canAdd ? () => setAdding(true) : undefined;
    return org ? (
      <OrgView list={data} onOpen={url.open} onAdd={onAdd} />
    ) : (
      <TenantView list={data} onOpen={url.open} onAdd={onAdd} />
    );
  };

  return (
    <>
      <div className="ad-head">
        <Titles title={title} lead={data || member ? lead : undefined} />
        {end}
      </div>
      {ws.error && !card.shown && (
        <ErrorRow text={ws.error.text} onRetry={() => void ws.open(ws.error!.id)} />
      )}
      <OpenCtx.Provider value={ws}>{content()}</OpenCtx.Provider>
      {canAdd && (
        <AddMachineSheet
          open={adding}
          tenants={addable}
          onClose={() => setAdding(false)}
          onAdded={list.refresh}
        />
      )}
      {!member && card.shown && (
        <MachineDrawer
          key={`${card.shown}:${card.n}`}
          id={card.shown}
          open={url.m !== null}
          hint={hint}
          mlUrl={data?.mlCoreUrl ?? null}
          url={url}
          onChanged={list.patch}
          onRemoved={() => {
            url.close();
            list.refresh();
          }}
        />
      )}
    </>
  );
};

export default Screen;
