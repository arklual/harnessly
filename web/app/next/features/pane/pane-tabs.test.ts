import { describe, expect, it } from 'vitest';
import { requestOf, type TIntentCommand } from '@/next/features/pane/intents';
import { placeRequest, type IPaneTabs, type IPlaced } from '@/next/features/pane/pane-tabs';

const empty: IPaneTabs = { tabs: [], active: '', unread: [] };

const ids = () => {
  let n = 0;
  return () => `t${++n}`;
};

const place = (state: IPaneTabs, command: TIntentCommand, busy = false, nextId = ids()): IPlaced => {
  const request = requestOf(command);
  if (!request) throw new Error('no request');
  return placeRequest(state, request, { busy, cwd: '/home/me/work/', nextId });
};

describe('placeRequest: a request becomes the pane’s state', () => {
  it('a design opens in the tab of designs, which comes forward; the pane comes out', () => {
    const placed = place(empty, { kind: 'open', tab: 'design', target: 'landing' });
    expect(placed.next).toEqual({
      tabs: [{ id: 't1', kind: 'designly', design: 'landing' }],
      active: 't1',
      unread: [],
    });
    expect(placed.show).toBe(true);
    const again = place(placed.next, { kind: 'open', tab: 'design', target: 'promo' });
    expect(again.next.tabs).toEqual([{ id: 't1', kind: 'designly', design: 'promo' }]);
  });

  it('a port opens in a browser tab; the same address again focuses it instead of a second one', () => {
    const first = place(empty, { kind: 'open', tab: 'browser', target: 'localhost:4400/app' });
    const tab = first.next.tabs[0];
    expect(tab.kind === 'browser' && tab.hist[0].text).toBe('localhost:4400/app');
    const other = place(
      { ...first.next, active: 'x' },
      { kind: 'open', tab: 'browser', target: 'localhost:4400/app' },
    );
    expect(other.next.tabs).toHaveLength(1);
    expect(other.next.active).toBe('t1');
  });

  it('a browser asked for with no page comes to the new tab that is there, not a second blank one', () => {
    const blank: IPaneTabs = {
      tabs: [{ id: 'b1', kind: 'browser', hist: [], hi: -1 }],
      active: '',
      unread: [],
    };
    const placed = place(blank, { kind: 'open', tab: 'browser' });
    expect(placed.next.tabs).toHaveLength(1);
    expect(placed.next.active).toBe('b1');
  });

  it('the subagents tile opens the list: a card put in focus earlier is let go', () => {
    const was: IPaneTabs = {
      tabs: [{ id: 't1', kind: 'subagent', focus: 'agent-1' }],
      active: '',
      unread: [],
    };
    const placed = place(was, { kind: 'open', tab: 'subagents' });
    expect(placed.next).toEqual({
      tabs: [{ id: 't1', kind: 'subagent', focus: null }],
      active: 't1',
      unread: [],
    });
    expect(placed.show).toBe(true);
  });

  it('a stand opens as a site in the browser', () => {
    const placed = place(empty, {
      kind: 'open',
      tab: 'browser',
      target: 'https://stand-2.preview.example.org/admin',
    });
    const tab = placed.next.tabs[0];
    expect(tab.kind === 'browser' && tab.hist[0].target).toEqual({
      kind: 'url',
      origin: 'https://stand-2.preview.example.org',
    });
  });

  it('while the person is busy in a tab, what comes goes behind it with a dot', () => {
    const busy: IPaneTabs = { tabs: [{ id: 'mine', kind: 'plan' }], active: 'mine', unread: [] };
    const placed = place(busy, { kind: 'open', tab: 'design', target: 'landing' }, true);
    expect(placed.next.active).toBe('mine');
    expect(placed.next.unread).toEqual(['t1']);
  });

  it('a file of the chat’s folder opens in the pane’s files tab; one outside it in Legacy’s', () => {
    const inside = place(empty, { kind: 'open', tab: 'files', target: '/home/me/work/docs/plan.md' });
    expect(inside.next.tabs).toEqual([{ id: 't1', kind: 'files', path: 'docs/plan.md', file: true }]);
    const folder = place(empty, { kind: 'open', tab: 'files', target: '/home/me/work/src' });
    expect(folder.next.tabs).toEqual([{ id: 't1', kind: 'files', path: 'src', file: false }]);
    const outside = place(empty, { kind: 'open', tab: 'files', target: '/etc/hosts' });
    expect(outside.next).toBe(empty);
    expect(outside.effect).toEqual({
      k: 'legacy',
      kind: 'files',
      target: '/etc/hosts',
      background: false,
      reload: false,
    });
  });

  it('documents, boards, merge requests, tasks and changes are Legacy’s tabs, behind when busy', () => {
    for (const tab of ['artifacts', 'gitlab', 'task', 'changes'])
      expect(place(empty, { kind: 'open', tab, target: 'x' }, true).effect).toEqual({
        k: 'legacy',
        kind: tab,
        target: 'x',
        background: true,
        reload: false,
      });
  });

  it('the plan and the subagents have one tab each', () => {
    const plan = place(empty, { kind: 'open', tab: 'plan' });
    const twice = place(plan.next, { kind: 'open', tab: 'plan' });
    expect(twice.next.tabs).toHaveLength(1);
    const subs = place(twice.next, { kind: 'open', tab: 'subagents' });
    expect(subs.next.tabs.map((x) => x.kind)).toEqual(['plan', 'subagent']);
  });

  it('the terminal and the side chat are effects, not tabs of the pane', () => {
    expect(place(empty, { kind: 'open', tab: 'terminal' })).toEqual({
      next: empty,
      effect: { k: 'terminal' },
      show: false,
    });
    expect(place(empty, { kind: 'open', tab: 'side-chat' }).effect).toEqual({ k: 'side' });
  });

  it('a reload touches only a tab that is there and does not bring the pane out', () => {
    const page = place(empty, { kind: 'open', tab: 'browser', target: 'localhost:4400' }).next;
    const reloaded = place(page, { kind: 'open', tab: 'browser', target: 'localhost:4400', reload: true });
    const tab = reloaded.next.tabs[0];
    expect(tab.kind === 'browser' && tab.reload).toBe(1);
    expect(reloaded.show).toBe(false);
    expect(place(empty, { kind: 'open', tab: 'browser', target: 'localhost:5000', reload: true })).toEqual({
      next: empty,
      effect: null,
      show: false,
    });
    expect(place(empty, { kind: 'open', tab: 'artifacts', target: '/agents/docs/x', reload: true })).toEqual({
      next: empty,
      effect: { k: 'legacy', kind: 'artifacts', target: '/agents/docs/x', background: false, reload: true },
      show: false,
    });
  });

  it('show brings the pane out and changes no tab', () => {
    expect(place(empty, { kind: 'show' })).toEqual({ next: empty, effect: null, show: true });
  });
});
