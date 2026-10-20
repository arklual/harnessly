import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type HTMLAttributes,
  type Ref,
} from 'react';
import { readEditableText } from '@/lib/editable-text';
import { htmlToMarkdown } from '@/lib/html-to-markdown';
import { startKeywordShimmer } from '@/lib/keyword-shimmer';

export interface PromptEditorHandle {
  focus: () => void;
  blur: () => void;
  focusEnd: () => void;
  insertText: (text: string, surroundWithSpaces?: boolean) => void;
}

interface PromptEditorProps extends Omit<HTMLAttributes<HTMLDivElement>, 'children' | 'onChange'> {
  editorRef: Ref<PromptEditorHandle>;
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
  /**
   * Pictures from the phone keyboard (Gboard, Samsung): Chrome offers them only to a rich field, so the field turns
   * `contentEditable="true"` and keeps itself plain by hand — lines as text, pictures handed over, formatting refused.
   */
  onFiles?: (files: File[]) => void;
}

/** What a paste puts in the field: a rich copy as Markdown (links, lists, bold survive), anything else as plain text. */
const pastedText = (data: DataTransfer): string => {
  const html = data.getData('text/html');
  const markdown = html ? htmlToMarkdown(new DOMParser().parseFromString(html, 'text/html').body) : null;
  return markdown ?? data.getData('text/plain');
};

const escapeHtml = (text: string) => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/**
 * Puts text at the caret as one editing step, so the browser's undo takes it back. Chrome types a multiline
 * `insertText` line by line, each break a paragraph command of its own: a thousand pasted lines held the page for 4 s,
 * three thousand for 50. In a field that keeps its line breaks the same text goes in as one escaped `insertHTML`, a single
 * text node with the breaks inside, in 30 ms.
 */
const execInsert = (element: HTMLElement, text: string): boolean => {
  if (
    text.includes('\n') &&
    /^(pre|pre-wrap|pre-line|break-spaces)$/.test(getComputedStyle(element).whiteSpace) &&
    document.execCommand('insertHTML', false, escapeHtml(text))
  )
    return true;
  return document.execCommand('insertText', false, text);
};

const imageFileOf = async (image: HTMLImageElement, index: number): Promise<File | null> => {
  try {
    const blob = await (await fetch(image.src)).blob();
    if (!blob.type.startsWith('image/')) return null;
    return new File([blob], `keyboard-${Date.now()}-${index}.${blob.type.split('/')[1] ?? 'png'}`, {
      type: blob.type,
    });
  } catch {
    return null;
  }
};

// A message is editable document text, not a credential/form field. Keep the DOM
// browser-owned while typing so React cannot reset the caret, undo stack or IME.
/**
 * On a desktop the field is always in view, and scrolling it into view laid the whole page out once more
 * in the frame of a conversation switch or of the first paint. A phone keeps the browser's scroll to the keyboard.
 */
const focusInPlace = (element: HTMLElement) =>
  element.focus({
    preventScroll: typeof window !== 'undefined' && window.matchMedia?.('(pointer: fine)').matches,
  });

