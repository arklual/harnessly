// The picture editor: the full-size viewer (ui/image-view.tsx) with its tools. Under the picture five tools and a field
// «Что поменять?»; the person draws or frames a place, says what to change, and the change runs on the host (a chat's
// machine, a Sinopia design) while the picture waits blurred. The new version appears among the thumbnails only when
// the host has it. A run belongs to the picture, not to the window (run-store.ts): closing does not stop it.
import { useEffect, useState } from 'react';
import { EditDock, VersionStrip, type IFailure } from '@/next/features/image-edit/edit-dock';
import {
  EDIT_MODE,
  type IImageEdit,
  type IImageEditHost,
  type IImageVersion,
  type TEditFormat,
} from '@/next/features/image-edit/host';
import MarkLayer from '@/next/features/image-edit/mark-layer';
import {
  DEFAULT_BRUSH,
  MARK_COLORS,
  MARK_KIND,
  NO_MARKS,
  exportOverlay,
  type IMarks,
  type TBrushId,
  type TMarkKind,
} from '@/next/features/image-edit/marks';
import {
  RUN_STATE,
  type IRun,
  dismissRun,
  retryRun,
  runOf,
  startRun,
  useRun,
} from '@/next/features/image-edit/run-store';
import {
  DEFAULT_FORMAT,
  TOOL,
  canSend,
  isDrawing,
  modeOf,
  ratioOfFormat,
  type TTool,
} from '@/next/features/image-edit/tool-rules';
import { t } from '@/next/i18n';
import ImageView, { IMAGE_VIEW_EXIT_MS, type IImageViewItem } from '@/next/ui/image-view';
import { usePresence } from '@/next/ui/presence';

/** A new version comes in from blur over this long (image-edit.css). */
const FRESH_MS = 800;

export interface IImageEditing {
  /** The picture as it was when the viewer opened (the first version). */
  original: IImageVersion;
  host: IImageEditHost;
}

interface IImageEditViewProps {
  item: IImageViewItem | null;
  onClose: () => void;
  position?: { at: number; of: number };
  onStep?: (delta: 1 | -1) => void;
  /** Absent: a plain viewer (a gallery, a side chat, a picture the person sent). */
  editing: IImageEditing | null;
}

interface ISession {
  /** The original this state belongs to; another picture (a step) starts afresh. */
  forId: string;
  tool: TTool | null;
  marks: IMarks;
  color: string;
  brush: TBrushId;
  format: TEditFormat;
  text: string;
  /** The version shown. */
  at: string;
  versions: readonly IImageVersion[];
  natural: { w: number; h: number } | null;
  fresh: boolean;
  /** The finished run already taken in. */
  taken: IRun | null;
}

const startOf = (original: IImageVersion): ISession => ({
  forId: original.id,
  tool: null,
  marks: NO_MARKS,
  color: MARK_COLORS[0],
  brush: DEFAULT_BRUSH,
  format: DEFAULT_FORMAT,
  text: '',
  at: original.id,
  versions: [original],
  natural: null,
  fresh: false,
  taken: null,
});

const KIND_OF: Partial<Record<TTool, TMarkKind>> = {
  [TOOL.mark]: MARK_KIND.mark,
  [TOOL.note]: MARK_KIND.note,
  [TOOL.erase]: MARK_KIND.erase,
};

/** `have` and then what `more` adds that is not in it yet. */
const union = (have: readonly IImageVersion[], more: readonly IImageVersion[]): IImageVersion[] => {
  const known = new Set(have.map((version) => version.id));
  return [...have, ...more.filter((version) => !known.has(version.id))];
};

const fileNameOf = (version: IImageVersion, fallback: string): string => {
  const segment = version.id.slice(version.id.lastIndexOf('/') + 1);
  return /\.[A-Za-z0-9]{2,5}$/.test(segment) ? segment : fallback;
};

