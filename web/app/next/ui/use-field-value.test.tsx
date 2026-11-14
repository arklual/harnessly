// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { useRef } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { useFieldValue } from '@/next/ui/use-field-value';

afterEach(cleanup);

const Field = ({ value }: { value: string }) => {
  const ref = useRef<HTMLTextAreaElement>(null);
  useFieldValue(ref, value);
  return <textarea ref={ref} aria-label="field" />;
};

const field = () => document.querySelector('textarea') as HTMLTextAreaElement;

describe('useFieldValue', () => {
  it('writes nothing when the field already shows the text: the caret stays', () => {
    const view = render(<Field value="hello" />);
    const el = field();
    el.focus();
    el.value = 'xhello';
    el.setSelectionRange(1, 1);
    view.rerender(<Field value="xhello" />);
    expect(el.selectionStart).toBe(1);
  });

  // a text from elsewhere (a draft from another device) into the field the person is in.
  it('keeps the caret of the person in the field when a text from elsewhere comes in', () => {
    const view = render(<Field value="hello world" />);
    const el = field();
    el.focus();
    el.setSelectionRange(2, 4);
    view.rerender(<Field value="hello there, world" />);
    expect(el.value).toBe('hello there, world');
    expect([el.selectionStart, el.selectionEnd]).toEqual([2, 4]);
    // A shorter text: the caret stays inside it.
    el.setSelectionRange(10, 12);
    view.rerender(<Field value="hey" />);
    expect([el.selectionStart, el.selectionEnd]).toEqual([3, 3]);
  });
});
