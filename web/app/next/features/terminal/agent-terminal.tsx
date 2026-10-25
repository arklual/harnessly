// The agent's terminal as the design draws it (.hx-term): pulled up from under the stage, its head says the folder and
// the session, the body is the agent's real CLI pane in xterm over the runtime's terminal socket. The grip changes the
// height; ⌃` and the chevron fold it. Typing goes to the agent as in its own terminal.
import { useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { useTerminalSocket } from '@/next/features/terminal/xterm-socket';
import Icon from '@/next/ui/icon';
import type { Provider } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import '@/next/styles/term.css';

const MIN_H = 160;
const DEFAULT_H = 320;

export interface IAgentTerminalProps {
  open: boolean;
  /** The socket's address without the size: the conversation's agent pane. */
  url: string;
  cwd: string;
  title: string;
  provider: Provider;
  onClose: () => void;
}

const AgentTerminal = ({ open, url, cwd, title, provider, onClose }: IAgentTerminalProps) => {
  const [height, setHeight] = useState(DEFAULT_H);
  const [drag, setDrag] = useState(false);
  const rootRef = useRef<HTMLElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const start = useRef<{ y: number; h: number } | null>(null);
  useTerminalSocket(open, url, bodyRef);

  useLayoutEffect(() => {
    rootRef.current?.style.setProperty('--term-h', `${height}px`);
  }, [height]);

  const onGripDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    start.current = { y: event.clientY, h: height };
    setDrag(true);
  };
  const onGripMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const from = start.current;
    if (!from) return;
    const max = Math.round(window.innerHeight * 0.8);
    setHeight(Math.max(MIN_H, Math.min(max, from.h + from.y - event.clientY)));
  };
  const onGripUp = () => {
    start.current = null;
    setDrag(false);
  };

  return (
    <section
      ref={rootRef}
      className="hx-term"
      data-open={open}
      data-drag={drag}
      aria-label={t('next.term.label')}
      inert={!open}
    >
      <div className="hx-term__inner">
        <div
          className="hx-term__grip"
          title={t('next.term.grip')}
          onPointerDown={onGripDown}
          onPointerMove={onGripMove}
          onPointerUp={onGripUp}
          onPointerCancel={onGripUp}
        >
          <span />
        </div>
        <div className="hx-term__head">
          <Icon name="square-terminal" size="sm" />
          <span className="hx-term__title">{t('next.term.title')}</span>
          <span className="hx-term__path">{cwd}</span>
          <span className="hx-spacer" />
          <span className="hx-term__session">
            <i className="hx-agent" data-agent={provider} aria-hidden="true" />
            {title}
          </span>
          <button
            type="button"
            className="hx-term__btn hx-tipped"
            aria-label={t('next.term.fold')}
            onClick={onClose}
          >
            <Icon name="chevron-down" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.term.foldShort')}
              <kbd>⌃</kbd>
              <kbd>`</kbd>
            </span>
          </button>
        </div>
        <div className="hx-term__body" ref={bodyRef} />
      </div>
    </section>
  );
};

export default AgentTerminal;
