// A row of fixed amounts to pick a limit from (no native range, no free typing of a number that may be wrong). Amounts
// below the floor (what is already spent) are locked; the current value is marked.
import { inRubles, shownSymbol } from '../../money';
import { kRub, thousand } from './format';

export const LimitSteps = ({
  label,
  steps,
  value,
  current,
  floorRub,
  disabled,
  onPick,
}: {
  label: string;
  steps: number[];
  value: number;
  current: number | null;
  floorRub: number;
  disabled?: boolean;
  onPick: (rub: number) => void;
}) => {
  const sel = steps.indexOf(value);
  return (
    <div className="ht-steps">
      <div className="ht-steps__head">
        <span>{inRubles() ? `тыс ${shownSymbol()}` : shownSymbol()}</span>
        <span className="ht-steps__val">
          {current !== null && current !== value && <s>{kRub(current)}</s>} <b>{kRub(value)}</b>
        </span>
      </div>
      <div className="ht-steps__row" role="radiogroup" aria-label={label}>
        {steps.map((v, i) => (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={i === sel}
            disabled={disabled || v < floorRub}
            className="ht-stop"
            data-before={i > 0 && i <= sel ? 'on' : undefined}
            data-after={i < steps.length - 1 && i < sel ? 'on' : undefined}
            data-cur={v === current || undefined}
            onClick={() => onPick(v)}
          >
            <i className="ht-stop__dot" />
            <span className="ht-stop__t">{thousand(v).replace(' тыс', '')}</span>
          </button>
        ))}
      </div>
    </div>
  );
};
