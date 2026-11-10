// The kit screen's editing mode (HRN-903), the way a Sinopia project has it: «Редактировать» opens the kit's draft on
// the canvas with the draft's chat beside it (the same design chat a project has, working in the draft's folder), what
// is picked on the canvas goes with the next message as its chip, and «Опубликовать версию» makes the next version. A
// kit its maker set for viewing only shows «Только просмотр» and is not edited, the server refusing it too; the maker
// turns it on and off in the kit's settings.
import { useCallback, useEffect, useState } from 'react';
import { useSiteStage } from '@/next/bindings/site-stage';
import { fetchLibrary, type IDesignLibrary } from '@/next/features/designly/api';
import { DiscardPop, PublishPop } from '@/next/features/designly/versions/kit-band';
import {
  fetchKitState,
  setKitReadOnly,
  startKitDraft,
  type IKitState,
} from '@/next/features/designly/versions/versions-api';
import type { MachineId } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import Switch from '@/next/ui/switch';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import '@/next/styles/kit-edit.css';

/** How often the kit's state is read while the screen is open, ms; the draft's edits are read sooner (CANVAS_MS). */
const STATE_MS = 15000;

export interface IKitEdit {
  state: IKitState | null;
  reload: () => void;
  /** Starts the draft (or takes the one there is); false when the server refused. */
  start: () => Promise<boolean>;
}

/** How long «Редактировать» waits for a draft whose copy outlasts the answer (a large kit on a busy machine), ms. */
const START_WAIT_MS = 180_000;
const START_LOOK_MS = 4000;

const pause = (ms: number) => new Promise((done) => setTimeout(done, ms));

/**
 * The kit's state for its editing mode: its draft, whether it is for viewing only, whether the person may edit it. It
 * is read without comparing the draft's files (a cheap read); while editing, with them too, for «Опубликовать
 * версию»; when that read fails the cheap one stands.
 */
export const useKitEdit = (machine: MachineId, kit: string, editing: boolean): IKitEdit => {
  const [state, setState] = useState<IKitState | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const ac = new AbortController();
    const read = async () => {
      try {
        const light = await fetchKitState(machine, kit, ac.signal, false);
        setState((was) => (was && editing && was.draft?.project === light.draft?.project ? was : light));
        if (editing && light.draft) setState(await fetchKitState(machine, kit, ac.signal));
      } catch {
        // A machine without kit versions has no editing mode; a slow comparison keeps the cheap state.
      }
    };
    void read();
    const timer = setInterval(() => {
      if (!document.hidden) void read();
    }, STATE_MS);
    return () => {
      ac.abort();
      clearInterval(timer);
    };
  }, [machine, kit, editing, tick]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  const start = useCallback(async () => {
    try {
      await startKitDraft(machine, kit);
      reload();
      return true;
    } catch (failure) {
      // A large kit is copied longer than an answer is waited for: the draft is there once the copy ends.
      for (let waited = 0; waited < START_WAIT_MS; waited += START_LOOK_MS) {
        await pause(START_LOOK_MS);
        const st = await fetchKitState(machine, kit, undefined, false).catch(() => null);
        if (st?.draft) {
          setState(st);
          return true;
        }
        if (st && !st.mayEdit) break;
      }
      toast(t('next.versions.kit.startFailed'), { tone: 'error', description: (failure as Error).message });
      return false;
    }
  }, [machine, kit, reload]);
  return { state, reload, start };
};

interface IKitSettingsPopProps {
  machine: MachineId;
  state: IKitState;
  onDone: () => void;
}

