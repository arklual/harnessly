import { describe, expect, it } from 'vitest';
import { reasonOf } from '@/next/features/app-main/apps-list';

describe('the reason an app does nothing (HRN-693)', () => {
  it('says what it waits for, and nothing for a ready or turned-off app', () => {
    expect(reasonOf({ state: 'ready' })).toBeNull();
    expect(reasonOf({ state: 'disabled', reason: 'off' })).toBeNull();
    expect(reasonOf({ state: 'waiting', reason: 'secret', detail: 'api-key' })).toContain('api-key');
    expect(reasonOf({ state: 'waiting', reason: 'host', detail: '>=0.18' })).toContain('>=0.18');
    expect(reasonOf({ state: 'failed', reason: 'package' })).not.toBeNull();
  });
});
