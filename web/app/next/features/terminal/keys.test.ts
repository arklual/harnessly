import { describe, expect, it } from 'vitest';
import {
  isCopyKey,
  isPasteKey,
  isReservedKey,
  mappedBytes,
  type ITerminalKey,
} from '@/next/features/terminal/keys';

const key = (init: Partial<ITerminalKey> & { key: string }): ITerminalKey => ({
  code: '',
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  ...init,
});

describe('terminal keys', () => {
  it('leaves the page only its reserved keys', () => {
    expect(isReservedKey(key({ key: '`', ctrlKey: true }))).toBe(true);
    expect(isReservedKey(key({ key: 's', code: 'KeyS', altKey: true, metaKey: true }))).toBe(true);
    expect(isReservedKey(key({ key: 's', code: 'KeyS', altKey: true, ctrlKey: true }))).toBe(true);
    expect(isReservedKey(key({ key: 'k', code: 'KeyK', metaKey: true }))).toBe(true);
    expect(isReservedKey(key({ key: 'b', code: 'KeyB', metaKey: true }))).toBe(true);
  });

  it('keeps the shell its own keys', () => {
    for (const init of [
      { key: 'k', code: 'KeyK', ctrlKey: true },
      { key: 'b', code: 'KeyB', ctrlKey: true },
      { key: 'c', code: 'KeyC', ctrlKey: true },
      { key: 'd', code: 'KeyD', ctrlKey: true },
      { key: 'r', code: 'KeyR', ctrlKey: true },
      { key: 'Tab', code: 'Tab', shiftKey: true },
      { key: 'Escape', code: 'Escape' },
      { key: '1', code: 'Digit1' },
      { key: 'ArrowLeft', code: 'ArrowLeft', altKey: true },
    ]) {
      expect(isReservedKey(key(init))).toBe(false);
    }
  });

  it('knows the copy and the paste of the selection', () => {
    expect(isCopyKey(key({ key: 'C', code: 'KeyC', ctrlKey: true, shiftKey: true }))).toBe(true);
    expect(isCopyKey(key({ key: 'c', code: 'KeyC', ctrlKey: true }))).toBe(false);
    expect(isPasteKey(key({ key: 'V', code: 'KeyV', ctrlKey: true, shiftKey: true }))).toBe(true);
    expect(isPasteKey(key({ key: 'v', code: 'KeyV', metaKey: true }))).toBe(false);
  });

  it('sends the word jumps of the option arrows as the shells expect', () => {
    expect(mappedBytes(key({ key: 'ArrowLeft', altKey: true }), false)).toBe('\x1bb');
    expect(mappedBytes(key({ key: 'ArrowRight', altKey: true }), true)).toBe('\x1bf');
    expect(mappedBytes(key({ key: 'ArrowLeft', altKey: true, shiftKey: true }), false)).toBeNull();
    expect(mappedBytes(key({ key: 'ArrowLeft' }), false)).toBeNull();
  });

  it('maps the line editing of ⌘ on a Mac only', () => {
    expect(mappedBytes(key({ key: 'ArrowLeft', metaKey: true }), true)).toBe('\x01');
    expect(mappedBytes(key({ key: 'ArrowRight', metaKey: true }), true)).toBe('\x05');
    expect(mappedBytes(key({ key: 'Backspace', metaKey: true }), true)).toBe('\x15');
    expect(mappedBytes(key({ key: 'Backspace', metaKey: true }), false)).toBeNull();
  });
});
