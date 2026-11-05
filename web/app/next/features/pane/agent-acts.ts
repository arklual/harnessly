// The agent's actions on the page of the pane's browser: what the machine hands the page (GET
// /api/v2/pane-act), whether the person lets the agent act on the page's site, and what goes back (POST
// /api/v2/pane-act). The page's frame does the work (`tk:act` of the slot's bridge); the pane shows the agent's cursor.
import type { IBrowserAgentChoice } from '@/next/stores/browser-agent';

export const AGENT_OP = {
  snapshot: 'snapshot',
  /** The page's text and the state of its videos. */
  read: 'read',
  click: 'click',
  type: 'type',
  press: 'press',
  hover: 'hover',
  select: 'select',
  scroll: 'scroll',
  wait: 'wait',
  navigate: 'navigate',
  back: 'back',
  /** The site's own tools (WebMCP): list them, call one. */
  tools: 'tools',
  call: 'call',
} as const;
export type TAgentOp = (typeof AGENT_OP)[keyof typeof AGENT_OP];

const OPS = new Set<string>(Object.values(AGENT_OP));
/** What a wait may be for on a video or audio of the page. */
const WAIT_EVENTS = new Set(['ended', 'playing', 'paused']);
/** A wait for a video's event has more time than one for a text. */
export const WAIT_EVENT_MS = 25_000;

export interface IAgentAct {
  id: string;
  op: TAgentOp;
  ref: string;
  /** The agent's words for the element, shown at its cursor. */
  element: string;
  text: string;
  key: string;
  value: string;
  url: string;
  dy: number;
  ms: number;
  submit: boolean;
  /** What a wait is for on a video or audio: ended, playing, paused; '' for a text or a time. */
  event: string;
  /** A site tool's name and its arguments as JSON text (call). */
  tool: string;
  input: string;
}

/** The ops the page's frame runs itself; navigate and back are the tab's. */
export const FRAME_OPS: ReadonlySet<TAgentOp> = new Set([
  AGENT_OP.snapshot,
  AGENT_OP.read,
  AGENT_OP.click,
  AGENT_OP.type,
  AGENT_OP.press,
  AGENT_OP.hover,
  AGENT_OP.select,
  AGENT_OP.scroll,
  AGENT_OP.wait,
  AGENT_OP.tools,
  AGENT_OP.call,
]);

/** The ops aimed at an element: the cursor goes to it first. */
export const POINTED_OPS: ReadonlySet<TAgentOp> = new Set([
  AGENT_OP.click,
  AGENT_OP.type,
  AGENT_OP.hover,
  AGENT_OP.select,
]);

const str = (v: unknown, max: number): string => (typeof v === 'string' ? v.slice(0, max) : '');
const num = (v: unknown, min: number, max: number): number =>
  typeof v === 'number' && Number.isFinite(v) ? Math.max(min, Math.min(max, v)) : 0;

/** The action the machine handed over, checked field by field; null for anything else. */
export const readAgentAct = (data: unknown): IAgentAct | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (typeof m.id !== 'string' || !m.id || m.id.length > 64) return null;
  if (typeof m.op !== 'string' || !OPS.has(m.op)) return null;
  const ref = str(m.ref, 8);
  if (ref && !/^e\d{1,6}$/.test(ref)) return null;
  const tool = str(m.tool, 129);
  if (tool && !/^[A-Za-z0-9_.:-]{1,128}$/.test(tool)) return null;
  const input = typeof m.input === 'string' ? m.input : '';
  if (input.length > 8000) return null;
  const event = str(m.event, 16);
  if (event && !WAIT_EVENTS.has(event)) return null;
  return {
    id: m.id,
    op: m.op as TAgentOp,
    ref,
    element: str(m.element, 200).replace(/\s+/g, ' ').trim(),
    text: str(m.text, 4000),
    key: str(m.key, 32),
    value: str(m.value, 500),
    url: str(m.url, 2048),
    dy: num(m.dy, -20000, 20000),
    ms: num(m.ms, 0, event ? WAIT_EVENT_MS : 10000),
    submit: m.submit === true,
    event,
    tool,
    input,
  };
};

/** The site a permission is about: the host of the page's own address; null for one that is not http(s). */
export const actHost = (url: string | null): string | null => {
  if (url === null) return null;
  try {
    const u = new URL(url);
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.hostname : null;
  } catch {
    return null;
  }
};

/** Whether the person blocked the site for the agent (Settings → General); anywhere else it acts without asking. */
export const actBlocked = (choice: IBrowserAgentChoice, host: string): boolean =>
  choice.blocked.includes(host);

/** What the agent hears when it may not act: plain sentences, the agent's language. */
export const ACT_REFUSED = {
  denied: (host: string) =>
    `The person blocked ${host} for agents (Settings → General): nothing was done there. Ask them in the chat if it is needed.`,
  noPage: 'The browser tab has no page open yet: open one with browser_navigate or `harnessly open <url>`.',
  noBridge:
    'This page does not take actions (its frame did not answer): it may still be loading; wait and take a snapshot again.',
  badUrl: 'Give an http(s) address.',
} as const;

/** How long the frame has for one action: wait takes up to 10 s of its own (a video's event: its ms on top). */
export const FRAME_ACT_MS = 13_000;
/** The cursor's way to its element before the action, as long as hx-agent-cursor's move in pane.css. */
export const CURSOR_MOVE_MS = 360;
