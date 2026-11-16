// An app opened as the main thing, /apps/<id>[?o=<object>][&chat=<conv>]: Sinopia's stage (features/app-main/
// app-stage.tsx) with the app's `app` view where a design has its page — the app's home, or one of its objects (a
// document, a board) the view opened with `hl.pane.open('object:<slug>')`. The chat is Sinopia's: bound to the app's
// home or to the object, docked, in a window or folded into the field, with the right pane of the chat beside it. The
// first words sent from the field or from the view (`hl.chat.send`) make the chat, with the app on in it. The strip's
// «…» holds the app's rights, the debug section of a dev install and turning the app off in the chat.
import { useSpaceDir } from '@/next/features/space-field/space-dir';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import AppAsks from '@/next/features/app-views/asks';
import AppFrame, { fromApp } from '@/next/features/app-views/app-frame';
import AppUnavailable from '@/next/features/app-views/app-unavailable';
import {
  appService,
  isSpaceApp,
  SERVICE_STATE,
  useAppService,
} from '@/next/features/app-views/service-health';
import { SpacePageWait, SpaceSkeleton } from '@/next/features/app-views/space-skeleton';
import {
  onPage,
  callMessage,
  spaceChatContext,
  spaceCleared,
  spaceContextOf,
} from '@/next/features/app-views/space-context';
import { ConsentStage } from '@/next/features/app-consent/consent-ask';
import { useInstalledApps, type IAppView, type IInstalledApp } from '@/next/features/app-views/api';
import ConsentBlock from '@/next/features/app-consent/consent-block';
import AppInspector from '@/next/features/app-views/inspector';
import { onOpenAppTab } from '@/next/features/app-views/open';
import { keptFrames } from '@/next/features/app-views/kept-frames';
import { parseAppTarget } from '@/next/features/app-views/target';
import type { IAppMenuItem } from '@/next/features/app-main/app-menu';
import type { IHostAction } from '@/next/features/app-views/bridge';
import AppStage, { appStageDesign } from '@/next/features/app-main/app-stage';
import type { TChatOpeners } from '@/next/bindings/chat-view';
import { startFolder } from '@/next/bindings/home-composer';
import { useSiteStage } from '@/next/bindings/site-stage';
import { useChatApps } from '@/next/bindings/use-chat-apps';
import { setConversationApps } from '@/next/commands';
import { settled } from '@/next/bindings/outcome';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { useConversation, useConversationList, useConvsAnswered } from '@/next/stores/conversations';
import { useDefaultCwd } from '@/next/stores/projects';
import { useTabTitle } from '@/next/stores/tabs';
import { toast } from '@/next/ui/toast';
import { appBasePath } from '@/platform/config';
import { DOCS_APP, docsReaderPath } from '@/platform/router';
import { claimSpaceOpens } from '@/next/features/pane/space-claim';
import { BOARDS_APP } from '@/next/features/pane/board-link';
import ObjectShare, { type TShareKind } from '@/next/features/app-main/object-share';
import { cardKindOf, cardSlugOf } from '@/next/features/chat/link-card';
import { t } from '@/next/i18n';
import { useAppsDebug } from '@/next/features/app-views/debug-mode';

export interface IAppMainPageProps {
  /** The machine of the screen; the chat's own machine wins when the address names a chat. */
  machine: MachineId;
  id: string;
  /** One of the app's objects open (`?o=`); null: the app's home. */
  object: string | null;
  /** A comment thread on the object to open (`?th=`, a mention in the bell). */
  thread?: string | null;
  chat: string | null;
  onOpenDrawer: () => void;
  /** Where a link, a file or a card of the chat opens (the pane beside the app). */
  open?: TChatOpeners;
  /** The right pane of the chat, as beside a design. */
  toolPane?: { open: boolean; onToggle: () => void } | null;
}

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

interface IStageApp {
  app: IInstalledApp;
  view: IAppView;
  target: 'app' | 'pane';
}

