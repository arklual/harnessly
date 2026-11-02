// A PowerPoint file in the pane: the slide as the file lays it out, with a strip of the rest under it. Words are typed right on
// the slide; a change goes into the slide's XML at once and to the disk a moment later. Its own chunk.
import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import Tooltip from '@/next/ui/tooltip';
import { parsePagePlace } from '@/next/features/pane/file-place';
import FileHistory from '@/next/features/pane/file-history';
import { slideSelection, type IFileSelection } from '@/next/features/pane/file-selection';
import { SAVE, SaveConflict, SaveWord } from '@/next/features/pane/file-save-state';
import { OfficeImages } from '@/next/features/pane/office-parts';
import {
  caretIn,
  paragraphAt,
  putCaret,
  useTextEditing,
  type ITextEditable,
  type ITextPlace,
} from '@/next/features/pane/text-editing';
import { useOfficeFile, type IOfficeKind } from '@/next/features/pane/use-office-file';
import {
  drawnSlide,
  mergePptx,
  pptxParagraphs,
  pptxSnapshot,
  readPptx,
  rereadSlide,
  slideTitle,
  writePptx,
  type IPptxBook,
} from '@/next/features/pane/pptx/pptx-core';
import { drawSlide } from '@/next/features/pane/pptx/pptx-draw';

export interface IPptxViewProps {
  machine: MachineId;
  path: string;
  src: string;
  name: string;
  onSelection?: (selection: IFileSelection | null) => void;
  /** A slide, from 1, the agent asked to show; onPlaced when it is there. */
  place?: string | null;
  onPlaced?: () => void;
}

const PPTX: IOfficeKind<IPptxBook, readonly (readonly string[])[]> = {
  read: readPptx,
  write: writePptx,
  snapshot: pptxSnapshot,
  merge: mergePptx,
};

const REPORT_PAUSE_MS = 200;
const THUMB_WIDTH = 132;

const otherWords = () => ({
  chart: t('next.pptx.chart'),
  diagram: t('next.pptx.diagram'),
  object: t('next.pptx.object'),
});

interface IThumbProps {
  book: IPptxBook;
  images: OfficeImages;
  at: number;
  current: boolean;
  /** Grows when this slide should be drawn again. */
  version: number;
  onPick: () => void;
}

/** A slide in the strip, drawn when it comes into view. */
const Thumb = ({ book, images, at, current, version, onPick }: IThumbProps) => {
  const button = useRef<HTMLButtonElement | null>(null);
  const stage = useRef<HTMLDivElement | null>(null);
  const [seen, setSeen] = useState(false);

  useEffect(() => {
    const element = button.current;
    if (!element) return;
    const watch = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) setSeen(true);
    });
    watch.observe(element);
    return () => watch.disconnect();
  }, []);

  useEffect(() => {
    if (!seen || !stage.current) return;
    drawSlide(stage.current, drawnSlide(book, at), book.width, book.height, {
      images,
      editable: false,
      words: otherWords(),
    });
    stage.current.style.setProperty('transform', `scale(${THUMB_WIDTH / book.width})`);
    button.current?.style.setProperty('--hx-slide-thumb-h', `${(THUMB_WIDTH * book.height) / book.width}px`);
  }, [seen, book, images, at, version]);

  useEffect(() => {
    if (current) button.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [current]);

  return (
    <button
      ref={button}
      type="button"
      className="hx-slide__thumb"
      aria-label={t('next.pptx.slideN', { slide: at + 1 })}
      aria-current={current ? 'true' : undefined}
      onClick={onPick}
    >
      <span className="hx-slide__thumb-frame">
        <div ref={stage} className="hx-slide__stage" inert />
      </span>
      <span className="hx-slide__thumb-n">{at + 1}</span>
    </button>
  );
};

