// Where the chat stands when a design is open in the right pane: «Слева» — the conversation the pane is
// beside is the editor, as it is; «Поверх» — that conversation also in a compact window over the design, moved by its
// head and folded; «Снизу» — its field docked at the foot of the design. The last two are the full view's window and
// field (site-stage.tsx), the left chat stays where it is. The choice is this browser's: kept in its storage.
import type { TSiteMode } from '@/next/features/designly/site-chat';

export const PANE_CHAT = { left: 'left', float: 'float', bottom: 'bottom' } as const;
export type TPaneChat = (typeof PANE_CHAT)[keyof typeof PANE_CHAT];
export const PANE_CHATS = [PANE_CHAT.left, PANE_CHAT.float, PANE_CHAT.bottom] as const;

const KEY = 'hx-dl-pane-chat';

export const parsePaneChat = (raw: string | null): TPaneChat =>
  PANE_CHATS.find((mode) => mode === raw) ?? PANE_CHAT.left;

export const readPaneChat = (): TPaneChat => {
  try {
    return parsePaneChat(window.localStorage.getItem(KEY));
  } catch {
    return PANE_CHAT.left;
  }
};

export const keepPaneChat = (mode: TPaneChat) => {
  try {
    window.localStorage.setItem(KEY, mode);
  } catch {
    // Blocked storage: the choice holds while the pane is open.
  }
};

/** The full view's mode a pane's choice stands for; null for «Слева», which has none of its own. */
export const siteModeOf = (mode: TPaneChat): TSiteMode | null => {
  if (mode === PANE_CHAT.float) return 'float';
  if (mode === PANE_CHAT.bottom) return 'island';
  return null;
};
