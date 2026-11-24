import { readDocx } from '@/next/features/pane/docx/docx-core';
import { drawDocx, looksToCss } from '@/next/features/pane/docx/docx-draw';
import { readPptx, drawnSlide } from '@/next/features/pane/pptx/pptx-core';
import { drawSlide } from '@/next/features/pane/pptx/pptx-draw';
import { OfficeImages } from '@/next/features/pane/office-parts';
import '@/next/styles/docx-view.css';
import '@/next/styles/pptx-view.css';
import type { Copy } from './storage';

export async function showOffice(copy: Copy, view: HTMLElement, signal: AbortSignal) {
  const bytes = new Uint8Array(await copy.body.arrayBuffer());
  if (signal.aborted) return;
  const root = document.createElement('div');
  view.append(root);
  if (copy.kind === 'docx') {
    const book = readDocx(bytes),
      images = new OfficeImages(book.zip);
    signal.addEventListener('abort', () => images.stop(), { once: true });
    const style = document.createElement('style');
    style.textContent = looksToCss(book.looks);
    const page = document.createElement('div');
    page.className = 'hx-docx__page';
    page.dataset.ready = 'true';
    root.className = 'hx-docx';
    root.append(style, page);
    drawDocx(page, book, images);
    return;
  }
  const book = readPptx(bytes),
    images = new OfficeImages(book.zip);
  const stage = document.createElement('div');
  stage.className = 'hx-slide__stage';
  stage.dataset.ready = 'true';
  root.style.position = 'relative';
  root.append(stage);
  const scale = () => {
    const ratio = root.clientWidth / book.width;
    stage.style.transform = 'scale(' + ratio + ')';
    root.style.height = book.height * ratio + 'px';
  };
  const resize = new ResizeObserver(scale);
  resize.observe(root);
  signal.addEventListener(
    'abort',
    () => {
      images.stop();
      resize.disconnect();
    },
    { once: true },
  );
  const picker = document.createElement('select');
  picker.ariaLabel = copy.title;
  for (let i = 0; i < book.slides.length; i++) picker.add(new Option(String(i + 1), String(i)));
  view.insertBefore(picker, root);
  const draw = () => {
    drawSlide(stage, drawnSlide(book, picker.selectedIndex), book.width, book.height, {
      images,
      editable: false,
      words: { chart: 'Chart', diagram: 'Diagram', object: 'Object' },
    });
    scale();
  };
  picker.onchange = draw;
  if (book.slides.length) draw();
}
