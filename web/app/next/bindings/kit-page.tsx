// The kit screen over the machine: reads the theme's canvas, opens the library's preview slot for the live
// pages (entered once by a hidden frame, then plain addresses), reads where the kit is used and the decks a layout can
// go into, and turns the screen's actions into the machine's: a template as a project, a slide added to a deck, a text
// put into a chat's field (never sent by itself).
import { useCallback, useEffect, useMemo, useState } from 'react';
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
import { chatTextOf, findNode } from '@/next/features/designly/kit/kit-node';
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
import Spinner from '@/next/ui/spinner';
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

const KitPage = ({ machine, route }: IKitPageProps) => {
  const [load, setLoad] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
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
        if (!ac.signal.aborted) setLoad({ k: 'failed', message: failure.message });
      },
    );
    return () => ac.abort();
  }, [machine, route.id, route.version, route.theme, attempt]);

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

  const sendToChat = useCallback(
    (node: string, extra: Record<string, TPropValue>) => {
      if (!canvas) return;
      const n = findNode(canvas, node);
      if (!n) return;
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
      const chat = route.chat?.startsWith('tab-') ? (route.chat as ConvId) : null;
      const key: DraftKey = chat ?? NEW_CHAT;
      const before = draftText(key);
      setDraftText(key, before ? `${before}\n\n${text}` : text);
      toast(t('next.kit.inField'));
      goTo(chat ? { kind: ROUTE.chat, id: chat } : { kind: ROUTE.home });
    },
    [canvas, route.chat],
  );

  const insert = useCallback(
    async (deck: string, layout: IKitLayout) => {
      if (!canvas || !layout.address) return;
      const [kit, ...rest] = layout.address.split('/');
      try {
        await addDeckSlide(machine, deck, `${kit}@${canvas.version}/${rest.join('/')}`);
        toast(t('next.kit.inserted'), {
          action: { label: t('next.kit.openDeck'), onClick: () => goTo({ kind: ROUTE.site, id: deck }) },
        });
      } catch (failure) {
        toast(t('next.kit.insertFailed'), { tone: 'error', description: (failure as Error).message });
      }
    },
    [machine, canvas],
  );

  const asProject = useCallback(
    async (template: string) => {
      if (!canvas) return;
      try {
        const made = await openKitProject(machine, {
          kit: canvas.kit,
          version: canvas.version,
          theme: canvas.theme,
          template,
        });
        goTo({ kind: ROUTE.site, id: made.id });
      } catch (failure) {
        toast(t('next.kit.projectFailed'), { tone: 'error', description: (failure as Error).message });
      }
    },
    [machine, canvas],
  );

  const enter = useMemo(() => slot?.enterUrl ?? null, [slot]);

  if (load.k === 'loading')
    return (
      <div className="hx-kit hx-kit--wait" role="status" aria-label={t('next.kit.loading')}>
        <Spinner />
      </div>
    );
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
