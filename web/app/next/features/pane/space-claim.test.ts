import { describe, expect, it, vi } from 'vitest';
import { claimedBySpace, claimSpaceOpens } from '@/next/features/pane/space-claim';

describe('claimSpaceOpens: the space on screen opens what the agent asks', () => {
  it('asks the claim of that chat only', () => {
    const claim = vi.fn((href: string) => href.includes('/b/'));
    const off = claimSpaceOpens('tab-1', claim);
    expect(claimedBySpace('tab-1', 'https://boards.example.test/b/plan')).toBe(true);
    expect(claimedBySpace('tab-1', 'https://example.test/x')).toBe(false);
    expect(claimedBySpace('tab-2', 'https://boards.example.test/b/plan')).toBe(false);
    expect(claimedBySpace('tab-1', undefined)).toBe(false);
    off();
    expect(claimedBySpace('tab-1', 'https://boards.example.test/b/plan')).toBe(false);
  });
  it('an older page letting go keeps the newer claim', () => {
    const offOld = claimSpaceOpens('tab-3', () => true);
    claimSpaceOpens('tab-3', () => false);
    offOld();
    expect(claimedBySpace('tab-3', 'https://boards.example.test/b/plan')).toBe(false);
  });
});
