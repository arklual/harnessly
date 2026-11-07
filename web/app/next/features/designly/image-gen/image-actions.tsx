// What the buttons on a picture of a page open (features/designly/image-gen/image-act.ts says which was pressed):
// «Изменить» the shared full-screen editor over the picture, «Сгенерировать» the generator panel placed by the block (a
// card under or over it on a wide window, a sheet from the bottom on a phone). The deck, the canvas and the site all
// mount this one component; they differ only in how a change is written into their board (`write`).
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/features/designly/sdk/toast';
import { usePortalContainer } from '@/next/ui/portal';
import ImageEditView from '@/next/features/image-edit/image-editor';
import {
  designImageHost,
  versionOf,
  type IDesignPicture,
  type IImageChange,
} from '@/next/features/designly/image-gen/design-host';
import { readDesignSource, type TBoardAddress } from '@/next/features/designly/image-gen/api';
import ImageGenPanel from '@/next/features/designly/image-gen/gen-panel';
import {
  genKey,
  latestGenWas,
  useGenRun,
  type IGenApplied,
  type TGenApply,
} from '@/next/features/designly/image-gen/gen-store';
import { IMAGE_ACT, type IImageAct, type IScreenBox } from '@/next/features/designly/image-gen/image-act';

const GEN_EXIT_MS = 160;
const ROOM = 8;
const PANEL_W = 296;

/** A button pressed on a picture of the page, and where the block is on the screen at that moment. */
export interface IImageOpen {
  act: IImageAct;
  box: IScreenBox;
}

export interface IImageActionsProps {
  machine: MachineId;
  artifact: string;
  /** The board: a deck's slide (its page id), or a board of a canvas or a site (its file). It also names the runs. */
  address: TBoardAddress;
  open: IImageOpen | null;
  /** Writes the change into the board (the machine confirmed it: `was` is the element's new fingerprint); null: refused. */
  write: (change: IImageChange) => Promise<IGenApplied | null>;
  onClose: () => void;
}

/** The writer the host gave last, behind one function that never changes. */
class WriteRelay {
  private latest: IImageActionsProps['write'] | null = null;
  use(write: IImageActionsProps['write']) {
    this.latest = write;
  }
  readonly send: IImageActionsProps['write'] = async (change) =>
    this.latest === null ? null : this.latest(change);
}

const baseName = (path: string) => path.slice(path.lastIndexOf('/') + 1);

/** The card by the block: below it when there is room, else above, else over its lower part. */
const place = (el: HTMLElement, box: IScreenBox) => {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const w = Math.min(PANEL_W, vw - ROOM * 2);
  const h = el.offsetHeight;
  const left = Math.min(Math.max(box.left + box.width / 2 - w / 2, ROOM), vw - w - ROOM);
  const below = box.top + box.height + ROOM;
  const above = box.top - h - ROOM;
  let top = below;
  if (below + h > vh - ROOM) top = above >= ROOM ? above : box.top + box.height - h - ROOM * 2;
  top = Math.min(Math.max(top, ROOM), Math.max(ROOM, vh - h - ROOM));
  el.style.setProperty('--ix', `${Math.round(left)}px`);
  el.style.setProperty('--iy', `${Math.round(top)}px`);
  el.style.setProperty('--iw', `${Math.round(w)}px`);
};

/** A generator panel on screen: the button that opened it and how a picture it draws is written. */
interface IGenOpen {
  open: IImageOpen;
  apply: TGenApply;
}

interface IGenFloatProps {
  machine: MachineId;
  artifact: string;
  board: string;
  state: IGenOpen;
  closing: boolean;
  onClose: () => void;
}

