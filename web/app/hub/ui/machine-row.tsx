import { type ReactNode } from 'react';

import { Dot } from '@kit';

import { machineLine, viaLabel } from '../lib/format';
import { t } from '../lib/i18n';
import type { Machine } from '../lib/types';
import { HardDrive, Server } from './icons';
import { Msg } from './parts';

const statusOf = (m: Machine) => {
  if (m.status === 'online') return { tone: 'success' as const, text: t('machine.online') };
  if (m.status === 'pending') return { tone: 'warning' as const, text: t('machine.pending') };
  return { tone: 'neutral' as const, text: t('machine.offline') };
};

/** A machine as a row: name, how it came, its line from the preflight, its state. `actions` go at the right. */
export const MachineRow = ({
  machine,
  actions,
  details,
}: {
  machine: Machine;
  actions?: ReactNode;
  /** Shows the preflight line and its warnings under the row. */
  details?: boolean;
}) => {
  const st = statusOf(machine);
  const pre = machine.preflight;
  const problems = pre?.checks.filter((c) => c.level !== 'ok') ?? [];
  return (
    <div className="hub-item">
      <div className="hub-item__head">
        <span className="hub-ic">
          {machine.via === 'local' ? <HardDrive size={18} /> : <Server size={18} />}
        </span>
        <span className="hub-t">
          <span className="hub-t__n">{machine.name}</span>
          <span className="hub-item__s">
            {viaLabel(machine.via)}
            {machine.hostname ? ` · ${machine.hostname}` : ''}
          </span>
        </span>
        <span className="hub-item__st" data-keep>
          <Dot tone={st.tone} live={machine.status === 'pending'} label={st.text} /> {st.text}
        </span>
        {actions}
      </div>
      {details && pre && machine.status !== 'pending' && (
        <div className="hub-item__sub">{machineLine(pre)}</div>
      )}
      {details &&
        problems.map((c) => (
          <Msg key={c.id} tone={c.level === 'fail' ? 'danger' : 'warn'}>
            {c.message}
          </Msg>
        ))}
    </div>
  );
};
