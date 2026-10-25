// A role and its term from one small menu, the Google Docs way: the button says «Может смотреть ▾»; the menu has the
// roles, then «Доступ до…» that unfolds the terms (for ever, an hour, a day, 7 or 30 days, up to a date), then
// «Убрать доступ» where a person can lose the role. On the phone the menu is a bottom sheet (ui/popover).
import { useState } from 'react';
import { endOfDay, type TGrantRole, type TTtlValue } from '@/next/features/designly/access/api';
import { canLabel, roleLabel, untilLabel } from '@/next/features/designly/access/labels';
import { STEP_KEY, STEPS, today, ttlLabel } from '@/next/features/designly/access/ttl-picker';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { t } from '@/next/i18n';

export interface IRoleTerm {
  value: TTtlValue;
  onChange: (value: TTtlValue) => void;
}

interface IRoleMenuProps<R extends TGrantRole> {
  /** The name of the button and of its menu («Роль: ann@x.ru»). */
  label: string;
  role: R;
  roles: readonly R[];
  onRole: (role: R) => void;
  /** The term chosen under «Доступ до…»; a menu without it has no such item. */
  term?: IRoleTerm;
  /** Says the term in force beside the role on the button (the add field: nothing else shows it). */
  showTerm?: boolean;
  onRemove?: () => void;
  disabled?: boolean;
}

const RoleMenu = <R extends TGrantRole>({
  label,
  role,
  roles,
  onRole,
  term,
  showTerm = false,
  onRemove,
  disabled = false,
}: IRoleMenuProps<R>) => {
  const [open, setOpen] = useState(false);
  const [terms, setTerms] = useState(false);
  const [dating, setDating] = useState(false);
  // A date reads «до 12 ноя», as under the person's address; a step or for ever in its words.
  const termText = term ? (untilLabel(term.value) ?? ttlLabel(term.value)) : null;
  const close = () => {
    setOpen(false);
    setTerms(false);
    setDating(false);
  };
  const pickTerm = (next: TTtlValue) => {
    close();
    if (term && next !== term.value) term.onChange(next);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => (next ? setOpen(true) : close())}
      label={label}
      side="bottom"
      align="end"
      className="hx-acc-pop"
      trigger={
        <button type="button" className="hx-acc-role" aria-label={label} disabled={disabled}>
          <span className="hx-acc-role__text">{canLabel(role)}</span>
          <span className="hx-acc-role__short">{roleLabel(role)}</span>
          {showTerm && term && term.value !== null && <span className="hx-acc-role__term">{termText}</span>}
          <Icon name="chevron-down" size="sm" />
        </button>
      }
    >
      <div role="menu" aria-label={label} className="hx-acc-menu">
        {roles.map((value) => (
          <button
            key={value}
            type="button"
            role="menuitemradio"
            aria-checked={value === role}
            className="hx-acc-mi"
            onClick={() => {
              close();
              if (value !== role) onRole(value);
            }}
          >
            <span className="hx-acc-mi__text">{canLabel(value)}</span>
            <Icon name="check" size="sm" className="hx-acc-mi__check" />
          </button>
        ))}
        {term && (
          <>
            <span className="hx-acc-mi__sep" role="separator" />
            <button
              type="button"
              role="menuitem"
              aria-expanded={terms}
              className="hx-acc-mi"
              onClick={() => setTerms((on) => !on)}
            >
              <Icon name="clock" size="sm" className="hx-acc-mi__icon" />
              <span className="hx-acc-mi__text">{t('next.access.until')}</span>
              <span className="hx-acc-mi__aside">{termText}</span>
              <Icon name="chevron-down" size="sm" className="hx-acc-mi__chev" />
            </button>
            <div className="hx-acc-fold" data-open={terms}>
              <div className="hx-acc-fold__in" inert={!terms}>
                {([null, ...STEPS] as const).map((step) => (
                  <button
                    key={step ?? 'forever'}
                    type="button"
                    role="menuitemradio"
                    aria-checked={term.value === step}
                    className="hx-acc-mi hx-acc-mi--sub"
                    onClick={() => pickTerm(step)}
                  >
                    <span className="hx-acc-mi__text">
                      {step === null ? t('next.access.ttl.forever') : t(STEP_KEY[step])}
                    </span>
                    <Icon name="check" size="sm" className="hx-acc-mi__check" />
                  </button>
                ))}
                <button
                  type="button"
                  role="menuitem"
                  aria-expanded={dating}
                  className="hx-acc-mi hx-acc-mi--sub"
                  onClick={() => setDating((on) => !on)}
                >
                  <span className="hx-acc-mi__text">{t('next.access.ttl.date')}</span>
                </button>
                {dating && (
                  <input
                    type="date"
                    className="hx-acc-date"
                    aria-label={t('next.access.ttl.date')}
                    min={today()}
                    onChange={(event) => {
                      if (event.target.value) pickTerm(endOfDay(event.target.value));
                    }}
                  />
                )}
              </div>
            </div>
          </>
        )}
        {onRemove && (
          <>
            <span className="hx-acc-mi__sep" role="separator" />
            <button
              type="button"
              role="menuitem"
              className="hx-acc-mi"
              data-danger="true"
              onClick={() => {
                close();
                onRemove();
              }}
            >
              <span className="hx-acc-mi__text">{t('next.access.removeAccess')}</span>
            </button>
          </>
        )}
      </div>
    </Popover>
  );
};

export default RoleMenu;
