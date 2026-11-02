// Typing into an office file drawn on the page. The browser edits a paragraph's words by itself and they go into the file;
// what would rearrange paragraphs (Enter, a stretch across them, a paste) goes into the file first and the page is drawn again
// from it. A paragraph on the page carries its number in `data-p`.
import { useEffect, useEffectEvent, useRef, type RefObject } from 'react';

export interface ITextPlace {
  index: number;
  offset: number;
}

/** What a file gives to take edits: paragraphs by their number. */
export interface IParagraphs {
  text: (index: number) => string;
  setText: (index: number, text: string) => boolean;
  /** What stands after `offset` becomes the next paragraph. */
  split: (index: number, offset: number) => void;
  /** Joins it to the one before it; answers where the join is, null when one cannot join: another block stands between them. */
  join: (index: number) => number | null;
  /** May a stretch from this paragraph to the next be taken out? */
  joinable: (index: number) => boolean;
}

/**
 * Replaces what stands between two places with `text`, whose line breaks become new paragraphs. Answers where the caret goes, or
 * null, with nothing changed, when the stretch cannot be joined.
 */
export const replaceRange = (
  paragraphs: IParagraphs,
  from: ITextPlace,
  to: ITextPlace,
  text: string,
): ITextPlace | null => {
  for (let at = from.index; at < to.index; at += 1) if (!paragraphs.joinable(at)) return null;
  if (from.index === to.index && !paragraphs.joinable(from.index) && text.includes('\n')) return null;
  const head = paragraphs.text(from.index).slice(0, from.offset);
  const tail = paragraphs.text(to.index).slice(to.offset);
  for (let at = to.index; at > from.index; at -= 1) paragraphs.join(at);
  const lines = text.split(/\r\n|\r|\n/);
  let index = from.index;
  let cut = head.length;
  // The words are in place first, so the looks around each cut stay.
  paragraphs.setText(index, head + lines.join('') + tail);
  for (const line of lines.slice(0, -1)) {
    cut += line.length;
    paragraphs.split(index, cut);
    cut = 0;
    index += 1;
  }
  return { index, offset: cut + (lines.at(-1) ?? '').length };
};

export interface ITextEditable<TState> extends IParagraphs {
  /** The paragraphs changed: read them again and draw the page, with the caret here. */
  draw: (caret: ITextPlace | null) => void;
  /** A state to come back to, and the coming back. */
  capture: () => TState;
  restore: (state: TState) => void;
  changed: () => void;
}

/** Typing with pauses shorter than this is one step of undo. */
const UNDO_BURST_MS = 800;
const MAX_UNDO = 30;

const paragraphOf = (node: Node | null): HTMLElement | null => {
  const home = node instanceof Element ? node : (node?.parentElement ?? null);
  return home?.closest<HTMLElement>('[data-p]') ?? null;
};

/** The paragraph's number at this node, on the page. */
export const paragraphAt = (node: Node | null): number | null => {
  const element = paragraphOf(node);
  return element ? Number(element.dataset.p) : null;
};

const offsetIn = (element: HTMLElement, node: Node, offset: number): number => {
  const range = document.createRange();
  range.setStart(element, 0);
  range.setEnd(node, offset);
  return range.toString().replace(/\n/g, '').length;
};

const placeOf = (node: Node | null, offset: number): ITextPlace | null => {
  const element = paragraphOf(node);
  return element && node
    ? { index: Number(element.dataset.p), offset: offsetIn(element, node, offset) }
    : null;
};

/** Where the caret stands inside this element, if it does. */
export const caretIn = (root: HTMLElement): ITextPlace | null => {
  const selection = document.getSelection();
  if (!selection?.anchorNode || !root.contains(selection.anchorNode)) return null;
  return placeOf(selection.focusNode, selection.focusOffset);
};

export const putCaret = (root: HTMLElement, place: ITextPlace): void => {
  const element = root.querySelector<HTMLElement>(`[data-p="${place.index}"]`);
  const selection = document.getSelection();
  if (!element || !selection) return;
  const host = element.closest<HTMLElement>('[contenteditable]');
  if (host && document.activeElement !== host) host.focus({ preventScroll: true });
  const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
  let left = place.offset;
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const size = node.textContent?.length ?? 0;
    if (left <= size) {
      selection.collapse(node, left);
      return;
    }
    left -= size;
  }
  selection.collapse(element, element.childNodes.length);
};

const orderedSelection = (): { from: ITextPlace; to: ITextPlace } | null => {
  const selection = document.getSelection();
  if (!selection?.anchorNode || !selection.focusNode) return null;
  const a = placeOf(selection.anchorNode, selection.anchorOffset);
  const b = placeOf(selection.focusNode, selection.focusOffset);
  if (!a || !b) return null;
  const inOrder = a.index < b.index || (a.index === b.index && a.offset <= b.offset);
  return inOrder ? { from: a, to: b } : { from: b, to: a };
};

