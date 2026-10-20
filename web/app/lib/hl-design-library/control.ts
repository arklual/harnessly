/**
 * An agent drives the open preview of a design: which board, desktop or phone, the "States" panel and
 * the board's props. Commands go to the pages of the design listening on the library's event stream; each page
 * reports what it shows, which the agent reads back. In memory: it is about the preview open right now.
 */
export interface IDesignControl {
  board?: string;
  view?: 'desktop' | 'phone';
  panel?: 'show' | 'hide' | 'fold' | 'unfold';
  props?: Record<string, string | number | boolean>;
}

export interface IDesignState {
  at: number;
  state: unknown;
}

const PANEL = new Set(['show', 'hide', 'fold', 'unfold']);
const MAX_STATE = 4_000;

type TListener = (command: IDesignControl) => void;

const g = globalThis as unknown as {
  __ptDesignControl?: { listeners: Map<string, Set<TListener>>; states: Map<string, IDesignState> };
};
if (!g.__ptDesignControl) g.__ptDesignControl = { listeners: new Map(), states: new Map() };
const { listeners, states } = g.__ptDesignControl;

/** A command as an agent sent it, with only the known fields of the known shapes kept. */
export const parseDesignControl = (raw: unknown): IDesignControl | null => {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return null;
  const input = raw as Record<string, unknown>;
  const command: IDesignControl = {};
  if (typeof input.board === 'string' && input.board) command.board = input.board.slice(0, 300);
  if (input.view === 'desktop' || input.view === 'phone') command.view = input.view;
  if (typeof input.panel === 'string' && PANEL.has(input.panel))
    command.panel = input.panel as IDesignControl['panel'];
  if (typeof input.props === 'object' && input.props !== null && !Array.isArray(input.props)) {
    const props: Record<string, string | number | boolean> = {};
    for (const [name, value] of Object.entries(input.props).slice(0, 40)) {
      if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean')
        props[name.slice(0, 80)] = value;
    }
    if (Object.keys(props).length) command.props = props;
  }
  return Object.keys(command).length ? command : null;
};

export const onDesignControl = (ref: string, listener: TListener): (() => void) => {
  const set = listeners.get(ref) ?? new Set<TListener>();
  set.add(listener);
  listeners.set(ref, set);
  return () => {
    set.delete(listener);
    if (!set.size) listeners.delete(ref);
  };
};

/** Sends the command to every open page of the design; the number of pages it reached. */
export const sendDesignControl = (ref: string, command: IDesignControl): number => {
  const set = listeners.get(ref);
  if (!set) return 0;
  for (const listener of set) listener(command);
  return set.size;
};

export const recordDesignState = (ref: string, raw: string | null): boolean => {
  if (!raw || raw.length > MAX_STATE) return false;
  try {
    states.set(ref, { at: Date.now(), state: JSON.parse(raw) });
    return true;
  } catch {
    return false;
  }
};

export const readDesignState = (ref: string): IDesignState | null => states.get(ref) ?? null;
