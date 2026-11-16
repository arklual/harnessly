// «Продолжить в Sinopia Design» on an ```html card of the feed: the machine makes a board of the mockup — in
// the design open beside the chat or the chat's own project when the person may edit it, else in a new project — and
// the chat's pane shows it. The design goes on the chat's field, so the agent reads that the mockup is that board now
// and changes it there. The same mockup is the same board: the machine keeps the link, this browser keeps it too, so
// the button says «Открыть» after a reload and a second press makes no copy.
import { createContext, useContext, useEffect, useState } from 'react';
import { mockupToDesign } from '@/next/features/designly/api';
import { chatPaneOf } from '@/next/stores/pane-state';
import { addDraftRef } from '@/next/stores/drafts';
import {
  REFS_MAX,
  type ConvId,
  type DesignlyFromHTML,
  type DesignlyMockup,
  type MachineId,
} from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import Spinner from '@/next/ui/spinner';
import { t } from '@/next/i18n';

/** The answer the card is in: its first line names the mockup when the page has no heading. */
export const AnswerText = createContext<string>('');

/** Where the card is: a chat of a machine. Absent (a gallery, the docs), the card has no button. */
export interface IMockupHost {
  machine: MachineId;
  conv: ConvId;
  /** Opens the design beside the chat when the machine could not ask the chat's pane itself. */
  open?: (machine: MachineId, design: string, conv: ConvId) => void;
  /** Makes the board instead of the machine (a gallery's mock). */
  make?: (body: DesignlyFromHTML) => Promise<DesignlyMockup>;
}
export const MockupHost = createContext<IMockupHost | null>(null);

type TState =
  { s: 'idle' } | { s: 'busy' } | { s: 'done'; board: IKeptBoard } | { s: 'failed'; message: string };

export interface IKeptBoard {
  project: string;
  projectTitle: string;
  file: string;
}

const TITLE_MAX = 80;
const clean = (s: string): string => s.replace(/\s+/g, ' ').trim();

/** The mockup's own name: its title or its first heading; else the answer's first line of words. */
export const mockupTitle = (html: string, answer: string): string => {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const own = [doc.title, doc.querySelector('h1,h2,h3')?.textContent ?? ''].map(clean).find((x) => x !== '');
  if (own) return own.slice(0, TITLE_MAX);
  const line = answer
    .split('\n')
    .map((l) => clean(l.replace(/[#>*_`[\]]/g, '')))
    .find((l) => l !== '' && !l.startsWith('```'));
  return (line ?? '').replace(/[:.]$/, '').slice(0, TITLE_MAX);
};

/** The browser's note of the board a mockup became, by chat and source. */
const keyOf = async (conv: string, html: string): Promise<string> => {
  const bytes = new TextEncoder().encode(`${conv}\u0000${html}`);
  const sum = await crypto.subtle.digest('SHA-256', bytes);
  return `hx-mockup:${Array.from(new Uint8Array(sum).slice(0, 12), (b) => b.toString(16).padStart(2, '0')).join('')}`;
};

const readKept = (key: string): IKeptBoard | null => {
  try {
    const raw = localStorage.getItem(key);
    const v = raw ? (JSON.parse(raw) as Partial<IKeptBoard>) : null;
    return v && typeof v.project === 'string' && typeof v.file === 'string'
      ? { project: v.project, projectTitle: String(v.projectTitle ?? ''), file: v.file }
      : null;
  } catch {
    return null;
  }
};

const keep = (key: string, b: IKeptBoard): void => {
  try {
    localStorage.setItem(key, JSON.stringify(b));
  } catch {
    // A private window: the machine still knows the board, the next press finds it.
  }
};

/** The design the chat's pane has open, if any: a new mockup's board goes there. */
const openDesignOf = (conv: string): string => {
  for (const tab of chatPaneOf(conv).tabs) if (tab.kind === 'designly' && tab.design) return tab.design;
  return '';
};

interface IProps {
  html: string;
  host: IMockupHost;
}

const MockupToDesign = ({ html, host }: IProps) => {
  const answer = useContext(AnswerText);
  const [state, setState] = useState<TState>({ s: 'idle' });
  const [key, setKey] = useState<string | null>(null);

  // What this browser already knows of the mockup: the button says «Открыть» after a reload.
  useEffect(() => {
    if (typeof crypto === 'undefined' || !crypto.subtle) return;
    let live = true;
    void keyOf(host.conv, html).then((k) => {
      if (!live) return;
      setKey(k);
      const kept = readKept(k);
      if (kept) setState((s) => (s.s === 'idle' ? { s: 'done', board: kept } : s));
    });
    return () => {
      live = false;
    };
  }, [host.conv, html]);

  const press = async () => {
    if (state.s === 'busy') return;
    setState({ s: 'busy' });
    try {
      const body = {
        conv: host.conv,
        html,
        title: mockupTitle(html, answer),
        project: openDesignOf(host.conv),
      };
      const made: DesignlyMockup = await (host.make ? host.make(body) : mockupToDesign(host.machine, body));
      const board = { project: made.project, projectTitle: made.projectTitle, file: made.file };
      if (key) keep(key, board);
      // The agent of the chat reads, with the next message, that the mockup is this board now (MockupNote).
      addDraftRef(
        host.conv,
        { kind: 'design', id: made.project, title: made.projectTitle || made.title, target: made.file },
        REFS_MAX,
      );
      if (!made.opened) host.open?.(host.machine, made.project, host.conv);
      setState({ s: 'done', board });
    } catch (e) {
      const message = e instanceof Error && e.message ? e.message : t('next.rich.toDesign.failed');
      setState({ s: 'failed', message });
    }
  };

  const busy = state.s === 'busy';
  const done = state.s === 'done';
  const label = busy
    ? t('next.rich.toDesign.busy')
    : done
      ? t('next.rich.toDesign.open')
      : state.s === 'failed'
        ? t('next.rich.toDesign.retry')
        : t('next.rich.toDesign.start');
  return (
    <>
      <button
        type="button"
        className="hx-rich__btn"
        aria-label={label}
        title={label}
        aria-busy={busy || undefined}
        data-busy={busy || undefined}
        data-state={state.s}
        disabled={busy}
        onClick={() => void press()}
      >
        {busy ? <Spinner /> : <Icon name={state.s === 'failed' ? 'rotate-ccw' : 'palette'} size="sm" />}
        <span className="hx-rich__lbl">{label}</span>
      </button>
      {state.s === 'failed' && (
        <span className="hx-rich__fail" role="status" title={state.message}>
          {state.message}
        </span>
      )}
    </>
  );
};

export default MockupToDesign;
