import { noticeKey } from './agent-browser-input';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  parseBrowserFrame,
  type ITabsMessage,
  type IStatus,
  type ISelectMenu,
  type TBrowserMessage,
} from './agent-browser-frame';

/** One socket owns its selection and discards frames decoded after a selection change. */
export function useBrowserSocket(url: string) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const socket = useRef<WebSocket | null>(null);
  const page = useRef({ w: 0, h: 0 });
  const [status, setStatus] = useState<IStatus | null>(null);
  const [tabs, setTabs] = useState<ITabsMessage>({
    t: 'tabs',
    tabs: [],
    current: '',
    agentTab: '',
    following: true,
    gen: 0,
  });
  const [menu, setMenu] = useState<ISelectMenu | null>(null);
  const [notice, setNotice] = useState<{ key: string; at: number } | null>(null);
  const retryPending = useRef(false),
    copyUntil = useRef(0);
  const [framed, setFramed] = useState(false);
  const [imageSize, setImageSize] = useState({ w: 1, h: 1 });
  useEffect(() => {
    let gone = false,
      generation = -1,
      epoch = 0;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let drawing = false,
      waiting: ArrayBuffer | null = null;
    const draw = (data: ArrayBuffer, source: number) => {
      if (drawing) {
        waiting = data;
        return;
      }
      const frame = parseBrowserFrame(data);
      if (!frame || frame.gen !== generation) return;
      drawing = true;
      void createImageBitmap(frame.jpeg)
        .then((bitmap) => {
          try {
            const el = canvas.current;
            if (gone || source !== epoch || frame.gen !== generation || !el) return;
            page.current = { w: frame.w, h: frame.h };
            el.width = bitmap.width;
            el.height = bitmap.height;
            el.getContext('2d')?.drawImage(bitmap, 0, 0);
            setImageSize({ w: frame.w, h: frame.h });
            setFramed(true);
          } finally {
            bitmap.close();
          }
        })
        .catch(() => undefined)
        .finally(() => {
          drawing = false;
          const next = waiting;
          waiting = null;
          if (next && !gone) draw(next, epoch);
        });
    };
    const open = () => {
      epoch++;
      generation = -1;
      waiting = null;
      const ws = new WebSocket(url);
      socket.current = ws;
      ws.binaryType = 'arraybuffer';
      ws.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
        if (gone || socket.current !== ws) return;
        if (typeof event.data !== 'string') {
          draw(event.data, epoch);
          return;
        }
        let m: TBrowserMessage;
        try {
          m = JSON.parse(event.data) as TBrowserMessage;
        } catch {
          return;
        }
        if (m.t === 'status') setStatus(m);
        if (m.t === 'tabs') {
          if (m.gen !== generation) {
            generation = m.gen;
            setFramed(false);
            setMenu(null);
            setNotice(null);
          }
          setTabs(m);
        }
        if (m.t === 'select') setMenu(m);
        if (m.t === 'notice') {
          console.warn('Agent browser command failed:', m.message);
          // Older runtimes do not implement retry yet. Suppress only their corresponding rejection.
          if (m.message === 'invalid panel command' && retryPending.current) {
            retryPending.current = false;
          } else setNotice({ key: noticeKey(m.message), at: Date.now() });
        }
        if (m.t === 'clipboard' && Date.now() < copyUntil.current) {
          copyUntil.current = 0;
          void navigator.clipboard.writeText(m.text).catch((error: unknown) => {
            console.warn('Agent browser clipboard failed:', error);
            setNotice({ key: 'noticeClipboard', at: Date.now() });
          });
        }
      };
      ws.onclose = () => {
        if (gone) return;
        setStatus((old) =>
          old?.state === 'error'
            ? old
            : { t: 'status', state: 'down', profile: old?.profile ?? { id: '', name: '' } },
        );
        setFramed(false);
        setMenu(null);
        retry = setTimeout(open, 2000);
      };
    };
    open();
    return () => {
      gone = true;
      clearTimeout(retry);
      socket.current?.close();
      socket.current = null;
    };
  }, [url]);
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(null), 5000);
    return () => clearTimeout(timer);
  }, [notice]);
  const send = useCallback((input: Record<string, unknown>) => {
    if (input.t === 'browser.retry') retryPending.current = true;
    if (input.t === 'clipboard.read') copyUntil.current = Date.now() + 5000;
    if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify(input));
  }, []);
  return { canvas, page, status, tabs, menu, setMenu, notice, framed, imageSize, send };
}
