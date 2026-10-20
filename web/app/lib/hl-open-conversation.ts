import { OPEN_CONVERSATION_EVENT } from '@/hooks/use-web-push';

/** Asks the shell to open the conversation of a tab, wherever the request comes from (a panel tab, a section of the centre). */
export const openConversation = (tabId: string): void => {
  window.dispatchEvent(new CustomEvent(OPEN_CONVERSATION_EVENT, { detail: tabId }));
};
