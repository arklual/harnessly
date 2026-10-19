import { describe, expect, it } from 'vitest';

import { activityOf, emptyStatus, reduceStatus } from './status';
import type { StatusEntry } from './types';

const entry = (over: Partial<StatusEntry> = {}): StatusEntry => ({
  cliState: 'idle',
  workspaceId: 'ws-a',
  tabName: 'Claude',
  ...over,
});

describe('reduceStatus', () => {
  it('replaces every tab on a sync', () => {
    const s1 = reduceStatus(emptyStatus, { type: 'status:sync', tabs: { 'tab-1': entry() } });
    const s2 = reduceStatus(s1, { type: 'status:sync', tabs: { 'tab-2': entry({ cliState: 'busy' }) } });
    expect(Object.keys(s2.tabs)).toEqual(['tab-2']);
  });

  it('takes an update as the whole entry', () => {
    const s1 = reduceStatus(emptyStatus, {
      type: 'status:sync',
      tabs: { 'tab-1': entry({ agentSummary: 'x' }) },
    });
    const s2 = reduceStatus(s1, { type: 'status:update', tabId: 'tab-1', ...entry({ cliState: 'busy' }) });
    expect(s2.tabs['tab-1']).toEqual(entry({ cliState: 'busy' }));
  });

  it('drops a removed tab', () => {
    const s1 = reduceStatus(emptyStatus, { type: 'status:sync', tabs: { 'tab-1': entry() } });
    const s2 = reduceStatus(s1, {
      type: 'status:update',
      tabId: 'tab-1',
      cliState: null,
      workspaceId: '',
      tabName: '',
    });
    expect(s2.tabs).toEqual({});
  });

  it('notes the last hook event', () => {
    const s1 = reduceStatus(emptyStatus, { type: 'status:sync', tabs: { 'tab-1': entry() } });
    const s2 = reduceStatus(s1, {
      type: 'status:hook-event',
      tabId: 'tab-1',
      event: { name: 'stop', at: 5, seq: 3 },
    });
    expect(s2.tabs['tab-1']?.lastEvent).toEqual({ name: 'stop', at: 5, seq: 3 });
  });

  it('keeps the rate limits and ignores unknown messages', () => {
    const s1 = reduceStatus(emptyStatus, { type: 'rate-limits:update', data: { claude: null } });
    expect(s1.rateLimits).toEqual({ claude: null });
    expect(reduceStatus(s1, { type: 'background:sync', tabs: {} })).toBe(s1);
  });
});

describe('activityOf', () => {
  it('reads the agent state', () => {
    expect(activityOf(undefined)).toBe('offline');
    expect(activityOf(entry({ cliState: 'busy' }))).toBe('working');
    expect(activityOf(entry({ cliState: 'ready-for-review' }))).toBe('done');
    expect(activityOf(entry({ cliState: 'idle', launchingSince: 1 }))).toBe('starting');
    expect(activityOf(entry({ cliState: 'needs-input' }))).toBe('waiting');
  });
});
