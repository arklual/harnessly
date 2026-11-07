// An app's view in a sandboxed frame: shown through a preview slot (so it runs on an origin of its own, never the
// page's, and the preview's DevTools see it), and talked to only through the bridge (bridge.ts).
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { slotLeases } from '@/next/features/pane/slot-lease';
import { openPreview } from '@/next/core/net/preview';
import {
  callFiles,
  callSettings,
  callSinopia,
  callStorage,
  callTool,
  createViewSession,
  renewViewSession,
  type IAppView,
  type IInstalledApp,
} from '@/next/features/app-views/api';
import {
  AppBridge,
  BridgeError,
  type IBridgeHandlers,
  type IBridgeLogEntry,
  type IViewContext,
  type TTarget,
} from '@/next/features/app-views/bridge';
import { logCall, logKey } from '@/next/features/app-views/bridge-log';
import { pageOffer } from '@/next/features/app-views/page-tools';
import { askConfirm } from '@/next/features/app-views/confirm';
import { watchLoads } from '@/next/features/app-views/frame-loads';
import { chatOf, useHostHandlers } from '@/next/features/app-views/host-handlers';
import { conversationsStore, useConversation } from '@/next/stores/conversations';
import { openAppTab } from '@/next/features/app-views/open';
import { agentRows, chatTarget, openConversation, watchAgents } from '@/next/features/app-views/agents';
import { reaches, subscribeStorageChanges } from '@/next/features/app-views/storage-events';
import { viewTheme } from '@/next/features/app-views/theme';
import { sendMessage } from '@/next/commands/messages';
import { toast } from '@/next/ui/toast';
import { useTheme } from '@/platform/theme';
import { t, locale } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/**
 * The frame: scripts and forms, no top navigation, and no allow-same-origin, so the view's origin is opaque: it cannot
 * reach the slot's storage or another app's view. What it keeps goes through the bridge. (The view server's CSP says
 * the same, for a frame made some other way.)
 */
