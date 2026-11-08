// A view asks to open a chat (pane.open "chat:<id>", the Коворкинг app of the fork), and the chat opens the way the view
// is shown:
// - an app's own page with a chat beside it (/apps/<id>?chat=…): the chat beside it changes, the layout stays;
// - an app's own page alone (kept open on a second screen, say): the chat goes to the page of this browser the person
//   was in last, over a BroadcastChannel, and a new tab opens when there is none;
// - a pane tab of a chat: this page goes to the chat, and the app's tab opens in its pane too.
import { appBasePath } from '@/platform/config';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { openAppTab } from '@/next/features/app-views/open';
import { createConversation, deleteConversation, resumeConversation, stopTerminal } from '@/next/commands';
import { conversationsStore } from '@/next/stores/conversations';
import { failureText, settled } from '@/next/bindings/outcome';
import { BridgeError } from '@/next/features/app-views/bridge';
import { defaultCwdStore } from '@/next/stores/projects';
import type { ConvId, MachineId } from '@/next/protocol/v2';

const CHANNEL = 'hx-chat-handoff';
const ASK_MS = 250;
const CHAT_ID = /^[\w:-]{1,128}$/;

type Msg =
  | { t: 'who'; ask: string }
  | { t: 'here'; ask: string; from: string; focus: number }
  | { t: 'go'; to: string; id: string };

const me = Math.random().toString(36).slice(2);

// The chats an app's own page moved to because its view asked: the page keeps the view as it is for them (no new
// session, no reload) and only tells it which chat is beside it now.
const viewSwitches = new Set<string>();
export const switchedByView = (appId: string, chat: string | null) => chat !== null && viewSwitches.has(`${appId}|${chat}`);
let lastFocus = 0;

const appPage = (): string | null => {
  const m = /^\/apps\/([^/?#]+)/.exec(window.location.pathname.slice(appBasePath().replace(/\/+$/, '').length));
  return m ? decodeURIComponent(m[1] ?? '') : null;
};
const onAppPage = () => appPage() !== null;

const channel = (): BroadcastChannel | null => {
  try {
    return typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL);
  } catch {
    return null;
  }
};

/** Answers other pages of this browser: a page with chats says when it was in focus last, and takes a chat it is given. */
export const listenChatHandoff = (): (() => void) => {
  const ch = channel();
  if (!ch) return () => {};
  if (document.hasFocus()) lastFocus = Date.now();
  const focused = () => {
    lastFocus = Date.now();
  };
  window.addEventListener('focus', focused);
  ch.onmessage = (event: MessageEvent<Msg>) => {
    const m = event.data;
    if (!m || typeof m !== 'object') return;
    if (m.t === 'who' && !onAppPage()) ch.postMessage({ t: 'here', ask: m.ask, from: me, focus: lastFocus } satisfies Msg);
    if (m.t === 'go' && m.to === me && CHAT_ID.test(m.id)) {
      goTo({ kind: ROUTE.chat, id: m.id });
      window.focus();
    }
  };
  return () => {
    window.removeEventListener('focus', focused);
    ch.close();
  };
};

/**
 * The Коворкинг's empty desk: `resume:<id>` wakes a stopped chat (its agent starts) and opens it; `new:<provider>:<project
 * id or ->` starts a chat in that project, or in the machine's default folder for `-`, and opens it. Both open the way
 * openChat does. `stop:<id>` ends the chat's terminal (`stop!:<id>` also a working one); its history stays and the
 * next message resumes it. A chat nobody wrote to yet has nothing to resume on, so the machine will not stop it: such a
 * chat is deleted instead (it holds nothing to lose). False for a target that is none of these.
 */
export const openWork = async (target: string, appId: string, machine: MachineId): Promise<boolean> => {
  const stop = /^stop(!?):([\w:-]{1,128})$/.exec(target);
  if (stop?.[2]) {
    const id = stop[2] as ConvId;
    const stopped = await stopTerminal(machine, id, stop[1] === '!').done;
    const record = conversationsStore.get(id);
    if (stopped.k === 'failed' && stopped.failure.r === 'refused' && stopped.failure.code === 'unsupported' &&
        record && record.lastTurn === null) {
      await settled(deleteConversation(machine, id, true)).catch((e: unknown) => {
        throw new BridgeError('policy_blocked', e instanceof Error ? e.message : String(e));
      });
      return true;
    }
    try {
      if (stopped.k === 'failed') throw new Error(failureText(stopped.failure));
    } catch (e) {
      // the machine's refusal in words (a working agent, a terminal that does not run): the view shows it
      throw new BridgeError('policy_blocked', e instanceof Error ? e.message : String(e));
    }
    return true;
  }
  const wake = /^resume:([\w:-]{1,128})$/.exec(target);
  if (wake?.[1]) {
    const id = wake[1] as ConvId;
    // a chat whose agent is on already answers no: it opens all the same
    await resumeConversation(machine, id).done.catch(() => undefined);
    return openChat(id, appId);
  }
  const fresh = /^new:(claude|codex):([\w.:-]{1,128})$/.exec(target);
  if (fresh?.[1] && fresh[2]) {
    const cwd = defaultCwdStore.get(machine);
    const where = fresh[2] !== '-' ? { projectId: fresh[2] } : cwd ? { cwd } : null;
    if (!where) return false;
    const created = createConversation(machine, { where, provider: fresh[1] as 'claude' | 'codex', model: null });
    const id = await created.conversation;
    return id ? openChat(id, appId) : false;
  }
  return false;
};

/** Opens chat id for the view of app appId, in the way that view is shown (see the top of the file). */
export const openChat = async (id: string, appId: string): Promise<boolean> => {
  if (!CHAT_ID.test(id)) return false;
  const page = appPage();
  if (page === null) {
    goTo({ kind: ROUTE.chat, id });
    // the pane of the chat it goes to mounts after the move: the app's tab comes along once it listens
    setTimeout(() => openAppTab(`app:${appId}`), 300);
    return true;
  }
  if (new URLSearchParams(window.location.search).get('chat')) {
    viewSwitches.add(`${page}|${id}`);
    goTo({ kind: ROUTE.app, id: page, chat: id });
    return true;
  }
  const ch = channel();
  const href = `${appBasePath().replace(/\/+$/, '')}${routePath({ kind: ROUTE.chat, id })}`;
  if (!ch) {
    window.open(href, 'hx-main');
    return true;
  }
  const ask = Math.random().toString(36).slice(2);
  const pages: { from: string; focus: number }[] = [];
  ch.onmessage = (event: MessageEvent<Msg>) => {
    const m = event.data;
    if (m && m.t === 'here' && m.ask === ask && m.from !== me) pages.push({ from: m.from, focus: m.focus });
  };
  ch.postMessage({ t: 'who', ask } satisfies Msg);
  await new Promise((resolve) => setTimeout(resolve, ASK_MS));
  const best = pages.sort((a, b) => b.focus - a.focus)[0];
  if (best) ch.postMessage({ t: 'go', to: best.from, id } satisfies Msg);
  else window.open(href, 'hx-main');
  ch.close();
  return true;
};
