import { afterEach, describe, expect, it, vi } from 'vitest';
import { forgetSpaceDirs, spaceDirOf } from './space-dir';
import type { MachineId } from '@/next/protocol/v2';

const json = vi.hoisted(() => ({ runtimeJson: vi.fn() }));
vi.mock('@/next/core/net/runtime-json', () => json);

const m = 'm1' as MachineId;

describe('spaceDirOf', () => {
  afterEach(() => {
    forgetSpaceDirs();
    json.runtimeJson.mockReset();
  });
  it("is the machine's own folder, asked once", async () => {
    json.runtimeJson.mockResolvedValue({ dir: '/home/p/harnessly-spaces' });
    expect(await spaceDirOf(m)).toBe('/home/p/harnessly-spaces');
    expect(await spaceDirOf(m)).toBe('/home/p/harnessly-spaces');
    expect(json.runtimeJson).toHaveBeenCalledTimes(1);
  });
  it('is none on a machine without the route, and the chat starts as before', async () => {
    json.runtimeJson.mockRejectedValue(new Error('404'));
    expect(await spaceDirOf(m)).toBeNull();
  });
  it('takes only an absolute folder', async () => {
    json.runtimeJson.mockResolvedValue({ dir: 'relative' });
    expect(await spaceDirOf(m)).toBeNull();
  });
});
