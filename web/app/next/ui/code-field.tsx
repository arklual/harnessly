// A one-time code typed into cells (2FA, SMS, e-mail): the cells are drawn over one real input, so typing moves on by
// itself, Backspace goes back, a pasted code fills every cell, and the phone's keyboard offers the code from a message
// (`autocomplete="one-time-code"`). The last digit hands the code over: there is no button to press. Any place that asks
// a person for a code uses it (the panel's «Ждут код», the island of notifications).
import { useRef, type Ref } from 'react';
import '@/next/styles/code-field.css';

export const CODE_FIELD = { idle: 'idle', busy: 'busy', done: 'done', invalid: 'invalid' } as const;
export type TCodeFieldState = (typeof CODE_FIELD)[keyof typeof CODE_FIELD];

interface ICodeFieldProps {
  value: string;
  onChange: (value: string) => void;
  /** Called with the code once every cell is filled. */
  onComplete: (code: string) => void;
  /** How many digits, 4 to 8. */
  length: number;
  label: string;
  state?: TCodeFieldState;
  disabled?: boolean;
  /** Offer the code from a message (`one-time-code`). Off where the code is never one sent by message (an
   *  authenticator's): Safari on a Mac takes such a field for a login one and offers its passwords over it. */
  fromMessages?: boolean;
  ref?: Ref<HTMLInputElement>;
}

/** Where the dash between the halves goes: a six-digit code reads 3–3, an eight-digit one 4–4. */
const dashAt = (length: number): number => (length % 2 === 0 && length >= 6 ? length / 2 : -1);

const CodeField = ({
  value,
  onChange,
  onComplete,
  length,
  label,
  state = CODE_FIELD.idle,
  disabled = false,
  fromMessages = true,
  ref,
}: ICodeFieldProps) => {
  const own = useRef<HTMLInputElement | null>(null);
  const setRef = (node: HTMLInputElement | null) => {
    own.current = node;
    if (typeof ref === 'function') ref(node);
    else if (ref) ref.current = node;
  };
  const dash = dashAt(length);
  const locked = disabled || state === CODE_FIELD.busy || state === CODE_FIELD.done;

  const take = (raw: string) => {
    const next = raw.replace(/\D/g, '').slice(0, length);
    onChange(next);
    if (next.length === length && next !== value) onComplete(next);
  };

  // The caret stays at the end: a digit is typed after the last one, Backspace takes the last one.
  const toEnd = () => {
    const input = own.current;
    if (input) input.setSelectionRange(input.value.length, input.value.length);
  };

  return (
    <div
      className="hx-otp"
      data-state={state}
      onPointerDown={(event) => {
        if (locked) return;
        event.preventDefault();
        own.current?.focus();
        toEnd();
      }}
    >
      {Array.from({ length }, (_, i) => (
        <span key={i} className="hx-otp__slot" data-dash={i === dash || undefined}>
          <span
            className="hx-otp__cell"
            data-filled={i < value.length || undefined}
            data-active={(i === value.length && !locked) || undefined}
          >
            {value[i] ?? ''}
          </span>
        </span>
      ))}
      <input
        ref={setRef}
        className="hx-otp__input"
        type="text"
        inputMode="numeric"
        pattern="[0-9]*"
        autoComplete={fromMessages ? 'one-time-code' : 'off'}
        autoCorrect="off"
        autoCapitalize="off"
        data-1p-ignore
        data-lpignore="true"
        data-form-type="other"
        enterKeyHint="done"
        spellCheck={false}
        maxLength={length}
        value={value}
        disabled={disabled}
        readOnly={locked}
        aria-label={label}
        aria-invalid={state === CODE_FIELD.invalid || undefined}
        onChange={(event) => take(event.target.value)}
        onFocus={toEnd}
        onSelect={toEnd}
      />
    </div>
  );
};

export default CodeField;
