// The heading of the design's chat column names the chat, never the design: a chat called «Аналоги Dayflow»
// that works in the project «harnessly» is headed «Аналоги Dayflow». Before the chat itself has come (its row in the
// conversations store), its title is taken from the design's chat list; a chat with no title yet is «Чат». Only a chat
// still to be made is named by what it will be about: the design, or the space.
import { t } from '@/next/i18n';

export interface IChatTitleOf {
  /** The chat on screen; null: a new chat for the design. */
  convId: string | null;
  /** Its title as the conversations store has it; null while it has not come. */
  convTitle: string | null;
  /** Its title in the design's chat list; null where it is not listed. */
  rowTitle: string | null;
  /** The new chat would be the space's (its name is the design's title). */
  space: boolean;
  designTitle: string;
}

const named = (title: string | null): title is string => title !== null && title.trim() !== '';

export const chatTitleOf = ({ convId, convTitle, rowTitle, space, designTitle }: IChatTitleOf): string => {
  if (convId !== null) {
    if (named(convTitle)) return convTitle;
    if (named(rowTitle)) return rowTitle;
    return t('next.site.chatUntitled');
  }
  if (space) return t('next.site.newSpaceChat', { name: designTitle });
  return t('next.site.newDesignChat');
};