interface IAppStagePageProps extends Omit<IAppMainPageProps, 'machine'>, IStageApp {
  machine: MachineId;
  viewPort: number;
  agentWalled: boolean;
  /** The person's spaces with a window (boards, documents): each one's window is kept once opened. */
  spaces: readonly IStageApp[];
}

/** The space that opens each kind of our links. */
const SPACE_OF_CARD = { board: BOARDS_APP, doc: DOCS_APP } as const;
/** What «Поделиться» shares in each app of the boards and documents. */
const SHARE_KIND = { [BOARDS_APP]: 'board', [DOCS_APP]: 'artifact' } as const satisfies Record<
  string,
  TShareKind
>;

/** The one layout of the spaces' chat window, whichever space is open. */
const SPACE_LAYOUT = 'app:spaces';

/** What each space's view has open inside itself, kept past a new mount of the page (a chat put in its address). */
const openInView = new Map<string, string>();

/** The data a space's window is given: what to open in it, and the thread on it (a mention in the bell). */
const useSpaceData = (opens: string | null, thread: string | null) =>
  useMemo(() => (opens ? { object: opens, ...(thread ? { thread } : {}) } : undefined), [opens, thread]);

/** What the host's own strip offers, for a view that draws its own bar instead. */
interface IHostActions {
  actions: readonly IHostAction[];
  run: (id: string) => void;
}

interface IStageFrameProps extends IStageApp {
  machine: MachineId;
  viewPort: number;
  conv: ConvId | null;
  reload: number;
  space: boolean;
  /** On screen: the others of the spaces wait hidden, alive, with what they had open. */
  active: boolean;
  object: string | null;
  opens: string | null;
  thread: string | null;
  /** The page's calls, the same for every window: each window names itself. */
  sendFrom: (name: string, text: string) => Promise<void>;
  goIn: (app: string, object: string | null) => void;
  /** `hl.pane.open('share:<slug>')` of a view: «Поделиться» of one of its objects. */
  shareObject: (app: string, slug: string) => void;
  publish?: (app: string, data: unknown, frame: DOMRect | null) => void;
  /** The host's actions for a view that draws its own bar, read when the view asks. */
  host: RefObject<IHostActions>;
}

/** One app's window on the stage: its view, or why its service does not open now. */
const StageFrame = ({
  machine,
  app,
  view,
  target,
  viewPort,
  conv,
  reload,
  space,
  active,
  object,
  opens,
  thread,
  sendFrom,
  goIn,
  shareObject,
  publish,
  host,
}: IStageFrameProps) => {
  // Handlers that never change for the window: its bridge keeps the port it shook hands on.
  const handlers = useMemo(
    () => ({
      chatSend: (text: string) => sendFrom(app.name, text),
      hostActions: () => host.current.actions,
      hostRun: (id: string) => host.current.run(id),
    }),
    [sendFrom, app.name, host],
  );
  const onOpenObject = useCallback((to: string | null) => goIn(app.id, to), [goIn, app.id]);
  const onShareObject = useCallback((slug: string) => shareObject(app.id, slug), [shareObject, app.id]);
  const onPublish = useMemo(
    () => (publish ? (data: unknown, frame: DOMRect | null) => publish(app.id, data, frame) : undefined),
    [publish, app.id],
  );
  // A built-in app whose service is down shows «сейчас не открывается» in place of its view, and the view once it is back.
  const service = appService(app.id);
  const health = useAppService(machine, app.id);
  const data = useSpaceData(opens, thread);
  // The app's own page is its one view: a pane view of it kept out of sight goes, rather than two players sounding.
  useEffect(() => keptFrames.dropApp(app.id), [app.id]);
  const body =
    service !== null && health.state === SERVICE_STATE.down ? (
      <AppUnavailable
        service={service}
        object={object !== null}
        icon={app.icon || 'layout-grid'}
        checking={health.checking}
        onRetry={health.retry}
      />
    ) : (
      <ConsentStage machine={machine} app={app}>
        <AppFrame
          machine={machine}
          app={app}
          view={view}
          target={target}
          viewPort={viewPort}
          conversation={conv}
          reloadToken={reload}
          data={data}
          handlers={handlers}
          onOpenObject={onOpenObject}
          onShareObject={onShareObject}
          skeleton={space ? <SpaceSkeleton object={object !== null} /> : undefined}
          onPublish={onPublish}
        />
      </ConsentStage>
    );
  if (!space) return body;
  return (
    <div className="hx-spaceframe" data-on={active} inert={!active} aria-hidden={!active}>
      {body}
    </div>
  );
};

