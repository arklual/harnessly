// Designly over the machine's library: loads it with the design systems, asks again while a card is drawing (a
// conversation in its folder works), starts a new project from a brief and waits for the runtime before the field
// clears — the card comes from the library itself. A pin waits for the machine too.
import { useTourFill } from '@/next/features/tour/use-tour-fill';
import { uploadSrc } from '@/next/commands/uploads';
import { AttachmentSrc } from '@/next/ui/attachment-thumb';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import DesignlyHome, {
  DESIGN_KIND,
  type IDesignCard,
  type IDesignStart,
  type TDesignKind,
} from '@/next/features/designly/designly-home';
import {
  createDesign,
  fetchLibrary,
  fetchSystems,
  pinDesign,
  restoreDemoDesigns,
  thumbPath,
  type IDesign,
  type IDesignSystem,
} from '@/next/features/designly/api';
import { useLibraryOrganizer } from '@/next/bindings/use-library-organizer';
import type { ILibraryLayout } from '@/next/features/designly/api';
import { useLinkRestored } from '@/next/stores/connection';
import { useConversationList } from '@/next/stores/conversations';
import { bindDesignConv } from '@/next/bindings/design-conv';
import { designKey } from '@/next/features/designly/design-conv';
import type { Conversation, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { toast } from '@/next/ui/toast';
import { fetchKitCards, kitCover, shelfCards, type IKitCard } from '@/next/features/designly/kit/kit-api';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { rememberChoice } from '@/next/features/composer/start-choice';
import type { IModelPickerProps } from '@/next/features/composer/model-picker';
import { useVoice } from '@/next/bindings/use-voice';
import { useUploads } from '@/next/bindings/use-uploads';
import { clearDraft, DESIGN_START_DRAFT, setDraftText, useDraft } from '@/next/stores/drafts';
import {
  createKindDocument,
  loadKindDocs,
  loadKinds,
  type IAppKind,
  type IKindDoc,
} from '@/next/features/app-catalog/api';
import { t } from '@/next/i18n';

/** How often the library is asked again while an agent draws. */
const DRAWING_POLL_MS = 5000;
const TITLE_MAX = 42;

/** A conversation of the project is starting or in a turn. */
const isDrawing = (design: IDesign, working: ReadonlySet<string>) =>
  design.convs.some((id) => working.has(id));

const workingOf = (list: Conversation[] | null) =>
  new Set((list ?? []).filter((c) => c.state.k === 'starting' || c.state.k === 'working').map((c) => c.id));

/** Only the owner deletes a project; a runtime that restricts nobody names no role, and then the owner is who made it. */
const DELETERS: ReadonlySet<string> = new Set(['owner']);

const titleOf = (brief: string) => (brief.length > TITLE_MAX ? `${brief.slice(0, TITLE_MAX)}…` : brief);

const cardOf = (
  m: MachineId,
  design: IDesign,
  working: ReadonlySet<string>,
  layout: ILibraryLayout,
): IDesignCard => {
  const board = design.boards[0];
  return {
    id: design.id,
    title: design.title,
    boards: design.boards.length,
    updatedAt: design.updatedAt,
    image: board ? thumbPath(m, design.id, board.path, design.updatedAt) : null,
    kind: design.kind,
    pinned: design.pinned,
    drawing: isDrawing(design, working),
    owner: design.owner ?? null,
    canDelete: design.role === undefined ? design.owner?.me === true : DELETERS.has(design.role),
    folder: layout.placed[design.id] ?? null,
    system: layout.systems[design.id] ?? null,
  };
};

/** A document of an app's type as a card: no picture, no boards; the type's name stands where a design shows its kind. */
const appDocCard = (doc: IKindDoc): IDesignCard => ({
  id: doc.document,
  title: doc.title,
  boards: 0,
  updatedAt: doc.updatedAt,
  image: null,
  kind: 'site',
  pinned: false,
  drawing: false,
  owner: null,
  canDelete: false,
  appKind: doc.kindTitle,
});

const useNow = (ms: number) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(timer);
  }, [ms]);
  return now;
};

