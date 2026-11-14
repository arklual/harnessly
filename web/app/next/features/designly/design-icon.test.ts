import { describe, expect, it } from 'vitest';

import { designIcon } from '@/next/features/designly/design-icon';

describe('designIcon', () => {
  it('draws each kind, an app document and an unknown project', () => {
    expect(designIcon({ kind: 'site' })).toBe('app-window');
    expect(designIcon({ kind: 'slides' })).toBe('presentation');
    expect(designIcon({ kind: 'video' })).toBe('clapperboard');
    expect(designIcon({ kind: 'ds' })).toBe('palette');
    expect(designIcon({ kind: 'site', appKind: { id: 'x' } })).toBe('file-text');
    expect(designIcon({ kind: null })).toBe('pen-tool');
  });
});
