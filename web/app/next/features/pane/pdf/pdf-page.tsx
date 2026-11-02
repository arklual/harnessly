// One page: the picture on a canvas and, over it, the invisible text that makes it selectable. A page far from the view
// holds neither, only its place. A new width keeps the old picture stretched until the new one is drawn.
import { useEffect, useRef } from 'react';
import { TextLayer, type PDFDocumentProxy, type PDFPageProxy } from 'pdfjs-dist';

const RENDER_PAUSE_MS = 140;

interface IPdfCanvasProps {
  doc: PDFDocumentProxy;
  page: number;
  /** The width on screen, in CSS pixels. */
  width: number;
  /** The page has text over it. A thumbnail does not. */
  text?: boolean;
  /** The text layer is ready, or gone (null). */
  onText?: (page: number, layer: HTMLElement | null) => void;
}

export const PdfCanvas = ({ doc, page, width, text = false, onText }: IPdfCanvasProps) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const textRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let task: ReturnType<PDFPageProxy['render']> | null = null;
    let layer: TextLayer | null = null;
    let stopped = false;
    const timer = setTimeout(() => {
      void doc
        .getPage(page + 1)
        .then(async (proxy) => {
          const canvas = canvasRef.current;
          if (stopped || !canvas) return;
          const scale = width / proxy.getViewport({ scale: 1 }).width;
          const viewport = proxy.getViewport({ scale });
          const ratio = window.devicePixelRatio || 1;
          // The picture is drawn aside and put in whole, so the page never flashes empty while it draws.
          const draft = document.createElement('canvas');
          draft.width = Math.floor(viewport.width * ratio);
          draft.height = Math.floor(viewport.height * ratio);
          task = proxy.render({
            canvas: draft,
            viewport,
            transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0],
          });
          await task.promise;
          if (stopped) return;
          canvas.width = draft.width;
          canvas.height = draft.height;
          canvas.getContext('2d')?.drawImage(draft, 0, 0);
          const host = textRef.current;
          if (!text || !host) return;
          host.replaceChildren();
          host.style.setProperty('--scale-factor', String(scale));
          layer = new TextLayer({ textContentSource: proxy.streamTextContent(), container: host, viewport });
          await layer.render();
          if (!stopped) onText?.(page, host);
        })
        .catch((failure: unknown) => {
          // A page left behind cancels its drawing; that is no failure. A page that cannot be drawn stays blank in its place.
          if (!stopped) console.warn('pdf page', page + 1, failure);
        });
    }, RENDER_PAUSE_MS);
    return () => {
      stopped = true;
      clearTimeout(timer);
      task?.cancel();
      layer?.cancel();
      onText?.(page, null);
    };
  }, [doc, page, width, text, onText]);

  return (
    <>
      <canvas ref={canvasRef} className="hx-pdf__canvas" />
      {text && <div ref={textRef} className="hx-pdf__text textLayer" />}
    </>
  );
};
