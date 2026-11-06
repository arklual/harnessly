// The presets of a layer (docs/hub/environment.md, «Presets»): a named set of the layer's groups. An editor keeps them
// in a block above the groups; a member picks one in a click and the active one is lit.
import { type FormEvent, useState } from 'react';

import type { EnvGroup, EnvPreset, PatchPresetRequest } from '../../../../hub-api/env';
import { useClosing } from '../../../org/ui/closing';
import { closingAttr, Icon, POP_MS } from '../../ui';
import type { MemberPreset } from './scope';

export interface PresetActions {
  onAdd: (name: string, groupIds: string[]) => Promise<boolean>;
  onPatch: (p: EnvPreset, req: PatchPresetRequest) => Promise<boolean>;
  onRemove: (p: EnvPreset) => Promise<boolean>;
  /** A member's pick; null for an editor. */
  onMine: ((p: EnvPreset) => Promise<boolean>) | null;
}

/** Name and the layer's groups to tick; `preset` is the one being changed, none for a new one. */
const PresetForm = ({
  groups,
  preset,
  onDone,
  onSave,
}: {
  groups: EnvGroup[];
  preset: EnvPreset | null;
  onDone: () => void;
  onSave: (name: string, groupIds: string[]) => Promise<boolean>;
}) => {
  const [name, setName] = useState(preset?.name ?? '');
  const [ids, setIds] = useState<string[]>(preset?.groupIds ?? []);
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    if (!n || busy) return;
    setBusy(true);
    const ok = await onSave(n, ids);
    setBusy(false);
    if (ok) onDone();
  };
  const tick = (id: string, on: boolean) => setIds(on ? [...ids, id] : ids.filter((x) => x !== id));
  return (
    <form className="ev-pform" onSubmit={(e) => void submit(e)}>
      <input
        autoFocus
        value={name}
        maxLength={40}
        placeholder="Название пресета, например «Фронтенд»"
        aria-label="Название пресета"
        disabled={busy}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onDone();
        }}
      />
      <div className="ev-pform__g" role="group" aria-label="Группы пресета">
        {groups.length === 0 && <small>Сначала заведите группы — пресет собирается из них</small>}
        {groups.map((g) => (
          <label className="ev-pck" key={g.id}>
            <input
              type="checkbox"
              checked={ids.includes(g.id)}
              disabled={busy}
              onChange={(e) => tick(g.id, e.target.checked)}
            />
            <span>{g.name}</span>
          </label>
        ))}
      </div>
      <div className="ev-pform__b">
        <button type="button" className="hx-btn" data-size="sm" data-variant="ghost" onClick={onDone}>
          Отмена
        </button>
        <button
          type="submit"
          className="hx-btn"
          data-size="sm"
          data-variant="primary"
          disabled={busy || !name.trim()}
        >
          {preset ? 'Сохранить' : 'Создать'}
        </button>
      </div>
    </form>
  );
};

/** A form that opens and closes with a short ease-out. */
const Folded = ({ open, children }: { open: boolean; children: React.ReactNode }) => {
  const { mounted, closing } = useClosing(open, POP_MS * 2);
  if (!mounted) return null;
  return (
    <div className="ev-pform-w" data-closing={closingAttr(closing)}>
      <div>{children}</div>
    </div>
  );
};

const PresetRow = ({
  p,
  groups,
  a,
  editing,
  onEdit,
}: {
  p: EnvPreset;
  groups: EnvGroup[];
  a: PresetActions;
  editing: boolean;
  onEdit: (on: boolean) => void;
}) => {
  const names = p.groupIds.flatMap((id) => groups.filter((g) => g.id === id).map((g) => g.name));
  return (
    <>
      <div className="ev-prow">
        <span className="ev-prow__t">
          <b>{p.name}</b>
          <small>{p.hint}</small>
        </span>
        <span className="ev-prow__g">
          {names.length === 0 && <small>Без групп</small>}
          {names.map((n) => (
            <span className="ev-pchip" key={n}>
              {n}
            </span>
          ))}
        </span>
        <span className="ev-pen-w hx-tipped">
          <button
            type="button"
            className="hx-btn ev-pen"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            aria-label={`Изменить пресет «${p.name}»`}
            aria-expanded={editing ? 'true' : 'false'}
            onClick={() => onEdit(!editing)}
          >
            <Icon i="pencil" size="sm" />
          </button>
          <span className="hx-tip pz-tip" data-align="end">
            Изменить
          </span>
        </span>
        <span className="ev-pen-w hx-tipped">
          <button
            type="button"
            className="hx-btn ev-pen"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            aria-label={`Убрать пресет «${p.name}»`}
            onClick={() => void a.onRemove(p)}
          >
            <Icon i="trash-2" size="sm" />
          </button>
          <span className="hx-tip pz-tip" data-align="end">
            Убрать пресет — группы останутся
          </span>
        </span>
      </div>
      <Folded open={editing}>
        <PresetForm
          groups={groups}
          preset={p}
          onDone={() => onEdit(false)}
          onSave={(name, groupIds) => a.onPatch(p, { name, groupIds })}
        />
      </Folded>
    </>
  );
};

/** The editor's block «Пресеты» above the groups of a layer. */
export const PresetsEditor = ({
  presets,
  groups,
  a,
}: {
  presets: EnvPreset[];
  groups: EnvGroup[];
  a: PresetActions;
}) => {
  const [adding, setAdding] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const pickable = groups.filter((g) => !g.required);
  return (
    <div className="ev-presets">
      <div className="ev-presets__h">
        <b>Пресеты</b>
        <small>Набор групп: человек выбирает его, и нужные группы включаются сами</small>
      </div>
      {[...presets]
        .sort((x, y) => x.position - y.position)
        .map((p) => (
          <PresetRow
            key={p.id}
            p={p}
            groups={groups}
            a={a}
            editing={editId === p.id}
            onEdit={(on) => setEditId(on ? p.id : null)}
          />
        ))}
      {!adding && (
        <button type="button" className="ev-addrow ev-addrow--in" onClick={() => setAdding(true)}>
          <Icon i="layers" size="sm" />
          Новый пресет
        </button>
      )}
      <Folded open={adding}>
        <PresetForm
          groups={pickable}
          preset={null}
          onDone={() => setAdding(false)}
          onSave={(name, groupIds) => a.onAdd(name, groupIds)}
        />
      </Folded>
    </div>
  );
};

const activeOf = (p: EnvPreset | MemberPreset): boolean => 'active' in p && p.active;

/** A member's chips: one click sets their groups to the preset's. */
export const PresetChips = ({ presets, a }: { presets: EnvPreset[]; a: PresetActions }) => {
  const mine = a.onMine;
  if (!mine) return null;
  return (
    <div className="ev-pchips" role="group" aria-label="Пресеты">
      {[...presets]
        .sort((x, y) => x.position - y.position)
        .map((p) => (
          <button
            type="button"
            className="ev-pchoose"
            key={p.id}
            aria-pressed={activeOf(p) ? 'true' : 'false'}
            title={p.hint || undefined}
            onClick={() => {
              if (!activeOf(p)) void mine(p);
            }}
          >
            {activeOf(p) && <Icon i="check" size="sm" />}
            {p.name}
          </button>
        ))}
    </div>
  );
};
