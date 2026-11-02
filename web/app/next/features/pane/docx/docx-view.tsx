// A Word file in the pane: a sheet of paper with the file's own text, typed into in place. A change goes into the file's XML at
// once and to the disk a moment later; what is not text stays in the file as it came. Its own chunk.
import { useCallback, useEffect, useEffectEvent, useMemo, useRef } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import LoadFailed from '@/next/ui/load-failed';
import { parsePagePlace } from '@/next/features/pane/file-place';
import FileHistory from '@/next/features/pane/file-history';
import { passageSelection, type IFileSelection } from '@/next/features/pane/file-selection';
import { SAVE, SaveConflict, SaveWord } from '@/next/features/pane/file-save-state';
import { useOfficeFile, type IOfficeKind } from '@/next/features/pane/use-office-file';
import { OfficeImages } from '@/next/features/pane/office-parts';
import {
  caretIn,
  paragraphAt,
  putCaret,
  useTextEditing,
  type ITextEditable,
  type ITextPlace,
} from '@/next/features/pane/text-editing';
import {
  docxParagraphs,
  docxSnapshot,
  mergeDocx,
  readDocx,
  writeDocx,
  type IDocxBook,
} from '@/next/features/pane/docx/docx-core';
import { drawDocx, headingBefore, looksToCss } from '@/next/features/pane/docx/docx-draw';

export interface IDocxViewProps {
  machine: MachineId;
  path: string;
  src: string;
  name: string;
  onSelection?: (selection: IFileSelection | null) => void;
  /** A paragraph, from 1, the agent asked to show; onPlaced when it is there. */
  place?: string | null;
  onPlaced?: () => void;
}

const DOCX: IOfficeKind<IDocxBook, readonly string[]> = {
  read: readDocx,
  write: writeDocx,
  snapshot: docxSnapshot,
  merge: mergeDocx,
};

const REPORT_PAUSE_MS = 200;

const DocxView = ({ machine, path, src, name, onSelection, place, onPlaced }: IDocxViewProps) => {
  const file = useOfficeFile({ machine, path, src, kind: DOCX });
  const { book, touch } = file;
  const scroller = useRef<HTMLDivElement | null>(null);
  const page = useRef<HTMLDivElement | null>(null);
  const sheet = useRef<HTMLStyleElement | null>(null);
  const images = useRef<OfficeImages | null>(null);

  const draw = useCallback((next: IDocxBook, caret: ITextPlace | null) => {
    const root = page.current;
    if (!root || !images.current) return;
    drawDocx(root, next, images.current);
    if (sheet.current) sheet.current.textContent = looksToCss(next.looks);
    root.style.setProperty('--hx-docx-page', `${Math.round(next.pageWidth)}px`);
    if (caret) putCaret(root, caret);
  }, []);

  const editable = useMemo<ITextEditable<Element> | null>(() => {
    if (!book) return null;
    return {
      ...docxParagraphs(book),
      draw: (caret) => {
        book.walk();
        draw(book, caret);
      },
      capture: () => book.doc.documentElement.cloneNode(true) as Element,
      restore: (state) => {
        const caret = page.current ? caretIn(page.current) : null;
        book.doc.replaceChild(state, book.doc.documentElement);
        book.walk();
        draw(book, caret);
      },
      changed: touch,
    };
  }, [book, draw, touch]);
  const forgetUndo = useTextEditing(page, editable);

  // The book came: first opened, read again because the file changed, or with the edits put on top of a change. The scroll and
  // the caret stay where they were.
  useEffect(() => {
    if (!book) return;
    images.current?.stop();
    images.current = new OfficeImages(book.zip);
    forgetUndo();
    const top = scroller.current?.scrollTop ?? 0;
    draw(book, page.current ? caretIn(page.current) : null);
    if (scroller.current) scroller.current.scrollTop = top;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- forgetting is the same whichever render made it
  }, [book, draw]);
  useEffect(() => () => images.current?.stop(), []);

  // A passage selected goes to the chat. Only what happens in the page counts: going to the field to write about it moves the
  // browser's selection away, and that takes nothing back.
  const reportText = useEffectEvent(() => {
    const selection = document.getSelection();
    const root = page.current;
    const anchor = selection?.anchorNode ?? null;
    if (!selection || !root || !anchor || !book || !root.contains(anchor)) return;
    if (selection.isCollapsed) {
      onSelection?.(null);
      return;
    }
    const text = selection.toString().trim();
    const index = paragraphAt(anchor);
    if (!text || index === null) return;
    onSelection?.(passageSelection(index + 1, headingBefore(book, index), text));
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

  // The agent asked to show a paragraph.
  const goToPlace = useEffectEvent((asked: string) => {
    const number = parsePagePlace(asked);
    const element =
      number === null ? null : page.current?.querySelector<HTMLElement>(`[data-p="${number - 1}"]`);
    if (element) {
      element.scrollIntoView({ block: 'center' });
      element.dataset.flash = 'true';
      element.addEventListener('animationend', () => delete element.dataset.flash, { once: true });
    }
    onPlaced?.();
  });
  const ready = book !== null;
  useEffect(() => {
    if (place && ready) goToPlace(place);
  }, [place, ready]);

  if (file.failed !== null)
    return <LoadFailed title={t('next.docx.failed')} reason={file.failed} onRetry={file.reload} />;

  return (
    <div className="hx-docx">
      <div className="hx-docx__bar">
        <SaveWord save={file.save} />
        <FileHistory machine={machine} path={path} current={file.revision} onRestored={file.reload} />
      </div>
      {file.save === SAVE.conflict && <SaveConflict onReload={file.reload} onOverwrite={file.overwrite} />}
      <div className="hx-docx__scroll" ref={scroller}>
        <style ref={sheet} />
        {!ready && <span className="hx-sk hx-docx__wait" role="status" aria-label={t('next.docx.opening')} />}
        <div
          ref={page}
          className="hx-docx__page"
          data-ready={ready ? 'true' : undefined}
          aria-label={name}
          contentEditable="plaintext-only"
          suppressContentEditableWarning
          spellCheck={false}
        />
      </div>
    </div>
  );
};

export default DocxView;
