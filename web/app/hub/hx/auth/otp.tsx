import { useRef } from 'react';

import { t } from '../../lib/i18n';

const CELLS = [0, 1, 2, -1, 3, 4, 5] as const;

/** Six digits drawn as cells over one real input (system keyboard, paste and «fill from message» work). */
export const HxOtp = ({
  value,
  onChange,
  busy,
  invalid,
}: {
  value: string;
  onChange: (code: string) => void;
  busy?: boolean;
  invalid?: boolean;
}) => {
  const input = useRef<HTMLInputElement>(null);
  return (
    <div
      className="hxa__otp"
      data-busy={busy || undefined}
      data-invalid={invalid || undefined}
      onClick={() => input.current?.focus()}
    >
      {CELLS.map((i) =>
        i < 0 ? (
          <span key="dash" className="hxa__otp-dash" />
        ) : (
          <span key={i} className="hxa__otp-c" data-active={i === value.length || undefined}>
            {value[i] ?? ''}
          </span>
        ),
      )}
      <input
        ref={input}
        className="hxa__otp-in"
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        value={value}
        autoFocus
        aria-label={t('auth.codeAria')}
        onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 6))}
      />
    </div>
  );
};
