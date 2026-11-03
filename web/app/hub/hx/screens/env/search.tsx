// The search field and the kind segment of the board's bar.
import { Icon } from '../../ui';
import { KIND_FILTERS, type KindFilter } from './helpers';

export const SearchField = ({
  value,
  onChange,
  placeholder,
  label,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  label: string;
}) => (
  <label className="mo-search">
    <Icon i="search" size="sm" />
    <input
      type="search"
      placeholder={placeholder}
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onChange('');
      }}
    />
    {value !== '' && (
      <button type="button" className="mo-search__x" aria-label="Очистить" onClick={() => onChange('')}>
        <Icon i="x" size="sm" />
      </button>
    )}
  </label>
);

export const Segment = <T extends string>({
  items,
  value,
  onChange,
  label,
}: {
  items: readonly { id: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  label: string;
}) => (
  <div className="hx-set__seg" role="radiogroup" aria-label={label}>
    {items.map((it) => (
      <button
        key={it.id}
        type="button"
        className="hx-set__seg-item"
        role="radio"
        aria-checked={it.id === value ? 'true' : 'false'}
        onClick={() => onChange(it.id)}
      >
        {it.label}
      </button>
    ))}
  </div>
);

export const KindSegment = ({
  value,
  onChange,
}: {
  value: KindFilter;
  onChange: (v: KindFilter) => void;
}) => <Segment items={KIND_FILTERS} value={value} onChange={onChange} label="Что показать" />;
