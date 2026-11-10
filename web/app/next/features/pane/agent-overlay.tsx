// What the person sees of the agent in the browser tab (HRN-678): its cursor over the page, going to the element it
// acts on with the agent's words for it. Nothing else (HRN-681): no bar, no question.
import { useLayoutEffect, useRef, type RefObject } from 'react';
import { PRESENCE, usePresence } from '@/next/ui/presence';

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
  /** The page's box in the stage: the proxy's frame, or the live browser's picture. */
  frame: RefObject<HTMLElement | null>;
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
