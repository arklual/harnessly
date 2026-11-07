// An app's own terminal (terminal.pty): the page holds the socket to the runtime (a shell on a pty inside the
// agent's sandbox, cmd/runtime routes_apps_terminal.go) and the bridge carries its bytes to the view and back. Binary
// frames are what is typed and what the shell prints; a text frame {cols, rows} sets the size; the runtime's last text
// frame {exit} says how the shell ended.
import { platformPath, runtimeSocket } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';
import { BridgeError } from './bridge';

export interface IAppTerminalEvents {
  data: (bytes: Uint8Array) => void;
  /** The shell's exit code; null when the socket went without one. */
  exit: (code: number | null) => void;
}

export interface IAppTerminal {
  write: (bytes: Uint8Array) => void;
  resize: (cols: number, rows: number) => void;
  close: () => void;
}

export interface IAppTerminalOpen {
  cols: number;
  rows: number;
  cwd: string;
  confirmed?: boolean;
}

/** The query of the terminal's socket. */
export const terminalQuery = (session: string, o: IAppTerminalOpen): string => {
  const q = new URLSearchParams({ session, cols: String(o.cols), rows: String(o.rows) });
  if (o.cwd !== '') q.set('cwd', o.cwd);
  if (o.confirmed === true) q.set('confirmed', '1');
  return q.toString();
};

/** The exit code of the runtime's last text frame, or undefined for another frame. */
export const exitOf = (text: string): number | undefined => {
  try {
    const v: unknown = JSON.parse(text);
    if (typeof v === 'object' && v !== null && 'exit' in v && typeof v.exit === 'number') return v.exit;
  } catch {
    // not a frame of the runtime's
  }
  return undefined;
};

/** Opens the socket; the promise holds once it is open. */
export const openAppTerminal = (
  m: MachineId,
  session: string,
  o: IAppTerminalOpen,
  on: IAppTerminalEvents,
): Promise<IAppTerminal> =>
  new Promise((resolve, reject) => {
    const ws = new WebSocket(runtimeSocket(platformPath(`/apps/terminal?${terminalQuery(session, o)}`), m));
    ws.binaryType = 'arraybuffer';
    let opened = false;
    let code: number | null = null;
    ws.onopen = () => {
      opened = true;
      resolve({
        write: (bytes) => {
          // A copy: the view's bytes may sit on a shared buffer, which a socket does not send.
          if (ws.readyState === WebSocket.OPEN) ws.send(new Uint8Array(bytes));
        },
        resize: (cols, rows) => {
          if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ cols, rows }));
        },
        close: () => ws.close(),
      });
    };
    ws.onmessage = (e: MessageEvent<ArrayBuffer | string>) => {
      if (typeof e.data === 'string') {
        const exit = exitOf(e.data);
        if (exit !== undefined) code = exit;
        return;
      }
      on.data(new Uint8Array(e.data));
    };
    ws.onclose = () => {
      if (!opened) {
        // A refused upgrade tells the page no reason: the right, the chat and the folder were checked before.
        reject(new BridgeError('host_unavailable', 'the terminal did not open'));
        return;
      }
      on.exit(code);
    };
  });
