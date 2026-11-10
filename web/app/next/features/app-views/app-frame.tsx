// An app's view in a sandboxed frame: shown through a preview slot (so it runs on an origin of its own, never the
// page's, and the preview's DevTools see it), and talked to only through the bridge (bridge.ts).
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { slotLeases } from '@/next/features/pane/slot-lease';
import { openPreview } from '@/next/core/net/preview';
import {
  callFiles,
  callFilesMore,
  callGit,
  callRun,
  callTerminalRead,
  callAgentsRun,
  callAI,
  callSecretsPut,
  callBrowser,
  callSettings,
  callSinopia,
  callStorage,
  callTool,
  APP_STATE,
  createViewSession,
  renewViewSession,
  type IAppView,
  type IInstalledApp,
} from '@/next/features/app-views/api';
import { watchChatEvents } from '@/next/features/app-views/chat-events';
import { subscribeFileChanges } from '@/next/features/app-views/file-events';
import { askFile } from '@/next/features/app-views/file-pick';
import {
  AppBridge,
  BridgeError,
  type IBridgeHandlers,
  type IBridgeLogEntry,
  type IViewContext,
  type TTarget,
} from '@/next/features/app-views/bridge';
import AppFail from '@/next/features/app-views/app-fail';
import { logCall, logKey } from '@/next/features/app-views/bridge-log';
import { bridgeLine, consoleLine, queueViewLog } from '@/next/features/app-views/view-log-report';
import { cleanUrl, followTrace, journal, newTrace } from '@/next/core/journal';
import {
  frameReason,
  journalReason,
  journalSource,
  withTraceParam,
  type IFrameSeen,
} from '@/next/features/app-views/view-journal';
import {
  FAIL_REASON,
  HANDSHAKE_WAIT_MS,
  LOADING,
  SESSION_WAIT_MS,
  classifySessionError,
  failureOfViewError,
  keepTelling,
  nextFrameState,
  viewErrorOf,
  watchHandshake,
  withTimeout,
  type IFailure,
  type TFrameEvent,
  type TFrameState,
} from '@/next/features/app-views/frame-health';
import { pageOffer } from '@/next/features/app-views/page-tools';
import { askConfirm } from '@/next/features/app-views/confirm';
import { hostKeys } from '@/next/features/app-views/host-keys';
import { watchLoads } from '@/next/features/app-views/frame-loads';
import { chatOf, useHostHandlers } from '@/next/features/app-views/host-handlers';
import { conversationsStore, useConversation } from '@/next/stores/conversations';
import { openAppTerminal } from '@/next/features/app-views/app-terminal';
import { openAddressTab, openAppTab } from '@/next/features/app-views/open';
import { agentRows, chatTarget, openConversation, watchAgents } from '@/next/features/app-views/agents';
import { reaches, subscribeStorageChanges } from '@/next/features/app-views/storage-events';
import { viewTheme } from '@/next/features/app-views/theme';
import { sendMessage } from '@/next/commands/messages';
import { toast } from '@/next/ui/toast';
import { useTheme } from '@/platform/theme';
import Icon from '@/next/ui/icon';
import { t, locale } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/**
 * The frame: scripts and forms, no top navigation, and no allow-same-origin, so the view's origin is opaque: it cannot
 * reach the slot's storage or another app's view. What it keeps goes through the bridge. (The view server's CSP says
 * the same, for a frame made some other way.)
 */
/** The action an elevation dialog names, in the person's words. */
const elevationAction = (method: string): string => {
  if (method === 'harnessly/terminal.run') return t('next.appview.elevate.run');
  if (method === 'harnessly/process.start') return t('next.appview.elevate.start');
  if (method === 'harnessly/process.stop') return t('next.appview.elevate.stop');
  if (method === 'harnessly/preview.open') return t('next.appview.elevate.preview');
  if (method === 'harnessly/terminal.open') return t('next.appview.elevate.terminalOpen');
  if (method === 'harnessly/terminal.write') return t('next.appview.elevate.terminalWrite');
  if (method === 'harnessly/terminal.read') return t('next.appview.elevate.terminalRead');
  if (method === 'harnessly/browser.read') return t('next.appview.elevate.browserRead');
  if (method === 'harnessly/browser.drive') return t('next.appview.elevate.browserDrive');
  if (method === 'harnessly/ai.complete') return t('next.appview.elevate.ai');
  if (method === 'harnessly/agents.run') return t('next.appview.elevate.agentsRun');
  if (method === 'harnessly/secrets.put') return t('next.appview.elevate.secretsPut');
  return t('next.appview.elevate.other');
};
export const APP_FRAME_SANDBOX = 'allow-scripts allow-forms allow-popups';
/**
 * A view the app's own server hosts runs on that server's origin (its own sign-in and storage work), which is never the
 * page's: frameSrc refuses the page's origin, so allow-same-origin gives it nothing of the host.
 */
