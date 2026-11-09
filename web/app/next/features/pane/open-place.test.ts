import { describe, expect, it, vi } from 'vitest';
import { openPlace, type IOpenPlaceActions } from '@/next/features/pane/open-place';

const actions = (shown: string | null = 'tab-7') => {
  const a = {
    browser: vi.fn(),
    show: vi.fn(() => shown),
    reveal: vi.fn(),
    notYet: vi.fn(),
    connect: vi.fn(),
    original: vi.fn(),
  } satisfies IOpenPlaceActions;
  return a;
};

describe('openPlace', () => {
  it('opens a connection issue in the native task tab with its own connection, never in the live browser', () => {
    const a = actions();
    openPlace({ tab: 'task', target: 'conn:c-2:issue:Proj-Abc-12' }, a);
    expect(a.show).toHaveBeenCalledWith('task', 'conn:c-2:issue:Proj-Abc-12');
    expect(a.reveal).toHaveBeenCalledWith('tab-7');
    expect(a.browser).not.toHaveBeenCalled();
  });

  it('opens a wiki page and a document through native tabs too', () => {
    const a = actions();
    openPlace({ tab: 'task', target: 'conn:c-1:page:123' }, a);
    openPlace({ tab: 'artifacts', target: 'x-mfrggzdf' }, a);
    expect(a.show).toHaveBeenNthCalledWith(1, 'task', 'conn:c-1:page:123');
    expect(a.show).toHaveBeenNthCalledWith(2, 'artifacts', 'x-mfrggzdf');
    expect(a.browser).not.toHaveBeenCalled();
  });

  it('starts the browser only for a place that is a plain site', () => {
    const a = actions();
    openPlace({ tab: 'browser', target: 'https://example.com/' }, a);
    expect(a.browser).toHaveBeenCalledWith('https://example.com/');
    expect(a.show).not.toHaveBeenCalled();
  });

  it('asks to connect or offers the original link, starting nothing', () => {
    const a = actions();
    openPlace({ tab: 'link', target: 'https://wiki.example.com/p/1', reason: 'connect' }, a);
    openPlace({ tab: 'link', target: 'https://docs.example.com/d/1', reason: 'embed' }, a);
    expect(a.connect).toHaveBeenCalledTimes(1);
    expect(a.original).toHaveBeenCalledWith('https://docs.example.com/d/1');
    expect(a.browser).not.toHaveBeenCalled();
    expect(a.show).not.toHaveBeenCalled();
  });

  it('says so when the pane cannot show a tab yet', () => {
    const a = actions(null);
    openPlace({ tab: 'task', target: 'conn:c:issue:K-1' }, a);
    expect(a.notYet).toHaveBeenCalled();
    expect(a.reveal).not.toHaveBeenCalled();
  });
});
