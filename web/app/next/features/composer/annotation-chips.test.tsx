// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AnnotationChips } from '@/next/features/composer/annotation-chips';
import { NOTE_EXIT_MS } from '@/next/ui/notes/note-chips';
import type { IAnnotation } from '@/next/features/pane/annotations';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const note = (id: string, comment = ''): IAnnotation => ({
  id,
  comment,
  pick: 1,
  selector: `#${id}`,
  tag: 'button',
  text: id,
  rect: { x: 0, y: 0, width: 10, height: 10 },
  html: '',
  component: { name: 'BuyButton', file: null, line: null },
  url: 'http://localhost:3000/shop',
});

describe('AnnotationChips', () => {
  it('draws a chip per note with its number and name, and the card says page, element, component', () => {
    render(<AnnotationChips notes={[note('a'), note('b')]} onEdit={vi.fn()} onRemove={vi.fn()} />);
    expect(screen.getAllByRole('button', { name: /^Annotation \d: / })).toHaveLength(2);
    expect(screen.getByRole('button', { name: 'Annotation 2: «b»' })).toBeTruthy();
    expect(document.body.textContent).toContain('http://localhost:3000/shop');
    expect(document.body.textContent).toContain('BuyButton');
  });

  it('saves a changed comment on Enter and takes it back on Esc', () => {
    const onEdit = vi.fn();
    render(<AnnotationChips notes={[note('a', 'old')]} onEdit={onEdit} onRemove={vi.fn()} />);
    const field = document.querySelector('textarea') as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: 'new' } });
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(field.value).toBe('old');
    expect(onEdit).not.toHaveBeenCalled();
    fireEvent.change(field, { target: { value: 'new' } });
    fireEvent.keyDown(field, { key: 'Enter' });
    expect(onEdit).toHaveBeenCalledWith('a', 'new');
  });

  it('deletes by × and by «Delete»; a removed chip leaves after its animation', () => {
    const onRemove = vi.fn();
    const { rerender } = render(
      <AnnotationChips notes={[note('a'), note('b')]} onEdit={vi.fn()} onRemove={onRemove} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Remove «a»' }));
    expect(onRemove).toHaveBeenCalledWith('a');
    fireEvent.click(screen.getAllByText('Delete')[1]);
    expect(onRemove).toHaveBeenCalledWith('b');
    rerender(<AnnotationChips notes={[note('b')]} onEdit={vi.fn()} onRemove={onRemove} />);
    expect(document.querySelectorAll('[data-leaving]')).toHaveLength(1);
    act(() => vi.advanceTimersByTime(NOTE_EXIT_MS + 1));
    expect(document.querySelectorAll('[data-leaving]')).toHaveLength(0);
    expect(document.querySelectorAll('.hx-pagenote')).toHaveLength(1);
  });

  it('puts a batch of new chips on a ladder', () => {
    render(<AnnotationChips notes={[note('a'), note('b'), note('c')]} onEdit={vi.fn()} onRemove={vi.fn()} />);
    const steps = [...document.querySelectorAll<HTMLElement>('.hx-pagenote')].map((el) =>
      el.style.getPropertyValue('--step'),
    );
    expect(steps).toEqual(['0', '1', '2']);
  });
});