const GenFloat = ({ machine, artifact, board, state, closing, onClose }: IGenFloatProps) => {
  const { act, box } = state.open;
  const ref = useRef<HTMLDivElement>(null);
  const key = genKey(artifact, board, act.tpl);
  const run = useGenRun(key);
  const busy = useRef(false);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const put = () => place(el, box);
    put();
    const watch = new ResizeObserver(put);
    watch.observe(el);
    window.addEventListener('resize', put);
    return () => {
      watch.disconnect();
      window.removeEventListener('resize', put);
    };
  }, [box]);

  // The panel closes once its picture is written: a run that was there and is gone.
  useEffect(() => {
    if (run !== null) busy.current = true;
    else if (busy.current) onClose();
    // onClose is the host's; a new one each render must not run it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run]);

  useEffect(() => {
    const leave = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', leave);
    // A finger would raise the keyboard over the block: only a pointer that hovers puts the cursor in the field.
    if (window.matchMedia('(pointer: fine)').matches)
      ref.current?.querySelector('textarea')?.focus({ preventScroll: true });
    return () => window.removeEventListener('keydown', leave);
    // onClose is the host's; a new one each render must not listen again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="hx-imgact" data-closing={closing ? '1' : undefined}>
      <button
        type="button"
        className="hx-imgact__scrim"
        tabIndex={-1}
        aria-label={t('next.imagegen.close')}
        onPointerDown={onClose}
      />
      <div ref={ref} className="hx-imgact__panel" role="dialog" aria-label={t('next.imagegen.generate')}>
        <ImageGenPanel
          key={`${act.tpl}:${act.gen?.job ?? ''}`}
          machine={machine}
          target={{
            key,
            artifact,
            tpl: act.tpl,
            ratio: act.ratio,
            gen: act.gen,
            brief: act.brief,
          }}
          apply={state.apply}
          embedded
        />
      </div>
    </div>
  );
};

export const ImageActions = ({ machine, artifact, address, open, write, onClose }: IImageActionsProps) => {
  const board = typeof address === 'string' ? address : address.file;
  const container = usePortalContainer();
  // The host's writer changes with every render; what the editor and the panel hold is this one relay, always the latest.
  const [relay] = useState(() => new WriteRelay());
  useLayoutEffect(() => {
    relay.use(write);
  });
  const act = open?.act ?? null;

  // A heavy picture is named by its content on the page: the board's source says which file it is.
  const [found, setFound] = useState<{ act: IImageAct; src: string } | null>(null);
  useEffect(() => {
    if (act === null || act.act !== IMAGE_ACT.edit || act.src !== '') return;
    const abort = new AbortController();
    readDesignSource(machine, artifact, address, act.tpl, act.was, abort.signal).then(
      (src) => setFound({ act, src }),
      (failure: Error) => {
        if (abort.signal.aborted) return;
        toast(t('next.imageEdit.failedPlain'), { tone: 'error', description: failure.message });
        onClose();
      },
    );
    return () => abort.abort();
    // The press says which picture; the host's address and close do not start another read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [act]);
  const editSrc = act === null ? '' : act.src !== '' ? act.src : found?.act === act ? found.src : '';

  const editing = useMemo(() => {
    if (act === null || act.act !== IMAGE_ACT.edit || editSrc === '') return null;
    const picture: IDesignPicture = {
      machine,
      artifact,
      key: genKey(artifact, board, act.tpl),
      tpl: act.tpl,
      was: act.was,
      gen: act.gen,
      from: act.from,
      label: act.label,
      write: relay.send,
    };
    return { original: versionOf(picture, editSrc), host: designImageHost(picture) };
  }, [act, editSrc, machine, artifact, board, relay]);
  const item = useMemo(
    () => (editing === null ? null : { src: editing.original.src, name: baseName(editing.original.id) }),
    [editing],
  );

  /** A picture drawn from a brief goes into the element as the machine's own file, against its latest fingerprint. */
  const gen = useMemo<IGenOpen | null>(() => {
    if (open === null || open.act.act !== IMAGE_ACT.gen) return null;
    const { tpl, was } = open.act;
    const key = genKey(artifact, board, tpl);
    const apply: TGenApply = (pic, drawn) =>
      relay.send({
        op: 'image',
        tpl,
        was: latestGenWas(key) ?? was,
        src: pic.src,
        job: drawn.job,
        prompt: drawn.prompt,
      });
    return { open, apply };
  }, [open, artifact, board, relay]);
  const shownGen = usePresence(gen, GEN_EXIT_MS);
  const shown = shownGen.value;
  const floating =
    shown === null ? null : (
      <GenFloat
        key={`${shown.open.act.tpl}:${shown.open.act.was}`}
        machine={machine}
        artifact={artifact}
        board={board}
        state={shown}
        closing={shownGen.state === PRESENCE.closing}
        onClose={onClose}
      />
    );
  return (
    <>
      <ImageEditView item={item} editing={editing} onClose={onClose} />
      {floating !== null && createPortal(floating, container?.current ?? document.body)}
    </>
  );
};

export default ImageActions;
