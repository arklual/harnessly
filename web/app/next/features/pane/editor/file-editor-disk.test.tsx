// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { FileContent } from '@/next/protocol/v2';
import FileEditor from './file-editor';

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const disk = (content: string, revision: string) =>
  ({ path: 'a.md', content, revision }) as unknown as FileContent;

const open = (text: string) => {
  const props = {
    path: '/work/a.md',
    initial: { text, base: text, revision: '1' },
    onSave: vi.fn(),
    onReload: vi.fn(),
    onLeave: vi.fn(),
    onState: vi.fn(),
    onHandle: vi.fn(),
  };
  return props;
};

// the agent writes the open file, the pane follows without a reload.
describe('the editor follows the disk', () => {
  it('takes the disk text of an unedited file in place', async () => {
    const props = open('one\ntwo\nthree');
    const view = render(<FileEditor {...props} />);
    expect(view.container.textContent).toContain('two');
    view.rerender(<FileEditor {...props} disk={disk('one\nTWO\nthree\nfour', '2')} />);
    await waitFor(() => expect(view.container.textContent).toContain('TWO'));
    expect(view.container.textContent).toContain('four');
    expect(screen.queryByRole('alert')).toBeNull();
    // The text is the disk's now, not an edit: the editor is not dirty.
    expect(props.onState).toHaveBeenLastCalledWith({ dirty: false, saving: false });
  });

  it('ignores the revision it already has', async () => {
    const props = open('one');
    const view = render(<FileEditor {...props} />);
    view.rerender(<FileEditor {...props} disk={disk('other', '1')} />);
    expect(view.container.textContent).toContain('one');
    expect(view.container.textContent).not.toContain('other');
  });
});