const Editor = ({
  editing,
  item,
  onClose,
  position,
  onStep,
}: IImageEditViewProps & { editing: IImageEditing }) => {
  const { original, host } = editing;
  const [state, setState] = useState<ISession>(() => startOf(original));
  // Another picture (the viewer stepped): its own state, from the first render.
  let s = state;
  if (s.forId !== original.id) {
    s = startOf(original);
    setState(s);
  }
  const patch = (change: Partial<ISession>) => setState((now) => ({ ...now, ...change }));
  const [focusField, setFocusField] = useState(0);
  const run = useRun(original.id);
  const running = run?.state === RUN_STATE.running;
  const selected = s.versions.find((version) => version.id === s.at) ?? s.versions[0];

  // The versions the host knows (a chat's come from the machine, so they outlive a reload); a run that was going on
  // when the viewer was closed puts the picture it changes back on screen.
  useEffect(() => {
    const abort = new AbortController();
    host
      .versions(original, abort.signal)
      .then((list) =>
        setState((now) => {
          if (now.forId !== original.id) return now;
          const going = runOf(original.id);
          const from = going?.state === RUN_STATE.running ? going.edit.from.id : null;
          const versions = union(list, now.versions);
          const at = from !== null && versions.some((version) => version.id === from) ? from : now.at;
          return { ...now, versions, at };
        }),
      )
      .catch(() => {
        // The host could not list them (offline, aborted): only the versions already known are shown, and no error
        // is made of it; a change that finishes adds its own.
      });
    return () => abort.abort();
  }, [host, original]);

  // A run that finished (maybe while the viewer was closed): its version is taken in, chosen, and comes in from blur.
  const done = run?.state === RUN_STATE.done ? run : null;
  const taking = done !== null && s.taken !== done ? done : null;
  if (taking !== null) {
    const last = taking.versions[taking.versions.length - 1];
    s = {
      ...s,
      taken: taking,
      versions: union(s.versions, taking.versions),
      at: last === undefined ? s.at : last.id,
      fresh: last !== undefined,
      marks: NO_MARKS,
    };
    setState(s);
  }
  useEffect(() => {
    if (done === null) return;
    dismissRun(original.id);
  }, [done, original.id]);
  useEffect(() => {
    if (!s.fresh) return;
    const timer = setTimeout(() => setState((now) => ({ ...now, fresh: false })), FRESH_MS);
    return () => clearTimeout(timer);
  }, [s.fresh]);

  const start = (edit: IImageEdit) => {
    startRun(original.id, host, edit);
    patch({ tool: null, marks: NO_MARKS, text: '' });
  };
  const pickTool = (tool: TTool) => {
    if (running) return;
    if (run?.state === RUN_STATE.failed) dismissRun(original.id);
    if (tool === TOOL.cut) {
      start({ from: selected, mode: EDIT_MODE.cut, prompt: '' });
      return;
    }
    patch({ tool: s.tool === tool ? null : tool, marks: NO_MARKS });
  };
  const send = () => {
    const kind = s.tool === null ? undefined : KIND_OF[s.tool];
    if (kind !== undefined && s.natural === null) return;
    start({
      from: selected,
      mode: modeOf(s.tool),
      prompt: s.text.trim(),
      format: s.tool === TOOL.size ? s.format : undefined,
      overlay:
        kind !== undefined && s.natural !== null
          ? exportOverlay(kind, s.marks, s.natural.w, s.natural.h)
          : undefined,
    });
  };
  const pickVersion = (version: IImageVersion) => {
    if (running || version.id === s.at) return;
    patch({ at: version.id, marks: NO_MARKS });
    host.chosen?.(version);
  };

  const drawing = isDrawing(s.tool);
  const kind = s.tool === null ? undefined : KIND_OF[s.tool];
  const sizing =
    s.tool === TOOL.size ? s.format : run?.edit.mode === EDIT_MODE.size ? run.edit.format : undefined;
  const ratio =
    sizing !== undefined ? ratioOfFormat(sizing) : s.natural === null ? undefined : s.natural.w / s.natural.h;
  const failure: IFailure | null =
    run?.state === RUN_STATE.failed ? { why: run.error, onRetry: () => retryRun(original.id) } : null;

  const shown: IImageViewItem | null = item === null ? null : { ...item, src: selected.src };
  const overlay = kind !== undefined && !running && (
    <MarkLayer
      kind={kind}
      marks={s.marks}
      color={s.color}
      brush={s.brush}
      label={t('next.imageEdit.canvas')}
      onChange={(marks) => patch({ marks })}
      onFramed={() => setFocusField((n) => n + 1)}
    />
  );
  const undo = () => patch({ marks: { ...s.marks, strokes: s.marks.strokes.slice(0, -1) } });

  return (
    <ImageView
      item={shown}
      onClose={onClose}
      position={position}
      onStep={onStep}
      edit={{
        ratio,
        clear: selected.clear,
        busy: running,
        fresh: s.fresh,
        locked: drawing,
        layer: overlay || undefined,
        onLoad: (natural) => patch({ natural }),
        onBack: () => {
          if (s.tool === null) return false;
          patch({ tool: null, marks: NO_MARKS });
          return true;
        },
        download:
          item === null
            ? undefined
            : {
                href: selected.src,
                name: fileNameOf(selected, item.name),
                label: t('next.imageEdit.download'),
              },
        versions: (
          <VersionStrip versions={s.versions} at={selected.id} disabled={running} onPick={pickVersion} />
        ),
        dock: (
          <EditDock
            tool={s.tool}
            onTool={pickTool}
            running={running}
            color={s.color}
            onColor={(color) => patch({ color })}
            brush={s.brush}
            onBrush={(brush) => patch({ brush })}
            format={s.format}
            onFormat={(format) => patch({ format })}
            canUndo={s.marks.strokes.length > 0}
            onUndo={undo}
            text={s.text}
            onText={(text) => patch({ text })}
            focusField={focusField}
            canSend={canSend({ tool: s.tool, marks: s.marks, text: s.text, running })}
            onSend={send}
            failure={failure}
          />
        ),
      }}
    />
  );
};

/**
 * The viewer of a picture that can be changed. `editing` null gives the plain viewer. The editor stays mounted through
 * the viewer's exit animation, so the dock leaves with the picture.
 */
const ImageEditView = (props: IImageEditViewProps) => {
  const { item, editing } = props;
  const open = item !== null && editing !== null;
  const { value: shownId } = usePresence(open ? editing.original.id : null, IMAGE_VIEW_EXIT_MS);
  // The last picture it was open on, for the exit.
  const [last, setLast] = useState<IImageEditing | null>(null);
  if (open && last !== editing) setLast(editing);
  if (shownId === null || last === null) {
    return <ImageViewPlain {...props} />;
  }
  return <Editor {...props} editing={open ? editing : last} />;
};

const ImageViewPlain = ({ item, onClose, position, onStep }: IImageEditViewProps) => (
  <ImageView item={item} onClose={onClose} position={position} onStep={onStep} />
);

export default ImageEditView;
