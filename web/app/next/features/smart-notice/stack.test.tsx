// @vitest-environment jsdom
// The stack of what waits in the right column: folded it is one card with the edges of the rest; a tap
// spreads it by chat with «Свернуть» and «Очистить всё»; a card opens in place, its line whole, and never leaves it.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { AUTO_OPEN_DELAY_MS, AUTO_OPEN_MS, resetAutoOpen } from '@/next/features/notifications/auto-open';
import NoticeStack from '@/next/features/smart-notice/stack';
import { SMART_KIND, type ISmartItem, type TSmartKind } from '@/next/features/smart-notice/smart-notice';
import {
  dismissDrops,
  landDrop,
  markArriving,
  setDrops,
  useDrops,
  type IDrop,
} from '@/next/stores/smart-drops';
import { renderHook } from '@testing-library/react';

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const NOW = 50_000_000;

const drop = (
  key: string,
  chatId: string | null,
  title: string,
  kind: TSmartKind = SMART_KIND.question,
): IDrop => {
  const item: ISmartItem = {
    key,
    kind,
    chatId,
    icon: 'message-circle-question',
    title,
    text: `What ${key} asks, a long line that is cut while the card is shut and whole once it is open`,
    at: NOW - 60_000,
    until: null,
  };
  return { item, what: `The whole question of ${key}` };
};

const stack = (drops: readonly IDrop[], onDismiss = vi.fn(), onBack = vi.fn()) =>
  render(
    <NoticeStack
      drops={drops}
      now={NOW}
      label="Waiting"
      onBack={onBack}
      onDismiss={onDismiss}
      onOpenChat={vi.fn()}
    />,
  );

