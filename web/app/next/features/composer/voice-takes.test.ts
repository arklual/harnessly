import { beforeEach, describe, expect, it, vi } from 'vitest';
import { putCloudTake } from '@/next/features/composer/voice-cloud';
import { failTake, recordTake, startTake, syncTakes, takesStore } from '@/next/features/composer/voice-takes';
import { meStore } from '@/next/stores/me';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/features/composer/voice-cloud', () => ({
  putCloudTake: vi.fn(),
  patchCloudTake: vi.fn(async () => true),
  deleteCloudTake: vi.fn(async () => true),
}));

const put = vi.mocked(putCloudTake);
const settled = () => new Promise((resolve) => setTimeout(resolve));

describe('a take on its way to the cluster', () => {
  beforeEach(() => {
    put.mockReset();
    meStore.set({ k: 'ready', me: { email: 'a@test' } } as never);
  });

  it('goes again when an upload failed, without a reload of the page', async () => {
    const take = startTake('m' as MachineId, 'audio/webm');
    recordTake(take.id, [new Blob(['speech'], { type: 'audio/webm' })]);
    put.mockResolvedValueOnce(false);
    failTake(take.id);
    await settled();
    expect(put).toHaveBeenCalledTimes(1);
    expect(takesStore.get(take.id)?.cloud).toBeUndefined();

    put.mockResolvedValueOnce(true);
    syncTakes();
    await settled();
    expect(put).toHaveBeenCalledTimes(2);
    expect(takesStore.get(take.id)?.cloud).toBe(true);

    syncTakes();
    await settled();
    expect(put).toHaveBeenCalledTimes(2);
  });
});