export default function PromptEditor({
  editorRef,
  value,
  onValueChange,
  placeholder,
  onFiles,
  onPaste,
  onKeyDown,
  className,
  ...props
}: PromptEditorProps) {
  const elementRef = useRef<HTMLDivElement>(null);
  const composing = useRef(false);
  const savedSelection = useRef<Range | null>(null);
  const adopted = useRef(false);
  const [empty, setEmpty] = useState(value.length === 0);
  const rich = onFiles !== undefined;
  const read = () => {
    const element = elementRef.current;
    if (!element) return '';
    return rich ? readEditableText(element) : (element.textContent ?? '');
  };
  const sync = () => setEmpty(read() === '');
  const report = () => {
    adopted.current = true;
    onValueChange(read());
  };

  const captureSelection = useCallback(() => {
    const selection = window.getSelection();
    if (
      selection?.rangeCount &&
      elementRef.current?.contains(selection.getRangeAt(0).commonAncestorContainer)
    ) {
      savedSelection.current = selection.getRangeAt(0).cloneRange();
    }
  }, []);

  const focusEnd = useCallback(() => {
    const element = elementRef.current;
    if (!element) return;
    focusInPlace(element);
    const range = document.createRange();
    range.selectNodeContents(element);
    range.collapse(false);
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    captureSelection();
  }, [captureSelection]);

  const insertText = (text: string, surroundWithSpaces = false) => {
    const element = elementRef.current;
    if (!element) return;
    captureSelection();
    element.focus();
    const selection = window.getSelection();
    const saved = savedSelection.current;
    if (saved && element.contains(saved.commonAncestorContainer)) {
      selection?.removeAllRanges();
      selection?.addRange(saved);
    } else {
      focusEnd();
    }
    if (!selection?.rangeCount) return;
    const range = selection.getRangeAt(0);
    if (surroundWithSpaces) {
      const before = document.createRange();
      before.selectNodeContents(element);
      before.setEnd(range.startContainer, range.startOffset);
      const after = document.createRange();
      after.selectNodeContents(element);
      after.setStart(range.endContainer, range.endOffset);
      const prefix = before.toString();
      const suffix = after.toString();
      text = `${prefix && !/\s$/.test(prefix) ? ' ' : ''}${text}${!suffix || !/^\s/.test(suffix) ? ' ' : ''}`;
    }
    // insertText preserves native undo, unlike replacing innerHTML/textContent.
    // Retain a Range fallback for browsers that don't implement the command.
    if (!execInsert(element, text)) {
      range.deleteContents();
      const node = document.createTextNode(text);
      range.insertNode(node);
      range.setStartAfter(node);
      range.collapse(true);
      selection.removeAllRanges();
      selection.addRange(range);
    }
    captureSelection();
    report();
    sync();
  };

  const onFilesRef = useRef(onFiles);
  useEffect(() => {
    onFilesRef.current = onFiles;
  });

  useEffect(() => {
    const element = elementRef.current;
    if (!element || !rich) return;
    const onBeforeInput = (event: InputEvent) => {
      const type = event.inputType;
      if (type.startsWith('format')) {
        event.preventDefault();
        return;
      }
      if (type !== 'insertFromPaste' && type !== 'insertFromDrop' && type !== 'insertReplacementText') return;
      const files = Array.from(event.dataTransfer?.files ?? []);
      if (files.length > 0) {
        event.preventDefault();
        onFilesRef.current?.(files);
        return;
      }
      const html = event.dataTransfer?.types.includes('text/html');
      if (html && type !== 'insertFromPaste') {
        event.preventDefault();
        execInsert(element, event.dataTransfer?.getData('text/plain') ?? '');
      }
    };
    element.addEventListener('beforeinput', onBeforeInput);
    return () => element.removeEventListener('beforeinput', onBeforeInput);
  }, [rich]);

  /** A keyboard that skipped every event above still leaves its picture as an <img>: take it out and attach it. */
  const adoptImages = () => {
    const images = Array.from(elementRef.current?.querySelectorAll('img') ?? []);
    if (images.length === 0) return;
    images.forEach((image) => image.remove());
    void Promise.all(images.map(imageFileOf)).then((files) => {
      const found = files.filter((file): file is File => file !== null);
      if (found.length > 0) onFilesRef.current?.(found);
    });
  };

  useImperativeHandle(editorRef, () => ({
    focus: () => {
      if (elementRef.current) focusInPlace(elementRef.current);
    },
    blur: () => elementRef.current?.blur(),
    focusEnd,
    insertText,
  }));

  // Text typed before hydration reaches the DOM without an input event we could report:
  // «ultracode» in the prompt shimmers like in Claude Code: coloured by the Highlight API, the text untouched.
  useEffect(() => {
    const element = elementRef.current;
    return element ? startKeywordShimmer(element) : undefined;
  }, []);

  // adopt it only while state has never held text; after that state is the single source of truth (a send must be able to clear it).
  useLayoutEffect(() => {
    const element = elementRef.current;
    if (!element || composing.current) return;
    const dom = read();
    if (dom !== value && value === '' && !adopted.current) {
      adopted.current = true;
      onValueChange(dom);
    } else if (dom !== value) {
      element.textContent = value;
      savedSelection.current = null;
      if (document.activeElement === element) focusEnd();
    }
    if (value !== '') adopted.current = true;
    sync();
  });

  return (
    <div
      {...props}
      ref={elementRef}
      role="textbox"
      aria-multiline="true"
      aria-placeholder={placeholder}
      contentEditable={rich ? 'true' : 'plaintext-only'}
      suppressContentEditableWarning
      data-placeholder={placeholder}
      data-empty={empty}
      data-1p-ignore="true"
      data-lpignore="true"
      data-bwignore="true"
      data-form-type="other"
      className={`min-w-0 whitespace-pre-wrap break-words ${className ?? ''}`}
      onInput={() => {
        if (rich) adoptImages();
        captureSelection();
        report();
        sync();
      }}
      onBlur={captureSelection}
      onCompositionStart={() => {
        composing.current = true;
      }}
      onCompositionEnd={() => {
        composing.current = false;
        report();
        sync();
      }}
      onKeyDown={(event) => {
        if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return;
        onKeyDown?.(event);
      }}
      onPaste={(event) => {
        onPaste?.(event);
        if (event.defaultPrevented) return;
        event.preventDefault();
        insertText(pastedText(event.clipboardData));
      }}
    />
  );
}