/** The machine's library as the cards show it: read on open and again when the link returns, kept while an agent draws. */
const useDesignLibrary = (machine: MachineId, withAppDocs = false) => {
  const [designs, setDesigns] = useState<IDesign[] | null>(null);
  // The documents of the types apps add; an older runtime, or none, lists none (they are an addition, not the library).
  const [appDocs, setAppDocs] = useState<IKindDoc[]>([]);
  const [error, setError] = useState<string | null>(null);
  const convs = useConversationList(machine);
  const [attempt, setAttempt] = useState(0);
  const now = useNow(60_000);
  const loaded = useRef(false);
  // The library on screen stays when the link goes; when it returns the library is read again, quietly.
  const restored = useLinkRestored(machine);

  const load = useCallback(
    (signal?: AbortSignal) =>
      fetchLibrary(machine, signal).then(
        async (library) => {
          if (withAppDocs) setAppDocs(await loadKindDocs(machine).catch(() => []));
          loaded.current = true;
          setDesigns(library.designs);
          setError(null);
        },
        (failure: Error) => {
          if (signal?.aborted) return;
          // A failed refresh keeps the cards on screen; only the first load turns into the error.
          if (!loaded.current) setError(failure.message);
        },
      ),
    [machine, withAppDocs],
  );

  useEffect(() => {
    const abort = new AbortController();
    void load(abort.signal);
    return () => abort.abort();
  }, [load, attempt, restored]);

  const working = useMemo(() => workingOf(convs), [convs]);

  const drawing = designs?.some((d) => isDrawing(d, working)) ?? false;
  useEffect(() => {
    if (!drawing) return;
    const timer = setInterval(() => void load(), DRAWING_POLL_MS);
    return () => clearInterval(timer);
  }, [drawing, load]);

  const forget = useCallback((ids: string[]) => {
    const gone = new Set(ids);
    setDesigns((list) => list?.filter((d) => !gone.has(d.id)) ?? null);
  }, []);
  const reload = useCallback(() => load(), [load]);
  const { layout, organizer, readLayout } = useLibraryOrganizer(machine, reload, forget);

  const cards = useMemo(() => {
    if (designs === null) return null;
    return [...designs.map((design) => cardOf(machine, design, working, layout)), ...appDocs.map(appDocCard)];
  }, [designs, appDocs, machine, working, layout]);
  const appDocIds = useMemo(() => new Set(appDocs.map((d) => d.document)), [appDocs]);

  const onPin = async (id: string, pinned: boolean) => {
    try {
      await pinDesign(machine, id, pinned);
    } catch (failure) {
      toast(t('next.designly.pinFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
    setDesigns((list) => list?.map((d) => (d.id === id ? { ...d, pinned } : d)) ?? null);
  };

  const onDelete = (id: string) => organizer.onDeleteMany([id]);

  const onRestoreDemo = async () => {
    try {
      await restoreDemoDesigns(machine);
    } catch (failure) {
      toast(t('next.designly.restoreDemoFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
    // The copies lie in the person's «Демо» folder: the library and the layout are read again.
    await Promise.all([load(), readLayout()]);
  };

  const onRetry = () => {
    loaded.current = false;
    setDesigns(null);
    setError(null);
    setAttempt((n) => n + 1);
  };

  return { cards, error, now, load, onPin, onDelete, onRestoreDemo, onRetry, appDocIds, organizer };
};

interface IDesignlyLibraryProps {
  machine: MachineId;
  onOpen: (id: string) => void;
}

/** The library alone, for the right pane: its chat is the field that starts a design, so there is none here. */
export const DesignlyLibrary = ({ machine, onOpen }: IDesignlyLibraryProps) => {
  // The documents of the types apps add are listed here as in the whole library; opening one is the pane's to tell.
  const { cards, error, now, onPin, onDelete, onRestoreDemo, onRetry, organizer } = useDesignLibrary(
    machine,
    true,
  );
  return (
    <DesignlyHome
      cards={cards}
      error={error}
      now={now}
      onPin={onPin}
      onDelete={onDelete}
      organizer={organizer}
      onRestoreDemo={onRestoreDemo}
      onOpen={onOpen}
      onRetry={onRetry}
      compose={null}
    />
  );
};

interface IDesignlyViewProps {
  machine: MachineId;
  /** The machines a design can be made on, for a person with several; picking one makes it the open machine. */
  machines?: IModelPickerProps['machine'];
  /** The kind the field opens on; an address of its own can ask for one. */
  initialKind?: TDesignKind;
}

/** The draft of the start's field, one for every kind: the brief and its uploaded files survive a reload. */
const START_DRAFT = DESIGN_START_DRAFT;
const setBrief = (value: string) => setDraftText(START_DRAFT, value);

const DesignlyView = ({ machine, machines = null, initialKind }: IDesignlyViewProps) => {
  const { cards, error, now, load, onPin, onDelete, onRestoreDemo, onRetry, appDocIds, organizer } =
    useDesignLibrary(machine, true);
  const [systems, setSystems] = useState<IDesignSystem[] | null>(null);
  const [kitsAdded, setKitsAdded] = useState(0);
  const restored = useLinkRestored(machine);

  // The systems are only an offer: without them the field has no chip, nothing else changes.
  useEffect(() => {
    const abort = new AbortController();
    fetchSystems(machine, abort.signal).then(setSystems, () => {
      // Without a list the field simply has no chip; a returning link asks again.
      if (!abort.signal.aborted) setSystems((have) => have ?? []);
    });
    return () => abort.abort();
  }, [machine, restored, kitsAdded]);

  // The installed kits for the library's row; without them the row is simply not there.
  const [kitCards, setKitCards] = useState<IKitCard[] | null>(null);
  useEffect(() => {
    const abort = new AbortController();
    fetchKitCards(machine, abort.signal).then(setKitCards, () => {
      if (!abort.signal.aborted) setKitCards([]);
    });
    return () => abort.abort();
  }, [machine, restored, kitsAdded]);

  // The agent that draws starts the way a new chat does: the person's pick, else the CLI settings file's model and
  // effort, else the last pick. A design starts through its own request, which has no backend profile.
  const { choice, agents, onPick } = useStartChoice(machine, true);

  const draft = useDraft(START_DRAFT);
  const brief = draft?.text ?? '';
  const uploads = useUploads(machine, START_DRAFT, draft?.attachments ?? []);
  const voice = useVoice(machine, brief, setBrief);
  useTourFill(brief, setBrief);

  // The document types installed apps add are only an offer: an older runtime, or none installed, shows no tile.
  const [appKinds, setAppKinds] = useState<IAppKind[] | null>(null);
  useEffect(() => {
    let live = true;
    loadKinds(machine).then(
      (kinds) => live && setAppKinds(kinds),
      () => live && setAppKinds(null),
    );
    return () => {
      live = false;
    };
  }, [machine, restored]);

  const onAppKind = async (kind: IAppKind, name: string) => {
    const title = name ? titleOf(name) : kind.title;
    let created: { document: string };
    try {
      created = await createKindDocument(machine, kind.id, title);
    } catch (failure) {
      toast(t('next.designly.createFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
    // The new document opens at once, in the app's editor.
    goTo({ kind: ROUTE.appDoc, id: created.document, edit: true });
  };

  const srcOf = useCallback((item: { id: string }) => uploadSrc(machine, item), [machine]);

  const onCreate = async ({ brief, kind, designSystem, deck, video, attachmentIds }: IDesignStart) => {
    const title = titleOf(brief);
    if (choice) rememberChoice(choice);
    let id: string;
    try {
      const created = await createDesign(machine, {
        title,
        brief,
        kind,
        provider: choice?.provider ?? 'claude',
        model: choice?.model ?? null,
        effort: choice?.effort ?? null,
        designSystem,
        ...(deck ? { deck } : {}),
        ...(video ? { video } : {}),
        ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
      });
      id = created.id;
      // The chat that draws the new design is the one its page opens on.
      bindDesignConv(designKey(machine, created.id), { id: created.conv, fresh: true });
    } catch (failure) {
      toast(t('next.designly.createFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
    clearDraft(START_DRAFT);
    uploads.reset();
    toast(t('next.designly.started', { title }));
    // A deck and a video are made inside their project; a site or a design system draws on its card here.
    if (kind === DESIGN_KIND.slides || kind === DESIGN_KIND.video) {
      goTo({ kind: ROUTE.site, id, edit: true });
      return;
    }
    await load();
  };

  return (
    <AttachmentSrc.Provider value={srcOf}>
      <DesignlyHome
        cards={cards}
        error={error}
        now={now}
        onPin={onPin}
        onDelete={onDelete}
        organizer={organizer}
        onRestoreDemo={onRestoreDemo}
        onOpen={(id) => goTo(appDocIds.has(id) ? { kind: ROUTE.appDoc, id } : { kind: ROUTE.site, id })}
        onRetry={onRetry}
        kits={
          kitCards
            ? {
                cards: shelfCards(kitCards),
                cover: (k) => kitCover(machine, { id: k.kit, version: k.version, cover: k.cover }),
                onOpen: (k) => goTo({ kind: ROUTE.kit, id: k.kit, ...(k.theme ? { theme: k.theme } : {}) }),
              }
            : undefined
        }
        compose={{
          machine,
          initialKind,
          systems,
          onKitAdded: () => setKitsAdded((n) => n + 1),
          onCreate,
          brief,
          onBrief: setBrief,
          uploads,
          picker: choice && {
            value: choice,
            agents,
            locked: false,
            offersUltracode: false,
            onChange: onPick,
            machine: machines,
          },
          appKinds,
          onAppKind,
          voice: {
            recording: voice.recording,
            onMic: voice.onMic,
            onCancel: voice.onCancelRecording,
            onFinish: voice.onFinishRecording,
          },
        }}
      />
    </AttachmentSrc.Provider>
  );
};

export default DesignlyView;
