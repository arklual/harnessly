// @vitest-environment jsdom
// «Сделано в этом чате»: nothing before the chat has said (a skeleton only when slow), nothing when nothing was made,
// cards, the rest behind «more», a press on a card says which, and the block leaves with an exit.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import MadeBlock, { MADE_VISIBLE, type IMadeCard } from '@/next/features/pane/made-block';

beforeAll(() => pinLocale('en'));
beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const CARDS: IMadeCard[] = [
  { id: 'doc:https://d/a/spec', icon: 'file-text', title: 'Spec', pending: false, about: 'd' },
  { id: 'design:landing', icon: 'palette', title: 'Landing', pending: false, about: 'Site' },
];

describe('MadeBlock', () => {
  it('draws nothing before the chat has said, not even a skeleton that would move the tools', () => {
    const { container } = render(<MadeBlock cards={null} onOpen={vi.fn()} />);
    act(() => void vi.advanceTimersByTime(2000));
    expect(container.querySelector('.hx-made')).toBeNull();
    expect(container.querySelectorAll('.hx-sk')).toHaveLength(0);
  });

  it('comes in collapsed and opens a couple of frames later, so the height grows', () => {
    const { container } = render(<MadeBlock cards={CARDS} onOpen={vi.fn()} />);
    expect(container.querySelector('.hx-made')?.getAttribute('data-state')).toBe('opening');
    act(() => void vi.advanceTimersByTime(100));
    expect(container.querySelector('.hx-made')?.getAttribute('data-state')).toBe('open');
  });

  it('is not there when nothing was made', () => {
    const { container } = render(<MadeBlock cards={[]} onOpen={vi.fn()} />);
    act(() => void vi.advanceTimersByTime(400));
    expect(container.querySelector('.hx-made')).toBeNull();
  });

  it('shows the cards in order, and says which card was pressed', () => {
    const onOpen = vi.fn();
    const { container } = render(<MadeBlock cards={CARDS} onOpen={onOpen} />);
    const names = [...container.querySelectorAll('.hx-made__name')].map((x) => x.textContent);
    expect(names).toEqual(['Spec', 'Landing']);
    fireEvent.click(screen.getByRole('button', { name: /Landing/ }));
    expect(onOpen).toHaveBeenCalledWith(CARDS[1]);
  });

  it('folds what is past the visible rows behind «more», and shows them on a press', () => {
    const many: IMadeCard[] = Array.from({ length: MADE_VISIBLE + 3 }, (_, i) => ({
      ...CARDS[0],
      id: `doc:https://d/a/${i}`,
      title: `Doc ${i}`,
    }));
    const { container } = render(<MadeBlock cards={many} onOpen={vi.fn()} />);
    expect(container.querySelectorAll('.hx-made__card')).toHaveLength(MADE_VISIBLE);
    fireEvent.click(screen.getByRole('button', { name: /3 more/ }));
    expect(container.querySelectorAll('.hx-made__card')).toHaveLength(MADE_VISIBLE + 3);
    fireEvent.click(screen.getByRole('button', { name: 'Show less' }));
    expect(container.querySelectorAll('.hx-made__card')).toHaveLength(MADE_VISIBLE);
  });

  it('does not fold for the sake of one row', () => {
    const few: IMadeCard[] = Array.from({ length: MADE_VISIBLE + 1 }, (_, i) => ({
      ...CARDS[0],
      id: `doc:https://d/a/${i}`,
    }));
    const { container } = render(<MadeBlock cards={few} onOpen={vi.fn()} />);
    expect(container.querySelectorAll('.hx-made__card')).toHaveLength(MADE_VISIBLE + 1);
    expect(container.querySelector('.hx-made__more')).toBeNull();
  });

  it('leaves with an exit when what it shows goes', () => {
    const { container, rerender } = render(<MadeBlock cards={CARDS} onOpen={vi.fn()} />);
    act(() => void vi.advanceTimersByTime(100));
    expect(container.querySelector('.hx-made')?.getAttribute('data-state')).toBe('open');
    rerender(<MadeBlock cards={[]} onOpen={vi.fn()} />);
    expect(container.querySelector('.hx-made')?.getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(400));
    expect(container.querySelector('.hx-made')).toBeNull();
  });

  it('stands a bar in for a name that is still being read, and the name once it is', () => {
    const wait: IMadeCard[] = [{ ...CARDS[0], title: 'Document', pending: true }];
    const { container, rerender } = render(<MadeBlock cards={wait} onOpen={vi.fn()} />);
    expect(container.querySelectorAll('.hx-made__name-sk')).toHaveLength(1);
    expect(screen.queryByText('Document')).toBeNull();
    rerender(<MadeBlock cards={[{ ...wait[0], title: 'Spec', pending: false }]} onOpen={vi.fn()} />);
    expect(container.querySelectorAll('.hx-made__name-sk')).toHaveLength(0);
    expect(screen.getByText('Spec')).toBeTruthy();
  });

  it('opens an epic into its tasks and says which task was pressed', () => {
    const onOpen = vi.fn();
    const epic: IMadeCard = {
      id: 'task:https://t/issue/E-1',
      icon: 'layers',
      title: 'Epic',
      pending: false,
      about: '2 tasks',
      kids: [
        { id: 'task:https://t/issue/E-2', key: 'E-2', title: 'First', state: '', done: false },
        { id: 'task:https://t/issue/E-3', key: 'E-3', title: 'Second', state: 'Done', done: true },
      ],
    };
    const { container } = render(<MadeBlock cards={[epic]} onOpen={onOpen} />);
    const toggle = container.querySelector('.hx-made__toggle') as HTMLElement;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('.hx-made__fold')?.hasAttribute('inert')).toBe(true);
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(container.querySelector('.hx-made__fold')?.hasAttribute('inert')).toBe(false);
    fireEvent.click(screen.getByText('Second'));
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: 'task:https://t/issue/E-3' }));
  });
});
