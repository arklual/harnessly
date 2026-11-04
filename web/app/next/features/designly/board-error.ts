// A board of the library that throws shows its own card («Доска не отрисовалась», the runtime's client.js). Its «Отправить
// в чат» posts `tk:board-error {url, message, stack}` to the page that frames it; the site puts the error into the chat's
// field and answers `tk:board-error-ack {message}`, and only then the card says «В чате ✓». The site tells the board
// whether it has a chat to put it in (`tk:board-chat {on}`): a guest's board has none, and its card offers to copy.
// The page's words are data for the agent: clipped, and the same error goes into the field once.

import { t } from '@/next/i18n';

export interface IBoardError {
  message: string;
  stack: string;
}

const MAX_MESSAGE = 600;
const MAX_STACK = 1500;
/** The stack's first lines name the place; the rest is the framework's. */
const STACK_LINES = 8;
/** A page is foreign code: a few errors at most go into the field, however many it throws. */
export const MAX_BOARD_ERRORS = 5;

const text = (v: unknown) => (typeof v === 'string' ? v : '');

export const readBoardError = (data: unknown): IBoardError | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (m.type !== 'tk:board-error') return null;
  const message = text(m.message).trim().slice(0, MAX_MESSAGE);
  if (message === '') return null;
  return { message, stack: text(m.stack).slice(0, MAX_STACK) };
};

/** What lands in the chat's field: which board, the error, and the top of its stack as code. */
export const boardErrorWords = (error: IBoardError, board: string): string => {
  const head = board === '' ? t('next.site.boardError.head') : t('next.site.boardError.headOf', { board });
  const stack = error.stack.split('\n').slice(0, STACK_LINES).join('\n').replaceAll('```', "'''").trim();
  return stack === '' ? `${head}\n${error.message}` : `${head}\n${error.message}\n\`\`\`\n${stack}\n\`\`\``;
};

export interface IBoardErrorSink {
  /**
   * Takes a board's error: puts it into the field unless it is there already (or the field has had enough of them).
   * True when the error is in the field now, and the board may say so.
   */
  take: (error: IBoardError, board: string) => boolean;
}

/** One per framed page: the errors it already put into the field are not put again. */
export const boardErrorSink = (attach: (words: string) => void): IBoardErrorSink => {
  const seen = new Set<string>();
  return {
    take: (error, board) => {
      const key = `${board}\n${error.message}`;
      if (seen.has(key)) return true;
      if (seen.size >= MAX_BOARD_ERRORS) return false;
      seen.add(key);
      attach(boardErrorWords(error, board));
      return true;
    },
  };
};

/** The board's answer: the card shows «В чате ✓» for this error only after it. */
export const boardErrorAck = (error: IBoardError) => ({ type: 'tk:board-error-ack', message: error.message });

/** Whether the board's card may offer to send to the chat. */
export const boardChat = (on: boolean) => ({ type: 'tk:board-chat', on });
