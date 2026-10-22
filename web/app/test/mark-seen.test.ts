import { beforeEach, describe, expect, it } from 'vitest';
import useTabStore, { selectGlobalStatus, shouldMarkSeen } from '@/hooks/use-tab-store';
import type { IStatusEntry } from '@/types/status';

const entry = (cliState: string) =>
  ({ cliState, workspaceId: 'ws-1', tabName: 'a' }) as unknown as IStatusEntry;

describe('a finished answer the person looked at', () => {
  beforeEach(() => useTabStore.getState().replaceAll({}));

  it('is seen only while its conversation is on screen', () => {
    expect(shouldMarkSeen('ready-for-review', true, true)).toBe(true);
    expect(shouldMarkSeen('ready-for-review', false, true)).toBe(false);
    expect(shouldMarkSeen('ready-for-review', true, false)).toBe(false);
    expect(shouldMarkSeen('busy', true, true)).toBe(false);
    expect(shouldMarkSeen('needs-input', true, true)).toBe(false);
    expect(shouldMarkSeen(undefined, true, true)).toBe(false);
  });

  it('turns the tab idle and dismissed in this page, so the eye goes out without an echo', () => {
    useTabStore.getState().upsert('t1', entry('ready-for-review'));
    expect(selectGlobalStatus(useTabStore.getState().tabs).attentionCount).toBe(1);
    useTabStore.getState().markSeen('t1', 1_000);
    const tab = useTabStore.getState().tabs.t1;
    expect(tab.cliState).toBe('idle');
    expect(tab.dismissedAt).toBe(1_000);
    expect(selectGlobalStatus(useTabStore.getState().tabs).attentionCount).toBe(0);
  });

  it('leaves a tab that is not waiting to be read as it is', () => {
    useTabStore.getState().upsert('t1', entry('busy'));
    const before = useTabStore.getState().tabs;
    useTabStore.getState().markSeen('t1', 1_000);
    useTabStore.getState().markSeen('missing', 1_000);
    expect(useTabStore.getState().tabs).toBe(before);
  });
});
