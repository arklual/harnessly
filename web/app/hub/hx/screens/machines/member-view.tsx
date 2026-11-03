// A participant's machines: their own and, when there are any, the shared ones. «Открыть» leads to the machine's
// runtime; the address comes from the hub when the button is pressed.
import { useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type { MachinesList, OrgMachine } from '../../../org/api/machines';
import { plural } from '../../../org/screens/machines/format';
import { useOrg } from '../../../org/context';
import { Btn, Icon } from '../../ui';
import { hwLine } from './calc';
import { ErrorRow } from './parts';

/** «Ubuntu 24.04 · 16 ядер · 64 ГБ · 3 чата идут» (the board's wording). */
export const memberMeta = (m: OrgMachine): string => {
  const chats = m.chats ?? 0;
  const busy =
    chats > 0
      ? `${chats} ${plural(chats, 'чат', 'чата', 'чатов')} ${chats === 1 ? 'идёт' : 'идут'}`
      : 'свободна';
  const tail = m.kind === 'shared' ? [m.purpose ?? '', busy] : [busy];
  return [m.os ?? '', hwLine(m), ...tail].filter(Boolean).join(' · ');
};

export const MemberSkeleton = () => (
  <div className="mx-skel-list" role="status" aria-label="Загрузка">
    {[0, 1, 2].map((i) => (
      <span key={i} className="mx-skel" />
    ))}
  </div>
);

export const MemberView = ({ list }: { list: MachinesList }) => {
  const { api } = useOrg();
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<{ id: string; text: string } | null>(null);
  const all = list.groups.flatMap((g) => g.machines);
  const mine = all.filter((m) => m.mine !== false);
  const shared = all.filter((m) => m.mine === false);

  const open = async (id: string) => {
    setBusyId(id);
    setError(null);
    try {
      const { url } = await api.openMachine(id);
      window.location.assign(url);
    } catch (e) {
      setError({ id, text: orgErrorText(e) });
    } finally {
      setBusyId(null);
    }
  };

  const group = (title: string, rows: readonly OrgMachine[]) =>
    rows.length === 0 ? null : (
      <div className="hx-set__group" key={title}>
        <div className="hx-set__group-title">{title}</div>
        <div className="hx-set__card">
          {rows.map((m) => {
            const on = m.state === 'online' || m.state === 'idle';
            return (
              <div className="ad-mrow" key={m.id}>
                <span className="ad-mrow__ic">
                  <Icon i="server" />
                </span>
                <span className="ad-mrow__t">
                  <b>{m.name}</b>
                  <small>{memberMeta(m)}</small>
                </span>
                <span className="ad-live" data-live={on ? '1' : '0'}>
                  {on ? 'на связи' : 'не на связи'}
                </span>
                <Btn
                  variant={on ? 'primary' : 'outline'}
                  size="sm"
                  busy={busyId === m.id}
                  onClick={() => void open(m.id)}
                >
                  Открыть
                </Btn>
              </div>
            );
          })}
        </div>
      </div>
    );

  return (
    <>
      {error && <ErrorRow text={error.text} onRetry={() => void open(error.id)} />}
      {group('Мои', mine)}
      {group(`Общие в тенанте ${shared[0]?.tenantName ?? ''}`.trim(), shared)}
      {all.length === 0 && <div className="mo-empty">Машин пока нет</div>}
    </>
  );
};
