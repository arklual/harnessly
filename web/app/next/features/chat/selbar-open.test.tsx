// @vitest-environment jsdom
// a press on a feed annotation's chip brings its words into view and opens there the editor the annotation was
// made with; words the feed does not hold leave it to the chip's card.
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SelBar, { showFeedNote } from '@/next/features/chat/selbar';
import type { ConvId } from '@/next/protocol/v2';
import { addFeedNote, clearFeedNotes } from '@/next/stores/quotes';

const CONV = 'c-annot' as ConvId;
const scrolled = vi.fn();

beforeEach(() => {
  Element.prototype.scrollIntoView = scrolled;
  // jsdom lays nothing out: the words have no boxes, the mark stands hidden, which these tests do not look at.
  Range.prototype.getClientRects ??= (() => []) as unknown as Range['getClientRects'];
  Range.prototype.getBoundingClientRect ??= (() => new DOMRect()) as Range['getBoundingClientRect'];
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  document.body.innerHTML =
    '<div class="hx-feed"><div class="hx-turnwrap" data-key="t1"><p>Приложения ChatGPT и Codex в одном месте</p></div></div>';
});

afterEach(() => {
  cleanup();
  clearFeedNotes(CONV);
  scrolled.mockReset();
  document.body.innerHTML = '';
});

const annotate = (id: string, comment: string) =>
  addFeedNote(CONV, {
    id,
    text: 'Приложения ChatGPT и Codex',
    note: comment,
    anchor: { key: 't1', start: 0, raw: 'Приложения ChatGPT и Codex' },
  });

describe('a press on a feed annotation chip', () => {
  it('scrolls to the words and opens their editor with the comment', async () => {
    annotate('f1', 'Хотелось бы голосовой режим');
    render(<SelBar conv={CONV} />);
    const missing = vi.fn();
    act(() => showFeedNote(CONV, 'f1', missing));
    await waitFor(() => expect(scrolled).toHaveBeenCalled());
    const editor = await screen.findByRole('textbox', { name: /.+/ });
    expect((editor as HTMLTextAreaElement).value).toBe('Хотелось бы голосовой режим');
    expect(missing).not.toHaveBeenCalled();
  });

  it('leaves the change to the card when the annotation has no words in this feed', async () => {
    render(<SelBar conv={CONV} />);
    const missing = vi.fn();
    act(() => showFeedNote(CONV, 'gone', missing));
    await waitFor(() => expect(missing).toHaveBeenCalledTimes(1));
    expect(scrolled).not.toHaveBeenCalled();
  });

  it('leaves it to the card when the words are no longer in the feed', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    annotate('f2', 'Старый ход');
    document.querySelector('.hx-turnwrap')?.remove();
    render(<SelBar conv={CONV} />);
    const missing = vi.fn();
    act(() => showFeedNote(CONV, 'f2', missing));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(missing).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});