const PptxView = ({ machine, path, src, name, onSelection, place, onPlaced }: IPptxViewProps) => {
  const file = useOfficeFile({ machine, path, src, kind: PPTX });
  const { book, touch } = file;
  const shell = useRef<HTMLDivElement | null>(null);
  const holder = useRef<HTMLDivElement | null>(null);
  const stage = useRef<HTMLDivElement | null>(null);
  const images = useMemo(() => (book ? new OfficeImages(book.zip) : null), [book]);
  const [slide, setSlide] = useState(0);
  const [thumbVersion, setThumbVersion] = useState(0);
  const slideRef = useRef(0);
  const picked = useRef<HTMLElement | null>(null);

  const count = book?.slides.length ?? 0;
  const at = Math.min(slide, Math.max(0, count - 1));

  const draw = useCallback(
    (caret: ITextPlace | null) => {
      const root = stage.current;
      if (!root || !book || !images || !book.slides[slideRef.current]) return;
      drawSlide(root, drawnSlide(book, slideRef.current), book.width, book.height, {
        images,
        editable: true,
        words: otherWords(),
      });
      picked.current = null;
      if (caret) putCaret(root, caret);
    },
    [book, images],
  );

  const editable = useMemo<ITextEditable<{ slide: number; root: Element }> | null>(() => {
    if (!book) return null;
    return {
      ...pptxParagraphs(book, at),
      draw,
      capture: () => ({
        slide: at,
        root: book.slides[at].doc.documentElement.cloneNode(true) as Element,
      }),
      restore: (state) => {
        const target = book.slides[state.slide];
        target.doc.replaceChild(state.root, target.doc.documentElement);
        rereadSlide(book, state.slide);
        slideRef.current = state.slide;
        setSlide(state.slide);
        draw(null);
      },
      changed: () => {
        touch();
        setThumbVersion((n) => n + 1);
      },
    };
  }, [book, at, draw, touch]);
  const forgetUndo = useTextEditing(stage, editable);

  // The file was read again: the steps of undo were over what it replaced.
  useEffect(() => {
    if (book) forgetUndo();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- forgetting is the same whichever render made it
  }, [book]);
  useEffect(() => () => images?.stop(), [images]);

  useEffect(() => {
    slideRef.current = at;
    draw(stage.current ? caretIn(stage.current) : null);
  }, [at, draw]);

  // The slide fills the width it is given, and the height: the file's pixels scaled as a whole.
  useEffect(() => {
    const box = holder.current;
    const root = stage.current;
    if (!box || !root || !book) return;
    const fit = () => {
      const k = Math.min(box.clientWidth / book.width, box.clientHeight / book.height);
      if (!Number.isFinite(k) || k <= 0) return;
      root.style.setProperty('transform', `scale(${k})`);
      root.style.setProperty('left', `${Math.max(0, (box.clientWidth - book.width * k) / 2)}px`);
      root.style.setProperty('top', `${Math.max(0, (box.clientHeight - book.height * k) / 2)}px`);
    };
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(box);
    return () => watch.disconnect();
  }, [book]);

  const go = useCallback(
    (to: number) => {
      const next = Math.max(0, Math.min(count - 1, to));
      slideRef.current = next;
      setSlide(next);
      onSelection?.(null);
    },
    [count, onSelection],
  );

  const pickShape = (target: EventTarget | null) => {
    const element = target instanceof Element ? target.closest<HTMLElement>('[data-shape]') : null;
    picked.current?.removeAttribute('data-picked');
    picked.current = element;
    if (!element || !book) {
      onSelection?.(null);
      return;
    }
    element.dataset.picked = 'true';
    onSelection?.(
      slideSelection(
        t('next.pptx.slideN', { slide: at + 1 }),
        at + 1,
        slideTitle(book.slides[at]),
        element.dataset.shape ?? '',
        element.textContent ?? '',
        true,
      ),
    );
  };

  // A passage selected on the slide goes to the chat in place of the shape it is in. Going to the field moves the browser's
  // selection away, and that takes nothing back.
  const reportText = useEffectEvent(() => {
    const selection = document.getSelection();
    const root = stage.current;
    const anchor = selection?.anchorNode ?? null;
    if (!selection || !root || !anchor || !book || !root.contains(anchor) || selection.isCollapsed) return;
    const text = selection.toString().trim();
    if (!text || paragraphAt(anchor) === null) return;
    const home = anchor instanceof Element ? anchor : anchor.parentElement;
    onSelection?.(
      slideSelection(
        t('next.pptx.slideN', { slide: at + 1 }),
        at + 1,
        slideTitle(book.slides[at]),
        home?.closest<HTMLElement>('[data-shape]')?.dataset.shape ?? '',
        text,
        false,
      ),
    );
  });
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const onChange = () => {
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(reportText, REPORT_PAUSE_MS);
    };
    document.addEventListener('selectionchange', onChange);
    return () => {
      document.removeEventListener('selectionchange', onChange);
      if (timer !== null) clearTimeout(timer);
    };
  }, []);

  // The agent asked to show a slide.
  const goToPlace = useEffectEvent((asked: string) => {
    const number = parsePagePlace(asked);
    if (number !== null && number <= count) go(number - 1);
    onPlaced?.();
  });
  const ready = book !== null;
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the agent's ask comes from outside, and takes this view to a slide
    if (place && ready) goToPlace(place);
  }, [place, ready]);

  // Arrows go through the slides unless the caret is in words.
  const onKey = (event: React.KeyboardEvent) => {
    if (event.target instanceof HTMLElement && event.target.isContentEditable) return;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown' || event.key === 'PageDown') {
      event.preventDefault();
      go(at + 1);
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp' || event.key === 'PageUp') {
      event.preventDefault();
      go(at - 1);
    }
  };

  const onReload = () => {
    forgetUndo();
    file.reload();
  };

  if (file.failed !== null)
    return <LoadFailed title={t('next.pptx.failed')} reason={file.failed} onRetry={file.reload} />;

  return (
    <div ref={shell} className="hx-slides" tabIndex={-1} onKeyDown={onKey}>
      <div className="hx-slides__bar">
        <Tooltip label={t('next.pptx.prev')}>
          <button
            type="button"
            className="hx-slides__btn"
            aria-label={t('next.pptx.prev')}
            disabled={at === 0}
            onClick={() => go(at - 1)}
          >
            <Icon name="chevron-left" size="sm" />
          </button>
        </Tooltip>
        <span className="hx-slides__count">{count > 0 ? `${at + 1} / ${count}` : ''}</span>
        <Tooltip label={t('next.pptx.next')}>
          <button
            type="button"
            className="hx-slides__btn"
            aria-label={t('next.pptx.next')}
            disabled={at >= count - 1}
            onClick={() => go(at + 1)}
          >
            <Icon name="chevron-right" size="sm" />
          </button>
        </Tooltip>
        <span className="hx-slides__gap" />
        <SaveWord save={file.save} />
        <FileHistory machine={machine} path={path} current={file.revision} onRestored={onReload} />
        <Tooltip label={t('next.pptx.full')}>
          <button
            type="button"
            className="hx-slides__btn"
            aria-label={t('next.pptx.full')}
            onClick={() => void holder.current?.requestFullscreen?.()}
          >
            <Icon name="maximize-2" size="sm" />
          </button>
        </Tooltip>
      </div>
      {file.save === SAVE.conflict && <SaveConflict onReload={onReload} onOverwrite={file.overwrite} />}
      <div
        ref={holder}
        className="hx-slides__holder"
        aria-label={name}
        onPointerDown={(event) => pickShape(event.target)}
      >
        {!ready && (
          <span className="hx-sk hx-slides__wait" role="status" aria-label={t('next.pptx.opening')} />
        )}
        <div ref={stage} className="hx-slide__stage" data-ready={ready ? 'true' : undefined} />
      </div>
      {book && images && count > 0 && (
        <div className="hx-slides__strip" role="list" aria-label={t('next.pptx.slides')}>
          {book.slides.map((item, index) => (
            <Thumb
              key={item.path}
              book={book}
              images={images}
              at={index}
              current={index === at}
              version={index === at ? thumbVersion : 0}
              onPick={() => go(index)}
            />
          ))}
        </div>
      )}
      {book && count === 0 && <div className="hx-tv__note">{t('next.pptx.empty')}</div>}
    </div>
  );
};

export default PptxView;
