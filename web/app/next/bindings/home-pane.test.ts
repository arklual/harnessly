// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import {
  HOME_GROUP,
  HOME_TOOL,
  contextOf,
  homeTools,
  paneMachine,
  withAddress,
  withChange,
  withMachineTool,
  withoutTab,
  withReview,
  withService,
  withShell,
} from '@/next/bindings/home-pane';
import { parseAddress } from '@/next/features/pane/address';
import { parseHomePane, serializeHomePane } from '@/next/stores/home-pane-state';
import type { IMachineChoice } from '@/next/features/sidebar/types';
import { changeLinkOf } from '@/next/features/review/gitlab-pick';
import { EMPTY_HOME_PANE, type THomeTab } from '@/next/stores/home-pane-state';
import type { IPicked } from '@/next/features/review/review-picker';

const link = changeLinkOf('https://gitlab.example.com/team/app/-/merge_requests/5');
if (!link) throw new Error('link');
const PICKED: IPicked = { link, title: 'app !5', name: 'Queue', source: 'feat', target: 'main' };

describe('the start page pane', () => {
  it('opens one review picker and brings it forward again', () => {
    const one = withReview(EMPTY_HOME_PANE, 'r1');
    expect(one).toMatchObject({ open: true, active: 'r1', tabs: [{ id: 'r1', kind: 'review' }] });
    const back = withReview({ ...one, active: '' }, 'r2');
    expect(back.tabs).toHaveLength(1);
    expect(back.active).toBe('r1');
  });

  it('opens a change once, in front', () => {
    const one = withChange(withReview(EMPTY_HOME_PANE, 'r'), PICKED, 'c1');
    expect(one.active).toBe('c1');
    expect(one.tabs[1]).toMatchObject({ kind: 'change', url: link.url, title: 'app !5', source: 'feat' });
    const again = withChange({ ...one, active: 'r' }, PICKED, 'c2');
    expect(again.tabs).toHaveLength(2);
    expect(again.active).toBe('c1');
  });

  it('closing the tab in front brings its neighbour, then the tools', () => {
    const pane = withChange(withReview(EMPTY_HOME_PANE, 'r'), PICKED, 'c');
    const left = withoutTab(pane, 'c');
    expect(left.active).toBe('r');
    expect(withoutTab(left, 'r').active).toBe('');
    expect(withoutTab(pane, 'nope')).toBe(pane);
    expect(withoutTab(pane, 'r').active).toBe('c');
  });

  it('tells the agent what the tab knows', () => {
    const tab = withChange(EMPTY_HOME_PANE, PICKED, 'c').tabs[0] as Extract<THomeTab, { kind: 'change' }>;
    expect(contextOf(tab)).toEqual({ link, title: 'Queue', source: 'feat', target: 'main' });
    expect(contextOf({ ...tab, url: 'https://example.com/x' })).toBeNull();
  });

  it('offers every tool that needs no chat, grouped as a chat pane does', () => {
    const tools = homeTools({ machineName: 'dev', shells: true, machineTool: true });
    const byGroup = (group: string) => tools.filter((x) => x.group === group).map((x) => x.id);
    expect(byGroup(HOME_GROUP.vm)).toEqual([
      HOME_TOOL.shell,
      HOME_TOOL.browser,
      HOME_TOOL.files,
      HOME_TOOL.machine,
    ]);
    expect(byGroup(HOME_GROUP.links)).toEqual([HOME_TOOL.gitlab, HOME_TOOL.vault, HOME_TOOL.designly]);
    expect(byGroup(HOME_GROUP.review)).toEqual([HOME_TOOL.review]);
    // What only a chat has is not a tile here.
    for (const chatOnly of [
      'plan',
      'side',
      'subagents',
      'L:changes',
      'L:peers',
      'L:economics',
      'agent-browser',
    ])
      expect(tools.map((x) => x.id)).not.toContain(chatOnly);
  });

  it("leaves out what the machine does not have, and the machine's tools while there is none", () => {
    const lean = homeTools({ machineName: 'dev', shells: false, machineTool: false });
    expect(lean.filter((x) => x.group === HOME_GROUP.vm).map((x) => x.id)).toEqual([
      HOME_TOOL.browser,
      HOME_TOOL.files,
    ]);
    const none = homeTools({ machineName: null, shells: true, machineTool: true });
    expect(none.some((x) => x.group === HOME_GROUP.vm)).toBe(false);
    expect(none.map((x) => x.id)).toContain(HOME_TOOL.review);
  });

  it("opens the machine's tools on their machine, once each", () => {
    const files = withMachineTool(EMPTY_HOME_PANE, 'm1', 'files', 'f1');
    expect(files.tabs[0]).toEqual({ id: 'f1', kind: 'files', machine: 'm1', path: '', file: false });
    expect(withMachineTool({ ...files, active: '' }, 'm1', 'files', 'f2').active).toBe('f1');
    // Another machine has its own.
    expect(withMachineTool(files, 'm2', 'files', 'f3').tabs).toHaveLength(2);
    const browser = withMachineTool(EMPTY_HOME_PANE, 'm1', 'browser', 'b1');
    expect(browser.tabs[0]).toMatchObject({ kind: 'browser', hist: [], hi: -1, machine: 'm1' });
    const address = parseAddress('localhost:5173');
    if (!address) throw new Error('address');
    const page = withAddress(browser, 'm1', address, 'b2');
    expect(page.tabs[1]).toMatchObject({ kind: 'browser', hi: 0, machine: 'm1' });
    expect(withAddress({ ...page, active: '' }, 'm1', address, 'b3').active).toBe('b2');
    const shells = withShell(withShell(EMPTY_HOME_PANE, 'm1', 't1', 's1'), 'm1', 't2', 's2');
    expect(shells.tabs.map((x) => x.kind)).toEqual(['shell', 'shell']);
    expect(withService(withService(EMPTY_HOME_PANE, 'vault', 'v1'), 'vault', 'v2').tabs).toHaveLength(1);
  });

  it("keeps the machine's tabs and the picked machine through a reload", () => {
    const pane = withShell(
      withMachineTool({ ...EMPTY_HOME_PANE, machine: 'm2' }, 'm2', 'machine', 'x'),
      'm2',
      't',
      's',
    );
    const back = parseHomePane(serializeHomePane(pane));
    expect(back.machine).toBe('m2');
    expect(back.tabs).toEqual(pane.tabs);
    // A machine's tab without its machine is not read.
    const broken = JSON.stringify({
      v: 1,
      pane: { tabs: [{ id: 'a', kind: 'shell', terminal: 't' }], active: 'a' },
    });
    expect(parseHomePane(broken).tabs).toEqual([]);
  });

  it("opens the tools on the picked machine while it is the person's", () => {
    const choices: IMachineChoice[] = [
      { id: 'm1', name: 'one', health: 'ok', note: '', off: false },
      { id: 'm2', name: 'two', health: 'ok', note: '', off: false },
    ];
    expect(paneMachine('m2', choices, 'm1')).toBe('m2');
    expect(paneMachine('gone', choices, 'm1')).toBe('m1');
    expect(paneMachine(null, choices, null)).toBeNull();
  });
});