/** The kit's settings, for its maker: «Только просмотр для всех». */
const KitSettingsPop = ({ machine, state, onDone }: IKitSettingsPopProps) => {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const readOnly = state.settings?.readOnly ?? false;
  const flip = async (on: boolean) => {
    setBusy(true);
    try {
      await setKitReadOnly(machine, state.id, on);
      onDone();
    } catch (failure) {
      toast(t('next.versions.failed'), { tone: 'error', description: (failure as Error).message });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={t('next.versions.kit.settings')}
      className="hx-kitpop"
      side="bottom"
      align="end"
      trigger={
        <button type="button" className="hx-kitedit__icon" aria-label={t('next.versions.kit.settings')}>
          <Icon name="settings" size="sm" />
        </button>
      }
    >
      <div className="hx-kitpop__body">
        <div className="hx-kitedit__row">
          <span className="hx-kitedit__label">
            <span>{t('next.versions.kit.readOnlyAll')}</span>
            <span className="hx-kitpop__muted">{t('next.versions.kit.readOnlyHint')}</span>
          </span>
          <Switch
            label={t('next.versions.kit.readOnlyAll')}
            checked={readOnly}
            disabled={busy}
            onChange={(on) => void flip(on)}
          />
        </div>
      </div>
    </Popover>
  );
};

export interface IKitToolsProps {
  machine: MachineId;
  edit: IKitEdit;
  /** The screen shows the draft (the editing mode). */
  editing: boolean;
  onEdit: () => void;
  onDone: () => void;
}

/** The head's part of the editing mode: «Только просмотр», the settings, «Редактировать» or «Опубликовать версию». */
export const KitTools = ({ machine, edit, editing, onEdit, onDone }: IKitToolsProps) => {
  const { state } = edit;
  const [starting, setStarting] = useState(false);
  if (state === null) return null;
  const readOnly = state.settings?.readOnly ?? false;
  const owner = state.settings?.owner ?? false;
  const begin = async () => {
    setStarting(true);
    if (await edit.start()) onEdit();
    setStarting(false);
  };
  return (
    <span className="hx-kitedit">
      {readOnly && (
        <span className="hx-kitedit__badge" role="status">
          <Icon name="eye" size="sm" />
          {t('next.versions.kit.readOnly')}
        </span>
      )}
      {editing && state.mayEdit && state.draft && (
        <>
          <DiscardPop
            machine={machine}
            state={state}
            onDone={() => {
              edit.reload();
              onDone();
            }}
          />
          <PublishPop machine={machine} state={state} onDone={edit.reload} />
        </>
      )}
      {editing && (
        <Button size="sm" variant="ghost" onClick={onDone}>
          {t('next.versions.kit.done')}
        </Button>
      )}
      {!editing && state.mayEdit && (
        <Button size="sm" icon="pencil" busy={starting} onClick={() => void begin()}>
          {t('next.versions.kit.edit')}
        </Button>
      )}
      {owner && <KitSettingsPop machine={machine} state={state} onDone={edit.reload} />}
    </span>
  );
};

export interface IKitChip {
  label: string;
  text: string;
}

export interface IKitChatProps {
  machine: MachineId;
  /** The draft's project in the library. */
  project: string;
  /** The chat the person came from (`?chat=`), when it is the draft's. */
  fromChat: string | null;
  /** What is picked on the canvas: it goes with the next message. */
  chip: IKitChip | null;
  /** The agent of the draft's chat is working (its edits are read while it does) or has just ended. */
  onBusy: (busy: boolean) => void;
  onOpenDrawer: () => void;
}

const NO_CHATS: readonly string[] = [];

/** The draft's chat beside the canvas: a Sinopia project's design chat, over the draft's project. */
export const KitChat = ({ machine, project, fromChat, chip, onBusy, onOpenDrawer }: IKitChatProps) => {
  const [library, setLibrary] = useState<IDesignLibrary | null>(null);
  useEffect(() => {
    const ac = new AbortController();
    fetchLibrary(machine, ac.signal).then(setLibrary, () => setLibrary(null));
    return () => ac.abort();
  }, [machine, project]);
  const design = library?.designs.find((d) => d.id === project) ?? null;
  const tied = design ? [...(design.convs ?? NO_CHATS), ...(design.touchedBy ?? NO_CHATS)] : NO_CHATS;
  const stage = useSiteStage({
    machine,
    design: project,
    fromChat,
    dir: library && design ? `${library.root.replace(/\/+$/, '')}/${project}` : null,
    tied,
    title: design?.title ?? '',
    pageUrl: `kit:${project}`,
    port: library?.port ?? null,
    onOpenDrawer,
    keep: true,
  });
  const { context } = stage;
  useEffect(() => {
    context?.(chip && { label: chip.label, text: chip.text, selected: true });
  }, [context, chip]);
  const busy = stage.busy === true || stage.run === 'run';
  useEffect(() => onBusy(busy), [busy, onBusy]);
  return (
    <aside className="hx-kitchat" aria-label={t('next.versions.kit.chat')}>
      <div className="hx-deck-chat">{stage.deckChat}</div>
    </aside>
  );
};
