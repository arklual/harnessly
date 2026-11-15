import { describe, expect, it, vi } from 'vitest';
import { flashQuickEdit, watchQuickEdits } from './quick-edit-flash';

describe('quick edit flash', () => {
  it('reaches the views of that app only, until they stop', () => {
    const boards = vi.fn();
    const docs = vi.fn();
    const stop = watchQuickEdits('x.boards', boards);
    watchQuickEdits('x.docs', docs);
    const flash = { object: 'b1', ids: ['a'], hint: 'line' };
    flashQuickEdit('x.boards', flash);
    expect(boards).toHaveBeenCalledWith(flash);
    expect(docs).not.toHaveBeenCalled();
    stop();
    flashQuickEdit('x.boards', flash);
    expect(boards).toHaveBeenCalledTimes(1);
  });
});