export const REMOTE_FRAME_SANDBOX = `${APP_FRAME_SANDBOX} allow-same-origin`;

/** The remote view's address with the nonce in its fragment (never sent to its server; the shim takes it out). */
export const withNonce = (src: string, nonce: string): string => {
  const u = new URL(src);
  const rest = u.hash.replace(/^#/, '');
  u.hash = rest === '' ? `hl-nonce=${nonce}` : `${rest}&hl-nonce=${nonce}`;
  return u.href;
};
/** A sandboxed frame's messages carry this origin. */
const OPAQUE = 'null';
const MIN_HEIGHT = 48;
const MAX_HEIGHT = 640;
const HOST_VERSION = '1';
/** A view session lives an hour unused (the runtime's SessionIdle); a mounted view renews well inside that. */
const RENEW_MS = 10 * 60 * 1000;

/**
 * What a view's message to the chat says: always the real name of the app first, so the person sees who wrote. The view
 * supplies no name of its own, and what it supplies cannot start another line (a second «[App]» line would pass for
 * another writer).
 */
export const fromApp = (name: string, text: string): string =>
  `[${name.replace(/[[\]\r\n]+/g, ' ').trim()}] ${text.replace(/\s*[\r\n]+\s*/g, ' ').trim()}`;

/** A view at an address of the app's own server: https, another origin than the page's, no credentials; else null. */
export const frameSrc = (url: string, page: string): string | null => {
  try {
    const u = new URL(url);
    return u.protocol === 'https:' && u.origin !== page && u.username === '' && u.password === ''
      ? u.href
      : null;
  } catch {
    return null;
  }
};

export interface IAppFrameProps {
  machine: MachineId;
  app: IInstalledApp;
  view: IAppView;
  target: TTarget;
  /** The port of the view server (installed apps' `viewPort`). */
  viewPort: number;
  conversation: ConvId | null;
  /** For a card: the data its tool returned. */
  data?: unknown;
  /** What the host adds or replaces: the chat's reading, a flow, a confirm dialog… */
  handlers?: IBridgeHandlers;
  /** A card grows to its content (up to a limit); a pane tab fills its room. */
  fit?: boolean;
  /** A new value mounts the view anew (the inspector's reload). */
  reloadToken?: number;
  onBridgeCall?: (entry: IBridgeLogEntry) => void;
  /** For a view of target `sinopia.<kind>`: the document it is shown in. */
  document?: string;
  /** The document may only be read: the view is told (`hl.context.readOnly`) and the bridge refuses what would change it. */
  readOnly?: boolean;
  /** A notification to send the view when `seq` changes (the Sinopia selection): the host's word, not the view's. */
  notice?: IViewNotice | null;
  /** The view belongs to the chat whatever its apps (an agent's own widget): it gets the chat without being turned on. */
  inChat?: boolean;
  /** A Document API call of the view that went through (the host may do more: a comment also goes to the chat). */
  onSinopiaCall?: (method: string, params: Record<string, unknown>) => void;
}

export interface IViewNotice {
  seq: number;
  method: string;
  params: unknown;
}

interface IOpened {
  key: string;
  src: string | null;
  /** The view's session; null for a view the app's own server hosts. */
  session: string | null;
  /** The nonce of the view's pages; null for a view the app's own server hosts. */
  nonce: string | null;
}

const isSlotTarget = (target: string) =>
  target === 'settings' || target === 'onboarding' || target === 'object-action' || target === 'hub-settings';

const AppFrame = ({
  machine,
  app,
  view,
  target,
  viewPort,
  conversation,
  data,
  handlers,
  fit = false,
  reloadToken = 0,
  onBridgeCall,
  document: doc,
  readOnly = false,
  notice = null,
  onSinopiaCall,
  inChat = false,
}: IAppFrameProps) => {
  const { resolvedTheme } = useTheme();
  const [frame, setFrame] = useState<HTMLIFrameElement | null>(null);
  const [opened, setOpened] = useState<IOpened | null>(null);
  const [height, setHeight] = useState(MIN_HEIGHT * 3);
  const [reload, setReload] = useState(0);
  // Is the view up? Only the handshake says so (frame-health.ts): the frame's `load` also fires for a 404 page.
  const [health, setHealth] = useState<{ key: string; state: TFrameState }>({ key: '', state: LOADING });
  // The most telling thing the view said about its failure before it was up; the timeout then says it.
  const seen = useRef<{ key: string; failure: IFailure } | null>(null);
  const bridge = useRef<AppBridge | null>(null);
  // This frame among the views of the chat that offer page tools.
  const frameId = useId();
  // A slot view holds a slot of its own: two views of one app on a page would share one cookie, and the second
  // one's session would replace the first one's.
  const lease = isSlotTarget(target) ? `app:${app.id}/${view.id}` : `app:${app.id}`;
  // An app that waits (for permission, a secret…) has no view to start: the card says so at once, and the key changes
  // when it stops waiting, so the view then starts anew.
  const waits = app.state === APP_STATE.waiting;
  const key = `${machine}|${app.id}|${view.id}|${viewPort}|${reload}|${reloadToken}|${waits ? 'w' : ''}`;
  const state: TFrameState = health.key === key ? health.state : LOADING;
  // The frame's key, by a name the bridge's effect does not shadow.
  const frameKey = key;
  // One trace for everything of this mount, from the page's ask for a session through the slot and the view server
  // to the view's calls, so `harnessly logs` reads one chain (HRN-976).
  const mountRef = useRef<{ key: string; trace: string; at: number } | null>(null);
  // Read in effects only: the first effect of a mount makes it.
  const mountOf = useCallback((k: string) => {
    if (mountRef.current?.key !== k) mountRef.current = { key: k, trace: newTrace('v-'), at: Date.now() };
    return mountRef.current;
  }, []);
  const send = useCallback(
    (event: TFrameEvent) =>
      setHealth((h) => {
        const was = h.key === key ? h.state : LOADING;
        return {
          key,
          state: nextFrameState(was, event, seen.current?.key === key ? seen.current.failure : null),
        };
      }),
    [key],
  );
  // An app gets a chat only where the person turned it on; the runtime checks the same when it issues the session.
  const turnedOn = useConversation(conversation)?.apps?.includes(app.id) ?? false;
  const on = inChat || turnedOn;
  const chat = on ? conversation : null;
  const work = useHostHandlers(machine, conversation, app, on);

  // Each app holds a slot of its own: a slot is one origin, and two apps on one would share it.
  useEffect(() => {
    let live = true;
    const fail = (failure: IFailure) => {
      if (live) send({ k: 'session-failed', failure });
    };
    const failText = (text: string) => fail({ reason: FAIL_REASON.refused, detail: text });
    const failError = (e: Error) => fail(classifySessionError(e.message));
    const slow = t('next.appfail.sessionTimeout', { s: SESSION_WAIT_MS / 1000 });
    // The session's request carries the mount's trace; the runtime keeps it with the session.
    const mount = mountOf(key);
    followTrace(mount.trace);
    journal(
      {
        level: 'info',
        src: journalSource(app.id),
        ev: 'view.open',
        app: app.id,
        view: view.id,
        trace: mount.trace,
        ...(chat === null ? {} : { chat }),
        ...(waits ? { text: `waiting: ${app.reason ?? ''}` } : {}),
      },
      machine,
    );
    if (waits) {
      fail({ reason: FAIL_REASON.waiting, detail: app.reason ?? '' });
      return () => {
        live = false;
      };
    }
    // A view the app's own server hosts needs no slot: the frame goes there directly, with a session the page holds.
    if (view.url) {
      const direct = frameSrc(view.url, window.location.origin);
      if (direct === null) {
        failText(t('next.appview.badAddressWhy'));
        return;
      }
      withTimeout(
        createViewSession(machine, {
          app: app.id,
          view: view.id,
          target,
          conversation: chat,
          ...(doc === undefined ? {} : { document: doc }),
        }),
        SESSION_WAIT_MS,
        slow,
      )
        .then((made) => {
          const at = made.url === undefined ? null : frameSrc(made.url, window.location.origin);
          if (!live) return;
          if (at === null) failText(t('next.appview.badAddressWhy'));
          else
            setOpened({
              key,
              src: withNonce(at, made.nonce),
              session: made.session,
              nonce: made.nonce,
            });
        })
        .catch(failError);
      return () => {
        live = false;
      };
    }
    if (view.path) {
      const slot = slotLeases.lease('port', lease);
      // The view server serves a view's files only under the token of a session the runtime issued for this mount.
      withTimeout(
        createViewSession(machine, {
          app: app.id,
          view: view.id,
          target,
          conversation: chat,
          ...(doc === undefined ? {} : { document: doc }),
        }),
        SESSION_WAIT_MS,
        slow,
      )
        .then(async (made) => {
          if (made.path === undefined) throw new Error(t('next.appview.noEntry'));
          const o = await withTimeout(
            openPreview(machine, { kind: 'port', port: viewPort }, slot, made.path, [], 'path'),
            SESSION_WAIT_MS,
            slow,
          );
          // The address carries the mount's trace: a gateway in front of the machine logs a refusal with it.
          if (live)
            setOpened({
              key,
              src: withTraceParam(o.enterUrl, mount.trace),
              session: made.session,
              nonce: made.nonce,
            });
        })
        .catch(failError);
    } else failText(t('next.appview.noEntry'));
    return () => {
      live = false;
    };
  }, [
    key,
    mountOf,
    send,
    waits,
    app.reason,
    machine,
    view.url,
    view.path,
    view.id,
    viewPort,
    lease,
    app.id,
    target,
    chat,
    doc,
  ]);

  // The slot went to a page used less recently: ask again, and get another.
  useEffect(
    () =>
      slotLeases.onTaken((taken) => {
        if (taken === lease) setReload((n) => n + 1);
      }),
    [lease],
  );

  const context = useMemo<IViewContext>(
    () => ({
      app: { id: app.id, version: app.version },
      target,
      view: view.id,
      theme: viewTheme(resolvedTheme === 'dark' ? 'dark' : 'light'),
      locale: locale(),
      chat: chat ? { id: chat } : null,
      user: null,
      ...(readOnly ? { readOnly } : {}),
      ...(data === undefined ? {} : { data }),
    }),
    [app.id, app.version, target, view.id, resolvedTheme, chat, data, readOnly],
  );
  const latest = useRef({ context, onBridgeCall, onSinopiaCall });
  useEffect(() => {
    latest.current = { context, onBridgeCall, onSinopiaCall };
  });

  const slotted = opened?.key === key ? opened : null;
  const remote = view.url !== undefined;
  const shown = slotted;
  const ready = shown?.src != null;
  const session = shown?.session ?? null;
  const nonce = shown?.nonce ?? null;
  const src = shown?.src ?? null;

  // A mounted view keeps its session while nobody uses it (a pane tab left open); if the runtime lost it, mount anew.
  useEffect(() => {
    if (session === null) return;
    const timer = window.setInterval(() => {
      renewViewSession(machine, session).catch(() => setReload((n) => n + 1));
    }, RENEW_MS);
    return () => window.clearInterval(timer);
  }, [machine, session]);

  // What the page sees of the frame itself (HRN-976): when its first document loaded, and a refusal of the page's own
  // policy. When the machine never saw the view's request, these are what the card can tell.
  const frameSeen = useRef<IFrameSeen | null>(null);
  useEffect(() => {
    if (!frame || src === null) return;
    const seenNow: IFrameSeen = { src, at: Date.now(), loadedAt: null, csp: null };
    frameSeen.current = seenNow;
    const mount = mountOf(key);
    const tags = { app: app.id, view: view.id, trace: mount.trace, ...(chat === null ? {} : { chat }) };
    const onLoad = () => {
      if (seenNow.loadedAt !== null) return;
      seenNow.loadedAt = Date.now();
      journal(
        {
          level: 'info',
          src: journalSource(app.id),
          ev: 'frame.load',
          url: cleanUrl(seenNow.src),
          dur: seenNow.loadedAt - seenNow.at,
          ...tags,
        },
        machine,
      );
    };
    let origin = '';
    try {
      origin = new URL(src, window.location.href).origin;
    } catch {
      origin = '';
    }
    const onCsp = (e: SecurityPolicyViolationEvent) => {
      if (origin === '' || !e.blockedURI.startsWith(origin)) return;
      if (!/^(frame-src|child-src|default-src)/.test(e.effectiveDirective)) return;
      seenNow.csp = e.effectiveDirective;
    };
    frame.addEventListener('load', onLoad);
    document.addEventListener('securitypolicyviolation', onCsp);
    return () => {
      frame.removeEventListener('load', onLoad);
      document.removeEventListener('securitypolicyviolation', onCsp);
    };
  }, [frame, src, machine, app.id, view.id, chat, key, mountOf]);

  // The first document the frame loads is the view; any later load is a navigation, which ends the bridge. It is kept
  // apart from the bridge's effect so that a bridge made afresh later does not take the navigated page for the first.
  const navigated = useRef(false);
  useEffect(() => {
    navigated.current = false;
    if (!frame || src === null) return;
    return watchLoads(frame, () => {
      navigated.current = true;
      bridge.current?.dispose();
      bridge.current = null;
    });
  }, [frame, src]);

  useEffect(() => {
    if (!frame || !ready || navigated.current) return;
    // Page tools and the published context reach the agent of the chat the app is on in; elsewhere they are refused.
    const offer =
      chat === null
        ? null
        : pageOffer(
            machine,
            chat,
            frameId,
            app.id,
            (id, name, args) => {
              bridge.current?.agentCall(id);
              bridge.current?.notify('harnessly/notifications/tool-call', { id, name, arguments: args });
            },
            (tool, shown) =>
              askConfirm(
                app.name,
                t('next.appview.pageToolConfirm', {
                  tool: tool.name,
                  action: tool.description || tool.name,
                  args: shown,
                }),
              ),
            (id, call) => {
              // The view decides in the agent's turn: its loud calls meanwhile are asked about, as a tool's are.
              bridge.current?.agentCall(id);
              bridge.current?.notify('harnessly/notifications/hook', {
                id,
                ...(typeof call === 'object' && call !== null ? call : {}),
              });
            },
          );
    let offFiles: (() => void) | null = null;
    const noChat = () => {
      throw new BridgeError('host_unavailable', 'page tools work in a chat the app is on in');
    };
    const host: IBridgeHandlers = {
      pageTools: (tools) => (offer ? offer.tools(tools) : noChat()),
      pageToolResult: (id, answer) => offer?.result(id, answer),
      hooks: (on) => (offer ? offer.hooks(on) : noChat()),
      publish: (data) => (offer ? offer.publish(data) : noChat()),
      // The runtime reads the app, the target and the chat from the session; none of them is sent again.
      tool: (name, args) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callTool(machine, session, name, args);
      },
      storage: (op, scope, k, value, ifVersion) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callStorage(machine, {
          session,
          op,
          scope,
          key: k,
          value,
          ...(ifVersion === undefined ? {} : { ifVersion }),
        });
      },
      settings: () => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callSettings(machine, session);
      },
      files: (op, path, text) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callFiles(machine, session, op, path, text);
      },
      filesMore: async (op, params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        const out = await callFilesMore(machine, session, op, params);
        // The names of the changed files come by the machine's stream, to this view's session only.
        if (op === 'watch' && offFiles === null)
          offFiles = subscribeFileChanges(machine, session, (paths) => bridge.current?.filesChanged(paths));
        if (op === 'unwatch') {
          offFiles?.();
          offFiles = null;
        }
        return out;
      },
      git: (op, params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callGit(machine, session, op, params);
      },
      run: (op, params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callRun(machine, session, op, params);
      },
      terminal: (params, events) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return openAppTerminal(machine, session, params, events);
      },
      browser: (params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callBrowser(machine, session, params);
      },
      ai: (params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callAI(machine, session, params);
      },
      secretsPut: (params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callSecretsPut(machine, session, params);
      },
      agentsRun: (params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callAgentsRun(machine, session, params);
      },
      terminalRead: (params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        return callTerminalRead(machine, session, params);
      },
      previewOpen: async (port, path) => {
        if (!openAddressTab(`localhost:${port}${path === '/' ? '' : path}`))
          throw new BridgeError('host_unavailable', 'the right pane is not on this page');
      },
      elevate: (method, detail) =>
        askConfirm(app.name, t('next.appview.agentElevate', { action: elevationAction(method), detail })),
      pick: (accept) => {
        if (chat === null)
          throw new BridgeError('host_unavailable', 'a file is picked in a chat the app is on in');
        return askFile(app.name, machine, chat, accept);
      },
      chatSend: async (text) => {
        sendMessage(machine, { conversationId: chatOf(conversation, on), text: fromApp(app.name, text) });
      },
      agents: async () => agentRows(machine),
      paneOpen: async (to) => {
        const chatId = chatTarget(to);
        if (chatId !== null) {
          // The bridge has checked agents.read; only a conversation of this machine opens.
          if (conversationsStore.get(chatId)?.machineId !== machine)
            throw new BridgeError('invalid_params', 'no such conversation on this machine');
          openConversation(chatId);
          return;
        }
        if (!openAppTab(to))
          throw new BridgeError('invalid_params', 'only app:<id>[/<view>] and chat:<id> open from a view');
      },
      sinopia: async (method, params) => {
        if (session === null) throw new BridgeError('host_unavailable', 'this view has no host session');
        const result = await callSinopia(machine, session, method, params);
        latest.current.onSinopiaCall?.(method, params);
        return result;
      },
      // A frame's toast is signed with the app's name: its words are the second line, never the host's own title.
      toast: (text, { tone, description }) =>
        void toast(app.name, { tone, description: description ? `${text} — ${description}` : text }),
      ...hostKeys(() => document.activeElement === frame),
      resize: (h) => setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, h))),
      ...work,
      // The host's own come last: the chat binding may read the chat or run a flow where a default has no way to.
      ...handlers,
    };
    // A remote view speaks from its own origin, and is answered only there.
    const remoteOrigin = remote && src !== null ? new URL(src).origin : null;
    const key = logKey(app.id, chat);
    // What the view reports goes to the debug section here and to the machine, where the CLI reads it (HRN-913).
    const report = (line: ReturnType<typeof bridgeLine>) =>
      line && queueViewLog(machine, app.id, view.id, line);
    const jsrc = journalSource(app.id);
    const mount = mountOf(frameKey);
    const tags = { app: app.id, view: view.id, trace: mount.trace, ...(chat === null ? {} : { chat }) };
    host.debugLog = (level, text) => {
      const at = Date.now();
      logCall(key, { method: `console.${level}`, permission: '', ok: false, message: text, at });
      report(consoleLine(level, text, at));
      if (level === 'error' || level === 'warn')
        journal({ at, level, src: jsrc, ev: 'view.console', text, ...tags }, machine);
    };
    const b = new AppBridge({
      frame: () => frame.contentWindow,
      origin: remoteOrigin ?? OPAQUE,
      replyTo: remoteOrigin ?? '*',
      app,
      context: latest.current.context,
      handlers: host,
      hostVersion: HOST_VERSION,
      onReady: () => {
        journal(
          { level: 'info', src: jsrc, ev: 'ui.initialize', dur: Date.now() - mount.at, ...tags },
          machine,
        );
        send({ k: 'handshake' });
      },
      ...(nonce === null ? {} : { nonce }),
      onCall: (entry) => {
        logCall(key, entry);
        report(bridgeLine(entry));
        if (!entry.ok && !entry.method.startsWith('console.'))
          journal(
            {
              at: entry.at,
              level: entry.code === 'host_unavailable' ? 'error' : 'warn',
              src: jsrc,
              ev: 'bridge',
              tool: entry.method,
              text: [entry.code, entry.message].filter(Boolean).join(': '),
              ...tags,
            },
            machine,
          );
        latest.current.onBridgeCall?.(entry);
      },
    });
    bridge.current = b;
    // The chat's events reach the view only for the kinds it subscribed to and holds events:<kind> for.
    const offChat = chat === null ? null : watchChatEvents(chat, (kind, params) => b.event(kind, params));
    const onMessage = (event: MessageEvent) => void b.onMessage(event);
    window.addEventListener('message', onMessage);
    // A view that never connects shows nothing of why: the debug section says so, with the cause the machine's journal
    // knows (a file the slot refused, a request the preview blocked), and the card shows that cause too (HRN-976).
    const watchdog = window.setTimeout(() => {
      if (b.ready || bridge.current !== b) return;
      const known = seen.current?.key === mount.key ? seen.current.failure.detail : '';
      const asked =
        known === '' ? journalReason(machine, app.id, chat, mount.at, mount.trace) : Promise.resolve(known);
      // The machine saw nothing of the view: the page says what it saw of the frame (a gateway, its own policy).
      const told = asked.then(async (why) => {
        if (why !== '' || b.ready || bridge.current !== b) return why;
        const shownFrame = frameSeen.current;
        const r = await frameReason(shownFrame, mount.trace);
        if (r.text !== '')
          journal(
            {
              level: 'error',
              src: jsrc,
              ev: 'frame.gateway',
              url: cleanUrl(shownFrame?.src ?? ''),
              text: r.text,
              ...(r.status === undefined ? {} : { status: r.status }),
              ...tags,
            },
            machine,
          );
        return r.text;
      });
      void told.then((why) => {
        if (b.ready || bridge.current !== b) return;
        const base = `ui/initialize — host_unavailable: no handshake in ${HANDSHAKE_WAIT_MS / 1000} s`;
        const message =
          t('next.inspector.noHandshake', { s: HANDSHAKE_WAIT_MS / 1000 }) + (why === '' ? '' : ` — ${why}`);
        logCall(key, {
          method: 'ui/initialize',
          permission: '',
          ok: false,
          code: 'host_unavailable',
          message,
          at: Date.now(),
        });
        report({ level: 'bridge', text: why === '' ? base : `${base} — ${why}`, at: Date.now() });
        journal(
          {
            level: 'error',
            src: jsrc,
            ev: 'ui.initialize',
            text: why === '' ? base : `${base} — ${why}`,
            dur: Date.now() - mount.at,
            ...tags,
          },
          machine,
        );
        if (why !== '' && known === '')
          send({ k: 'session-failed', failure: { reason: FAIL_REASON.silent, detail: why } });
      });
    }, HANDSHAKE_WAIT_MS);
    return () => {
      window.clearTimeout(watchdog);
      window.removeEventListener('message', onMessage);
      offChat?.();
      if (offFiles !== null && session !== null) {
        offFiles();
        void callFilesMore(machine, session, 'unwatch', {}).catch(() => undefined);
      }
      b.dispose();
      bridge.current = null;
      offer?.drop();
    };
  }, [
    frame,
    ready,
    session,
    nonce,
    machine,
    app,
    target,
    conversation,
    on,
    chat,
    handlers,
    work,
    frameId,
    remote,
    src,
    view.id,
    send,
    mountOf,
    frameKey,
  ]);

  // The view has HANDSHAKE_WAIT_MS from the moment its address is given to the frame; no handshake, no view. The wait
  // ends the moment the handshake comes (`state` leaves `loading`).
  const waiting = ready && state.k === 'loading';
  useEffect(() => {
    if (!waiting) return;
    const watch = watchHandshake(() => send({ k: 'timeout' }));
    return () => watch.cancel();
  }, [waiting, send, src]);

  // What the view says before the SDK is up (the shim of every page, or the server's page of refusal): a file that did
  // not load, an exception, a blocked request. It goes to the journal, and the most telling of it is the card's reason.
  useEffect(() => {
    if (!frame || !ready) return;
    const logKeyOfApp = logKey(app.id, chat);
    const onMessage = (event: MessageEvent) => {
      if (event.source !== frame.contentWindow) return;
      const said = viewErrorOf(event.data);
      if (said === null) return;
      const failure = failureOfViewError(said, app.id);
      const mount = mountOf(key);
      seen.current = {
        key,
        failure: keepTelling(seen.current?.key === key ? seen.current.failure : null, failure),
      };
      const at = Date.now();
      const message = said.src === '' ? said.text : `${said.text} ${failure.detail || said.src}`.trim();
      logCall(logKeyOfApp, {
        method: said.kind === 'resource' ? 'view.file' : `view.${said.kind}`,
        permission: '',
        ok: false,
        message,
        at,
      });
      // The same line goes to the machine, where `harnessly app logs` reads it (HRN-913), and to the machine's
      // journal with the file's address and the mount's trace, where `harnessly logs` reads it (HRN-976).
      queueViewLog(machine, app.id, view.id, { level: 'error', text: message, at });
      journal(
        {
          at,
          level: 'error',
          src: journalSource(app.id),
          ev: said.kind === 'resource' ? 'resource' : `view.${said.kind}`,
          text: said.text,
          ...(said.src === '' ? {} : { url: cleanUrl(said.src) }),
          ...(said.status === null ? {} : { status: said.status }),
          app: app.id,
          view: view.id,
          trace: mount.trace,
          ...(chat === null ? {} : { chat }),
        },
        machine,
      );
      // A refusal of the view server is the answer, not a guess: the card needs no more waiting.
      if (said.kind === 'refused') send({ k: 'session-failed', failure });
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [frame, ready, machine, app.id, view.id, chat, key, send, mountOf]);

  // `storage`: a key of the app's storage was written (by the agent, another view of the app, the view itself), told at
  // once from the runtime's stream. It names the scope and the key, never the value; the view reads the key again.
  // Only this app's, and an area of a chat or document only to the views of that chat or document.
  useEffect(() => {
    if (!ready) return;
    return subscribeStorageChanges(machine, (change) => {
      if (!reaches(change, { app, chat, document: doc })) return;
      bridge.current?.notify('harnessly/notifications/storage', { scope: change.scope, key: change.key });
    });
  }, [ready, machine, app, chat, doc]);

  // `agents`: the list of the person's conversations on this machine, or the status of one, changed. Told only to a
  // view that holds agents.read, with the rows themselves (no text); the view need not ask again.
  const readsAgents = app.permissions.includes('agents.read');
  useEffect(() => {
    if (!ready || !readsAgents) return;
    return watchAgents(machine, (agents) =>
      bridge.current?.notify('harnessly/notifications/agents', { agents }),
    );
  }, [ready, readsAgents, machine]);

  // `visibility`: the view is shown or hidden (its pane tab left, the pane folded, the page in the background).
  useEffect(() => {
    if (!frame || !ready) return;
    let inView = true;
    let told = true;
    const report = () => {
      const visible = inView && !document.hidden;
      if (visible === told) return;
      told = visible;
      bridge.current?.notify('harnessly/notifications/visibility', { visible });
    };
    const observer = new IntersectionObserver((entries) => {
      const last = entries[entries.length - 1];
      if (last) inView = last.isIntersecting;
      report();
    });
    observer.observe(frame);
    document.addEventListener('visibilitychange', report);
    return () => {
      observer.disconnect();
      document.removeEventListener('visibilitychange', report);
    };
  }, [frame, ready]);

  // The host's notice to the view (a new Sinopia selection); a view not up yet reads the selection when it starts.
  const noticed = useRef(notice?.seq ?? 0);
  useEffect(() => {
    if (notice === null || notice.seq === noticed.current) return;
    noticed.current = notice.seq;
    bridge.current?.notify(notice.method, notice.params);
  }, [notice]);

  // The page's tokens are read a frame later: the theme provider repaints the page in its own effect, after this one,
  // so read now they would still be the old theme's.
  useEffect(() => {
    const b = bridge.current;
    if (!b) return;
    const id = requestAnimationFrame(() =>
      b.setContext({ ...context, theme: viewTheme(context.theme.mode) }),
    );
    return () => cancelAnimationFrame(id);
  }, [context]);

  // `load` is not «up»: a 404 page loads too. The frame is shown once the view says it is ready; until then the skeleton
  // is over it, and when it did not start the card takes its place (the frame stays, hidden, in case it connects late).
  return (
    <div className="hx-appview__box" data-fit={fit || undefined} data-state={state.k}>
      <iframe
        ref={setFrame}
        className="hx-appview"
        data-state={state.k}
        title={view.title ?? app.name}
        sandbox={remote ? REMOTE_FRAME_SANDBOX : APP_FRAME_SANDBOX}
        referrerPolicy="no-referrer"
        allow=""
        src={shown?.src ?? undefined}
        data-fit={fit || undefined}
        height={fit ? height : undefined}
      />
      {state.k !== 'failed' && (
        <div className="hx-appview__skeleton" data-state={state.k} aria-hidden={state.k === 'ready'}>
          <Icon name={app.icon || 'layout-grid'} />
          <span className="hx-appview__skeleton-name">{t('next.appfail.loading', { name: app.name })}</span>
          <span className="hx-appview__skeleton-bar" />
          <span className="hx-appview__skeleton-bar" />
        </div>
      )}
      {state.k === 'failed' && (
        <AppFail
          machine={machine}
          app={app}
          failure={state.failure}
          logKey={logKey(app.id, chat)}
          conversation={conversation}
          onRetry={() => setReload((n) => n + 1)}
        />
      )}
    </div>
  );
};

export default AppFrame;
