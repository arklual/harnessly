import { useEffect, useState } from 'react';

import { type BuilderPool, AUTO, builderOptions, loadBuilders } from './builders';

/** «Где считать»: auto or a builder, with its load. The value is "" for auto, else the builder's id (send it as the job's
 * `builder`). `caps` are the labels the job needs; builders without them are not offered. */
export function BuilderPicker({
  value,
  onChange,
  caps = [],
  base = '',
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  caps?: readonly string[];
  base?: string;
  disabled?: boolean;
}) {
  const [pool, setPool] = useState<BuilderPool>({ hub: false, builders: [] });
  useEffect(() => {
    let live = true;
    const read = () => void loadBuilders(fetch, base).then((p) => live && setPool(p));
    read();
    const t = setInterval(read, 15_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [base]);
  const options = builderOptions(pool, caps);
  return (
    <select
      aria-label="Где считать"
      value={options.some((o) => o.value === value) ? value : AUTO}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
    >
      {options.map((o) => (
        <option key={o.value || 'auto'} value={o.value} disabled={o.disabled}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
