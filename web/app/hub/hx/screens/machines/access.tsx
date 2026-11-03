// «Кто может запускать чаты» of the machine card: the rule in words, the people it covers, and the editor of the rule.
import { useState } from 'react';

import type { AccessMode, MachineDetail } from '../../../org/api/machines';
import { Btn, Icon, initial } from '../../ui';
import { peopleWord } from './calc';

export const accessSummary = (d: MachineDetail): string => {
  const { machine: m, access, audience } = d;
  if (access.mode === 'pick') return `Выбранные люди · ${access.people.length}`;
  if (access.mode === 'team')
    return `Команда ${d.teamLabel ?? m.teamName ?? ''} · ${peopleWord(audience.team ?? 0)}`.replace(
      '  ',
      ' ',
    );
  if (access.mode === 'tenant') return `Все в тенанте ${m.tenantName} · ${peopleWord(audience.tenant)}`;
  return 'Только владелец';
};

/** The names shown under the rule: the owner, the picked people, a few of the team with «+N», or everyone. */
export const accessNames = (d: MachineDetail): string[] => {
  const { machine: m, access, audience, candidates } = d;
  if (access.mode === 'owner') return [m.ownerName ?? 'Владелец'];
  if (access.mode === 'pick') return access.people.map((p) => p.name);
  if (access.mode === 'tenant') return ['Все', peopleWord(audience.tenant)];
  const head = candidates.slice(0, 3).map((p) => p.name);
  const rest = (audience.team ?? head.length) - head.length;
  return rest > 0 ? [...head, `+${rest}`] : head;
};

interface ModeOption {
  id: AccessMode;
  label: string;
  hint: string;
}

const modeOptions = (d: MachineDetail): ModeOption[] => {
  const { machine: m, audience } = d;
  const team = `Команда ${d.teamLabel ?? m.teamName ?? ''}`.trim();
  const all: Record<AccessMode, ModeOption> = {
    owner: {
      id: 'owner',
      label: `Только ${m.ownerName ?? 'владелец'}`,
      hint: 'личная машина — чаты запускает только он',
    },
    pick: { id: 'pick', label: 'Выбранные люди', hint: 'только те, кого отметите ниже' },
    team: {
      id: 'team',
      label: team,
      hint: `${peopleWord(audience.team ?? 0)}, состав меняется вместе с командой`,
    },
    tenant: {
      id: 'tenant',
      label: `Все в тенанте ${m.tenantName}`,
      hint: `${peopleWord(audience.tenant)}, включая новых`,
    },
  };
  const order: AccessMode[] = m.kind === 'shared' ? ['pick', 'team', 'tenant'] : ['owner', 'pick', 'team'];
  const ids = order.filter((id) =>
    id === 'owner' ? m.ownerName != null : id === 'team' ? m.teamName != null || d.teamLabel != null : true,
  );
  if (!ids.includes(d.access.mode)) ids.push(d.access.mode);
  return ids.map((id) => all[id]);
};

const Person = ({ name }: { name: string }) => (
  <span className="md-person">
    <span className="hx-avatar md-av">{initial(name)}</span>
    {name}
  </span>
);

const Editor = ({
  d,
  onCancel,
  onSave,
}: {
  d: MachineDetail;
  onCancel: () => void;
  onSave: (mode: AccessMode, personIds: string[]) => Promise<boolean>;
}) => {
  const [mode, setMode] = useState<AccessMode>(d.access.mode);
  const [picked, setPicked] = useState<string[]>(d.access.people.map((p) => p.id));
  const [saving, setSaving] = useState(false);
  const options = modeOptions(d);
  const empty = mode === 'pick' && picked.length === 0;
  const toggle = (id: string) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
  const save = async () => {
    if (saving || empty) return;
    setSaving(true);
    try {
      await onSave(mode, mode === 'pick' ? picked : []);
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="md-edit">
      <div className="md-radio" role="radiogroup" aria-label="Кто может запускать чаты">
        {options.map((o) => (
          <button
            key={o.id}
            type="button"
            className="md-radio__o"
            role="radio"
            aria-checked={mode === o.id}
            onClick={() => setMode(o.id)}
          >
            <span className="md-radio__dot" />
            <span>
              <b>{o.label}</b>
              <small>{o.hint}</small>
            </span>
          </button>
        ))}
      </div>
      {mode === 'pick' && (
        <div className="md-people">
          {d.candidates.map((p) => {
            const on = picked.includes(p.id);
            return (
              <button
                key={p.id}
                type="button"
                className="md-person md-person--pick"
                aria-pressed={on}
                onClick={() => toggle(p.id)}
              >
                <span className="hx-avatar md-av">{initial(p.name)}</span>
                {p.name}
                <Icon i={on ? 'x' : 'plus'} size="sm" />
              </button>
            );
          })}
        </div>
      )}
      <p className="md-note">Идущие чаты не прервутся — новые правила действуют для новых чатов.</p>
      <div className="md-edit__foot">
        <Btn variant="ghost" size="sm" onClick={onCancel}>
          Отмена
        </Btn>
        <button
          type="button"
          className="hx-btn md-save"
          data-variant="primary"
          data-size="sm"
          data-busy={saving ? '1' : '0'}
          aria-disabled={empty}
          onClick={() => void save()}
        >
          <span className="md-spin" aria-hidden="true" />
          {saving ? 'Сохраняем' : 'Сохранить'}
        </button>
      </div>
    </div>
  );
};

export const AccessSection = ({
  d,
  editing,
  locked,
  onEdit,
  onCancel,
  onSave,
}: {
  d: MachineDetail;
  editing: boolean;
  locked: boolean;
  onEdit: () => void;
  onCancel: () => void;
  onSave: (mode: AccessMode, personIds: string[]) => Promise<boolean>;
}) => {
  const m = d.machine;
  return (
    <div className="md-sec">
      <div className="md-sec__head">
        <span className="md-sec__t">Кто может запускать чаты</span>
        {!editing && !locked && (
          <button
            type="button"
            className="ad-add"
            aria-disabled={!m.canManage}
            title={!m.canManage ? 'Может поменять: лид команды' : undefined}
            onClick={() => m.canManage && onEdit()}
          >
            Изменить
          </button>
        )}
      </div>
      {!editing && (
        <>
          <p className="md-acc-sum">{accessSummary(d)}</p>
          <div className="md-access">
            {accessNames(d).map((n) => (
              <Person key={n} name={n} />
            ))}
          </div>
        </>
      )}
      {editing && <Editor d={d} onCancel={onCancel} onSave={onSave} />}
    </div>
  );
};
