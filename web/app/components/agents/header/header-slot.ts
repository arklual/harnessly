import type { Conversation } from '@/lib/hl-conversations';

/**
 * Props of every header slot. Slots render nothing when they have nothing to show;
 * text next to an icon goes into `.tw-header-slot-label`, which the header hides below 1100px.
 */
export interface IHeaderSlotProps {
  conversation: Conversation | null;
  /** Machine of the conversation, or the machine new conversations go to on the home screen. */
  machineId?: string;
  /** The conversation runs in a remote machine's iframe: its composer and inspector live there. */
  remote: boolean;
  /** tmux session of a local agent conversation; null for chats and remote conversations. */
  sessionName: string | null;
}
