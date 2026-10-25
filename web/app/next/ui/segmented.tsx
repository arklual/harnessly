// One of a few choices side by side (radio group), after .hx-gate__seg of the design.
interface ISegmentedProps<V extends string> {
  label: string;
  value: V;
  options: readonly { value: V; label: string }[];
  onChange: (value: V) => void;
  disabled?: boolean;
}

const Segmented = <V extends string>({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: ISegmentedProps<V>) => (
  <div className="hx-segmented" role="radiogroup" aria-label={label} aria-disabled={disabled || undefined}>
    {options.map((option) => (
      <button
        key={option.value}
        type="button"
        role="radio"
        aria-checked={option.value === value}
        disabled={disabled}
        onClick={() => onChange(option.value)}
      >
        {option.label}
      </button>
    ))}
  </div>
);

export default Segmented;
