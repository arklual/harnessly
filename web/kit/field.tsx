import {
  type InputHTMLAttributes,
  type ReactNode,
  type Ref,
  type TextareaHTMLAttributes,
  useId,
} from 'react';

import { cx } from './cx';

interface FieldFrameProps {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  id: string;
  className?: string;
  children: ReactNode;
}

function FieldFrame({ label, hint, error, id, className, children }: FieldFrameProps) {
  return (
    <div className={cx('k-field', className)} data-invalid={error ? true : undefined}>
      {label != null && (
        <label className="k-field__label" htmlFor={id}>
          {label}
        </label>
      )}
      {children}
      {error != null ? (
        <div className="k-field__error" id={`${id}-msg`} role="alert">
          {error}
        </div>
      ) : (
        hint != null && (
          <div className="k-field__hint" id={`${id}-msg`}>
            {hint}
          </div>
        )
      )}
    </div>
  );
}

export interface TextFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size'> {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  size?: 's' | 'm' | 'l';
  /** Shown inside the frame, before the text. */
  start?: ReactNode;
  /** Shown inside the frame, after the text (a button, a unit). */
  end?: ReactNode;
  ref?: Ref<HTMLInputElement>;
  frameClassName?: string;
}

export function TextField({
  label,
  hint,
  error,
  size = 'm',
  start,
  end,
  id,
  className,
  frameClassName,
  ...rest
}: TextFieldProps) {
  const auto = useId();
  const fieldId = id ?? auto;
  return (
    <FieldFrame label={label} hint={hint} error={error} id={fieldId} className={className}>
      <div className={cx('k-input', frameClassName)} data-size={size}>
        {start != null && <span className="k-input__side">{start}</span>}
        <input
          {...rest}
          id={fieldId}
          className="k-input__control"
          aria-invalid={error ? true : undefined}
          aria-describedby={error != null || hint != null ? `${fieldId}-msg` : undefined}
        />
        {end != null && <span className="k-input__side">{end}</span>}
      </div>
    </FieldFrame>
  );
}

export interface TextAreaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  ref?: Ref<HTMLTextAreaElement>;
}

export function TextArea({ label, hint, error, id, className, ...rest }: TextAreaProps) {
  const auto = useId();
  const fieldId = id ?? auto;
  return (
    <FieldFrame label={label} hint={hint} error={error} id={fieldId} className={className}>
      <textarea
        {...rest}
        id={fieldId}
        className="k-textarea"
        aria-invalid={error ? true : undefined}
        aria-describedby={error != null || hint != null ? `${fieldId}-msg` : undefined}
      />
    </FieldFrame>
  );
}
