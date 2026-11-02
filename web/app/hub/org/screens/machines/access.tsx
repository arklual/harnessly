// Who may start chats on a machine: one of four rules, and for «Выбранные люди» the list of people.
import { useState } from 'react';
import { Plus, X } from 'lucide-react';

import { Button } from '@kit';

import type { AccessMode, MachineAccess, MachineDetail } from '../../api/machines';
import { orgErrorText } from '../../api/errors';
import { plural } from './format';

const people = (n: number) => `${n} ${plural(n, 'человек', 'человека', 'человек')}`;

export const accessSummary = (d: MachineDetail): string => {
  const { machine: m, access, audience } = d;
  if (access.mode === 'pick') return `Выбранные люди · ${access.people.length}`;
  if (access.mode === 'team') return `Команда ${m.teamName ?? ''} · ${people(audience.team ?? 0)}`;
  if (access.mode === 'tenant') return `Все в тенанте ${m.tenantName} · ${people(audience.tenant)}`;
  return `Только ${m.ownerName ?? 'владелец'}`;
};

export const Chip = ({ name, on, onToggle }: { name: string; on?: boolean; onToggle?: () => void }) => {
  const body = (
    <>
      <span className="ho-mark ho-mark--dark">{name.slice(0, 1)}</span>
      {name}
      {onToggle && (on ? <X size={12} aria-hidden="true" /> : <Plus size={12} aria-hidden="true" />)}
    </>
  );
  return onToggle ? (
    <button type="button" className="hm-chip" data-on={on || undefined} onClick={onToggle} aria-pressed={on}>
      {body}
    </button>
  ) : (
    <span className="hm-chip" data-on>
      {body}
    </span>
  );
};

export const AccessEditor = ({
  d,
  pending,
  error,
  onCancel,
  onSave,
}: {
  d: MachineDetail;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onSave: (mode: AccessMode, personIds: string[]) => void;
}) => {
  const { machine: m, audience } = d;
  const [mode, setMode] = useState<AccessMode>(d.access.mode);
  const [picked, setPicked] = useState<string[]>(d.access.people.map((p) => p.id));
  const opts: { id: AccessMode; label: string; hint: string }[] = [
    ...(m.ownerName
      ? [
          {
            id: 'owner' as const,
            label: `Только ${m.ownerName}`,
            hint: 'машина личная, чаты запускает владелец',
          },
        ]
      : []),
    { id: 'pick', label: 'Выбранные люди', hint: 'только те, кого отметите ниже' },
    ...(m.teamName
      ? [
          {
            id: 'team' as const,
            label: `Команда ${m.teamName}`,
            hint: `${people(audience.team ?? 0)}, состав меняется вместе с командой`,
          },
        ]
      : []),
    {
      id: 'tenant',
      label: `Все в тенанте ${m.tenantName}`,
      hint: `${people(audience.tenant)}, включая новых`,
    },
  ];
  const toggle = (id: string) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
  const same = (a: MachineAccess) =>
    a.mode === mode &&
    (mode !== 'pick' ||
      [...picked].sort().join() ===
        a.people
          .map((p) => p.id)
          .sort()
          .join());
  return (
    <div className="hm-edit">
      <div className="hm-radios" role="radiogroup" aria-label="Кто может запускать чаты">
        {opts.map((o) => (
          <button
            key={o.id}
            type="button"
            role="radio"
            aria-checked={mode === o.id}
            className="hm-radio"
            onClick={() => setMode(o.id)}
          >
            <i className="hm-radio__dot" aria-hidden="true" />
            <span>
              <b>{o.label}</b>
              <small>{o.hint}</small>
            </span>
          </button>
        ))}
      </div>
      {mode === 'pick' && (
        <div className="hm-chips">
          {d.candidates.map((p) => (
            <Chip key={p.id} name={p.name} on={picked.includes(p.id)} onToggle={() => toggle(p.id)} />
          ))}
        </div>
      )}
      {error != null && <p className="hm-dlg__err">{orgErrorText(error)}</p>}
      <div className="hm-edit__actions">
        <Button appearance="ghost" onClick={onCancel} disabled={pending}>
          Отмена
        </Button>
        <Button
          appearance="primary"
          loading={pending}
          disabled={same(d.access) || (mode === 'pick' && picked.length === 0)}
          onClick={() => onSave(mode, mode === 'pick' ? picked : [])}
        >
          Сохранить
        </Button>
      </div>
    </div>
  );
};
