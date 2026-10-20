import { useId, type ReactNode } from 'react';
import { cx } from './dom';
import { TwSpinner } from './icons';

export interface ITwSwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
  /** A pessimistic save is in flight: the switch keeps its old position, shows a spinner and is locked. */
  pending?: boolean;
  id?: string;
  className?: string;
}

/** On/off switch (role="switch"). The whole row is the click target; the label names the switch. */
export const TwSwitch = ({
  checked,
  onCheckedChange,
  label,
  description,
  disabled = false,
  pending = false,
  id,
  className,
}: ITwSwitchProps) => {
  const ownId = useId();
  const switchId = id ?? ownId;
  const descriptionId = description ? `${switchId}-description` : undefined;

  return (
    <label className={cx('tw-ui-switch-row', className)} data-disabled={disabled || pending ? '' : undefined}>
      <span className="tw-ui-switch-text">
        <span className="tw-ui-switch-label">{label}</span>
        {description && (
          <span id={descriptionId} className="tw-ui-switch-description">
            {description}
          </span>
        )}
      </span>
      <button
        id={switchId}
        type="button"
        role="switch"
        aria-checked={checked}
        aria-describedby={descriptionId}
        aria-busy={pending || undefined}
        aria-disabled={pending || undefined}
        disabled={disabled}
        className="tw-ui-switch"
        onClick={() => {
          if (!pending) onCheckedChange(!checked);
        }}
      >
        <span className="tw-ui-switch-thumb">{pending && <TwSpinner />}</span>
      </button>
    </label>
  );
};
