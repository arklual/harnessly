// The groups of a layer (docs/hub/environment.md, «Groups»): a head per group over its items. An editor sets a group
// required or optional and whether an optional one starts on; a member turns an optional group on or off for themselves.
import { type FormEvent, useRef, useState } from 'react';

import type { EnvEntryRow, EnvGroup, PatchGroupRequest } from '../../../../hub-api/env';
import { Icon, usePop } from '../../ui';

export interface GroupActions {
  onAdd: (name: string) => Promise<boolean>;
  onPatch: (g: EnvGroup, req: PatchGroupRequest) => Promise<boolean>;
  onRemove: (g: EnvGroup) => Promise<boolean>;
  onMove: (row: EnvEntryRow, g: EnvGroup | null) => Promise<boolean>;
  /** A member's own switch; null for an editor. */
  onMine: ((g: EnvGroup, on: boolean) => Promise<boolean>) | null;
}

const Switch = ({ on, label, onClick }: { on: boolean; label: string; onClick: () => void }) => (
  <button
    type="button"
    className="hx-switch"
    role="switch"
    aria-checked={on ? 'true' : 'false'}
    aria-label={label}
    onClick={onClick}
  >
    <i />
  </button>
);

const Tipped = ({ tip, children }: { tip: string; children: React.ReactNode }) => (
  <span className="ev-req hx-tipped">
    {children}
    <span className="hx-tip pz-tip" data-align="end">
      {tip}
    </span>
  </span>
);

export const GroupHead = ({
  g,
  count,
  edit,
  a,
}: {
  g: EnvGroup;
  count: number;
  edit: boolean;
  a: GroupActions;
}) => {
  const mine = a.onMine;
  return (
    <div className="ev-grp" data-required={g.required ? 'true' : undefined}>
      <span className="ev-grp__t">
        <b>{g.name}</b>
        <small>{g.hint || (count === 0 ? 'Пока пусто — перенесите сюда пункты' : `${count} в группе`)}</small>
      </span>
      {edit && !g.builtIn && (
        <>
          {!g.required && (
            <Tipped
              tip={
                g.defaultOn
                  ? 'Кто сам не выбирал, получит группу. Выключите — получат только те, кто включит'
                  : 'Получат только те, кто включит у себя. Включите — получат все, кто не выключил'
              }
            >
              <span className="ev-req__l">{g.defaultOn ? 'включена сразу' : 'выключена'}</span>
              <Switch
                on={g.defaultOn}
                label="Включена сразу"
                onClick={() => void a.onPatch(g, { defaultOn: !g.defaultOn })}
              />
            </Tipped>
          )}
          <Tipped
            tip={
              g.required
                ? 'Поставится всем, выключить нельзя. Выключите — каждый выберет сам'
                : 'Каждый выбирает у себя. Включите — поставится всем'
            }
          >
            <span className="ev-req__l">{g.required ? 'обязательная' : 'по желанию'}</span>
            <Switch
              on={g.required}
              label="Обязательная"
              onClick={() => void a.onPatch(g, { required: !g.required })}
            />
          </Tipped>
          <span className="ev-pen-w hx-tipped">
            <button
              type="button"
              className="hx-btn ev-pen"
              data-variant="ghost"
              data-size="sm"
              data-shape="icon"
              aria-label={`Убрать группу «${g.name}»`}
              onClick={() => void a.onRemove(g)}
            >
              <Icon i="trash-2" size="sm" />
            </button>
            <span className="hx-tip pz-tip" data-align="end">
              Убрать группу — пункты останутся
            </span>
          </span>
        </>
      )}
      {!edit && mine && !g.required && (
        <Tipped
          tip={
            g.defaultOn
              ? 'Выключите — машина не будет ставить новое из группы'
              : 'Включите — машина поставит группу'
          }
        >
          <span className="ev-req__l">{g.defaultOn ? 'включена' : 'выключена'}</span>
          <Switch on={g.defaultOn} label={`Группа «${g.name}»`} onClick={() => void mine(g, !g.defaultOn)} />
        </Tipped>
      )}
      {edit && g.builtIn && <span className="ev-req__ro">от платформы</span>}
      {!edit && (g.required || !mine) && (
        <span className="ev-req__ro">{g.required ? 'обязательная' : 'по желанию'}</span>
      )}
    </div>
  );
};

/** The button of a row that moves the entry into a group of its layer or out of it. */
export const MoveTo = ({ row, groups, a }: { row: EnvEntryRow; groups: EnvGroup[]; a: GroupActions }) => {
  const root = useRef<HTMLSpanElement>(null);
  const pop = usePop(root);
  return (
    <span className="ev-pen-w ev-move hx-tipped" ref={root} onClick={(e) => e.stopPropagation()}>
      <button
        type="button"
        className="hx-btn ev-pen"
        data-variant="ghost"
        data-size="sm"
        data-shape="icon"
        aria-label="В группу"
        aria-expanded={pop.open ? 'true' : 'false'}
        onClick={pop.toggle}
      >
        <Icon i="folder-input" size="sm" />
      </button>
      {!pop.open && (
        <span className="hx-tip pz-tip" data-align="end">
          В группу
        </span>
      )}
      {pop.mounted && (
        <div className="ad-pop ev-move__pop" data-closing={pop.attr} role="listbox" aria-label="В группу">
          {[null, ...groups].map((g) => {
            const on = (g?.id ?? '') === row.groupId;
            return (
              <button
                type="button"
                className="ad-pop__item"
                role="option"
                aria-selected={on ? 'true' : 'false'}
                key={g?.id ?? ''}
                onClick={() => {
                  pop.close();
                  if (!on) void a.onMove(row, g);
                }}
              >
                <span>{g ? g.name : 'Без группы'}</span>
                {g && <small>{g.required ? 'обязательная' : 'по желанию'}</small>}
                {on && <Icon i="check" size="sm" />}
              </button>
            );
          })}
        </div>
      )}
    </span>
  );
};

/** «Новая группа»: a name, Enter. */
export const NewGroup = ({ onAdd }: { onAdd: (name: string) => Promise<boolean> }) => {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    if (!n || busy) return;
    setBusy(true);
    const ok = await onAdd(n);
    setBusy(false);
    if (ok) {
      setName('');
      setOpen(false);
    }
  };
  if (!open)
    return (
      <button type="button" className="ev-addrow" onClick={() => setOpen(true)}>
        <Icon i="folder-plus" size="sm" />
        Новая группа
      </button>
    );
  return (
    <form className="ev-newgrp" onSubmit={(e) => void submit(e)}>
      <Icon i="folder-plus" size="sm" />
      <input
        autoFocus
        value={name}
        placeholder="Название группы, например «Бэкенд»"
        aria-label="Название группы"
        disabled={busy}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false);
        }}
      />
      <button
        type="submit"
        className="hx-btn"
        data-size="sm"
        data-variant="primary"
        disabled={busy || !name.trim()}
      >
        Создать
      </button>
    </form>
  );
};
