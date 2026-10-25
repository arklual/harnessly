import './terminal.css';
import { Button } from '@/components/ui/button';
import Spinner from '@/components/ui/spinner';
import { t } from '@/lib/i18n';
import { useTheme } from '@/platform/theme';
import { FitAddon } from '@xterm/addon-fit';
import { type ITheme, Terminal } from '@xterm/xterm';
import { useEffect, useRef, useState } from 'react';

import { socketUrl } from '@/runtime/base';
import { ReconnectingSocket } from '@/runtime/socket';
import {
  decodeFrame,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
  terminalClosedForGood,
} from '@/runtime/terminal-protocol';

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** The terminal's colours from the theme's tokens (xterm needs concrete values). */
function terminalTheme(): ITheme {
  return {
    background: cssVar('--t-bg') || '#ffffff',
    foreground: cssVar('--t-text') || '#1f1f1f',
    cursor: cssVar('--t-text') || '#1f1f1f',
    cursorAccent: cssVar('--t-bg') || '#ffffff',
    selectionBackground: cssVar('--t-accent-purple-bg') || 'rgba(120,100,255,0.25)',
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
  const { resolvedTheme: resolved } = useTheme();
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
    <div className={`terminal-view ${className ?? ''}`} data-state={state} data-testid="terminal">
      <div ref={host} className="terminal__screen" />
      {state === 'connecting' && (
        <div className="terminal__overlay terminal__overlay--soft">
          <Spinner /> {t('workspace.terminalView.connecting')}
        </div>
      )}
      {state === 'ended' && (
        <div className="terminal__overlay">
          <span>{t('workspace.terminalView.ended')}</span>
          <Button
            size="sm"
            onClick={async () => {
              await onRestart?.();
              setGeneration((g) => g + 1);
            }}
          >
            {onRestart ? t('workspace.terminalView.restart') : t('workspace.terminalView.reconnect')}
          </Button>
        </div>
      )}
    </div>
  );
}
