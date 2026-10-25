import { describe, expect, it } from 'vitest';
import { PANE_CHAT, parsePaneChat, siteModeOf } from '@/next/features/designly/pane-chat';

describe('where the chat stands in the pane', () => {
  it('reads the kept choice, «Слева» for anything else', () => {
    expect(parsePaneChat('float')).toBe(PANE_CHAT.float);
    expect(parsePaneChat('bottom')).toBe(PANE_CHAT.bottom);
    expect(parsePaneChat('chat')).toBe(PANE_CHAT.left);
    expect(parsePaneChat(null)).toBe(PANE_CHAT.left);
  });

  it('is the full view’s window or field', () => {
    expect(siteModeOf(PANE_CHAT.float)).toBe('float');
    expect(siteModeOf(PANE_CHAT.bottom)).toBe('island');
    expect(siteModeOf(PANE_CHAT.left)).toBeNull();
  });
});
