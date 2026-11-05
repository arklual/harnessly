// The agent's hands in the open browser tab: while the page is in view, the tab waits for the next action the
// chat's agent asks for, moves the agent's cursor to the element, has the page's frame do the action and tells the
// machine how it ended. One action at a time. The agent is not asked for on a site: the person sees its
// cursor, and the sites they blocked in Settings → General refuse at once.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import {
  ACT_REFUSED,
  AGENT_OP,
  CURSOR_MOVE_MS,
  FRAME_ACT_MS,
  FRAME_OPS,
  POINTED_OPS,
  actBlocked,
  actHost,
  readAgentAct,
  type IAgentAct,
} from '@/next/features/pane/agent-acts';
import { PANE_ACT_STATE, PaneActClosed, answerPaneAct, pollPaneAct } from '@/next/core/net/pane-act';
import { browserAgentSnapshot } from '@/next/stores/browser-agent';
import { parseAddress } from '@/next/features/pane/address';
import type { IFrameActed } from '@/next/features/pane/frame-bridge';
import type { IFrameAct } from '@/next/features/pane/use-frame-bridge';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/** Where the agent's cursor is: the centre of its element in the page's viewport, and what it does there. */
export interface IAgentCursor {
  x: number;
  y: number;
  label: string;
  /** Bumped on every press, for the cursor's ripple. */
  press: number;
}

interface IAgentActsArgs {
  machine: MachineId;
  conv: ConvId;
  /** The page's own address now; null while the tab has none. */
  pageUrl: () => string | null;
  act: (message: IFrameAct, timeoutMs: number) => Promise<IFrameActed | null>;
  nextHello: (timeoutMs: number) => Promise<boolean>;
  back: () => void;
  /** Opens an address in the tab. */
  go: (next: NonNullable<ReturnType<typeof parseAddress>>) => void;
}

interface IOutcome {
  ok: boolean;
  text: string;
}

/** How long the cursor stays after the last action. */
const CURSOR_STAYS_MS = 4000;

const sleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, ms);
    signal.addEventListener(
      'abort',
      () => {
        window.clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });

const whenVisible = (signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const check = () => {
      if (document.visibilityState !== 'visible' && !signal.aborted) return;
      document.removeEventListener('visibilitychange', check);
      resolve();
    };
    document.addEventListener('visibilitychange', check);
    signal.addEventListener('abort', check, { once: true });
    check();
  });

const center = (rect: IFrameActed['rect']) =>
  rect ? { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 } : null;

