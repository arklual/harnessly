// @vitest-environment jsdom
import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NoteStrip, type INote } from '@/next/features/designly/site-notes';
import { NO_EDIT } from '@/next/features/designly/element-style';

afterEach(cleanup);

const note: INote = {
  id: 'c1',
  picked: {
    id: 1,
    look: null,
    selector: 'h1',
    tag: 'h1',
    text: 'Title',
    rect: { x: 0, y: 0, width: 1, height: 1 },
    component: null,
    html: '',
  },
  page: 'a.html',
  text: 'Ann: too big',
  at: { x: 0, y: 0, right: false },
  style: NO_EDIT,
};

describe('the list of a guest comments', () => {
  it('is read only: no clear, edit or remove', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
    const view = render(
      <NoteStrip notes={[note]} readOnly onEdit={vi.fn()} onRemove={vi.fn()} onClear={vi.fn()} />,
    );
    expect(view.container.querySelector('.hx-anns__x')).toBeNull();
    fireEvent.click(view.container.querySelector('.hx-anns__main') as Element);
    expect(view.container.textContent).toContain('Ann: too big');
    expect(view.container.querySelector('.hx-anns__acts')).toBeNull();
  });

  it('keeps the owner actions where it is not read only', () => {
    const view = render(<NoteStrip notes={[note]} onEdit={vi.fn()} onRemove={vi.fn()} onClear={vi.fn()} />);
    expect(view.container.querySelector('.hx-anns__x')).not.toBeNull();
  });
});