/** The app in Sinopia's stage, once the app and its view are known. */
const AppStagePage = ({
  machine,
  id,
  object,
  thread,
  chat,
  app,
  view,
  target,
  viewPort,
  agentWalled,
  spaces,
  onOpenDrawer,
  open,
  toolPane = null,
}: IAppStagePageProps) => {
  const [inspector, setInspector] = useState<'closed' | 'about' | 'debug'>('closed');
  const debugMode = useAppsDebug();
  const [reload, setReload] = useState(0);
  const chats = useConversationList(machine);
  const space = isSpaceApp(id);
  const defaultCwd = useDefaultCwd(machine);
  const spaceDir = useSpaceDir(machine, space, startFolder(chats, machine, defaultCwd ?? null));
  const fromChat = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(fromChat);
  // The spaces share one chat: the chats any of them is on in are theirs, as a design's chats are the design's.
  const spaceIds = useMemo(() => (space ? spaces.map((s) => s.app.id) : [id]), [space, spaces, id]);
  const tied = useMemo(
    () => (chats ?? []).filter((c) => c.apps?.some((a) => spaceIds.includes(a))).map((c) => c.id as string),
    [chats, spaceIds],
  );
  const address = useMemo(
    () => ({ kind: ROUTE.app, id, ...(object ? { object } : {}) }) as const,
    [id, object],
  );
  const appObject = useMemo(() => (object ? { app: id, object } : null), [id, object]);
  const stage = useSiteStage({
    machine,
    design: appStageDesign(id, object),
    fromChat,
    // A space's chat starts in the machine's spaces' folder, not the last chat's project.
    dir: record?.cwd ?? spaceDir ?? null,
    tied,
    title: app.name,
    pageUrl: '',
    port: null,
    open,
    keep: true,
    // The boards and the documents share one place for the chat: going from one to the other it stays put.
    ...(space ? { space: { layoutKey: SPACE_LAYOUT } } : {}),
    app: {
      address,
      // The open board or document takes the voice's quick edits through its app.
      object: appObject,
      // A chat made over a space is the chat of all of them: each space's window gets it, wherever the person goes.
      onCreated: (made) => void settled(setConversationApps(machine, made, spaceIds)).catch(() => undefined),
    },
  });
  const conv = stage.conv ?? null;
  const chatApps = useChatApps(machine, conv);
  useTabTitle(routePath({ kind: ROUTE.app, id, ...(object ? { object } : {}) }), object ?? app.name);

  // A card's «Открыть в панели» of another app takes this page to that app; this one is already open. The chat goes
  // along only where that app is on.
  const chatOn = useConversation(conv)?.apps;
  useEffect(
    () =>
      onOpenAppTab((to) => {
        const next = parseAppTarget(to);
        if (next && next.app !== id)
          goTo({
            kind: ROUTE.app,
            id: next.app,
            ...(conv && chatOn?.includes(next.app) ? { chat: conv } : {}),
          });
      }),
    [id, conv, chatOn],
  );
  // A chat of one space that another space is not on in yet gets it when the person goes there: one chat over all.
  useEffect(() => {
    if (!space || conv === null || chatOn === undefined || chatOn.includes(id)) return;
    if (!chatOn.some((a) => spaceIds.includes(a))) return;
    void settled(setConversationApps(machine, conv, [...new Set([...chatOn, id])])).catch(() => undefined);
  }, [space, conv, chatOn, id, spaceIds, machine]);

  // The chat goes along to the object and back home: it is the one the person works in.
  const goIn = useCallback(
    (appId: string, to: string | null) =>
      goTo({ kind: ROUTE.app, id: appId, ...(to ? { object: to } : {}), ...(conv ? { chat: conv } : {}) }),
    [conv],
  );
  const go = useCallback((to: string | null) => goIn(id, to), [goIn, id]);
  // What the agent opens of the spaces (`harnessly open` a board or a document) opens here, in the space's own window,
  // not in the right pane beside it.
  useEffect(() => {
    if (!space || conv === null) return;
    return claimSpaceOpens(conv, (href) => {
      const kind = cardKindOf(href);
      const slug = cardSlugOf(href);
      const to = kind === null ? null : SPACE_OF_CARD[kind];
      if (to === null || slug === null || !spaceIds.includes(to)) return false;
      goIn(to, slug);
      return true;
    });
  }, [space, conv, spaceIds, goIn]);
  const { ask, fill, context, anchor, fold } = stage;
  // What each space's view has open inside itself: a board opened in the view never reaches the page's
  // address, and a view kept hidden keeps it while the person is in another space.
  const [inView, setInViewState] = useState<Readonly<Record<string, string>>>(() =>
    Object.fromEntries(openInView),
  );
  const setInView = useCallback((appId: string, slug: string | null) => {
    if (slug === null) openInView.delete(appId);
    else openInView.set(appId, slug);
    setInViewState((was) => {
      if ((was[appId] ?? null) === slug) return was;
      const next = { ...was };
      if (slug === null) delete next[appId];
      else next[appId] = slug;
      return next;
    });
  }, []);
  const on = conv !== null && (chatOn?.includes(id) ?? false);
  // The stage's calls are new on each of its renders; the view's bridge is rebuilt when its handlers change, and a view
  // that already shook hands keeps the old bridge's port: so the frame gets handlers that never change.
  const live = useRef({ ask, fill, context, anchor, fold, on, id });
  useLayoutEffect(() => {
    live.current = { ask, fill, context, anchor, fold, on, id };
  });
  // A space's view says what is open and selected: the chip of the chat's next message, and the place its
  // window opens beside. A call written on the board puts its words in the field, over the board, for the person.
  // Only the space on screen speaks to the chat; a hidden one only keeps what it has open.
  const publish = useCallback(
    (appId: string, data: unknown, frame: DOMRect | null) => {
      const now = live.current;
      const shown = now.id === appId;
      const ctx = spaceContextOf(data);
      if (!ctx) {
        // Back to the list: nothing is open, the chip and the place go.
        if (spaceCleared(data)) {
          setInView(appId, null);
          if (!shown) return;
          now.context?.(null);
          now.anchor?.(null);
          now.fold?.();
        }
        return;
      }
      if (ctx.object !== null) setInView(appId, ctx.object);
      if (!shown) return;
      now.context?.(spaceChatContext(ctx));
      now.anchor?.(onPage(ctx.rect, frame));
      if (ctx.call === null) return;
      // A call in a comment goes to the agent at once and is answered in its thread; one written on the board waits in
      // the field for the person's send.
      const message = callMessage(ctx);
      if (message === null) now.fill?.(ctx.call);
      // A board's or a document's comment that calls the agent is the hub's (board_call.go): it reaches the
      // agent with the board or the document closed too, so the page does not send it a second time.
      else if (appId !== BOARDS_APP && appId !== DOCS_APP) now.ask?.(message);
    },
    [setInView],
  );
  // The windows on the stage: the app's own, or each space opened on this page so far, kept alive.
  const [visited, setVisited] = useState<readonly string[]>([id]);
  if (space && !visited.includes(id)) setVisited([...visited, id]);
  const frames = useMemo(
    () =>
      space
        ? [
            ...spaces.filter((s) => visited.includes(s.app.id)),
            ...(spaces.some((s) => s.app.id === id) ? [] : [{ app, view, target } satisfies IStageApp]),
          ]
        : [{ app, view, target } satisfies IStageApp],
    [space, spaces, visited, app, view, target, id],
  );
  // The view's words go to the chat beside it where the app is on. Elsewhere they wait in the chat's field: the person's
  // own send makes the chat and turns the app on in it, never the view.
  const sendFrom = useCallback(async (name: string, text: string) => {
    const now = live.current;
    if (now.on) now.ask?.(fromApp(name, text));
    else now.fill?.(text);
  }, []);

  // The page's own base (/m/<machine> on the hub) goes into the link, as the chat's «Скопировать ссылку» does.
  const appLink = `${window.location.origin}${appBasePath()}${routePath({ kind: ROUTE.app, id, ...(object ? { object } : {}) })}`;
  // A board or a document is shared as in Sinopia, with its people and links; the app's home and other apps
  // copy the link to the app.
  const [sharing, setSharing] = useState<{ kind: TShareKind; slug: string } | null>(null);
  const shareObject = useCallback((appId: string, slug: string) => {
    const kind = SHARE_KIND[appId as keyof typeof SHARE_KIND];
    if (kind) setSharing({ kind, slug });
  }, []);
  const share = () => {
    if (object && (id === BOARDS_APP || id === DOCS_APP)) {
      shareObject(id, object);
      return;
    }
    const link = appLink;
    navigator.clipboard.writeText(link).then(
      () => toast(t('next.appmain.shared'), { description: t('next.appmain.sharedHint') }),
      () => toast(t('next.appmain.shareFailed'), { tone: 'error', description: link }),
    );
  };
  const menu: IAppMenuItem[] = [
    {
      id: 'about',
      icon: 'info',
      label: t('next.appmain.about'),
      onSelect: () => setInspector((now) => (now === 'about' ? 'closed' : 'about')),
    },
    {
      id: 'settings',
      icon: 'settings',
      label: t('next.appmain.settings'),
      onSelect: () => goTo({ kind: ROUTE.apps }),
    },
    ...(app.dev || debugMode
      ? [
          {
            id: 'debug',
            icon: 'bug',
            label: t('next.appmain.debug'),
            onSelect: () => setInspector((now) => (now === 'debug' ? 'closed' : 'debug')),
          },
        ]
      : []),
    ...(conv && chatApps.on.includes(app.id)
      ? [
          {
            id: 'off',
            icon: 'x',
            label: t('next.appmain.off'),
            danger: true,
            onSelect: () => void chatApps.turn(app.id, false),
          },
        ]
      : []),
    // The app sees this chat only where the person turned it on: the page offers it, it never takes it.
    ...(conv && !chatApps.on.includes(app.id)
      ? [
          {
            id: 'on',
            icon: 'plus',
            label: t('next.appmain.on'),
            onSelect: () => void chatApps.turn(app.id, true),
          },
        ]
      : []),
  ];

  // A space, or a view that draws its own bar, shows no strip of the host: the view shows these actions in its own bar.
  const bare = space || (target === 'app' && view.chrome === 'none');
  const host = useRef<IHostActions>({ actions: [], run: () => undefined });
  useLayoutEffect(() => {
    const items = [
      { id: 'share', label: t('next.appmain.share'), icon: 'share', run: share },
      ...(toolPane
        ? [
            {
              id: 'pane',
              label: t('next.header.pane'),
              icon: 'panel-right',
              pressed: toolPane.open,
              run: toolPane.onToggle,
            },
          ]
        : []),
      ...menu.map((m) => ({
        id: m.id,
        label: m.label,
        icon: m.icon,
        ...(m.danger ? { danger: true } : {}),
        run: m.onSelect,
      })),
    ];
    host.current = {
      actions: items.map(({ run: _run, ...action }) => action),
      run: (id) => items.find((i) => i.id === id)?.run(),
    };
  });

  return (
    <>
      <AppStage
        bare={bare}
        title={object ?? (space ? app.name : t('next.appmain.home'))}
        onBack={object ? () => go(null) : undefined}
        stage={stage}
        menu={menu}
        onShare={share}
        toolPane={toolPane}
        onOpenDrawer={onOpenDrawer}
        space={space}
        frame={frames.map((f) => {
          const shown = f.app.id === id;
          return (
            <StageFrame
              key={f.app.id}
              machine={machine}
              app={f.app}
              view={f.view}
              target={f.target}
              viewPort={viewPort}
              conv={conv}
              reload={shown ? reload : 0}
              space={space}
              active={shown}
              object={shown ? object : null}
              opens={shown ? (object ?? inView[f.app.id] ?? null) : (inView[f.app.id] ?? null)}
              thread={shown ? (thread ?? null) : null}
              sendFrom={sendFrom}
              goIn={goIn}
              shareObject={shareObject}
              publish={space ? publish : undefined}
              host={host}
            />
          );
        })}
        inspector={
          inspector === 'closed' ? null : (
            <AppInspector
              machine={machine}
              app={app}
              view={view}
              target={target}
              conversation={conv}
              debug={inspector === 'debug'}
              onReload={() => setReload((n) => n + 1)}
              agentWalled={agentWalled}
            />
          )
        }
      />
      <AppAsks machine={machine} conversation={conv} />
      {sharing && (
        <ObjectShare
          key={`${sharing.kind}:${sharing.slug}`}
          kind={sharing.kind}
          slug={sharing.slug}
          open
          onClose={() => setSharing(null)}
          fallbackLink={appLink}
        />
      )}
    </>
  );
};