export const useAgentActs = ({ machine, conv, pageUrl, act, nextHello, back, go }: IAgentActsArgs) => {
  const [cursor, setCursor] = useState<IAgentCursor | null>(null);
  const workTimer = useRef(0);

  /** The cursor goes away a moment after the last action. */
  const busy = () => {
    window.clearTimeout(workTimer.current);
    workTimer.current = window.setTimeout(() => setCursor(null), CURSOR_STAYS_MS);
  };

  const frameOp = async (a: IAgentAct, host: string): Promise<IOutcome> => {
    if (POINTED_OPS.has(a.op) || (a.op === AGENT_OP.scroll && a.ref)) {
      const located = await act({ id: `${a.id}:locate`, op: 'locate', ref: a.ref, host }, 3000);
      if (!located) return { ok: false, text: ACT_REFUSED.noBridge };
      if (!located.ok) return { ok: false, text: located.text };
      const at = center(located.rect);
      if (at) setCursor((was) => ({ ...at, label: a.element, press: was?.press ?? 0 }));
      await new Promise((resolve) => window.setTimeout(resolve, CURSOR_MOVE_MS));
    } else {
      const label = a.op === AGENT_OP.press ? a.key : a.element;
      setCursor((was) => (was ? { ...was, label } : was));
    }
    if (a.op === AGENT_OP.click) setCursor((was) => (was ? { ...was, press: was.press + 1 } : was));
    // A press that takes the page elsewhere unloads the frame before it answers: the next page's hello is the answer.
    // Listened for before the press, since it may come before the frame's load is heard.
    const moves =
      a.op === AGENT_OP.click ||
      a.op === AGENT_OP.press ||
      a.op === AGENT_OP.call ||
      (a.op === AGENT_OP.type && a.submit);
    const arrived = moves ? nextHello(FRAME_ACT_MS + 8000) : Promise.resolve(false);
    const acted = await act(
      {
        id: a.id,
        op: a.op,
        ref: a.ref,
        text: a.text,
        key: a.key,
        value: a.value,
        dy: a.dy,
        ms: a.ms,
        submit: a.submit,
        event: a.event,
        tool: a.tool,
        input: a.input,
        host,
      },
      FRAME_ACT_MS + (a.op === AGENT_OP.wait && a.event ? a.ms : 0),
    );
    if (!acted) {
      if (await arrived)
        return {
          ok: true,
          text: `Done; the page went to ${pageUrl() ?? 'another address'}. Take a snapshot.`,
        };
      return { ok: false, text: ACT_REFUSED.noBridge };
    }
    const at = center(acted.rect);
    if (at) setCursor((was) => ({ ...at, label: a.element, press: was?.press ?? 0 }));
    return { ok: acted.ok, text: acted.text };
  };

  const run = useEffectEvent(async (a: IAgentAct): Promise<IOutcome> => {
    const target = a.op === AGENT_OP.navigate ? parseAddress(a.url) : null;
    if (a.op === AGENT_OP.navigate && (!target || !/^https?:\/\//i.test(a.url)))
      return { ok: false, text: ACT_REFUSED.badUrl };
    const host = actHost(a.op === AGENT_OP.navigate ? a.url : pageUrl());
    if (host === null) return { ok: false, text: ACT_REFUSED.noPage };

    if (actBlocked(browserAgentSnapshot(), host)) return { ok: false, text: ACT_REFUSED.denied(host) };

    busy();
    if (a.op === AGENT_OP.navigate && target) {
      go(target);
      const loaded = await nextHello(15_000);
      return {
        ok: true,
        text: loaded
          ? `Opened ${a.url}. Take a snapshot to see it.`
          : `Asked the tab to open ${a.url}; it has not finished loading yet.`,
      };
    }
    if (a.op === AGENT_OP.back) {
      back();
      const loaded = await nextHello(8000);
      return { ok: true, text: loaded ? `Went back to ${pageUrl() ?? 'the previous page'}.` : 'Went back.' };
    }
    if (FRAME_OPS.has(a.op)) {
      const outcome = await frameOp(a, host);
      busy();
      return outcome;
    }
    return { ok: false, text: `Unknown action ${a.op}.` };
  });

  useEffect(() => {
    const abort = new AbortController();
    const { signal } = abort;
    const loop = async () => {
      let failures = 0;
      while (!signal.aborted) {
        if (document.visibilityState !== 'visible') {
          await whenVisible(signal);
          continue;
        }
        let data: unknown;
        try {
          data = await pollPaneAct(machine, conv, signal);
          failures = 0;
        } catch (failure) {
          // An older machine has no such door: nothing to wait for in this tab.
          if (signal.aborted || failure instanceof PaneActClosed) return;
          failures += 1;
          await sleep(Math.min(30_000, 1000 * 2 ** failures), signal);
          continue;
        }
        const a = readAgentAct(data);
        if (!a) continue;
        const outcome = await run(a);
        await answerPaneAct(
          machine,
          conv,
          a.id,
          outcome.ok ? PANE_ACT_STATE.done : PANE_ACT_STATE.failed,
          outcome.text,
        );
      }
    };
    void loop();
    return () => {
      abort.abort();
      window.clearTimeout(workTimer.current);
    };
  }, [machine, conv]);

  return { cursor };
};
