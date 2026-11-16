// «Чей проект» (features/people): everyone, or one of the people of the machine; the project's settings and the dialog
// of a new project ask it. Nothing is drawn while the machine names nobody.
import { t } from '@/next/i18n';

interface IOwnerPickProps {
  /** The people of the machine; with none the pick is not there. */
  people: readonly string[];
  /** '' for everyone's. */
  value: string;
  disabled?: boolean;
  onPick: (owner: string) => void;
}

const OwnerPick = ({ people, value, disabled = false, onPick }: IOwnerPickProps) => {
  if (people.length === 0) return null;
  // everyone first, then the people the machine names, and an owner it no longer names, so it can be given away
  const owners = ['', ...people, ...(value !== '' && !people.includes(value) ? [value] : [])];
  return (
    <div className="hx-create__f">
      <span className="hx-create__l">{t('next.people.owner')}</span>
      <div className="hx-owner" role="radiogroup" aria-label={t('next.people.owner')}>
        {owners.map((choice) => (
          <button
            key={choice}
            type="button"
            role="radio"
            className="hx-owner__opt"
            aria-checked={value === choice}
            disabled={disabled}
            onClick={() => onPick(choice)}
          >
            {choice === '' ? t('next.people.everyone') : choice}
          </button>
        ))}
      </div>
      <span className="hx-create__hint">{t('next.people.ownerHint')}</span>
    </div>
  );
};

export default OwnerPick;
