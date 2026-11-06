// One card on the new hub's tokens for sign-in, sign-up, the invitation and the organization step. The entrance is
// motion.css's (`hx-enter`, cascaded by `--i`); the theme button is the shell's own.
import { type ReactNode, useEffect, useState } from 'react';

import { useTheme } from '@kit';

import { Btn, Icon } from '../ui';
import { useHxTheme } from '../theme';

export const AuthFrame = ({
  title,
  lead,
  children,
  footer,
}: {
  title: string;
  lead?: string;
  children: ReactNode;
  footer?: ReactNode;
}) => {
  const theme = useHxTheme();
  const { resolved, setMode } = useTheme();
  // The entrance plays once the page has painted: `data-play` flips from 0 to 1.
  const [play, setPlay] = useState('0');
  useEffect(() => {
    const id = requestAnimationFrame(() => setPlay('1'));
    return () => cancelAnimationFrame(id);
  }, []);
  return (
    <div className="hx-root hxa" data-hx-theme={theme} data-play={play}>
      <main className="hxa__card hx-enter" data-enter="up">
        <div className="hxa__brand">
          <span className="hxa__brand-dot" aria-hidden="true" />
          harnessly
          <Btn
            variant="ghost"
            shape="icon"
            size="sm"
            aria-label={resolved === 'dark' ? 'Светлая тема' : 'Тёмная тема'}
            onClick={() => setMode(resolved === 'dark' ? 'light' : 'dark')}
          >
            <Icon i={resolved === 'dark' ? 'sun' : 'moon'} />
          </Btn>
        </div>
        <div className="hxa__head hx-enter" data-enter="up" style={{ '--i': 1 } as React.CSSProperties}>
          <h1 className="hxa__title">{title}</h1>
          {lead && <p className="hxa__lead">{lead}</p>}
        </div>
        <div className="hxa__stack hx-enter" data-enter="up" style={{ '--i': 2 } as React.CSSProperties}>
          {children}
        </div>
        {footer && (
          <div className="hx-enter" data-enter="fade" style={{ '--i': 3 } as React.CSSProperties}>
            {footer}
          </div>
        )}
      </main>
    </div>
  );
};

export const Or = ({ children }: { children: ReactNode }) => <div className="hxa__or">{children}</div>;

/** A message in the card: info (grey), warn or danger. */
export const Note = ({
  tone,
  icon,
  children,
}: {
  tone?: 'warn' | 'danger';
  icon?: string;
  children: ReactNode;
}) => (
  <div className="hxa__msg" data-tone={tone} role={tone === 'danger' ? 'alert' : undefined}>
    <Icon i={icon ?? (tone ? 'circle-alert' : 'info')} />
    <span>{children}</span>
  </div>
);

/** A screen that explains why the way is closed (expired, closed, not trusted) and gives one next step. */
export const Problem = ({
  title,
  text,
  tone = 'warn',
  icon = 'triangle-alert',
  actions,
}: {
  title: string;
  text: string;
  tone?: 'warn' | 'danger';
  icon?: string;
  actions?: ReactNode;
}) => (
  <div className="hxa__center" role="alert">
    <span className="hxa__badge" data-tone={tone}>
      <Icon i={icon} />
    </span>
    <strong>{title}</strong>
    <span>{text}</span>
    {actions && <div className="hxa__actions">{actions}</div>}
  </div>
);
