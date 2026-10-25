import { describe, expect, it } from 'vitest';
import { KEY_ACTION, keyAction } from '@/next/features/composer/keys';

const key = (over: Partial<Parameters<typeof keyAction>[0]> = {}) => ({
  key: 'Enter',
  shiftKey: false,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  isComposing: false,
  ...over,
});

describe('keyAction', () => {
  it('sends on Enter, right now on Ctrl/Cmd+Enter, a new line on Shift+Enter', () => {
    expect(keyAction(key(), false)).toBe(KEY_ACTION.send);
    expect(keyAction(key({ ctrlKey: true }), false)).toBe(KEY_ACTION.sendNow);
    expect(keyAction(key({ metaKey: true }), false)).toBe(KEY_ACTION.sendNow);
    expect(keyAction(key({ shiftKey: true }), false)).toBe(KEY_ACTION.newline);
  });

  it('leaves Enter to a touch keyboard and to an IME, and other keys to the field', () => {
    expect(keyAction(key(), true)).toBe(KEY_ACTION.newline);
    expect(keyAction(key({ isComposing: true }), false)).toBeNull();
    expect(keyAction(key({ isComposing: true }), false, false)).toBeNull();
    expect(keyAction(key({ altKey: true }), false)).toBeNull();
    expect(keyAction(key({ key: 'a' }), false)).toBeNull();
  });

  it('makes Enter a new line when Enter does not send, and keeps Ctrl/Cmd+Enter sending', () => {
    expect(keyAction(key(), false, false)).toBe(KEY_ACTION.newline);
    expect(keyAction(key({ shiftKey: true }), false, false)).toBe(KEY_ACTION.newline);
    expect(keyAction(key({ ctrlKey: true }), false, false)).toBe(KEY_ACTION.sendNow);
    expect(keyAction(key({ metaKey: true }), false, false)).toBe(KEY_ACTION.sendNow);
    expect(keyAction(key({ key: 'a' }), false, false)).toBeNull();
  });
});
