// «Настраиваете ▾» and «Куда»: the same list of layers in two places. An admin picks the organization, a tenant or a team
// (with a search: there are many); a lead picks one of their teams; a member has only their own and no list.
import { useRef, useState } from 'react';

import type { EnvResponse } from '../../../../hub-api/env';
import type { Role } from '../../../org/types';
import { Icon, usePop } from '../../ui';
import { metaOf, pickHasSearch, pickList, same, type Target } from './scope';

export const ScopePicker = ({
  view,
  role,
  target,
  onPick,
  variant,
  label,
  disabled,
}: {
  view: Pick<EnvResponse, 'layers' | 'teams'>;
  role: Role;
  target: Target;
  onPick: (t: Target) => void;
  variant: 'page' | 'sheet';
  label: string;
  disabled?: boolean;
}) => {
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const [q, setQ] = useState('');
  const layer = view.layers.find((l) => same(l, target));
  if (!layer) return null;
  const m = metaOf(layer);
  const list = pickList(view, role, q);
  const search = pickHasSearch(view, role);
  const ph = role === 'admin' ? 'Найти тенант или команду' : 'Найти команду';
  const toggle = () => {
    if (pop.open) pop.close();
    else {
      setQ('');
      pop.setOpen(true);
    }
  };
  return (
    <div className="ev-scope" ref={root}>
      {variant === 'page' ? (
        <button
          type="button"
          className="ev-scope__btn"
          aria-expanded={pop.open ? 'true' : 'false'}
          disabled={disabled}
          onClick={toggle}
        >
          <span className="hb-dot" data-l={m.kind}>
            {m.initial}
          </span>
          <span className="ev-scope__t">
            <small>{label}</small>
            <b>{m.pick}</b>
          </span>
          <Icon i="chevrons-up-down" size="sm" />
        </button>
      ) : (
        <button
          type="button"
          className="ev-to__btn"
          aria-expanded={pop.open ? 'true' : 'false'}
          disabled={disabled}
          onClick={toggle}
        >
          <span className="hb-dot" data-l={m.kind}>
            {m.initial}
          </span>
          <span>{m.pick}</span>
          <Icon i="chevrons-up-down" size="sm" />
        </button>
      )}
      {pop.mounted && (
        <div className="hb-pop ev-scope__pop" data-closing={pop.attr} role="listbox" aria-label={label}>
          {search && (
            <label className="ev-pop-search">
              <Icon i="search" size="sm" />
              <input placeholder={ph} value={q} aria-label={ph} onChange={(e) => setQ(e.target.value)} />
            </label>
          )}
          {list.map((t, i) => {
            if (t.head)
              return (
                <div className="hb-pop__label" key={`h${i}`}>
                  {t.head}
                </div>
              );
            if (t.more)
              return (
                <div className="ev-pop-more" key={`m${i}`}>
                  {t.more}
                </div>
              );
            const on = t.target ? same(t.target, target) : false;
            return (
              <button
                type="button"
                className="hb-pop__item"
                role="option"
                aria-selected={on ? 'true' : 'false'}
                key={t.target ? `${t.target.layer}:${t.target.scopeId}` : i}
                onClick={() => {
                  if (t.target) onPick(t.target);
                  pop.close();
                }}
              >
                <span className="hb-dot" data-l={t.kind}>
                  {t.initial}
                </span>
                <span>{t.name}</span>
                <small>{t.sub}</small>
                {on && <Icon i="check" size="sm" />}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};
