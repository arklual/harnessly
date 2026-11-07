// The tools a view offers from its page (tools.page) and what it publishes for the agent (context.publish), HRN-808.
// A frame that shows an app of a chat keeps here what its view registered; while any is kept, the chat's page reports
// them to the runtime every few seconds (a report runs out there in 15 s, so a closed view's tools go with it) and takes
// the agent's calls, which go to the frame of the app that offers the tool and come back with its answer.
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { IPageTool } from '@/next/features/app-views/bridge';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/** How often the page renews its report while a view offers something. */
export const REPORT_MS = 5000;
/** How often the page asks for the agent's calls while a view offers tools. */
export const CALL_POLL_MS = 1000;
/** How long a call waits for the view; the runtime waits a minute, the page a little less. */
const CALL_TIMEOUT_MS = 55_000;

export interface IPageCall {
  id: string;
  app: string;
  tool: string;
  arguments: unknown;
}

interface IOffer {
  app: string;
  tools: readonly IPageTool[];
  context: unknown;
  /** Sends the call to the view; the answer comes to answerPageCall. */
  invoke: (id: string, name: string, args: unknown) => void;
  /** Asks the person whether the agent may run a tool marked confirm, showing the arguments the agent gave. */
  ask: (tool: IPageTool, shownArgs: string) => Promise<boolean>;
}

/** At most this many characters of arguments are asked about; a longer call is refused rather than shown in part. */
export const MAX_SHOWN_ARGS = 1500;
// Characters that turn or break a line of text: a key or a value with them could make the dialog say something else.
const MISLEADING = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}\u115f\u1160\u3164\uffa0]/u;

/**
 * The arguments of a call as the confirm dialog shows them: the whole JSON, nothing cut, so the person sees exactly
 * what runs. null when it cannot be shown whole and plainly: the call is then refused, not asked.
 */
export const argsText = (args: unknown): string | null => {
  let compact: string;
  try {
    compact = JSON.stringify(args ?? {}) ?? 'null';
  } catch {
    return null;
  }
  // JSON escapes the control characters of strings, so the compact form holds only what the dialog would print.
  if (compact.length > MAX_SHOWN_ARGS || MISLEADING.test(compact)) return null;
  // Indentation adds to the length, deep nesting the most: what is shown is held to the same limit and to 40 lines.
  const shown = JSON.stringify(args ?? {}, null, 2) ?? 'null';
  if (shown.length > MAX_SHOWN_ARGS || shown.split('\n').length > 40) return null;
  return shown;
};

/** The answer to a call whose arguments are too many to show the person. */
export const UNSHOWABLE =
  'the arguments are too long or hold characters that cannot be shown to the person; pass shorter, plain ones';

/** The answer to a call the person did not allow. */
export const DECLINED = 'the person did not allow this action';

type TAnswer = { result?: unknown; error?: string };

interface IChat {
  machine: MachineId;
  conversation: ConvId;
  /** By frame: a frame is one view of one app. */
  offers: Map<string, IOffer>;
  /** The calls sent to a frame and not answered yet, with the frame they went to: only that frame answers. */
  waiting: Map<string, { frame: string; done: (a: TAnswer) => void }>;
  stop: (() => void) | null;
}

const chats = new Map<string, IChat>();
const chatKey = (machine: MachineId, conversation: ConvId) => `${machine}\u0000${conversation}`;

/** The reports of a chat: one per app, the tools of all its frames and the last context published. */
export const reportsOf = (offers: Iterable<IOffer>) => {
  const byApp = new Map<string, { app: string; tools: IPageTool[]; context?: unknown }>();
  for (const o of offers) {
    const r = byApp.get(o.app) ?? { app: o.app, tools: [] };
    for (const t of o.tools) if (!r.tools.some((have) => have.name === t.name)) r.tools.push(t);
    if (o.context !== undefined) r.context = o.context;
    byApp.set(o.app, r);
  }
  return [...byApp.values()].filter((r) => r.tools.length > 0 || r.context !== undefined);
};

const report = (chat: IChat) =>
  runtimeJson<object>(
    chat.machine,
    platformPath('/apps/page-tools'),
    postJson({ conversation: chat.conversation, apps: reportsOf(chat.offers.values()) }),
  );

const answer = (chat: IChat, id: string, a: TAnswer) =>
  runtimeJson<object>(
    chat.machine,
    platformPath('/apps/page-calls/answer'),
    postJson({ conversation: chat.conversation, id, ...a }),
  ).catch(() => undefined);