describe('NoticeStack', () => {
  const three = [drop('a', 'c1', 'Landing'), drop('b', 'c1', 'Landing'), drop('c', 'c2', 'Billing')];

  it('folds several into one card with the count, and spreads them by chat on a tap', () => {
    const { container } = stack(three);
    expect(container.querySelector('.hx-ns')?.getAttribute('data-state')).toBe('folded');
    expect(container.querySelectorAll('.hx-ns__under')).toHaveLength(2);
    expect(screen.getByText('3')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Show all: 3' }));
    expect(container.querySelector('.hx-ns')?.getAttribute('data-state')).toBe('spread');
    expect(container.querySelectorAll('.hx-ns__card')).toHaveLength(3);
    // Two of one chat stand together under its name; the chat of one has no heading.
    const names = [...container.querySelectorAll('.hx-ns__gname')].map((n) => n.textContent);
    expect(names).toEqual(['Landing2']);
    expect(screen.getByRole('button', { name: 'Show less' })).toBeTruthy();
  });

  it('folds back with its animation before the stack shows again', () => {
    vi.useFakeTimers();
    const { container } = stack(three);
    fireEvent.click(screen.getByRole('button', { name: 'Show all: 3' }));
    fireEvent.click(screen.getByRole('button', { name: 'Show less' }));
    expect(container.querySelector('.hx-ns__list')?.hasAttribute('data-folding')).toBe(true);
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(container.querySelector('.hx-ns__list')).toBeNull();
    expect(container.querySelector('.hx-ns__pile')).toBeTruthy();
  });

  it('clears all at once', () => {
    const onDismiss = vi.fn();
    stack(three, onDismiss);
    fireEvent.click(screen.getByRole('button', { name: 'Show all: 3' }));
    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    expect(onDismiss).toHaveBeenCalledWith(['a', 'b', 'c']);
  });

  it('opens a card in place, the question whole, without asking the notice back', () => {
    const onBack = vi.fn();
    const { container } = stack([drop('a', 'c1', 'Landing')], vi.fn(), onBack);
    const card = container.querySelector('.hx-ns__card') as HTMLElement;
    fireEvent.click(card.querySelector('.hx-ns__hit') as HTMLElement);
    expect(card.hasAttribute('data-open')).toBe(true);
    expect(screen.getByText('The whole question of a')).toBeTruthy();
    expect(onBack).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('takes a card off with its flight, then tells', () => {
    vi.useFakeTimers();
    const onDismiss = vi.fn();
    const { container } = stack([drop('a', 'c1', 'Landing')], onDismiss);
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    expect(container.querySelector('.hx-ns__card')?.hasAttribute('data-leaving')).toBe(true);
    expect(onDismiss).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(onDismiss).toHaveBeenCalledWith(['a']);
  });

  it('keeps a card answered elsewhere for its flight off', () => {
    vi.useFakeTimers();
    const { container, rerender } = stack([drop('a', 'c1', 'Landing')]);
    rerender(<NoticeStack drops={[]} now={NOW} label="Waiting" onBack={vi.fn()} onDismiss={vi.fn()} />);
    expect(container.querySelector('.hx-ns__card')?.hasAttribute('data-leaving')).toBe(true);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(container.querySelector('.hx-ns')).toBeNull();
  });
});

describe('NoticeStack without blinks (HRN-1285)', () => {
  const props = { now: NOW, label: 'Waiting', onBack: vi.fn(), onDismiss: vi.fn() };
  /** The cards put into the page since the last look: each one is a mount. */
  const mounts = (container: HTMLElement) => {
    const seen = new MutationObserver(() => undefined);
    seen.observe(container, { childList: true, subtree: true });
    return () =>
      seen
        .takeRecords()
        .flatMap((r) => [...r.addedNodes])
        .flatMap((n) =>
          n instanceof HTMLElement
            ? [...(n.matches('.hx-ns__card') ? [n] : []), ...n.querySelectorAll('.hx-ns__card')]
            : [],
        )
        .map((n) => n.getAttribute('data-drop-key'));
  };

  it('mounts a new card once and leaves the others where they are', () => {
    const a = drop('a', 'c1', 'Landing');
    const b = drop('b', 'c2', 'Billing');
    const { container, rerender } = render(<NoticeStack drops={[a, b]} {...props} />);
    fireEvent.click(screen.getByRole('button', { name: 'Show all: 2' }));
    const before = container.querySelector('[data-drop-key="a"]');
    const look = mounts(container);
    rerender(<NoticeStack drops={[drop('c', 'c3', 'Docs'), a, b]} {...props} />);
    expect(look()).toEqual(['c']);
    expect(container.querySelector('[data-drop-key="a"]')).toBe(before);
    // The same drops drawn again (the notice's binding builds them anew) mount nothing.
    const again = mounts(container);
    rerender(<NoticeStack drops={[drop('c', 'c3', 'Docs'), drop('a', 'c1', 'Landing'), b]} {...props} />);
    expect(again()).toEqual([]);
  });

  it('makes the stack at once when a second comes: the lone card does not fold away first', () => {
    const a = drop('a', 'c1', 'Landing');
    const { container, rerender } = render(<NoticeStack drops={[a]} {...props} />);
    rerender(<NoticeStack drops={[drop('b', 'c2', 'Billing'), a]} {...props} />);
    expect(container.querySelector('.hx-ns__list')).toBeNull();
    expect(container.querySelector('.hx-ns__pile')?.hasAttribute('data-calm')).toBe(true);
  });

  it('draws a card the notice flies into hidden from its first frame, then as it is, with no entrance again', () => {
    const a = drop('a', 'c1', 'Landing');
    const { container, rerender } = render(<NoticeStack drops={[]} {...props} />);
    act(() => markArriving('a'));
    rerender(<NoticeStack drops={[a]} {...props} />);
    const card = container.querySelector('[data-drop-key="a"]') as HTMLElement;
    expect(card.hasAttribute('data-arriving')).toBe(true);
    act(() => landDrop('a'));
    expect(card.hasAttribute('data-arriving')).toBe(false);
    expect(card.hasAttribute('data-landed')).toBe(true);
    expect(container.querySelector('[data-drop-key="a"]')).toBe(card);
  });

  it('lets a card the person took off go without flying it a second time', () => {
    vi.useFakeTimers();
    const a = drop('a', 'c1', 'Landing');
    const b = drop('b', 'c2', 'Billing');
    const onDismiss = vi.fn();
    const { container, rerender } = render(<NoticeStack drops={[a, b]} {...props} onDismiss={onDismiss} />);
    fireEvent.click(screen.getByRole('button', { name: 'Show all: 2' }));
    const card = container.querySelector('[data-drop-key="a"]') as HTMLElement;
    fireEvent.click(card.querySelector('.hx-ns__x') as HTMLElement);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(card.getAttribute('data-leaving')).toBe('flown');
    expect(onDismiss).toHaveBeenCalledWith(['a']);
    rerender(<NoticeStack drops={[b]} {...props} onDismiss={onDismiss} />);
    expect(container.querySelector('[data-drop-key="a"]')).toBeNull();
  });
});

describe('dismissDrops', () => {
  it('hides a drop from the column until it stops waiting, and lets it back when it waits again', () => {
    const a = drop('a', 'c1', 'Landing');
    const b = drop('b', 'c2', 'Billing');
    const { result } = renderHook(() => useDrops());
    act(() => setDrops([a, b]));
    act(() => dismissDrops(['a']));
    expect(result.current.map((d) => d.item.key)).toEqual(['b']);
    act(() => setDrops([b]));
    act(() => setDrops([a, b]));
    expect(result.current.map((d) => d.item.key)).toEqual(['a', 'b']);
    act(() => setDrops([]));
  });
});

// a machine about to fail its work opens in place for a few seconds and folds, seldom; nothing else does.
describe('a card that opens by itself', () => {
  const machine = (key: string, urgent: boolean): IDrop => {
    const d = drop(key, null, 'dev-machine', SMART_KIND.machine);
    return {
      ...d,
      item: { ...d.item, urgent, machine: { id: 'dev-machine', metric: 'mem', waiting: [] } },
    };
  };
  const more = () => <p>What weighs on it</p>;
  const show = (drops: readonly IDrop[]) =>
    render(
      <NoticeStack
        drops={drops}
        now={NOW}
        label="Machine"
        onBack={vi.fn()}
        onDismiss={vi.fn()}
        renderAnswer={more}
      />,
    );
  beforeEach(() => {
    resetAutoOpen();
    vi.useFakeTimers();
  });

  const land = () =>
    act(() => {
      vi.advanceTimersByTime(AUTO_OPEN_DELAY_MS);
    });

  it('opens an urgent card, shows what is wrong in place, and folds it after five seconds', () => {
    const { container } = show([machine('machine:m:1', true)]);
    const card = container.querySelector('.hx-ns__card') as HTMLElement;
    expect(card.hasAttribute('data-open')).toBe(false);
    land();
    expect(card.hasAttribute('data-open')).toBe(true);
    expect(screen.getByText('What weighs on it')).toBeTruthy();
    // The details are in the card: nothing asks the smart notice to take it back.
    expect(screen.queryByRole('button', { name: 'Details' })).toBeNull();
    act(() => {
      vi.advanceTimersByTime(AUTO_OPEN_MS - 1);
    });
    expect(card.hasAttribute('data-open')).toBe(true);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(card.hasAttribute('data-open')).toBe(false);
  });

  it('does not open again within five minutes, whatever comes and goes', () => {
    const first = show([machine('machine:m:1', true)]);
    land();
    expect(first.container.querySelector('.hx-ns__card')?.hasAttribute('data-open')).toBe(true);
    act(() => {
      vi.advanceTimersByTime(AUTO_OPEN_MS);
    });
    first.unmount();
    const again = show([machine('machine:m:2', true)]);
    land();
    expect(again.container.querySelector('.hx-ns__card')?.hasAttribute('data-open')).toBe(false);
  });

  it('never opens what is not urgent', () => {
    const { container } = show([machine('machine:m:1', false)]);
    land();
    expect(container.querySelector('.hx-ns__card')?.hasAttribute('data-open')).toBe(false);
  });

  it('leaves a card the person took into their hands open', () => {
    const { container } = show([machine('machine:m:1', true)]);
    const card = container.querySelector('.hx-ns__card') as HTMLElement;
    land();
    fireEvent.pointerEnter(container.querySelector('.hx-ns') as HTMLElement);
    act(() => {
      vi.advanceTimersByTime(AUTO_OPEN_MS * 2);
    });
    expect(card.hasAttribute('data-open')).toBe(true);
  });

  it('draws what a wait asks in the card from the first, with no second «Open chat» and no repeated line', () => {
    const onOpenChat = vi.fn();
    const { container } = render(
      <NoticeStack
        drops={[drop('a', 'c1', 'Landing')]}
        now={NOW}
        label="Waiting"
        onBack={vi.fn()}
        onDismiss={vi.fn()}
        onOpenChat={onOpenChat}
        renderAnswer={() => <div data-testid="body">what is asked</div>}
        inPlace={() => true}
      />,
    );
    expect(screen.getByTestId('body')).toBeTruthy();
    // The kind of the wait is the body's to say: the row has the name and the time only.
    expect(container.querySelector('.hx-ns__text')).toBeNull();
    // No fold with its own buttons: «Answer» and a second «Open chat» are gone.
    expect(container.querySelector('.hx-ns__acts')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open chat' })).toBeNull();
    fireEvent.click(container.querySelector('.hx-ns__hit') as HTMLElement);
    expect(onOpenChat).toHaveBeenCalledWith('c1');
  });
});
