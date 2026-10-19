import '@xterm/xterm/css/xterm.css';

import { Button, Spinner, useTheme } from '@kit';
import { FitAddon } from '@xterm/addon-fit';
import { type ITheme, Terminal } from '@xterm/xterm';
import { useEffect, useRef, useState } from 'react';

import { socketUrl } from '../api/base';
import { ReconnectingSocket } from '../api/socket';
import {
  decodeFrame,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
  terminalClosedForGood,
} from '../api/terminal-protocol';

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** The terminal's colours from the theme's tokens (xterm needs concrete values). */
function terminalTheme(): ITheme {
  return {
    background: cssVar('--k-term-bg'),
    foreground: cssVar('--k-term-fg'),
    cursor: cssVar('--k-term-cursor'),
    cursorAccent: cssVar('--k-term-bg'),
    selectionBackground: cssVar('--k-term-selection'),
  };
}

export interface TerminalViewProps {
  /** The tmux session the tab runs in. */
  session: string;
  focus?: boolean;
  className?: string;
  /** Starts the tab's session again after it ended. */
  onRestart?: () => Promise<unknown>;
}

/** A tab's tmux session in xterm.js over the runtime's terminal socket. */
export function TerminalView({ session, focus, className, onRestart }: TerminalViewProps) {
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const { resolved } = useTheme();
  const [state, setState] = useState<'connecting' | 'open' | 'ended'>('connecting');
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const t = new Terminal({
      fontFamily: cssVar('--k-font-mono') || 'monospace',
      fontSize: 13,
      lineHeight: 1.2,
      cursorBlink: true,
      allowProposedApi: false,
      scrollback: 5000,
      theme: terminalTheme(),
    });
    const fit = new FitAddon();
    t.loadAddon(fit);
    t.open(el);
    term.current = t;
    try {
      fit.fit();
    } catch {
      // not laid out yet; the observer fits it
    }

    let ended = false;
    const sock = new ReconnectingSocket({
      url: () =>
        socketUrl(`/api/terminal?session=${encodeURIComponent(session)}&cols=${t.cols}&rows=${t.rows}`),
      onState: (s) => !ended && setState(s === 'open' ? 'open' : 'connecting'),
      onOpen: (send) => send(encodeResize(t.cols, t.rows)),
      onMessage: (data) => {
        if (typeof data === 'string') return;
        const f = decodeFrame(data);
        if (f.type === 'output') t.write(f.data);
      },
      shouldRetry: (ev) => {
        if (terminalClosedForGood(ev.code, ev.reason)) {
          ended = true;
          setState('ended');
          return false;
        }
        return true;
      },
    });
    const input = t.onData((d) => sock.send(encodeInput(d)));
    const binary = t.onBinary((d) => {
      const bytes = Uint8Array.from(d, (c) => c.charCodeAt(0));
      const out = new Uint8Array(bytes.length + 1);
      out.set(bytes, 1);
      sock.send(out);
    });
    const resized = t.onResize(({ cols, rows }) => sock.send(encodeResize(cols, rows)));
    const beat = setInterval(() => sock.send(encodeHeartbeat()), 30_000);
    const observer = new ResizeObserver(() => {
      if (el.offsetWidth > 0 && el.offsetHeight > 0) {
        try {
          fit.fit();
        } catch {
          // a terminal being disposed
        }
      }
    });
    observer.observe(el);

    return () => {
      observer.disconnect();
      clearInterval(beat);
      input.dispose();
      binary.dispose();
      resized.dispose();
      sock.close();
      t.dispose();
      term.current = null;
    };
  }, [session, generation]);

  useEffect(() => {
    if (term.current) term.current.options.theme = terminalTheme();
  }, [resolved]);

  useEffect(() => {
    if (focus && state === 'open') term.current?.focus();
  }, [focus, state]);

  return (
    <div className={`terminal ${className ?? ''}`} data-state={state} data-testid="terminal">
      <div ref={host} className="terminal__screen" />
      {state === 'connecting' && (
        <div className="terminal__overlay terminal__overlay--soft">
          <Spinner size="s" /> Connecting…
        </div>
      )}
      {state === 'ended' && (
        <div className="terminal__overlay">
          <span>The session has ended.</span>
          <Button
            size="xs"
            onClick={async () => {
              await onRestart?.();
              setGeneration((g) => g + 1);
            }}
          >
            {onRestart ? 'Restart' : 'Reconnect'}
          </Button>
        </div>
      )}
    </div>
  );
}
