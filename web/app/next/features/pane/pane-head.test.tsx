// @vitest-environment jsdom
// The head row of a pane tab: the view's own controls go into the row over it, not into a bar of their own; the crumbs
// lead to the folders, the last one names the thing itself.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import PaneHead, { Crumbs, HeadTools } from '@/next/features/pane/pane-head';
import { crumbsOf } from '@/next/features/pane/crumbs';

afterEach(cleanup);

describe('PaneHead', () => {
  it('takes the controls of the view under it into its row', () => {
    render(
      <PaneHead lead="where" tools={<button type="button">Share</button>}>
        <HeadTools bar="own-bar">
          <button type="button">Edit</button>
        </HeadTools>
      </PaneHead>,
    );
    const row = document.querySelector('.hx-phead');
    expect(row?.contains(screen.getByText('Edit'))).toBe(true);
    // The view's controls come first, then the place's.
    const order = [...(row?.querySelectorAll('button') ?? [])].map((b) => b.textContent);
    expect(order).toEqual(['Edit', 'Share']);
    expect(document.querySelector('.own-bar')).toBeNull();
  });

  it('leaves a view shown without a head row its bar of its own', () => {
    render(
      <HeadTools bar="own-bar">
        <button type="button">Edit</button>
      </HeadTools>,
    );
    expect(document.querySelector('.own-bar')?.contains(screen.getByText('Edit'))).toBe(true);
  });
});

describe('Crumbs', () => {
  it('leads to each folder, and the last step is the file itself', () => {
    const onPick = vi.fn();
    render(<Crumbs items={crumbsOf('/home/me/p', 'docs/a/kit.md', undefined, true)} onPick={onPick} />);
    const steps = [...document.querySelectorAll('.hx-disk__crumb')];
    expect(steps.map((c) => c.textContent)).toEqual(['p', 'docs', 'a', 'kit.md']);
    expect(steps.map((c) => c.getAttribute('data-step'))).toEqual(['first', 'middle', 'parent', 'last']);
    fireEvent.click(screen.getByText('docs'));
    expect(onPick).toHaveBeenLastCalledWith('docs');
    // The file is where it is already: its step is no button.
    expect(screen.getByText('kit.md').tagName).toBe('SPAN');
    expect(screen.getByText('kit.md').getAttribute('aria-current')).toBe('true');
  });
});
