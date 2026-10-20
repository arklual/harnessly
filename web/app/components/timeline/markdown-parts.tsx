// The markdown renderers every message uses: rich links, link cards in paragraphs and list items, code blocks, images.
import remarkGfm from 'remark-gfm';
import type { Components } from 'react-markdown';
import MessageLink from '@/components/timeline/message-link';
import { MessageListItem, MessageParagraph } from '@/components/timeline/message-link-card';
import MessageCodeBlock from '@/components/timeline/message-code-block';
import MessageMarkdownImage from '@/components/timeline/message-markdown-image';

export const MESSAGE_COMPONENTS: Components = {
  a: MessageLink,
  p: MessageParagraph,
  li: MessageListItem,
  pre: MessageCodeBlock,
  img: MessageMarkdownImage,
};

export const MESSAGE_PLUGINS = [remarkGfm];

/** Prose look shared by assistant replies and long user messages. */
export const PROSE = 'prose prose-sm dark:prose-invert max-w-none break-words text-sm';
