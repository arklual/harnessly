// The board errors of a deck and a video: their boards are frames of their own, not the site view's one frame,
// but the board's error card is the same, and so is its «Отправить в чат». This listens to the frames' `tk:board-error`,
// hands the words to the host (which switches to editing, opens the chat and sends) and answers the board: whether
// there is a chat (`tk:board-chat`), and that the error is in it (`tk:board-error-ack`).
import { useEffect, useEffectEvent, useRef } from 'react';
import {
  boardChat,
  boardErrorAck,
  boardErrorSink,
  personIsActing,
  readBoardError,
  type IBoardErrorSink,
} from '@/next/features/designly/board-error';

export interface IBoardFrame {
  frame: HTMLIFrameElement | null;
  /** The address the frame shows: its origin is where a message to it goes. */
  src: string | null;
}

const originOf = (src: string): string | null => {
  try {
    return new URL(src, window.location.href).origin;
  } catch {
    return null;
  }
};

export const useBoardErrors = (opts: {
  /** Takes the words; absent where there is no chat to send to (a guest, a design one may not edit). */
  report: ((words: string) => void) | undefined;
  /** The frames that may hold a board: the ones on screen and the one coming. */
  frames: () => readonly IBoardFrame[];
  /** The board on screen, named as the chat reads it ('' for a single one). */
  board: () => string;
}) => {
  const sink = useRef<IBoardErrorSink | null>(null);
  // The frames that have spoken to the host since they loaded: a frame that was not told yet is told at its first word.
  const heard = useRef(new WeakSet<object>());
  const latest = useRef(opts);
  useEffect(() => {
    latest.current = opts;
  });
  const report = useEffectEvent((words: string) => opts.report?.(words));
  // A board that was told there is a chat is told when there is none any more; one that never was needs no word of it.
  const toldOn = useRef(false);
  const tell = useEffectEvent(() => {
    const chat = opts.report !== undefined;
    if (!chat && !toldOn.current) return;
    toldOn.current = chat;
    for (const { frame, src } of opts.frames()) {
      const origin = src === null ? null : originOf(src);
      if (!frame?.contentWindow || origin === null) continue;
      frame.contentWindow.postMessage(boardChat(opts.report !== undefined), origin);
    }
  });
  const on = opts.report !== undefined;
  useEffect(() => {
    tell();
  }, [on]);
  useEffect(() => {
    const hear = (event: MessageEvent) => {
      const slot = latest.current.frames().find((f) => f.frame?.contentWindow === event.source);
      // (A frame of another origin is a window that is `instanceof` nothing: it is told by the frame the host holds.)
      const board = slot?.frame?.contentWindow;
      if (!slot || !board || event.source === null) return;
      // A board that has loaded greets the host (`tk:hello`, `tk:images-hello`); one never heard from is told at its first word of any kind.
      const type: unknown = (event.data as { type?: unknown } | null)?.type;
      const greeting = typeof type === 'string' && type.endsWith('hello');
      if (greeting || !heard.current.has(event.source)) {
        heard.current.add(event.source);
        tell();
      }
      const error = readBoardError(event.data);
      if (error) {
        if (!latest.current.report || !personIsActing()) return;
        sink.current ??= boardErrorSink((words) => report(words));
        if (sink.current.take(error, latest.current.board()))
          board.postMessage(boardErrorAck(error), event.origin);
        return;
      }
    };
    window.addEventListener('message', hear);
    return () => window.removeEventListener('message', hear);
  }, []);
};
