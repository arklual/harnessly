// The text of a field kept by the page, put into an uncontrolled <textarea> only when it differs.
//
// A controlled textarea costs a whole page's style on each key in React 19: besides `value` it writes `defaultValue`,
// which replaces the textarea's text node; that child change re-checks every `:has()` rule up to <html>, and the page
// (about a thousand elements in a chat) recalculates its style, forced at once by the field's own measuring. The field
// is therefore left uncontrolled: what the person types is already in it, and only a text from elsewhere (a clear after
// a send, a dictation, a draft from another device) is written to its `value`, which touches no child.
import { useLayoutEffect, useRef, type RefObject } from 'react';

export const useFieldValue = (fieldRef: RefObject<HTMLTextAreaElement | null>, value: string) => {
  const shown = useRef<{ field: HTMLTextAreaElement | null; value: string | null }>({
    field: null,
    value: null,
  });
  // Before the field is measured. Only a new text (or a new field) is written: a render that still carries the text
  // from before the last key (the lent field renders twice on a key) must not put it back over what was typed.
  useLayoutEffect(() => {
    const field = fieldRef.current;
    if (!field || (shown.current.field === field && shown.current.value === value)) return;
    shown.current = { field, value };
    if (field.value === value) return;
    // Writing `value` throws the caret to the end: a text from elsewhere into the field the person is in keeps their
    // caret where it stood; one that set its own (a dictation, a pick) sets it after this.
    const focused = typeof document !== 'undefined' && document.activeElement === field;
    const { selectionStart: start, selectionEnd: end, selectionDirection: direction } = field;
    field.value = value;
    if (focused)
      field.setSelectionRange(Math.min(start, value.length), Math.min(end, value.length), direction);
  });
};
