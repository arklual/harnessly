import { isHot, pctText, sparkPath } from './geometry';

/** A row of «Нагрузка за сутки» of the machine card: the label, the 24-hour line and the latest value. */
export const LoadRow = ({
  label,
  values,
  off,
}: {
  label: string;
  values: readonly number[];
  off: boolean;
}) => {
  const shown = off ? [] : values;
  const now = shown.length > 0 ? (shown[shown.length - 1] ?? null) : null;
  const hot = isHot(now);
  return (
    <div className="md-load__row">
      <span>{label}</span>
      <svg viewBox="0 0 100 24" preserveAspectRatio="none" aria-hidden="true">
        <path d={sparkPath(shown)} data-hot={hot ? '1' : '0'} />
      </svg>
      <b data-hot={hot ? '1' : '0'}>{pctText(now)}</b>
    </div>
  );
};
