// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/platform/config', () => ({
  appConfig: () => ({ taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}' }),
}));

import Markdown from '@/next/features/chat/markdown';
import { RefContext } from '@/next/features/chat/ref-chip';

afterEach(cleanup);

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
