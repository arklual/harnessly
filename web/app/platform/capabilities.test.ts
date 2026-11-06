import { describe, expect, it } from 'vitest';
import { findCapability, isCapabilityOn, type ICapabilities } from './capabilities';

const doc: ICapabilities = {
  server: 'runtime',
  capabilities: [
    { id: 'gitlab', status: 'on' },
    { id: 'vault', status: 'off', reason: 'no gateway' },
    { id: 'tracker', status: 'needs_config', configure: 'RUNTIME_TRACKER_URL' },
  ],
};

describe('capabilities', () => {
  it('is on only when the server says on', () => {
    expect(isCapabilityOn(doc, 'gitlab')).toBe(true);
    expect(isCapabilityOn(doc, 'vault')).toBe(false);
    expect(isCapabilityOn(doc, 'tracker')).toBe(false);
    expect(isCapabilityOn(doc, 'unknown')).toBe(false);
    expect(isCapabilityOn(null, 'gitlab')).toBe(false);
  });
  it('finds a capability with its reason and setting', () => {
    expect(findCapability(doc, 'tracker')?.configure).toBe('RUNTIME_TRACKER_URL');
    expect(findCapability(null, 'x')).toBeUndefined();
  });
});
