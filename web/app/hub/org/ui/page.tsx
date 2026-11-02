import type { ReactNode } from 'react';

import { Spinner } from '@kit';

import { orgErrorText } from '../api/errors';
import { Button, EmptyState } from '@kit';

/** The head of a hub screen: the title is the biggest thing on the page. */
export const PageHead = ({
  title,
  sub,
  actions,
}: {
  title: string;
  sub?: ReactNode;
  actions?: ReactNode;
}) => (
  <header className="ho-head">
    <div className="ho-head__text">
      <h1 className="ho-head__title">{title}</h1>
      {sub != null && <p className="ho-head__sub">{sub}</p>}
    </div>
    {actions != null && <div className="ho-head__actions">{actions}</div>}
  </header>
);

export const Page = ({ children }: { children: ReactNode }) => <div className="ho-page">{children}</div>;

/** What a screen shows while its data is on the way (null, not an empty list). */
export const Loading = () => (
  <div className="ho-loading" role="status" aria-label="Загрузка">
    <Spinner size="m" />
  </div>
);

/** A failed load, as it is, with a retry. */
export const LoadError = ({ error, retry }: { error: unknown; retry: () => void }) => (
  <EmptyState title="Не загрузилось" actions={<Button onClick={retry}>Повторить</Button>}>
    {orgErrorText(error)}
  </EmptyState>
);

/** The segmented switch of the board («Неделя | Месяц», «По командам | Списком»). */
export const Seg = <V extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: V;
  options: readonly { value: V; label: string }[];
  onChange: (v: V) => void;
  label: string;
}) => (
  <div className="ho-seg" role="radiogroup" aria-label={label}>
    {options.map((o) => (
      <button
        key={o.value}
        type="button"
        role="radio"
        aria-checked={o.value === value}
        className="ho-seg__opt"
        onClick={() => onChange(o.value)}
      >
        {o.label}
      </button>
    ))}
  </div>
);
