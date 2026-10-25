// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import RefChip, { RefContext } from '@/next/features/chat/ref-chip';
import { changeLinkLabel, splitRefs } from '@/next/features/chat/refs';

afterEach(cleanup);

describe('a merge request', () => {
  it('is a chip only from its link, which opens it', () => {
    const onOpen = vi.fn();
    const href = 'https://gitlab.example.com/team/harnessly/-/merge_requests/80';
    render(
      <RefContext.Provider value={{ refs: {}, onOpen }}>
        <RefChip kind="mr" label={href} href={href} text="harnessly !80" />
      </RefContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'harnessly !80' }));
    expect(onOpen).toHaveBeenCalledWith('mr', href, href);
  });

  it('written bare is a word: the page does not guess its project', () => {
    expect(splitRefs('see harnessly !80 there')).toEqual([{ text: 'see harnessly !80 there' }]);
  });

  it('names its chip by the project and the number, a commit by its short hash', () => {
    expect(changeLinkLabel('https://gitlab.example.com/team/harnessly/-/merge_requests/80')).toBe(
      'harnessly !80',
    );
    expect(changeLinkLabel('https://github.com/ly-solutions/harnessly/pull/12')).toBe('harnessly #12');
    expect(changeLinkLabel('https://gitlab.example.com/team/harnessly/-/commit/1a2b3c4d5e6f')).toBe(
      'harnessly 1a2b3c4',
    );
    expect(changeLinkLabel('https://docs.example.com/a/plan')).toBeNull();
  });
});
