// The design system of a new project (the design's .hx-dl-ds): a chip in the field's bar, a panel with a search over
// the systems the machine has — «Недавние» (the last three picks) and «Все», «Найдено» while searching — and
// «Без дизайн-системы» at its foot. Making a design system itself needs none: the chip goes away then.
import { Fragment, useEffect, useRef, useState } from 'react';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import type { IDesignSystem } from '@/next/features/designly/api';
import { dotTone, readRecent, rememberRecent } from '@/next/features/designly/ds-recent';
import { t } from '@/next/i18n';

const POP_EXIT_MS = 200;

/** One more way to give a look, listed above «Без дизайн-системы» (a presentation's company template). */
export interface IDsExtra {
  label: string;
  desc: string;
  /** The way is in use; the chip then names it (`name`) when no system is picked. */
  active: boolean;
  name: string;
  onPick: () => void;
}

export interface IDsPickerProps {
  systems: readonly IDesignSystem[];
  /** null: no design system. */
  value: string | null;
  shown: boolean;
  onChange: (id: string | null) => void;
  /** 'chip': the picker sits in a composer's bar as a chip that keeps its name on a phone too. */
  variant?: 'chip';
  /** What the chip says while nothing is picked (default: «Без дизайн-системы»). */
  emptyLabel?: string;
  extra?: IDsExtra;
}

interface IDsGroup {
  key: string;
  label: string;
  items: readonly IDesignSystem[];
}

/** The panel's groups: what was found while searching; otherwise the recent picks and the rest. */
const groupsOf = (
  found: readonly IDesignSystem[],
  recent: readonly string[],
  searching: boolean,
): IDsGroup[] => {
  if (searching) return [{ key: 'found', label: t('next.designly.dsFound'), items: found }];
  const picked = recent.flatMap((id) => found.find((s) => s.id === id) ?? []);
  return [
    { key: 'recent', label: t('next.designly.dsRecent'), items: picked },
    { key: 'all', label: t('next.designly.dsAll'), items: found.filter((s) => !recent.includes(s.id)) },
  ].filter((group) => group.items.length > 0);
};

/** The dot of a system: the stylesheet colours the named ones by id, the others by a hashed tone. */
const Dot = ({ id }: { id: string }) => (
  <span className="hx-dl-dot" data-ds={id} data-tone={dotTone(id) ?? undefined} />
);

const DsPicker = ({ systems, value, shown, onChange, variant, emptyLabel, extra }: IDsPickerProps) => {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [recent, setRecent] = useState(readRecent);
  const pop = usePresentFlag(open && shown, POP_EXIT_MS);
  const rootRef = useRef<HTMLDivElement>(null);
  const current = systems.find((s) => s.id === value) ?? null;

  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const q = query.trim().toLowerCase();
  const found = systems.filter((s) => !q || `${s.label} ${s.desc}`.toLowerCase().includes(q));
  const groups = groupsOf(found, recent, q !== '');
  const viaExtra = extra?.active === true && current === null;
  const pickExtra = () => {
    extra?.onPick();
    setOpen(false);
  };
  const pick = (id: string | null) => {
    if (id !== null) setRecent((list) => rememberRecent(list, id));
    onChange(id);
    setOpen(false);
  };

  return (
    <div className="hx-dl-ds" data-on={shown} data-variant={variant} ref={rootRef}>
      <button
        type="button"
        className="hx-dl-ds__btn"
        aria-expanded={open && shown}
        tabIndex={shown ? 0 : -1}
        onClick={() => {
          setQuery('');
          setOpen((now) => !now);
        }}
      >
        {viaExtra ? <Icon name="presentation" size="sm" /> : <Dot id={current ? current.id : 'none'} />}
        <span className="hx-dl-ds__name">
          {current ? current.label : viaExtra && extra ? extra.name : (emptyLabel ?? t('next.designly.noDs'))}
        </span>
        <span className="hx-dl-ds__short">{current ? current.label : t('next.designly.dsShort')}</span>
        <i className="hx-icon" data-size="sm" data-i="chevron-down" data-chev="" aria-hidden="true" />
      </button>
      {pop.mounted && (
        <div className="hx-dl-pop" data-state={pop.state} role="dialog" aria-label={t('next.designly.ds')}>
          <label className="hx-dl-pop__search">
            <Icon name="search" size="sm" />
            <input
              type="search"
              autoFocus
              autoComplete="off"
              placeholder={t('next.designly.dsSearch')}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && found[0]) pick(found[0].id);
              }}
            />
          </label>
          <div className="hx-dl-pop__list">
            {groups.map((group) => (
              <Fragment key={group.key}>
                <div className="hx-dl-pop__label">{group.label}</div>
                {group.items.map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    className="hx-option"
                    aria-selected={s.id === value}
                    onClick={() => pick(s.id)}
                  >
                    <Dot id={s.id} />
                    <span className="hx-option__text">
                      <span className="hx-option__name">{s.label}</span>
                      {s.desc && <span className="hx-option__desc">{s.desc}</span>}
                    </span>
                    <Icon name="check" size="sm" className="hx-option__check" />
                  </button>
                ))}
              </Fragment>
            ))}
            {found.length === 0 && <div className="hx-dl-pop__none">{t('next.designly.dsNone')}</div>}
          </div>
          <div className="hx-dl-pop__foot">
            {extra && (
              <button type="button" className="hx-option" aria-selected={viaExtra} onClick={pickExtra}>
                <Icon name="presentation" size="sm" />
                <span className="hx-option__text">
                  <span className="hx-option__name">{extra.label}</span>
                  <span className="hx-option__desc">{extra.desc}</span>
                </span>
                <Icon name="check" size="sm" className="hx-option__check" />
              </button>
            )}
            <button
              type="button"
              className="hx-option"
              aria-selected={value === null && !viaExtra}
              onClick={() => pick(null)}
            >
              <Dot id="none" />
              <span className="hx-option__text">
                <span className="hx-option__name">{t('next.designly.noDs')}</span>
                <span className="hx-option__desc">{t('next.designly.noDsDesc')}</span>
              </span>
              <Icon name="check" size="sm" className="hx-option__check" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
};

export default DsPicker;
