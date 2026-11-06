import { afterEach, describe, expect, it, vi } from 'vitest';
import { openFileInChat } from '@/next/bindings/open-file-in-chat';
import { chatPaneOf, resetChatPanes } from '@/next/stores/pane-state';

afterEach(resetChatPanes);

describe('openFileInChat', () => {
  it('leaves the file for the chat pane and takes the person to the chat', () => {
    const goToChat = vi.fn();
    const path = '/tmp/claude-1000/-srv-site/0a1b/scratchpad/impl/sections.md';
    openFileInChat('tab-a', 'local', `${path}:12`, goToChat);
    expect(chatPaneOf('tab-a').pending).toEqual([{ k: 'files', target: path, reload: false }]);
    expect(goToChat).toHaveBeenCalledOnce();
  });
});
