// Small pieces the screen shares: the «?» with its tooltip, the search field, a segment, the loading skeleton.
import type { ReactNode } from 'react';

import { Btn, Icon } from '../../ui';
import { OrgApiError, orgErrorText } from '../../../org/api/errors';

/** «?»: a flat icon, a background on hover; the tooltip stands above it and never over what it explains. */
export const Help = ({ text }: { text: string }) => (
  <span className="hx-tipped ev-help" tabIndex={0} role="img" aria-label={text}>
    <Icon i="circle-question-mark" size="sm" />
    <span className="hx-tip pz-tip" data-align="start">
      {text}
    </span>
  </span>
);

export const Search = ({
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
  <label className="ev-search">
    <Icon i="search" size="sm" />
    <input
      type="search"
      placeholder={placeholder}
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && value !== '') {
          e.stopPropagation();
          onChange('');
        }
      }}
    />
  </label>
);

export const Seg = <T extends string>({
  items,
  value,
  onChange,
  label,
}: {
  items: readonly (readonly [T, string])[];
  value: T;
  onChange: (v: T) => void;
  label: string;
}) => (
  <div className="hx-set__seg" role="radiogroup" aria-label={label}>
    {items.map(([id, text]) => (
      <button
        key={id}
        type="button"
        className="hx-set__seg-item"
        role="radio"
        aria-checked={id === value ? 'true' : 'false'}
        onClick={() => onChange(id)}
      >
        {text}
      </button>
    ))}
  </div>
);

export const Skeleton = () => (
  <div className="ev-sk" aria-busy="true" aria-label="Загружаем">
    <span className="hx-sk ev-sk__bar" />
    <span className="hx-sk ev-sk__card" />
    <span className="hx-sk ev-sk__card" />
    <span className="hx-sk ev-sk__card" />
  </div>
);

export const LoadFailed = ({ error, onRetry }: { error: unknown; onRetry: () => void }) => (
  <div className="ev-fail" role="alert">
    <Icon i="circle-alert" />
    <span>{orgErrorText(error)}</span>
    <Btn size="sm" onClick={onRetry}>
      Повторить
    </Btn>
  </div>
);

export const Empty = ({ children }: { children: ReactNode }) => <div className="ev-empty">{children}</div>;

/** The feature is off for the organization: the hub answers `not_enabled`. */
export const isNotEnabled = (e: unknown): boolean => e instanceof OrgApiError && e.code === 'not_enabled';
