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
  const [notice, setNotice] = useState('');
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
            setNotice('');
          }
          setTabs(m);
        }
        if (m.t === 'select') setMenu(m);
        if (m.t === 'notice') setNotice(m.message);
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
    const timer = setTimeout(() => setNotice(''), 6000);
    return () => clearTimeout(timer);
  }, [notice]);
  const send = useCallback((input: Record<string, unknown>) => {
    if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify(input));
  }, []);
  return { canvas, page, status, tabs, menu, setMenu, notice, framed, imageSize, send };
}
