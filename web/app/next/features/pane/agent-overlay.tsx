// What the person sees of the agent in the browser tab: its cursor over the page, going to the element it
// acts on with the agent's words for it; the bar «Агент работает со страницей · Остановить»; and the question before
// its first action on a site.
import { useLayoutEffect, useRef, type RefObject } from 'react';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence, usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';

export interface IAgentCursorAt {
  x: number;
  y: number;
  label: string;
  press: number;
}

/** The exits, as long as hx-agent-* out in pane.css. */
const OUT_MS = 180;

interface IAgentCursorProps {
  cursor: IAgentCursorAt | null;
  frame: RefObject<HTMLIFrameElement | null>;
  /** How much the page's screen is scaled in the stage. */
  fit: number;
}

/** The agent's cursor: at its element's centre in the page, scaled with the screen; it glides from one to the next. */
export const AgentCursor = ({ cursor, frame, fit }: IAgentCursorProps) => {
  const shown = usePresence(cursor, OUT_MS);
  const el = useRef<HTMLDivElement>(null);
  const value = shown.value;

  useLayoutEffect(() => {
    const node = el.current;
    // The stage is the cursor's offset parent: its ref is not set yet while a child's layout effect runs.
    const box = (node?.offsetParent as HTMLElement | null)?.getBoundingClientRect();
    const page = frame.current?.getBoundingClientRect();
    if (!node || !value || !box || !page) return;
    node.style.setProperty('--x', `${Math.round(page.left - box.left + value.x * fit)}px`);
    node.style.setProperty('--y', `${Math.round(page.top - box.top + value.y * fit)}px`);
  }, [value, fit, frame]);

  if (!value) return null;
  return (
    <div
      ref={el}
      className="hx-agent-cursor"
      data-state={shown.state === PRESENCE.closing ? 'closing' : 'open'}
      aria-hidden="true"
    >
      <svg className="hx-agent-cursor__arrow" viewBox="0 0 20 20" width="20" height="20">
        <path d="M3 2 L3 16 L7 12 L10 18 L12.5 17 L9.6 11 L15 11 Z" />
      </svg>
      {value.press > 0 && <span key={value.press} className="hx-agent-cursor__ripple" />}
      {value.label && <span className="hx-agent-cursor__label">{value.label}</span>}
    </div>
  );
};

interface IAgentBarProps {
  working: boolean;
  paused: boolean;
  onStop: () => void;
  onResume: () => void;
}

/** Over the page while the agent acts on it, and while it is stopped. */
export const AgentBar = ({ working, paused, onStop, onResume }: IAgentBarProps) => {
  const { mounted, state } = usePresentFlag(working || paused, OUT_MS);
  if (!mounted) return null;
  return (
    <div
      className="hx-agent-bar"
      role="status"
      data-state={state === PRESENCE.closing ? 'closing' : 'open'}
      data-paused={paused || undefined}
    >
      <Icon name={paused ? 'pause' : 'sparkles'} size="sm" />
      <span className="hx-agent-bar__txt">
        {paused ? t('next.pane.agent.stopped') : t('next.pane.agent.working')}
      </span>
      {paused ? (
        <button type="button" className="hx-agent-bar__btn" onClick={onResume}>
          {t('next.pane.agent.resume')}
        </button>
      ) : (
        <button type="button" className="hx-agent-bar__btn" onClick={onStop}>
          {t('next.pane.agent.stop')}
        </button>
      )}
    </div>
  );
};

interface IAgentAskProps {
  /** The site the agent wants to act on; null closes the question. */
  host: string | null;
  onAllow: () => void;
  onDeny: () => void;
}

/** The question before the agent's first action on a site; the answer is remembered for the site. */
export const AgentAsk = ({ host, onAllow, onDeny }: IAgentAskProps) => {
  const shown = usePresence(host, OUT_MS);
  if (!shown.value) return null;
  return (
    <div
      className="hx-agent-ask"
      role="alertdialog"
      aria-label={t('next.pane.agent.ask', { host: shown.value })}
      data-state={shown.state === PRESENCE.closing ? 'closing' : 'open'}
    >
      <span className="hx-agent-ask__title">{t('next.pane.agent.ask', { host: shown.value })}</span>
      <span className="hx-agent-ask__text">{t('next.pane.agent.askText')}</span>
      <span className="hx-agent-ask__row">
        <button type="button" className="hx-pickcard__btn" onClick={onDeny}>
          {t('next.pane.agent.deny')}
        </button>
        <button type="button" className="hx-pickcard__btn" data-primary onClick={onAllow}>
          {t('next.pane.agent.allow')}
        </button>
      </span>
    </div>
  );
};
