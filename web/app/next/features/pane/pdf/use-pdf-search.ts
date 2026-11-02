// Search through a PDF: the pages' text is read one at a time on the first search, so a long file answers as it goes, and
// the matches are lit by the browser's own highlights over the text layer, without touching its markup.
import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import type { PDFDocumentProxy } from 'pdfjs-dist';
import { hitsIn, hitsOf, stepHit, type IPdfHit } from '@/next/features/pane/pdf/pdf-model';

const textOfPage = async (doc: PDFDocumentProxy, page: number): Promise<string> => {
  const proxy = await doc.getPage(page + 1);
  const content = await proxy.getTextContent();
  return content.items.map((item) => ('str' in item ? item.str : '')).join('');
};

/** Ranges of the query in a drawn text layer. The layer holds the same strings in the same order as the page's text. */
const rangesIn = (layer: HTMLElement, query: string): Range[] => {
  const walker = document.createTreeWalker(layer, NodeFilter.SHOW_TEXT);
  const nodes: { node: Text; from: number }[] = [];
  let text = '';
  while (walker.nextNode()) {
    const node = walker.currentNode as Text;
    nodes.push({ node, from: text.length });
    text += node.data;
  }
  const length = query.trim().length;
  const at = (place: number) => {
    let found = nodes[0];
    for (const entry of nodes) {
      if (entry.from > place) break;
      found = entry;
    }
    return { node: found.node, offset: place - found.from };
  };
  return hitsIn(text, query).map((start) => {
    const range = document.createRange();
    const first = at(start);
    const last = at(start + length);
    range.setStart(first.node, first.offset);
    range.setEnd(last.node, last.offset);
    return range;
  });
};

const paintHighlights = (all: Range[], now: Range | null) => {
  if (typeof CSS === 'undefined' || !('highlights' in CSS)) return;
  CSS.highlights.set('hx-pdf-hit', new Highlight(...all));
  if (now) CSS.highlights.set('hx-pdf-hit-now', new Highlight(now));
  else CSS.highlights.delete('hx-pdf-hit-now');
};

export interface IPdfSearch {
  query: string;
  setQuery: (query: string) => void;
  hits: readonly IPdfHit[];
  index: number;
  /** All pages are read: «ничего не нашлось» is true now. */
  done: boolean;
  step: (direction: 1 | -1) => void;
  /** A text layer was drawn (or dropped, null): its matches are lit. */
  onText: (page: number, layer: HTMLElement | null) => void;
  clear: () => void;
}

export const usePdfSearch = (
  doc: PDFDocumentProxy | null,
  scroller: RefObject<HTMLElement | null>,
  goTo: (page: number) => void,
): IPdfSearch => {
  const [query, setRawQuery] = useState('');
  const [pages, setPages] = useState<readonly (string | null)[]>([]);
  const [index, setIndex] = useState(0);
  const [drawn, setDrawn] = useState(0);
  const layers = useRef(new Map<number, HTMLElement>());
  const asked = query.trim() !== '';
  const onText = useCallback((page: number, layer: HTMLElement | null) => {
    if (layer) layers.current.set(page, layer);
    else layers.current.delete(page);
    setDrawn((n) => n + 1);
  }, []);

  useEffect(() => {
    if (!doc || !asked) return;
    let stopped = false;
    void (async () => {
      const known = new Array<string | null>(doc.numPages).fill(null);
      setPages((before) => (before.length === doc.numPages ? before : known));
      for (let page = 0; page < doc.numPages && !stopped; page += 1) {
        const text = await textOfPage(doc, page);
        if (stopped) return;
        setPages((before) => {
          if (before[page] !== null && before[page] !== undefined) return before;
          const next = before.length === doc.numPages ? [...before] : [...known];
          next[page] = text;
          return next;
        });
      }
    })().catch((failure: unknown) => console.warn('pdf search', failure));
    return () => {
      stopped = true;
    };
    // Once asked, the file's text stays read: only the first search reads it.
  }, [doc, asked]);

  const hits = useMemo(() => (asked ? hitsOf(pages, query) : []), [asked, pages, query]);
  const current = hits.at(Math.min(index, Math.max(0, hits.length - 1)));

  // The matches of the pages on screen light up, and the current one comes into view.
  useEffect(() => {
    const all: Range[] = [];
    let now: Range | null = null;
    if (asked) {
      for (const [page, layer] of layers.current) {
        const ranges = rangesIn(layer, query);
        all.push(...ranges);
        if (current?.page === page) now = ranges[current.nth] ?? null;
      }
    }
    paintHighlights(all, now);
    const box = scroller.current;
    if (now && box) {
      const rect = now.getBoundingClientRect();
      const frame = box.getBoundingClientRect();
      if (rect.top < frame.top + 40 || rect.bottom > frame.bottom - 40)
        box.scrollBy({ top: rect.top - frame.top - frame.height / 3 });
    }
    return () => paintHighlights([], null);
  }, [asked, query, current, drawn, scroller]);

  return {
    query,
    setQuery: (next) => {
      setRawQuery(next);
      setIndex(0);
      const first = hitsOf(pages, next)[0];
      if (first && next.trim() !== '') goTo(first.page);
    },
    hits,
    index: Math.min(index, Math.max(0, hits.length - 1)),
    done: pages.length > 0 && pages.every((text) => text !== null),
    step: (direction) => {
      const next = stepHit(index, hits.length, direction);
      setIndex(next);
      if (hits[next]) goTo(hits[next].page);
    },
    onText,
    clear: () => {
      setRawQuery('');
      setIndex(0);
    },
  };
};
