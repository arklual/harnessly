// The page shape of the hub's small pages (connect, network, switches, sites): a column under the brand.
import { type ReactNode } from 'react';

import { Link } from '../lib/router';

const Brand = ({ to = '/' }: { to?: string }) => (
  <Link to={to} className="hub-brand">
    <span className="hub-orb" />
    harnessly
  </Link>
);

export interface PlainPageProps {
  children: ReactNode;
  title?: string;
  /** The right side of the head. */
  side?: ReactNode;
  /** A page of tables and lists that needs the width of the window (the journal of events). */
  wide?: boolean;
}

export const PlainPage = ({ children, title, side, wide }: PlainPageProps) => (
  <div className="hub">
    <div className="hub-page" data-wide={wide ? '' : undefined}>
      <header className="hub-head">
        <Brand />
        <span className="hub-headside">{side}</span>
      </header>
      <div className="hub-page__body">
        {title && <h1 className="hub-page__title">{title}</h1>}
        {children}
      </div>
    </div>
  </div>
);
