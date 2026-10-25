// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const mermaidMock = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(async (_id: string, source: string) => {
    if (source.includes('BROKEN')) throw new Error('parse error');
    return { svg: '<svg data-testid="drawn"><title>diagram</title></svg>' };
  }),
}));
vi.mock('mermaid', () => ({ default: mermaidMock }));
vi.mock('@/platform/config', () => ({
  appConfig: () => ({
    taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}',
    artifactsUrl: 'https://docs.example.com',
    boardsUrl: 'https://boards.example.com',
    readerUrl: null,
  }),
}));

import Markdown from '@/next/features/chat/markdown';
import { RefContext } from '@/next/features/chat/ref-chip';

afterEach(cleanup);

describe('a mermaid fence', () => {
  it('is drawn as a diagram', async () => {
    const { container } = render(<Markdown md={'```mermaid\nflowchart LR\n  A --> B\n```'} />);
    await waitFor(() => expect(container.querySelector('.hx-rich__diagram svg')).not.toBeNull());
    expect(mermaidMock.initialize).toHaveBeenCalledWith(expect.objectContaining({ securityLevel: 'strict' }));
  });

  it('stays the code block when the renderer cannot draw it', async () => {
    const { container } = render(<Markdown md={'```mermaid\nBROKEN\n```'} />);
    await waitFor(() => expect(container.querySelector('.hx-code')).not.toBeNull());
    expect(container.querySelector('.hx-rich')).toBeNull();
  });

  it('opens full size', async () => {
    const { container } = render(<Markdown md={'```mermaid\nflowchart LR\n  A --> B\n```'} />);
    await waitFor(() => expect(container.querySelector('.hx-rich__diagram svg')).not.toBeNull());
    fireEvent.click(container.querySelector('.hx-rich__diagram') as Element);
    expect(await screen.findByRole('dialog')).toBeTruthy();
  });
});

describe('an html fence', () => {
  const md = '```html\n<p>hello</p>\n<script>parent.document.cookie</script>\n```';

  it('is a frame that is sandboxed away from the page and holds the source', () => {
    const { container } = render(<Markdown md={md} />);
    const frame = container.querySelector('iframe') as HTMLIFrameElement;
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin');
    expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer');
    expect(frame.getAttribute('srcdoc')).toContain("default-src 'none'");
    expect(frame.getAttribute('srcdoc')).toContain('<p>hello</p>');
    expect(frame.getAttribute('src')).toBeNull();
  });

  it('toggles between the page and its code', () => {
    const { container } = render(<Markdown md={md} />);
    expect(container.querySelector('.hx-rich__code')).toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: /Code|Код/ }));
    expect(container.querySelector('iframe')).toBeNull();
    expect(container.querySelector('.hx-rich__code')?.textContent).toContain('<p>hello</p>');
  });
});

describe('a link to our own page', () => {
  it('alone in a paragraph is a card that opens in the pane', () => {
    const onOpen = vi.fn();
    render(
      <RefContext.Provider value={{ refs: {}, onOpen }}>
        <Markdown md="[Payments flow](https://boards.example.com/b/abc)" />
      </RefContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: /Payments flow/ }));
    expect(onOpen).toHaveBeenCalledWith(
      'link',
      'https://boards.example.com/b/abc',
      'https://boards.example.com/b/abc',
    );
  });

  it('inside a sentence, or to another host, stays a chip', () => {
    const { container } = render(
      <RefContext.Provider value={{ refs: {}, onOpen: vi.fn() }}>
        <Markdown
          md={'See [it](https://docs.example.com/p/x) now.\n\n[other](https://elsewhere.example.org/x)'}
        />
      </RefContext.Provider>,
    );
    expect(container.querySelector('.hx-linkcard')).toBeNull();
    expect(container.querySelectorAll('.hx-ref')).toHaveLength(2);
  });
});
