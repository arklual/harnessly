// @vitest-environment jsdom
// What waits, in its card or row: what exactly is asked, the answer in place, and «Открыть чат» as one quiet
// link — never a second button beside the actions.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import WaitRow, { planGist, type TWaitDetail } from '@/next/features/notifications/wait-row';

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
});
afterEach(cleanup);

const show = (detail: TWaitDetail, over: Partial<Parameters<typeof WaitRow>[0]> = {}) => {
  const onOpenChat = vi.fn();
  const view = render(<WaitRow detail={detail} label="Waits" onOpenChat={onOpenChat} {...over} />);
  return { ...view, onOpenChat };
};

describe('WaitRow', () => {
  it('says the kind of the wait once and has one «Open chat», quiet', () => {
    const { container, onOpenChat } = show({ k: 'permission', command: 'npm test' }, { onDecide: vi.fn() });
    expect(screen.getAllByText('Waits')).toHaveLength(1);
    const links = screen.getAllByRole('button', { name: 'Open chat' });
    expect(links).toHaveLength(1);
    expect(links[0]?.className).toBe('hx-nc__link');
    expect(container.querySelectorAll('.hx-nc__btn')).toHaveLength(2);
    fireEvent.click(links[0] as HTMLElement);
    expect(onOpenChat).toHaveBeenCalledTimes(1);
  });

  it('allows or denies a command with the one main button first', async () => {
    const onDecide = vi.fn().mockResolvedValue(undefined);
    show({ k: 'permission', command: 'npm test' }, { onDecide });
    expect(screen.getByText('npm test')).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Allow' })));
    expect(onDecide).toHaveBeenCalledWith(true);
  });

  it('shows a plan in a few plain lines, whole on a press, and approves or revises it', async () => {
    const onPick = vi.fn().mockResolvedValue(undefined);
    const md = '# Plan the move\n\n- first **step**\n- second step\n\nMore words after.';
    const { container } = show({ k: 'plan', md, approve: 'o1', revise: 'o2' }, { onPick });
    expect(container.querySelector('.hx-nc__essence')?.textContent).toBe(
      'Plan the move\nfirst step\nsecond step\nMore words after.',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Show in full' }));
    expect(container.querySelector('.hx-nc__plan')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Collapse' })).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Approve' })));
    expect(onPick).toHaveBeenCalledWith('o1');
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Revise' })));
    expect(onPick).toHaveBeenCalledWith('o2');
  });

  it('puts the options of a menu of the agent screen on buttons', async () => {
    const onPick = vi.fn().mockResolvedValue(undefined);
    show(
      {
        k: 'choice',
        text: 'Session paused',
        options: [
          { id: 'a', label: 'Switch to Opus 4.8' },
          { id: 'b', label: 'Edit prompt' },
        ],
      },
      { onPick },
    );
    expect(screen.getByText('Session paused')).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole('button', { name: /Edit prompt/ })));
    expect(onPick).toHaveBeenCalledWith('b');
    expect(screen.queryByPlaceholderText('Your answer')).toBeNull();
  });

  it('answers a question by an option or in words', async () => {
    const onChoose = vi.fn().mockResolvedValue(undefined);
    const onText = vi.fn().mockResolvedValue(undefined);
    show({ k: 'question', text: 'Which one?', options: ['Red', 'Blue'] }, { onChoose, onText });
    await act(async () => fireEvent.click(screen.getByRole('button', { name: /Blue/ })));
    expect(onChoose).toHaveBeenCalledWith(1);
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'Green' } });
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Send' })));
    expect(onText).toHaveBeenCalledWith('Green');
  });

  it('writes back to a finished chat from a field in the card', async () => {
    const onText = vi.fn().mockResolvedValue(undefined);
    show({ k: 'reply', text: 'The answer starts here' }, { onText, label: undefined });
    expect(screen.getByText('The answer starts here')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Reply…'), { target: { value: 'Thanks, go on' } });
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Send' })));
    expect(onText).toHaveBeenCalledWith('Thanks, go on');
  });
});

describe('planGist', () => {
  it('drops the marks of the markup and keeps at most four lines', () => {
    expect(planGist('## A\n\n1. one\n2. two\n3. three\n4. four')).toBe('A\none\ntwo\nthree');
  });
});
