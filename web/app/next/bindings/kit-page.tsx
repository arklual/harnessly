// The kit screen over the machine (HRN-897): reads the theme's canvas, opens the library's preview slot for the live
// pages (entered once by a hidden frame, then plain addresses), reads where the kit is used and the decks a layout can
// go into, and turns the screen's actions into the machine's: a template as a project, a slide added to a deck, a text
// put into a chat's field (never sent by itself).
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import KitScreen from '@/next/features/designly/kit/kit-screen';
import {
  fetchKitCanvas,
  fetchKitUsage,
  openKitProject,
  prepUrl,
  themeFileUrl,
  kitViewPath,
  type IKitCanvas,
  type IKitLayout,
  type IKitLink,
  type TPropValue,
} from '@/next/features/designly/kit/kit-api';
import { chatTextOf, findNode, nameOf } from '@/next/features/designly/kit/kit-node';
import { KitChat, KitTools, useKitEdit, type IKitChip } from '@/next/bindings/kit-edit';
import { KIT_DRAFT_VERSION } from '@/next/features/designly/versions/versions-api';
import type { IDeckChoice } from '@/next/features/designly/kit/kit-panel';
import { fetchLibrary } from '@/next/features/designly/api';
import { addDeckSlide } from '@/next/features/designly/deck/api';
import { openPreview } from '@/next/core/net/preview';
import { slotLeases } from '@/next/features/pane/slot-lease';
import { draftText, NEW_CHAT, setDraftText, type DraftKey } from '@/next/stores/drafts';
import { useTabTitle } from '@/next/stores/tabs';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath, type TRoute } from '@/next/routes/routes';
import LoadFailed from '@/next/ui/load-failed';
import KitSkeleton from '@/next/features/designly/kit/kit-skeleton';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import '@/next/styles/kit.css';

type TKitRoute = Extract<TRoute, { kind: typeof ROUTE.kit }>;

/** The design page's slot: the kit and a design show the same library server, so they never fight over it. */
const PAGE_SLOT = 1;

interface IKitPageProps {
  machine: MachineId;
  route: TKitRoute;
  onOpenDrawer: () => void;
}

type TLoad = { k: 'loading' } | { k: 'failed'; message: string } | { k: 'ready'; canvas: IKitCanvas };

/** How often the draft's canvas is read again while the agent of its chat works, and otherwise, ms. */
const DRAFT_BUSY_MS = 4000;
const DRAFT_IDLE_MS = 15000;

