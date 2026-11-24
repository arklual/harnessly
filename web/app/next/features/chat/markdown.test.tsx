// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/platform/config', () => ({
  appConfig: () => ({ taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}' }),
}));

import Markdown from '@/next/features/chat/markdown';
import { RefContext } from '@/next/features/chat/ref-chip';

afterEach(cleanup);

it('opens an exact issue or numeric-project MR without turning commands or bare IIDs into chips', () => {
  const onOpen = vi.fn();
  render(
    <RefContext.Provider value={{ refs: {}, onOpen }}>
      <Markdown md={'See `ABC-10` and `123!4`, not `!4` or `echo ABC-10`.'} />
    </RefContext.Provider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'ABC-10' }));
  expect(onOpen).toHaveBeenCalledWith('task', 'ABC-10', null);
  fireEvent.click(screen.getByRole('button', { name: '123!4' }));
  expect(onOpen).toHaveBeenCalledWith('mr', '123!4', null);
  expect(screen.getByText('!4').tagName).toBe('CODE');
  expect(screen.getByText('echo ABC-10').tagName).toBe('CODE');
});
describe('a path in inline code', () => {
  const docs = new Set(['/home/dev/app/docs/plan.md']);

  it('to a Markdown file the conversation touched opens that file', () => {
    const onOpen = vi.fn();
    render(
      <RefContext.Provider value={{ refs: {}, onOpen, docs }}>
        <Markdown md="The plan is in `docs/plan.md`." />
      </RefContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'docs/plan.md' }));
    expect(onOpen).toHaveBeenCalledWith('file', '/home/dev/app/docs/plan.md', null);
  });

  it('to a file nobody touched stays code', () => {
    render(
      <RefContext.Provider value={{ refs: {}, onOpen: vi.fn(), docs }}>
        <Markdown md="See `docs/other.md`." />
      </RefContext.Provider>,
    );
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByText('docs/other.md').tagName).toBe('CODE');
  });
});

// the copy button of a code block keeps its word on one line, and a narrow block keeps its name for the icon.
describe('a code block', () => {
  it('names its copy button and keeps its word apart from the icon', () => {
    const { container } = render(<Markdown md={'```\n! cd ~/projects\n```'} />);
    const button = container.querySelector('.hx-code__copy');
    const word = button?.querySelector('.hx-code__copy-text')?.textContent;
    expect(word).toBeTruthy();
    expect(button?.getAttribute('aria-label')).toBe(word);
    expect(container.querySelector('.hx-code__lang')).not.toBeNull();
  });
});