const AppMainPage = ({
  machine: screen,
  id,
  object,
  thread = null,
  chat,
  onOpenDrawer,
  open,
  toolPane,
}: IAppMainPageProps) => {
  const conv = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(conv);
  const answered = useConvsAnswered(record?.machineId ?? screen);
  const machine = record?.machineId ?? screen;
  const { list, error } = useInstalledApps(machine);

  const app = list?.apps.find((a) => a.id === id) ?? null;
  // A document's address came here from the reader (docs-redirect.ts); without the documents app the reader shows it.
  const toReader = list !== null && !app && id === DOCS_APP && object !== null;
  useEffect(() => {
    if (toReader && object !== null) window.location.replace(`${appBasePath()}${docsReaderPath(object)}`);
  }, [toReader, object]);
  // An app with no screen of its own opens its pane view here, so a press in the sidebar never ends on a blank page.
  const own = app?.views.find((v) => v.targets.includes('app'));
  const view = own ?? app?.views.find((v) => v.targets.includes('pane')) ?? null;
  // The person's spaces with a window of their own: their windows stay alive on one stage.
  const spaces = useMemo(
    () =>
      (list?.apps ?? []).flatMap((a): IStageApp[] => {
        if (!isSpaceApp(a.id)) return [];
        const v = a.views.find((x) => x.targets.includes('app'));
        return v ? [{ app: a, view: v, target: 'app' }] : [];
      }),
    [list],
  );

  if (error) return <p className="hx-appview__error">{error}</p>;
  // The chat named by the address is not known yet: the page waits for the list rather than showing the app alone and
  // then pulling a column in.
  // A space shows its own shape from the first frame; another app its quiet wait.
  const wait = isSpaceApp(id) ? (
    <SpacePageWait object={object !== null} />
  ) : (
    <div className="hx-appview__wait" aria-busy="true" />
  );
  if (list === null || (conv !== null && record === undefined && !answered)) return wait;
  if (toReader) return wait;
  if (!app) return <p className="hx-appview__error">{t('next.appmain.gone')}</p>;
  // An app with a window that waits for consent shows the bar over it (ConsentStage); one with no window says
  // what it waits for here.
  if (!view && app.consent) return <ConsentBlock className="hx-consentblock--page" app={app} />;
  if (!view) return <p className="hx-appview__error">{t('next.appmain.noScreen')}</p>;

  return (
    <AppStagePage
      machine={machine}
      id={id}
      object={object}
      thread={thread}
      chat={chat}
      app={app}
      view={view}
      target={own ? 'app' : 'pane'}
      spaces={spaces}
      viewPort={list.viewPort}
      agentWalled={list.agentWalled}
      onOpenDrawer={onOpenDrawer}
      open={open}
      toolPane={toolPane}
    />
  );
};

export default AppMainPage;
