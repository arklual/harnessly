// Designly over the machine's library: loads it with the design systems, asks again while a card is drawing (a
// conversation in its folder works), starts a new project from a brief and waits for the runtime before the field
// clears — the card comes from the library itself. A pin waits for the machine too.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import DesignlyHome, { type IDesignCard, type TDesignKind } from '@/next/features/designly/designly-home';
import {
  createDesign,
  fetchEngine,
  fetchLibrary,
  fetchSystems,
  pinDesign,
  thumbPath,
  type IDesign,
  type IDesignEngine,
  type IDesignSystem,
} from '@/next/features/designly/api';
import type { IDesignlyNotice } from '@/next/features/designly/designly-home';
import { useLinkRestored } from '@/next/stores/connection';
import { useConversationList } from '@/next/stores/conversations';
import { bindDesignConv } from '@/next/bindings/design-conv';
import { designKey } from '@/next/features/designly/design-conv';
import type { Conversation, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { toast } from '@/next/ui/toast';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { rememberChoice } from '@/next/features/composer/start-choice';
import { useVoice } from '@/next/bindings/use-voice';
import { DECK_NEW_DRAFT } from '@/next/features/designly/deck/deck-create';
import { setDraftText } from '@/next/stores/drafts';
import { t } from '@/next/i18n';

/** How often the library is asked again while an agent draws. */
const DRAWING_POLL_MS = 5000;
const TITLE_MAX = 42;

/** A conversation of the project is starting or in a turn. */
const isDrawing = (design: IDesign, working: ReadonlySet<string>) =>
  design.convs.some((id) => working.has(id));

const workingOf = (list: Conversation[] | null) =>
  new Set((list ?? []).filter((c) => c.state.k === 'starting' || c.state.k === 'working').map((c) => c.id));

const titleOf = (brief: string) => (brief.length > TITLE_MAX ? `${brief.slice(0, TITLE_MAX)}…` : brief);

const cardOf = (m: MachineId, design: IDesign, working: ReadonlySet<string>): IDesignCard => {
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
  };
};

const useNow = (ms: number) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(timer);
  }, [ms]);
  return now;
};

/** The machine's library as the cards show it: read on open and again when the link returns, kept while an agent draws. */
const useDesignLibrary = (machine: MachineId) => {
  const [designs, setDesigns] = useState<IDesign[] | null>(null);
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
        (library) => {
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
    [machine],
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

  const cards = useMemo(
    () => designs?.map((design) => cardOf(machine, design, working)) ?? null,
    [designs, machine, working],
  );

  const onPin = async (id: string, pinned: boolean) => {
    try {
      await pinDesign(machine, id, pinned);
    } catch (failure) {
      toast(t('next.designly.pinFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
    setDesigns((list) => list?.map((d) => (d.id === id ? { ...d, pinned } : d)) ?? null);
  };

  const onRetry = () => {
    loaded.current = false;
    setDesigns(null);
    setError(null);
    setAttempt((n) => n + 1);
  };

  return { cards, error, now, load, onPin, onRetry };
};

/** The machine's engine, browser and ffmpeg; null until (or unless) the machine answered. */
const useDesignEngine = (machine: MachineId): IDesignEngine | null => {
  const [engine, setEngine] = useState<IDesignEngine | null>(null);
  const restored = useLinkRestored(machine);
  useEffect(() => {
    const abort = new AbortController();
    // An older runtime has no such route: nothing is said then.
    fetchEngine(machine, abort.signal).then(setEngine, () => undefined);
    return () => abort.abort();
  }, [machine, restored]);
  return engine;
};

/** What the person is told is missing: the engine first (nothing can be made without it), then the tools. */
const noticeOf = (engine: IDesignEngine | null): IDesignlyNotice | null => {
  if (engine === null) return null;
  if (!engine.ready)
    return {
      title: t('next.designly.engine.missing'),
      text: t('next.designly.engine.missingText'),
      command: engine.fetch,
    };
  const tools = [!engine.chrome && 'Chromium', !engine.ffmpeg && 'ffmpeg'].filter(Boolean).join(', ');
  if (!tools) return null;
  return {
    title: t('next.designly.engine.tools', { tools }),
    text: t('next.designly.engine.toolsText'),
    command: null,
  };
};

interface IDesignlyLibraryProps {
  machine: MachineId;
  onOpen: (id: string) => void;
}

/** The library alone, for the right pane: its chat is the field that starts a design, so there is none here. */
export const DesignlyLibrary = ({ machine, onOpen }: IDesignlyLibraryProps) => {
  const { cards, error, now, onPin, onRetry } = useDesignLibrary(machine);
  const notice = noticeOf(useDesignEngine(machine));
  return (
    <DesignlyHome
      notice={notice}
      cards={cards}
      error={error}
      now={now}
      onPin={onPin}
      onOpen={onOpen}
      onRetry={onRetry}
      compose={null}
    />
  );
};

interface IDesignlyViewProps {
  machine: MachineId;
}

const DesignlyView = ({ machine }: IDesignlyViewProps) => {
  const { cards, error, now, load, onPin, onRetry } = useDesignLibrary(machine);
  const notice = noticeOf(useDesignEngine(machine));
  const [systems, setSystems] = useState<IDesignSystem[] | null>(null);
  const restored = useLinkRestored(machine);

  // The systems are only an offer: without them the field has no chip, nothing else changes.
  useEffect(() => {
    const abort = new AbortController();
    fetchSystems(machine, abort.signal).then(setSystems, () => {
      // Without a list the field simply has no chip; a returning link asks again.
      if (!abort.signal.aborted) setSystems((have) => have ?? []);
    });
    return () => abort.abort();
  }, [machine, restored]);

  // The agent that draws starts the way a new chat does: the person's pick, else the CLI settings file's model and
  // effort, else the last pick. A design starts through its own request, which has no backend profile.
  const { choice, agents, onPick } = useStartChoice(machine, true);

  const [brief, setBrief] = useState('');
  const voice = useVoice(machine, brief, setBrief);

  const onCreate = async (brief: string, kind: TDesignKind, designSystem: string | null) => {
    const title = titleOf(brief);
    if (choice) rememberChoice(choice);
    try {
      const created = await createDesign(machine, {
        title,
        brief,
        kind,
        provider: choice?.provider ?? 'claude',
        model: choice?.model ?? null,
        effort: choice?.effort ?? null,
        designSystem,
      });
      // The chat that draws the new design is the one its page opens on.
      bindDesignConv(designKey(machine, created.id), { id: created.conv, fresh: true });
    } catch (failure) {
      toast(t('next.designly.createFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
    toast(t('next.designly.started', { title }));
    await load();
  };

  return (
    <DesignlyHome
      notice={notice}
      cards={cards}
      error={error}
      now={now}
      onPin={onPin}
      onOpen={(id) => goTo({ kind: ROUTE.site, id })}
      onRetry={onRetry}
      compose={{
        systems,
        onCreate,
        brief,
        onBrief: setBrief,
        picker: choice && {
          value: choice,
          agents,
          locked: false,
          offersUltracode: false,
          onChange: onPick,
        },
        onDeck: () => {
          // The brief typed so far goes with the person to the deck's create screen.
          if (brief.trim()) setDraftText(DECK_NEW_DRAFT, brief);
          setBrief('');
          goTo({ kind: ROUTE.designlyNew });
        },
        voice: {
          recording: voice.recording,
          onMic: voice.onMic,
          onCancel: voice.onCancelRecording,
          onFinish: voice.onFinishRecording,
        },
      }}
    />
  );
};

export default DesignlyView;
