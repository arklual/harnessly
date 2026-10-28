import { describe, expect, it } from 'vitest';
import { needsNewKey } from '@/next/features/settings/voice-api';

describe('needsNewKey', () => {
  const saved = { url: 'https://api.openai.com/v1', model: 'whisper-1', keyMask: '••••abcd' };
  it('keeps the saved key for the same origin only', () => {
    expect(needsNewKey(saved, 'https://API.openai.com:443/v2')).toBe(false);
    expect(needsNewKey(saved, 'https://evil.example/v1')).toBe(true);
    expect(needsNewKey(saved, 'http://api.openai.com/v1')).toBe(true);
    expect(needsNewKey(saved, 'not a url')).toBe(true);
  });
  it('asks nothing when no key was saved', () => {
    expect(needsNewKey({ ...saved, keyMask: '' }, 'https://evil.example/v1')).toBe(false);
    expect(needsNewKey(null, 'https://evil.example/v1')).toBe(false);
  });
});
