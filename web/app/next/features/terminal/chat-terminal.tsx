// The terminal of a design's chat for the places that show that chat: its panel stands in the chat's own column under
// its field (a slot the design's view gives, bindings/shell fills it), the button stands in the head of the chat. Outside a design's page there is no such button: a chat's page has its own.
import { createContext, useContext } from 'react';
import { TerminalButton, type IHeaderToggle } from '@/next/features/header/header-buttons';

export const ChatTerminalContext = createContext<IHeaderToggle | null>(null);

export const ChatTerminalButton = () => {
  const terminal = useContext(ChatTerminalContext);
  return terminal && <TerminalButton terminal={terminal} />;
};

/** Takes the slot of the design's view under the chat's field; the shell's panel is drawn into it. Null outside the shell. */
export const ChatTerminalSlotContext = createContext<((slot: HTMLElement | null) => void) | null>(null);
