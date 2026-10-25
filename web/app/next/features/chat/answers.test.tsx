// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Answers } from '@/next/features/chat/steps';
import type { AnswersEntry } from '@/next/features/chat/types';

afterEach(cleanup);

const entry = (pairs: AnswersEntry['pairs']) =>
  ({ id: 'a1', t: 'answers', questionId: 'q1', pairs }) as unknown as AnswersEntry;

describe('Answers', () => {
  it('stacks each question over its answer', () => {
    const { container } = render(
      <Answers
        entry={entry([
          { q: 'Какой текст?', a: 'Про задачи' },
          { q: 'Устройство', a: 'Телефон' },
        ])}
      />,
    );
    const pairs = container.querySelectorAll('.hx-qa__pair');
    expect(pairs).toHaveLength(2);
    expect(pairs[0].querySelector('.hx-qa__q')?.textContent).toBe('Какой текст?');
    expect(pairs[0].querySelector('.hx-qa__a')?.textContent).toBe('Про задачи');
  });

  it('shows a multi-choice answer as one row per choice', () => {
    const { container } = render(<Answers entry={entry([{ q: 'Отказ', a: 'Причину\nСумму\n' }])} />);
    expect(container.querySelectorAll('.hx-qa__a')).toHaveLength(2);
  });

  it('names the device the answer came from', () => {
    render(<Answers entry={entry([{ q: 'Q', a: 'A' }])} via="MacBook" />);
    expect(screen.getByText(/MacBook/)).toBeTruthy();
  });
});
