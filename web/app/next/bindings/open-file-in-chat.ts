// A file a chat names, opened from a place that has no pane of its own (a design's chat column): it waits in the
// chat's pane, in «Файлы», and the person is taken to the chat, where the pane places it (HRN-675).

import { chatPanes } from '@/next/stores/pane-state';

const LINE_SUFFIX = /:\d+$/;

export const openFileInChat = (conv: string, machine: string, path: string, goToChat: () => void): void => {
  chatPanes.queue(conv, machine, { k: 'files', target: path.replace(LINE_SUFFIX, ''), reload: false });
  goToChat();
};