/** How long a view has to make its handshake before the debug section says it did not. */
const HANDSHAKE_WAIT_MS = 8000;

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
  error: string | null;
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
  const bridge = useRef<AppBridge | null>(null);
  // This frame among the views of the chat that offer page tools.
  const frameId = useId();
  // A slot view holds a slot of its own: two views of one app on a page would share one cookie, and the second
  // one's session would replace the first one's.
  const lease = isSlotTarget(target) ? `app:${app.id}/${view.id}` : `app:${app.id}`;
  const key = `${machine}|${app.id}|${view.id}|${viewPort}|${reload}|${reloadToken}`;
  // An app gets a chat only where the person turned it on; the runtime checks the same when it issues the session.
  const turnedOn = useConversation(conversation)?.apps?.includes(app.id) ?? false;
  const on = inChat || turnedOn;
  const chat = on ? conversation : null;
  const work = useHostHandlers(machine, conversation, app, on);

  // Each app holds a slot of its own: a slot is one origin, and two apps on one would share it.
  useEffect(() => {
    let live = true;
    const fail = (error: string) => live && setOpened({ key, src: null, session: null, nonce: null, error });
    // A view the app's own server hosts needs no slot: the frame goes there directly, with a session the page holds.
    if (view.url) {
      const direct = frameSrc(view.url, window.location.origin);
      if (direct === null) {
        fail(t('next.appview.badAddress'));
        return;
      }
      createViewSession(machine, {
        app: app.id,
        view: view.id,
        target,
        conversation: chat,
        ...(doc === undefined ? {} : { document: doc }),
      })
        .then((made) => {
          const at = made.url === undefined ? null : frameSrc(made.url, window.location.origin);
          if (!live) return;
          if (at === null) fail(t('next.appview.badAddress'));
          else
            setOpened({
              key,
              src: withNonce(at, made.nonce),
              session: made.session,
              nonce: made.nonce,
              error: null,
            });
        })
        .catch((e: Error) => fail(e.message));
      return () => {
        live = false;
      };
    }
    if (view.path) {
      const slot = slotLeases.lease('port', lease);
      // The view server serves a view's files only under the token of a session the runtime issued for this mount.
      createViewSession(machine, {
        app: app.id,
        view: view.id,
        target,
        conversation: chat,
        ...(doc === undefined ? {} : { document: doc }),
      })
        .then(async (made) => {
          if (made.path === undefined) throw new Error(t('next.appview.noEntry'));
          const o = await openPreview(machine, { kind: 'port', port: viewPort }, slot, made.path);
          if (live)
            setOpened({ key, src: o.enterUrl, session: made.session, nonce: made.nonce, error: null });
        })
        .catch((e: Error) => fail(e.message));
    } else fail(t('next.appview.noEntry'));
    return () => {
      live = false;
    };
  }, [key, machine, view.url, view.path, view.id, viewPort, lease, app.id, target, chat, doc]);

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
  const ready = shown?.src != null && shown.error === null;
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
            (id, name, args) =>
              bridge.current?.notify('harnessly/notifications/tool-call', { id, name, arguments: args }),
            (tool, shown) =>
              askConfirm(
                app.name,
                t('next.appview.pageToolConfirm', {
                  tool: tool.name,
                  action: tool.description || tool.name,
                  args: shown,
                }),
              ),
          );
    const noChat = () => {
      throw new BridgeError('host_unavailable', 'page tools work in a chat the app is on in');
    };
    const host: IBridgeHandlers = {
      pageTools: (tools) => (offer ? offer.tools(tools) : noChat()),
      pageToolResult: (id, answer) => offer?.result(id, answer),
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
      toast: (text) => void toast(app.name, { description: text }),
      resize: (h) => setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, h))),
      ...work,
      // The host's own come last: the chat binding may read the chat or run a flow where a default has no way to.
      ...handlers,
    };
    // A remote view speaks from its own origin, and is answered only there.
    const remoteOrigin = remote && src !== null ? new URL(src).origin : null;
    const key = logKey(app.id, chat);
    host.debugLog = (level, text) =>
      logCall(key, { method: `console.${level}`, permission: '', ok: false, message: text, at: Date.now() });
    const b = new AppBridge({
      frame: () => frame.contentWindow,
      origin: remoteOrigin ?? OPAQUE,
      replyTo: remoteOrigin ?? '*',
      app,
      context: latest.current.context,
      handlers: host,
      hostVersion: HOST_VERSION,
      ...(nonce === null ? {} : { nonce }),
      onCall: (entry) => {
        logCall(key, entry);
        latest.current.onBridgeCall?.(entry);
      },
    });
    bridge.current = b;
    const onMessage = (event: MessageEvent) => void b.onMessage(event);
    window.addEventListener('message', onMessage);
    // A view that never connects shows nothing of why: the debug section says so.
    const watchdog = window.setTimeout(() => {
      if (!b.ready && bridge.current === b)
        logCall(key, {
          method: 'ui/initialize',
          permission: '',
          ok: false,
          code: 'host_unavailable',
          message: t('next.inspector.noHandshake', { s: HANDSHAKE_WAIT_MS / 1000 }),
          at: Date.now(),
        });
    }, HANDSHAKE_WAIT_MS);
    return () => {
      window.clearTimeout(watchdog);
      window.removeEventListener('message', onMessage);
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
  ]);

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

  if (shown?.error) return <p className="hx-appview__error">{shown.error}</p>;
  return (
    <iframe
      ref={setFrame}
      className="hx-appview"
      title={view.title ?? app.name}
      sandbox={remote ? REMOTE_FRAME_SANDBOX : APP_FRAME_SANDBOX}
      referrerPolicy="no-referrer"
      allow=""
      src={shown?.src ?? undefined}
      data-fit={fit || undefined}
      height={fit ? height : undefined}
    />
  );
};

export default AppFrame;