const KitPage = ({ machine, route, onOpenDrawer }: IKitPageProps) => {
  const [load, setLoad] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  // The editing mode (HRN-903): the screen shows the kit's draft with its chat; its edits are read again as they come.
  const editing = route.version === KIT_DRAFT_VERSION;
  const edit = useKitEdit(machine, route.id, editing);
  const [fresh, setFresh] = useState(0);
  const [agentBusy, setAgentBusy] = useState(false);
  // What «Отправить в чат» put over the draft's chat, at the node that was open then.
  const [sent, setSent] = useState<{ at: string | null; chip: IKitChip } | null>(null);
  const [slot, setSlot] = useState<{ origin: string; enterUrl: string } | null>(null);
  const [liveOrigin, setLiveOrigin] = useState<string | null>(null);
  const [usage, setUsage] = useState<IKitLink[] | null>(null);
  const [decks, setDecks] = useState<IDeckChoice[] | null>(null);
  const [port, setPort] = useState<number | null>(null);

  useEffect(() => {
    // The shell keys this page by the kit, version and theme; only «Повторить» asks again here, and it shows the wait.
    const ac = new AbortController();
    fetchKitCanvas(machine, route.id, route.version ?? null, route.theme ?? null, ac.signal).then(
      (canvas) => setLoad({ k: 'ready', canvas }),
      (failure: Error) => {
        // A read of the draft again that fails keeps the canvas there is.
        if (!ac.signal.aborted)
          setLoad((was) =>
            was.k === 'ready' && fresh > 0 ? was : { k: 'failed', message: failure.message },
          );
      },
    );
    return () => ac.abort();
  }, [machine, route.id, route.version, route.theme, attempt, fresh]);

  // The draft's edits are read every few seconds while its agent works, once more as its turn ends, and now and then
  // otherwise (an edit by hand or from another chat).
  const busyBefore = useRef(false);
  useEffect(() => {
    if (!editing) return;
    if (busyBefore.current && !agentBusy) setFresh((n) => n + 1);
    busyBefore.current = agentBusy;
    const timer = setInterval(
      () => {
        if (!document.hidden) setFresh((n) => n + 1);
      },
      agentBusy ? DRAFT_BUSY_MS : DRAFT_IDLE_MS,
    );
    return () => clearInterval(timer);
  }, [editing, agentBusy]);

  // The library: its port for the preview slot, its decks for «Вставить в доску».
  useEffect(() => {
    const ac = new AbortController();
    fetchLibrary(machine, ac.signal).then(
      (lib) => {
        setPort(lib.port);
        setDecks(lib.designs.filter((d) => d.kind === 'slides').map((d) => ({ id: d.id, title: d.title })));
      },
      () => setDecks([]),
    );
    fetchKitUsage(machine, route.id, ac.signal).then(setUsage, () => setUsage([]));
    return () => ac.abort();
  }, [machine, route.id]);

  const canvas = load.k === 'ready' ? load.canvas : null;
  useTabTitle(routePath({ kind: ROUTE.kit, id: route.id }), canvas?.title ?? null);

  // The slot for the live pages, kept for this page like a design's board.
  useEffect(() => slotLeases.pin(PAGE_SLOT, `design:${PAGE_SLOT}`), []);
  useEffect(() => {
    if (port === null || !canvas) return;
    let live = true;
    openPreview(machine, { kind: 'port', port }, PAGE_SLOT, kitViewPath(canvas, 'support.js')).then(
      (opened) => live && setSlot({ origin: opened.origin, enterUrl: opened.enterUrl }),
      () => live && setSlot(null),
    );
    return () => {
      live = false;
    };
  }, [machine, port, canvas]);

  const fileUrl = useCallback(
    (rel: string) => (canvas ? themeFileUrl(machine, canvas, rel) : ''),
    [machine, canvas],
  );
  const prep = useCallback(
    (file: string) => (canvas ? prepUrl(machine, canvas, file) : ''),
    [machine, canvas],
  );

  const onNode = useCallback(
    (node: string | null) =>
      goTo({ ...route, ...(node ? { node } : { node: undefined }) }, { replace: true }),
    [route],
  );

  const copy = useCallback((text: string) => {
    navigator.clipboard
      ?.writeText(text)
      .then(() => toast(t('next.kit.copied')))
      .catch((failure: Error) =>
        toast(t('next.kit.copyFailed'), { tone: 'error', description: failure.message }),
      );
  }, []);

  const textOf = useCallback(
    (node: string, extra: Record<string, TPropValue>): IKitChip | null => {
      if (!canvas) return null;
      const n = findNode(canvas, node);
      if (!n) return null;
      const here = window.location;
      const prefix = here.pathname.replace(/\/designly\/kit\/.*$/, '');
      const link = `${here.origin}${prefix}${routePath({ kind: ROUTE.kit, id: canvas.kit, version: canvas.version, theme: canvas.theme, node })}`;
      const text = chatTextOf(
        canvas,
        n,
        {
          kit: t('next.kit.chat.kit'),
          call: t('next.kit.chat.call'),
          slots: t('next.kit.chat.slots'),
          link: t('next.kit.chat.link'),
        },
        link,
        extra,
      );
      // In the draft the agent edits the theme's files: it is told where they are.
      const where = editing && canvas.base ? `\n${t('next.versions.kit.where')}: ${canvas.base}/` : '';
      return { label: nameOf(n), text: text + where };
    },
    [canvas, editing],
  );

  // What is picked on the draft's canvas goes with the next message of its chat.
  const at = route.node ?? null;
  const chip = useMemo(() => {
    if (!editing) return null;
    if (sent && sent.at === at) return sent.chip;
    return at ? textOf(at, {}) : null;
  }, [editing, sent, at, textOf]);

  const sendToChat = useCallback(
    (node: string, extra: Record<string, TPropValue>) => {
      const made = textOf(node, extra);
      if (!made) return;
      if (editing) {
        setSent({ at, chip: made });
        toast(t('next.versions.kit.inChat'));
        return;
      }
      const text = made.text;
      const chat = route.chat?.startsWith('tab-') ? (route.chat as ConvId) : null;
      const key: DraftKey = chat ?? NEW_CHAT;
      const before = draftText(key);
      setDraftText(key, before ? `${before}\n\n${text}` : text);
      toast(t('next.kit.inField'));
      goTo(chat ? { kind: ROUTE.chat, id: chat } : { kind: ROUTE.home });
    },
    [textOf, editing, at, route.chat],
  );

  // A layout or a project from the draft takes the newest version: the draft is not an address a board keeps.
  const versionOut = editing ? (edit.state?.latest ?? '') : (canvas?.version ?? '');

  const insert = useCallback(
    async (deck: string, layout: IKitLayout) => {
      if (!canvas || !layout.address) return;
      const [kit, ...rest] = layout.address.split('/');
      try {
        await addDeckSlide(machine, deck, `${kit}@${versionOut}/${rest.join('/')}`);
        toast(t('next.kit.inserted'), {
          action: { label: t('next.kit.openDeck'), onClick: () => goTo({ kind: ROUTE.site, id: deck }) },
        });
      } catch (failure) {
        toast(t('next.kit.insertFailed'), { tone: 'error', description: (failure as Error).message });
      }
    },
    [machine, canvas, versionOut],
  );

  const asProject = useCallback(
    async (template: string) => {
      if (!canvas) return;
      try {
        const made = await openKitProject(machine, {
          kit: canvas.kit,
          version: versionOut,
          theme: canvas.theme,
          template,
        });
        goTo({ kind: ROUTE.site, id: made.id });
      } catch (failure) {
        toast(t('next.kit.projectFailed'), { tone: 'error', description: (failure as Error).message });
      }
    },
    [machine, canvas, versionOut],
  );

  const draftProject = edit.state?.draft?.project ?? null;
  const toEdit = () =>
    goTo({
      kind: ROUTE.kit,
      id: route.id,
      version: KIT_DRAFT_VERSION,
      ...(route.theme ? { theme: route.theme } : {}),
    });
  const toView = () =>
    goTo({ kind: ROUTE.kit, id: route.id, ...(route.theme ? { theme: route.theme } : {}) });
  const tools = <KitTools machine={machine} edit={edit} editing={editing} onEdit={toEdit} onDone={toView} />;
  const chat =
    editing && draftProject && edit.state?.mayEdit ? (
      <KitChat
        machine={machine}
        project={draftProject}
        fromChat={route.chat ?? null}
        chip={chip}
        onBusy={setAgentBusy}
        onOpenDrawer={onOpenDrawer}
      />
    ) : undefined;

  const enter = useMemo(() => slot?.enterUrl ?? null, [slot]);

  if (load.k === 'loading') return <KitSkeleton />;
  if (load.k === 'failed')
    return (
      <div className="hx-kit hx-kit--wait">
        <LoadFailed
          reason={load.message}
          size="page"
          onRetry={() => {
            setLoad({ k: 'loading' });
            setAttempt((n) => n + 1);
          }}
        />
      </div>
    );
  return (
    <>
      <KitScreen
        canvas={load.canvas}
        startNode={route.node ?? null}
        fileUrl={fileUrl}
        prepUrl={prep}
        liveOrigin={liveOrigin}
        usage={usage}
        decks={decks}
        onBack={() => goTo({ kind: ROUTE.designly })}
        onTheme={(theme) =>
          goTo(
            {
              kind: ROUTE.kit,
              id: route.id,
              ...(route.version ? { version: route.version } : {}),
              theme,
              ...(route.chat ? { chat: route.chat } : {}),
            },
            { replace: true },
          )
        }
        onNode={onNode}
        onCopy={copy}
        onSendToChat={sendToChat}
        onInsert={insert}
        onOpenAsProject={asProject}
        onOpenBoard={(project) => goTo({ kind: ROUTE.site, id: project })}
        tools={tools}
        chat={chat}
      />
      {enter && !liveOrigin && (
        <iframe
          className="hx-kit-enter"
          src={enter}
          title=""
          aria-hidden="true"
          tabIndex={-1}
          onLoad={() => setLiveOrigin(slot?.origin ?? null)}
        />
      )}
    </>
  );
};

export default KitPage;
