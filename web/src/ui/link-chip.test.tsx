// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { none } from '../api/integrations';
import { integrationsStore, sidePaneStore } from '../stores/side-pane';
import { MarkdownLink, TextWithChips } from './link-chip';

const services = {
  boards: { url: 'https://boards.example.com' },
  artifacts: { url: 'https://docs.example.com' },
};
const board = 'https://boards.example.com/b/checkout-flow-k3j2h1g0f9e8?k=abcdefghijklmnopqrstuv';

afterEach(() => {
  cleanup();
  integrationsStore.set(none);
  sidePaneStore.set({ open: false, tab: 'board', board: null, document: null, reload: 0 });
});

describe('link chips', () => {
  it('turns links in a message into chips that open in the pane', () => {
    integrationsStore.set(services);
    render(
      <TextWithChips
        text={`see ${board}, and https://docs.example.com/a/plan-4k2m9x. Also https://example.org/x`}
      />,
    );
    const chip = screen.getByTestId('board-chip');
    expect(chip.textContent).toBe('Checkout flow');
    expect(screen.getByTestId('document-chip').textContent).toBe('Plan');
    expect(document.body.textContent).toContain('Also https://example.org/x');
    fireEvent.click(chip);
    expect(sidePaneStore.get()).toMatchObject({
      open: true,
      tab: 'board',
      board: { slug: 'checkout-flow-k3j2h1g0f9e8' },
    });
  });

  it('leaves text alone without services', () => {
    render(<TextWithChips text={`see ${board}`} />);
    expect(screen.queryByTestId('board-chip')).toBeNull();
    expect(document.body.textContent).toBe(`see ${board}`);
  });

  it('names a markdown chip after its link text, and opens other links in a new tab', () => {
    integrationsStore.set(services);
    render(
      <p>
        <MarkdownLink href={board}>the flow</MarkdownLink>
        <MarkdownLink href="https://example.org/">elsewhere</MarkdownLink>
      </p>,
    );
    expect(screen.getByTestId('board-chip').textContent).toBe('the flow');
    const a = screen.getByRole('link', { name: 'elsewhere' });
    expect(a.getAttribute('target')).toBe('_blank');
    expect(a.getAttribute('rel')).toBe('noopener noreferrer');
  });
});
