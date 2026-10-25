// A term for a role or a link: an hour, a day, 7 or 30 days, up to a date, or for ever. The button shows the one in
// force («до 30.10» for a date); the gateway counts a step from its own clock, so a step is sent as it is named.
import { useState } from 'react';
import { endOfDay, TTL, type TTtlValue } from '@/next/features/designly/access/api';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { t } from '@/next/i18n';

export const STEPS = [TTL.hour, TTL.day, TTL.week, TTL.month] as const;
export const STEP_KEY: Record<(typeof STEPS)[number], string> = {
  '1h': 'next.access.ttl.hour',
  '1d': 'next.access.ttl.day',
  '7d': 'next.access.ttl.week',
  '30d': 'next.access.ttl.month',
};

const isStep = (v: TTtlValue): v is (typeof STEPS)[number] => STEPS.some((step) => step === v);

/** «30.10»: the day of a moment, as the person's clock has it. */
export const dayLabel = (iso: string): string | null => {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return null;
  return `${String(at.getDate()).padStart(2, '0')}.${String(at.getMonth() + 1).padStart(2, '0')}`;
};

/** The term in words; an unknown one (not a step, not a date) says nothing: null. */
export const ttlLabel = (value: TTtlValue): string | null => {
  if (value === null) return t('next.access.ttl.forever');
  if (isStep(value)) return t(STEP_KEY[value]);
  const day = dayLabel(value);
  return day === null ? null : t('next.access.ttl.until', { date: day });
};

export const today = () => {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
};

interface ITtlPickerProps {
  label: string;
  value: TTtlValue;
  onChange: (value: TTtlValue) => void;
  /** A link lives at most 7 days: no date, no for ever. */
  steps?: readonly (typeof STEPS)[number][];
  withDate?: boolean;
  withForever?: boolean;
  disabled?: boolean;
  /** Quiet words before the term on the button («Ссылка живёт 7 дней»). */
  prefix?: string;
}

const TtlPicker = ({
  label,
  value,
  onChange,
  steps = STEPS,
  withDate = true,
  withForever = true,
  disabled = false,
  prefix,
}: ITtlPickerProps) => {
  const [open, setOpen] = useState(false);
  const [dating, setDating] = useState(false);
  const pick = (next: TTtlValue) => {
    setOpen(false);
    if (next !== value) onChange(next);
  };
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setDating(false);
      }}
      label={label}
      side="bottom"
      align="start"
      className="hx-acc-pop"
      trigger={
        <button type="button" className="hx-acc-pick" aria-label={label} disabled={disabled}>
          {prefix && <span className="hx-acc-pick__pre">{prefix}</span>}
          <span>{ttlLabel(value)}</span>
          <Icon name="chevron-down" size="sm" />
        </button>
      }
    >
      <div role="menu" aria-label={label}>
        {steps.map((step) => (
          <button
            key={step}
            type="button"
            role="menuitemradio"
            aria-checked={value === step}
            className="hx-option"
            onClick={() => pick(step)}
          >
            <span className="hx-option__text">
              <span className="hx-option__name">{t(STEP_KEY[step])}</span>
            </span>
            <Icon name="check" size="sm" className="hx-option__check" />
          </button>
        ))}
        {withDate && (
          <button
            type="button"
            role="menuitem"
            className="hx-option"
            aria-expanded={dating}
            onClick={() => setDating((on) => !on)}
          >
            <span className="hx-option__text">
              <span className="hx-option__name">{t('next.access.ttl.date')}</span>
            </span>
          </button>
        )}
        {withDate && dating && (
          <input
            type="date"
            className="hx-acc-date"
            aria-label={t('next.access.ttl.date')}
            min={today()}
            onChange={(event) => {
              if (event.target.value) pick(endOfDay(event.target.value));
            }}
          />
        )}
        {withForever && (
          <button
            type="button"
            role="menuitemradio"
            aria-checked={value === null}
            className="hx-option"
            onClick={() => pick(null)}
          >
            <span className="hx-option__text">
              <span className="hx-option__name">{t('next.access.ttl.forever')}</span>
            </span>
            <Icon name="check" size="sm" className="hx-option__check" />
          </button>
        )}
      </div>
    </Popover>
  );
};

export default TtlPicker;
