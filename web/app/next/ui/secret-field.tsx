// A field for a secret (an API key): masked, with an eye to show what was typed. It never gets a value from outside to
// show: only what the person typed in this field is there, and it goes with the field.
import { useState, type KeyboardEvent, type Ref } from 'react';
import { t } from '@/next/i18n';
import '@/next/styles/providers.css';
import Icon from '@/next/ui/icon';

interface ISecretFieldProps {
  value: string;
  onChange: (value: string) => void;
  label: string;
  placeholder?: string;
  disabled?: boolean;
  invalid?: boolean;
  onSubmit?: () => void;
  ref?: Ref<HTMLInputElement>;
}

const SecretField = ({
  value,
  onChange,
  label,
  placeholder,
  disabled = false,
  invalid = false,
  onSubmit,
  ref,
}: ISecretFieldProps) => {
  const [shown, setShown] = useState(false);
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' && onSubmit) onSubmit();
  };
  return (
    <div className="hx-secret" data-invalid={invalid || undefined}>
      <input
        ref={ref}
        className="hx-set__input hx-set__input--mono hx-secret__input"
        type={shown ? 'text' : 'password'}
        value={value}
        disabled={disabled}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        placeholder={placeholder}
        aria-label={label}
        aria-invalid={invalid || undefined}
        data-1p-ignore=""
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKey}
      />
      <button
        type="button"
        className="hx-secret__eye"
        aria-label={shown ? t('next.secret.hide') : t('next.secret.show')}
        aria-pressed={shown}
        onClick={() => setShown((on) => !on)}
      >
        <Icon name={shown ? 'eye-off' : 'eye'} size="sm" />
      </button>
    </div>
  );
};

export default SecretField;
