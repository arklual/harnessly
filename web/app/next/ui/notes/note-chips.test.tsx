// @vitest-environment jsdom
// a press on an annotation's chip changes the comment where its words stand (the host's editor); the chip's
// card opens only when the host cannot show them, and its field grows with a long comment.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NoteChips, type INoteChip } from '@/next/ui/notes/note-chips';

const note: INoteChip = {
  id: 'n1',
  name: '«Приложения ChatGPT и Codex»',
  comment: 'Хотелось бы голосовой режим в реальном времени',
  facts: [{ label: 'Фрагмент', value: 'Приложения ChatGPT и Codex' }],
};

const chip = () => document.querySelector('button[aria-haspopup="dialog"]') as HTMLButtonElement;
const field = () => screen.getByRole('dialog', { hidden: true }).querySelector('textarea') as HTMLTextAreaElement;

afterEach(cleanup);

describe('a press on an annotation chip', () => {
  it('goes to the host, which shows the words and their editor; the card stays shut', () => {
    const onOpen = vi.fn();
    render(<NoteChips notes={[note]} onEdit={vi.fn()} onRemove={vi.fn()} onOpen={onOpen} />);
    fireEvent.click(chip());
    expect(onOpen).toHaveBeenCalledWith('n1', expect.any(Function));
    expect(chip().getAttribute('aria-expanded')).toBe('false');
  });

  it('opens the card with the comment when the words are not in the feed', () => {
    render(
      <NoteChips notes={[note]} onEdit={vi.fn()} onRemove={vi.fn()} onOpen={(_, missing) => missing()} />,
    );
    act(() => fireEvent.click(chip()));
    expect(chip().getAttribute('aria-expanded')).toBe('true');
    expect(field().value).toBe(note.comment);
    expect(document.activeElement).toBe(field());
  });

  it('opens the card where the host has nothing to show', () => {
    render(<NoteChips notes={[note]} onEdit={vi.fn()} onRemove={vi.fn()} />);
    act(() => fireEvent.click(chip()));
    expect(chip().getAttribute('aria-expanded')).toBe('true');
  });
});

describe('the card of a chip', () => {
  const opened = (onEdit = vi.fn(), onRemove = vi.fn()) => {
    render(<NoteChips notes={[note]} onEdit={onEdit} onRemove={onRemove} onOpen={(_, missing) => missing()} />);
    act(() => fireEvent.click(chip()));
    return { onEdit, onRemove };
  };

  it('grows its field with a long comment', () => {
    const tall = vi.spyOn(HTMLTextAreaElement.prototype, 'scrollHeight', 'get').mockReturnValue(180);
    opened();
    fireEvent.change(field(), { target: { value: `${note.comment}\n`.repeat(6) } });
    expect(field().style.height).toBe('180px');
    tall.mockRestore();
  });

  it('keeps the change on Enter and takes it back on Esc', () => {
    const { onEdit } = opened();
    fireEvent.change(field(), { target: { value: 'Новый текст' } });
    fireEvent.keyDown(field(), { key: 'Enter' });
    expect(onEdit).toHaveBeenCalledWith('n1', 'Новый текст');
    fireEvent.change(field(), { target: { value: 'Ещё' } });
    fireEvent.keyDown(field(), { key: 'Escape' });
    expect(field().value).toBe(note.comment);
  });

  it('deletes the annotation', () => {
    const { onRemove } = opened();
    fireEvent.click(screen.getByRole('button', { name: /Удалить|Delete/, hidden: true }));
    expect(onRemove).toHaveBeenCalledWith('n1');
  });
});
