// The «Машины» screen of the board «Хаб». Its own chunk: it loads when the person opens the section.
import { useState } from 'react';

import { useLocation } from '../../lib/router';

import { liveOn } from '../flag';
import { BuilderPool } from './machines/builder';
import { useOrg } from '../../org/context';
import { orgErrorText } from '../../org/api/errors';
import { Btn } from '../ui';
import { Titles } from '../shell';
import { orgLead, tenantLead } from './machines/calc';
import { ConnectPage, readAdd, useAddUrl } from './machines/connect';
import { MachineDrawer } from './machines/drawer';
import { OpenCtx, useOpenMachine } from './machines/open';
import { MemberSkeleton, MemberView } from './machines/member-view';
import { OrgView } from './machines/org-view';
import { ErrorRow, ListSkeleton } from './machines/parts';
import { TenantView } from './machines/tenant-view';
import { TbotJoins } from './machines/tbot-joins';
import { useDrawerUrl } from './machines/url';
import { useMachineList } from './machines/use-machines';

import '../styles/machines.css';

const MEMBER_LEAD = 'Открыть — перейти в рантайм машины';

const Screen = () => {
  const { scope, shell } = useOrg();
  const url = useDrawerUrl();
  const list = useMachineList();
  const add = useAddUrl(readAdd(useLocation().search));
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
  // A member connects a machine of their own unless the organization's admin turned it off.
  const canAdd = !member || shell.org.memberMachines !== false;
  const adding = add.adding && canAdd;
  const data = list.data;
  const all = data ? data.groups.flatMap((g) => g.machines) : [];
  const title = 'Машины';
  const lead = member
    ? MEMBER_LEAD
    : org
      ? orgLead(shell.org.name, data?.summary.total ?? all.length, all)
      : tenantLead(scope.tenant.name, all);
  const addable = org ? shell.tenants : [scope.tenant];
  const hint = card.shown ? (all.find((m) => m.id === card.shown) ?? null) : null;

  // A member's machine is their own and waits for an administrator's confirmation.
  const end = canAdd ? (
    <div className="hb-tools">
      <Btn variant="outline" size="sm" icon="plus" onClick={() => add.open()}>
        Подключить машину
      </Btn>
    </div>
  ) : null;

  const content = () => {
    if (adding) {
      return (
        <ConnectPage
          tenants={addable}
          machines={all}
          refresh={list.refresh}
          onClose={add.close}
          onOpen={(id) => {
            add.close();
            url.open(id);
          }}
        />
      );
    }
    if (list.error && !data) {
      return (
        <ErrorRow text={`Не удалось загрузить машины: ${orgErrorText(list.error)}`} onRetry={list.reload} />
      );
    }
    if (!data) return member ? <MemberSkeleton /> : <ListSkeleton />;
    const onAdd = () => add.open();
    if (member) return <MemberView list={data} onAdd={canAdd ? onAdd : undefined} />;
    return org ? (
      <OrgView list={data} onOpen={url.open} onAdd={onAdd} />
    ) : (
      <TenantView list={data} onOpen={url.open} onAdd={onAdd} />
    );
  };

  if (adding) {
    return (
      <>
        <span className="hx-part-mark" data-hx-part="machines" aria-hidden="true" />
        {content()}
      </>
    );
  }

  return (
    <>
      <div className="hb-head hd-head">
        <Titles title={title} lead={data || member ? lead : undefined} />
        {end}
      </div>
      {ws.error && !card.shown && (
        <ErrorRow
          text={ws.error.text}
          onRetry={() => void ws.open(ws.error!.id)}
          {...(ws.error.ownWork && !member
            ? { action: 'Открыть для отладки', onAction: () => url.open(ws.error!.id) }
            : {})}
        />
      )}
      <TbotJoins />
      <span className="hx-part-mark" data-hx-part="machines" aria-hidden="true" />
      <OpenCtx.Provider value={ws}>{content()}</OpenCtx.Provider>
      {!member && liveOn(shell.features) && <BuilderPool tenants={org ? shell.tenants : [scope.tenant]} />}

      {!member && card.shown && (
        <MachineDrawer
          key={`${card.shown}:${card.n}`}
          id={card.shown}
          open={url.m !== null}
          hint={hint}
          mlUrl={data?.mlCoreUrl ?? null}
          url={url}
          opener={ws}
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