/** The frame of the app that offers the tool; the newest frame wins when two views offer it. */
export const offerFor = (
  offers: Iterable<[string, IOffer]>,
  call: Pick<IPageCall, 'app' | 'tool'>,
): [string, IOffer] | null => {
  let found: [string, IOffer] | null = null;
  for (const [frame, o] of offers)
    if (o.app === call.app && o.tools.some((t) => t.name === call.tool)) found = [frame, o];
  return found;
};

const run = async (chat: IChat, call: IPageCall) => {
  const found = offerFor(chat.offers.entries(), call);
  if (found === null) {
    void answer(chat, call.id, { error: 'the view that offered this tool was closed' });
    return;
  }
  const tool = found[1].tools.find((t) => t.name === call.tool);
  if (tool?.confirm) {
    // The call runs with the very arguments the person saw.
    const shown = argsText(call.arguments);
    if (shown === null) {
      void answer(chat, call.id, { error: UNSHOWABLE });
      return;
    }
    if (!(await found[1].ask(tool, shown).catch(() => false))) {
      void answer(chat, call.id, { error: DECLINED });
      return;
    }
  }
  const timer = window.setTimeout(() => {
    if (chat.waiting.delete(call.id)) void answer(chat, call.id, { error: 'the view did not answer' });
  }, CALL_TIMEOUT_MS);
  const [frame, offer] = found;
  chat.waiting.set(call.id, {
    frame,
    done: (a) => {
      window.clearTimeout(timer);
      void answer(chat, call.id, a);
    },
  });
  offer.invoke(call.id, call.tool, call.arguments);
};

const start = (chat: IChat): (() => void) => {
  let live = true;
  let reportTimer: number | undefined;
  let callTimer: number | undefined;
  const renew = () => {
    if (!live) return;
    void report(chat)
      .catch(() => undefined)
      .finally(() => {
        if (live) reportTimer = window.setTimeout(renew, REPORT_MS);
      });
  };
  const poll = () => {
    if (!live) return;
    const offersTools = [...chat.offers.values()].some((o) => o.tools.length > 0);
    if (!offersTools) {
      callTimer = window.setTimeout(poll, CALL_POLL_MS);
      return;
    }
    runtimeJson<{ calls: IPageCall[] }>(
      chat.machine,
      platformPath(`/apps/page-calls?conversation=${encodeURIComponent(chat.conversation)}`),
    ).then(
      (r) => {
        for (const call of r.calls) void run(chat, call);
        if (live) callTimer = window.setTimeout(poll, CALL_POLL_MS);
      },
      () => {
        if (live) callTimer = window.setTimeout(poll, CALL_POLL_MS * 3);
      },
    );
  };
  renew();
  poll();
  return () => {
    live = false;
    window.clearTimeout(reportTimer);
    window.clearTimeout(callTimer);
  };
};

const chatFor = (machine: MachineId, conversation: ConvId): IChat => {
  const key = chatKey(machine, conversation);
  let chat = chats.get(key);
  if (!chat) {
    chat = { machine, conversation, offers: new Map(), waiting: new Map(), stop: null };
    chats.set(key, chat);
  }
  return chat;
};

const changed = (chat: IChat) => {
  const empty = reportsOf(chat.offers.values()).length === 0;
  if (!empty && chat.stop === null) chat.stop = start(chat);
  else if (empty && chat.stop !== null) {
    chat.stop();
    chat.stop = null;
    // Told at once that nothing is offered, so the agent does not call a view that is gone.
    void report(chat).catch(() => undefined);
  } else if (!empty) void report(chat).catch(() => undefined);
};

/** What one frame of a chat's app offers; the frame calls set as its view registers or publishes, and drop when it goes. */
export const pageOffer = (
  machine: MachineId,
  conversation: ConvId,
  frame: string,
  app: string,
  invoke: IOffer['invoke'],
  ask: IOffer['ask'],
) => {
  const chat = chatFor(machine, conversation);
  const current = (): IOffer => chat.offers.get(frame) ?? { app, tools: [], context: undefined, invoke, ask };
  return {
    tools: (tools: readonly IPageTool[]) => {
      chat.offers.set(frame, { ...current(), tools });
      changed(chat);
    },
    publish: (context: unknown) => {
      chat.offers.set(frame, { ...current(), context });
      changed(chat);
    },
    result: (id: string, a: TAnswer) => {
      const waiting = chat.waiting.get(id);
      if (!waiting || waiting.frame !== frame) return;
      chat.waiting.delete(id);
      waiting.done(a);
    },
    drop: () => {
      if (!chat.offers.delete(frame)) return;
      changed(chat);
    },
  };
};
