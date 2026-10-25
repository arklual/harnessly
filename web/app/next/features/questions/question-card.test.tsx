// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import QuestionCard from '@/next/features/questions/question-card';
import type { Question } from '@/next/features/questions/types';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const question: Question = {
  id: 'q1',
  turnId: 't1',
  askedAt: 0,
  state: { k: 'open' },
  items: [
    {
      header: 'Layout',
      question: 'How to show the steps?',
      multi: false,
      options: [
        { label: 'Step by step', description: null, preview: null },
        { label: 'One screen', description: null, preview: null },
      ],
    },
    {
      header: 'Refusal',
      question: 'What to show on a refusal?',
      multi: true,
      options: [
        { label: 'Bank reason', description: null, preview: null },
        { label: 'Other card', description: null, preview: null },
      ],
    },
  ],
};

const activePane = () => document.querySelector('.hx-ask__pane[data-active="true"] .hx-ask__q')?.textContent;

describe('QuestionCard', () => {
  it('moves on after a single choice, takes a digit key, and keeps the choices when sending fails', async () => {
    let reject!: (error: Error) => void;
    const onAnswer = vi.fn(() => new Promise((_, fail) => (reject = fail)));
    render(<QuestionCard question={question} device="d1" ownAnswer="" onAnswer={onAnswer} />);
    expect(activePane()).toBe('How to show the steps?');

    fireEvent.click(screen.getByText('Step by step'));
    act(() => void vi.advanceTimersByTime(260));
    expect(activePane()).toBe('What to show on a refusal?');

    fireEvent.keyDown(document.body, { key: '2' });
    fireEvent.click(screen.getByText('Send answers'));
    expect(onAnswer).toHaveBeenCalledWith('q1', [
      { i: 0, choices: [0] },
      { i: 1, choices: [1] },
    ]);
    expect(document.querySelector('.hx-ask')?.getAttribute('data-sending')).toBe('true');

    await act(async () => reject(new Error('down')));
    expect(screen.getByText('The answer did not reach the agent')).toBeTruthy();
    expect(screen.getByText('Other card').closest('button')?.getAttribute('aria-pressed')).toBe('true');
  });

  it('leaves with a closing phase once the question is answered', () => {
    const { rerender } = render(
      <QuestionCard question={question} device="d1" ownAnswer="" onAnswer={vi.fn()} />,
    );
    rerender(
      <QuestionCard
        question={{ ...question, state: { k: 'answered', at: 1, byDevice: 'd1' } }}
        device="d1"
        ownAnswer=""
        onAnswer={vi.fn()}
      />,
    );
    expect(document.querySelector('.hx-ask')?.getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(260));
    expect(document.querySelector('.hx-ask')).toBeNull();
  });
});
