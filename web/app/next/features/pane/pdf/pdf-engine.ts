// pdf.js for the pane: the worker and the fonts come from our own build, nothing from outside. A PDF's scripts do not run
// and eval is off: the file is the agent's or anyone's, and it only gets drawn. This module arrives with the PDF view, in
// its own chunk, so the rest of the interface does not carry the engine.
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
import { getDocument, GlobalWorkerOptions, type PDFDocumentLoadingTask } from 'pdfjs-dist';

GlobalWorkerOptions.workerSrc = workerUrl;

// In a build the folders are put beside the chunks (vite.config.ts, pdfAssets); under the dev server they are read in place.
const assets = (folder: string): string =>
  import.meta.env.DEV
    ? `/node_modules/pdfjs-dist/${folder}/`
    : `${import.meta.url.slice(0, import.meta.url.lastIndexOf('/') + 1)}pdfjs/${folder}/`;

export const openPdf = (src: string): PDFDocumentLoadingTask =>
  getDocument({
    url: src,
    withCredentials: true,
    disableAutoFetch: true,
    isEvalSupported: false,
    enableXfa: false,
    cMapUrl: assets('cmaps'),
    cMapPacked: true,
    standardFontDataUrl: assets('standard_fonts'),
    wasmUrl: assets('wasm'),
    iccUrl: assets('iccs'),
  });
