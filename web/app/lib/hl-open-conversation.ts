import { OPEN_CONVERSATION_EVENT } from '@/hooks/use-web-push';

export interface IOpenConversationRequest {
  tabId: string;
  /** The machine the tab is on; without it the home machine's tab of that id is taken first. */
  machineId?: string;
}

/** Asks the shell to open the conversation of a tab, wherever the request comes from (a panel tab, a section of the centre). */
export const openConversation = (tabId: string, machineId?: string): void => {
  const detail: IOpenConversationRequest = machineId ? { tabId, machineId } : { tabId };
  window.dispatchEvent(new CustomEvent(OPEN_CONVERSATION_EVENT, { detail }));
};