/** Takes the typing on `root`. Answers a way to forget the undo steps, when the file under them was replaced. */
export const useTextEditing = <TState>(
  root: RefObject<HTMLElement | null>,
  editable: ITextEditable<TState> | null,
): (() => void) => {
  const undo = useRef<TState[]>([]);
  const redo = useRef<TState[]>([]);
  const lastEdit = useRef(0);
  const composing = useRef(false);

  const remember = (doc: ITextEditable<TState>) => {
    undo.current.push(doc.capture());
    if (undo.current.length > MAX_UNDO) undo.current.shift();
    redo.current = [];
  };

  const putBack = (doc: ITextEditable<TState>, from: TState[], to: TState[]) => {
    const state = from.pop();
    if (state === undefined) return;
    to.push(doc.capture());
    doc.restore(state);
    doc.changed();
  };

  /** A change that goes into the file first: the page is drawn from it. Refused, it leaves no undo step. */
  const change = (doc: ITextEditable<TState>, act: () => ITextPlace | null) => {
    remember(doc);
    const caret = act();
    if (!caret) {
      undo.current.pop();
      return;
    }
    doc.draw(caret);
    doc.changed();
  };

  const onBeforeInput = useEffectEvent((event: InputEvent) => {
    const doc = editable;
    const type = event.inputType;
    if (!doc) {
      event.preventDefault();
      return;
    }
    if (type === 'historyUndo' || type === 'historyRedo') {
      event.preventDefault();
      if (type === 'historyUndo') putBack(doc, undo.current, redo.current);
      else putBack(doc, redo.current, undo.current);
      return;
    }
    const range = orderedSelection();
    if (!range || type === 'insertFromDrop' || type === 'deleteByDrag') {
      event.preventDefault();
      return;
    }
    const { from, to } = range;
    const now = performance.now();
    const burst = now - lastEdit.current < UNDO_BURST_MS;
    lastEdit.current = now;

    if (type === 'insertParagraph' || type === 'insertLineBreak') {
      event.preventDefault();
      change(doc, () => replaceRange(doc, from, to, '\n'));
      return;
    }
    const collapsed = from.index === to.index && from.offset === to.offset;
    if (type === 'deleteContentBackward' && collapsed && from.offset === 0) {
      event.preventDefault();
      change(doc, () => {
        const at = doc.join(from.index);
        return at === null ? null : { index: from.index - 1, offset: at };
      });
      return;
    }
    if (type === 'deleteContentForward' && collapsed && from.offset === doc.text(from.index).length) {
      event.preventDefault();
      change(doc, () => {
        const at = doc.join(from.index + 1);
        return at === null ? null : { index: from.index, offset: at };
      });
      return;
    }
    // Across paragraphs the browser would rearrange the page.
    if (from.index !== to.index) {
      event.preventDefault();
      change(doc, () => replaceRange(doc, from, to, type === 'insertText' ? (event.data ?? '') : ''));
      return;
    }
    if (!burst) remember(doc);
  });

  const onInput = useEffectEvent(() => {
    if (composing.current || !editable) return;
    const element = paragraphOf(document.getSelection()?.anchorNode ?? null);
    if (!element) return;
    const words = (element.textContent ?? '').replace(/\n/g, '');
    if (editable.setText(Number(element.dataset.p), words)) editable.changed();
  });

  const onPaste = useEffectEvent((event: ClipboardEvent) => {
    event.preventDefault();
    const range = orderedSelection();
    const doc = editable;
    if (!doc || !range) return;
    const text = event.clipboardData?.getData('text/plain') ?? '';
    change(doc, () => replaceRange(doc, range.from, range.to, text));
  });

  const onKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if (!editable || !(event.ctrlKey || event.metaKey) || event.altKey) return;
    const key = event.key.toLowerCase();
    if (key === 'z' && !event.shiftKey) {
      event.preventDefault();
      putBack(editable, undo.current, redo.current);
    } else if (key === 'y' || (key === 'z' && event.shiftKey)) {
      event.preventDefault();
      putBack(editable, redo.current, undo.current);
    }
  });

  // A link opens with Ctrl or Cmd held, as a plain click puts the caret in it.
  const onClick = useEffectEvent((event: MouseEvent) => {
    const link = event.target instanceof Element ? event.target.closest('a') : null;
    if (!link) return;
    event.preventDefault();
    if (event.ctrlKey || event.metaKey) window.open(link.href, '_blank', 'noopener,noreferrer');
  });

  const onCompose = useEffectEvent((on: boolean) => {
    composing.current = on;
    if (!on) onInput();
  });

  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const listeners = {
      beforeinput: (event: Event) => onBeforeInput(event as InputEvent),
      input: () => onInput(),
      paste: (event: Event) => onPaste(event as ClipboardEvent),
      keydown: (event: Event) => onKeyDown(event as KeyboardEvent),
      click: (event: Event) => onClick(event as MouseEvent),
      compositionstart: () => onCompose(true),
      compositionend: () => onCompose(false),
    };
    for (const [name, listener] of Object.entries(listeners)) element.addEventListener(name, listener);
    return () => {
      for (const [name, listener] of Object.entries(listeners)) element.removeEventListener(name, listener);
    };
  }, [root]);

  return () => {
    undo.current = [];
    redo.current = [];
  };
};
