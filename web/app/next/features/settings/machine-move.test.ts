import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  moveBrief,
  moveCodes,
  moveDone,
  moveLine,
  moveStopped,
  readMove,
} from '@/next/features/settings/machine-move';

beforeAll(() => pinLocale('en'));

const stopped = {
  migration: '0001-data-to-hub',
  total: 420,
  verified: 412,
  pending: 0,
  failed: 8,
  skipped: 0,
  doc: 'docs/migrations/0001-data-to-hub.md',
  hub: 'on',
  running: false,
  failures: [
    {
      kind: 'uploads',
      object: 'up_2',
      state: 'failed',
      code: 'MIG-READ',
      error: 'permission denied',
      attempts: 2,
    },
    { kind: 'files', object: 'ab12', state: 'failed', code: 'MIG-HASH', error: 'changed', attempts: 1 },
    {
      kind: 'uploads',
      object: 'up_3',
      state: 'failed',
      code: 'MIG-READ',
      error: 'permission denied',
      attempts: 1,
    },
  ],
};

describe('the move of a machine’s data into the hub (HRN-883)', () => {
  it('reads the runtime’s answer, and none when the machine moves nothing', () => {
    const m = readMove(stopped);
    expect(m?.failed).toBe(8);
    expect(m?.failures).toHaveLength(3);
    expect(readMove({ off: 'no move on this machine' })).toBeNull();
    expect(readMove('nonsense')).toBeNull();
  });

  it('says how far it is, that it stopped, and nothing once it is over', () => {
    const m = readMove(stopped)!;
    expect(moveStopped(m)).toBe(true);
    expect(moveLine(m, 'vm-1')).toContain('vm-1');
    expect(moveCodes(m)).toBe('MIG-READ ×2, MIG-HASH ×1');
    const moving = { ...m, failed: 0, failures: [], pending: 8 };
    expect(moveStopped(moving)).toBe(false);
    expect(moveLine(moving, 'vm-1')).toContain('412');
    const done = { ...moving, pending: 0, verified: 420 };
    expect(moveDone(done)).toBe(true);
    expect(moveLine(done, 'vm-1')).toBeNull();
    expect(moveStopped({ ...m, retrying: 8 })).toBe(false);
  });

  it('briefs the agent with the objects, the commands and the page of the move', () => {
    const brief = moveBrief(readMove(stopped)!, 'vm-1');
    expect(brief).toContain('uploads/up_2: MIG-READ permission denied');
    expect(brief).toContain('harnessly migrate status --json');
    expect(brief).toContain('self-host/0001-data-to-hub');
    expect(brief).toContain('docs/migrations/0001-data-to-hub.md');
  });
});
