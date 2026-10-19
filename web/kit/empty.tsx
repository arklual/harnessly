import type { ReactNode } from 'react';

import { cx } from './cx';

export interface EmptyStateProps {
  icon?: ReactNode;
  title: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  className?: string;
}

export function EmptyState({ icon, title, children, actions, className }: EmptyStateProps) {
  return (
    <div className={cx('k-empty', className)}>
      {icon != null && <div className="k-empty__icon">{icon}</div>}
      <div className="k-empty__title">{title}</div>
      {children != null && <div className="k-empty__text">{children}</div>}
      {actions != null && <div className="k-empty__actions">{actions}</div>}
    </div>
  );
}
