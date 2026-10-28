import { useRef } from 'react';

import { t } from '../lib/i18n';

const CELLS = [0, 1, 2, -1, 3, 4, 5] as const;

/**
 * Six digits drawn as cells with a dash, over one real input (so the system keyboard, paste and the e-mail app's
 * "fill from message" all work with `autocomplete="one-time-code"`). The code is submitted by the caller when
 * `onChange` reports six digits.
 */
export const OtpInput = ({
  value,
  onChange,
  busy,
  invalid,
  disabled,
}: {
  value: string;
  onChange: (code: string) => void;
  busy?: boolean;
  invalid?: boolean;
  disabled?: boolean;
}) => {
  const input = useRef<HTMLInputElement>(null);
  return (
    <div
      className="hub-otp"
      data-busy={busy || undefined}
      data-invalid={invalid || undefined}
      onClick={() => input.current?.focus()}
    >
      {CELLS.map((i) =>
        i < 0 ? (
          <span key="dash" className="hub-otp__dash" />
        ) : (
          <span
            key={i}
            className="hub-otp__c"
            data-filled={i < value.length || undefined}
            data-active={i === value.length || undefined}
          >
            {value[i] ?? ''}
          </span>
        ),
      )}
      <input
        ref={input}
        className="hub-otp__in"
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        value={value}
        disabled={disabled}
        aria-label={t('auth.codeAria')}
        onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 6))}
      />
    </div>
  );
};
