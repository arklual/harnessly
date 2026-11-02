// A PDF in the pane, on pdf.js: pages one under another fitted to the pane's width, the page number, zoom, thumbnails with
// the file's contents and search through all its text. Only the pages near the view are drawn. Text can be selected.
// Its own chunk: the engine comes only when a PDF is opened.
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import { textSelection, type IFileSelection } from '@/next/features/pane/file-selection';
import { parsePagePlace } from '@/next/features/pane/file-place';
import type { PDFDocumentProxy } from 'pdfjs-dist';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';
import { openPdf } from '@/next/features/pane/pdf/pdf-engine';
import { PdfCanvas } from '@/next/features/pane/pdf/pdf-page';
import { usePdfSearch } from '@/next/features/pane/pdf/use-pdf-search';
import {
  NEAR_PAGES,
  offsetsOf,
  PAGE_GAP,
  pageAt,
  typedPage,
  zoomIn,
  zoomOut,
} from '@/next/features/pane/pdf/pdf-model';

export interface IPdfViewProps {
  src: string;
  name: string;
  saveHref: string;
  onSelection?: (selection: IFileSelection | null) => void;
  /** A page the agent asked to show; onPlaced when it is there. */
  place?: string | null;
  onPlaced?: () => void;
}

const REPORT_PAUSE_MS = 200;

const THUMB_WIDTH = 132;
const PADDING = 16;
const RAIL = { pages: 'pages', contents: 'contents' } as const;
type TRail = (typeof RAIL)[keyof typeof RAIL];

interface IOutlineEntry {
  title: string;
  dest: unknown;
  items: IOutlineEntry[];
}

const usePdf = (src: string) => {
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null);
  const [failed, setFailed] = useState<'locked' | 'broken' | null>(null);
  useEffect(() => {
    let stopped = false;
    const task = openPdf(src);
    task.promise.then(
      (next) => {
        if (!stopped) setDoc(next);
      },
      (failure: { name?: string; message?: string }) => {
        // A view that left destroys its loading: that is no failure.
        if (stopped) return;
        console.warn('pdf open', failure.name, failure.message);
        setFailed(failure.name === 'PasswordException' ? 'locked' : 'broken');
      },
    );
    return () => {
      stopped = true;
      void task.destroy();
    };
  }, [src]);
  return { doc, failed };
};

/** Every page's height for a width of 1. Until they are read, all are taken to be like the first. */
const useProportions = (doc: PDFDocumentProxy | null): readonly number[] => {
  const [known, setKnown] = useState<readonly number[]>([]);
  useEffect(() => {
    if (!doc) return;
    let stopped = false;
    void (async () => {
      const first = await doc.getPage(1);
      const box = first.getViewport({ scale: 1 });
      const all = new Array<number>(doc.numPages).fill(box.height / box.width);
      if (!stopped) setKnown(all);
      for (let page = 2; page <= doc.numPages && !stopped; page += 1) {
        const view = (await doc.getPage(page)).getViewport({ scale: 1 });
        all[page - 1] = view.height / view.width;
      }
      if (!stopped) setKnown([...all]);
    })().catch((failure: unknown) => console.warn('pdf sizes', failure));
    return () => {
      stopped = true;
    };
  }, [doc]);
  return known;
};

const ToolButton = ({
  icon,
  label,
  pressed,
  onClick,
}: {
  icon: string;
  label: string;
  pressed?: boolean;
  onClick: () => void;
}) => (
  <Tooltip label={label}>
    <button type="button" className="hx-pdf__btn" aria-label={label} aria-pressed={pressed} onClick={onClick}>
      <Icon name={icon} size="sm" />
    </button>
  </Tooltip>
);

const Outline = ({
  items,
  onPick,
}: {
  items: readonly IOutlineEntry[];
  onPick: (dest: unknown) => void;
}): ReactNode => (
  <ul className="hx-pdf__outline">
    {items.map((item, at) => (
      <li key={`${item.title}-${at}`}>
        <button type="button" className="hx-pdf__outline-item" onClick={() => onPick(item.dest)}>
          {item.title}
        </button>
        {item.items.length > 0 && <Outline items={item.items} onPick={onPick} />}
      </li>
    ))}
  </ul>
);

const PdfView = ({ src, name, saveHref, onSelection, place, onPlaced }: IPdfViewProps) => {
  const { doc, failed } = usePdf(src);
  const proportions = useProportions(doc);
  const scroller = useRef<HTMLDivElement | null>(null);
  const railRef = useRef<HTMLDivElement | null>(null);
  const searchInput = useRef<HTMLInputElement | null>(null);
  const [fit, setFit] = useState(0);
  const [zoom, setZoom] = useState(1);
  const [top, setTop] = useState(0);
  const [typed, setTyped] = useState<string | null>(null);
  const [railOn, setRailOn] = useState(false);
  const [railTab, setRailTab] = useState<TRail>(RAIL.pages);
  const [outline, setOutline] = useState<readonly IOutlineEntry[]>([]);
  const [searchOn, setSearchOn] = useState(false);

  const width = Math.round(fit * zoom);
  const heights = useMemo(() => proportions.map((p) => Math.round(p * width)), [proportions, width]);
  const offsets = useMemo(() => offsetsOf(heights), [heights]);
  const now = pageAt(offsets, top + PAGE_GAP);
  const count = doc?.numPages ?? 0;

  const goTo = useCallback(
    (page: number) => {
      const box = scroller.current;
      if (box && offsets[page] !== undefined) box.scrollTop = offsets[page];
    },
    [offsets],
  );
  const search = usePdfSearch(doc, scroller, goTo);

  // Text selected on a page goes to the chat. Only what happens in the pages counts: going to the field to write about it
  // moves the browser's selection away, and that takes nothing back.
  const reportText = useEffectEvent(() => {
    const selection = document.getSelection();
    const box = scroller.current;
    const anchor = selection?.anchorNode ?? null;
    if (!selection || !box || !anchor || !box.contains(anchor)) return;
    if (selection.isCollapsed) {
      onSelection?.(null);
      return;
    }
    const text = selection.toString().trim();
    const home = anchor instanceof Element ? anchor : anchor.parentElement;
    const page = Number(home?.closest('[data-page]')?.getAttribute('data-page'));
    if (text && page) onSelection?.(textSelection(t('next.pdf.pageN', { page }), page, text));
  });
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const onChange = () => {
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(reportText, REPORT_PAUSE_MS);
    };
    document.addEventListener('selectionchange', onChange);
    return () => {
      document.removeEventListener('selectionchange', onChange);
      if (timer !== null) clearTimeout(timer);
    };
  }, []);

  const goToPlace = useEffectEvent((asked: string) => {
    const page = parsePagePlace(asked);
    if (page !== null) goTo(Math.min(page, count) - 1);
    onPlaced?.();
  });
  const placeable = offsets.length > 0 && width > 0;
  useEffect(() => {
    if (place && placeable) goToPlace(place);
  }, [place, placeable]);

  useEffect(() => {
    const box = scroller.current;
    if (!box) return;
    const measure = () => setFit(Math.max(120, box.clientWidth - PADDING * 2));
    const observer = new ResizeObserver(measure);
    observer.observe(box);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!doc) return;
    let stopped = false;
    doc.getOutline().then(
      (items) => {
        if (!stopped) setOutline((items ?? []) as unknown as IOutlineEntry[]);
      },
      () => {
        if (!stopped) setOutline([]);
      },
    );
    return () => {
      stopped = true;
    };
  }, [doc]);

  // The rail follows the page in view, so the thumbnail drawn is the one on screen.
  useEffect(() => {
    if (!railOn) return;
    railRef.current?.querySelector('[data-now="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [railOn, now, proportions]);

  const onScrollFrame = useRef<number | null>(null);
  const onScroll = () => {
    if (onScrollFrame.current !== null) return;
    onScrollFrame.current = requestAnimationFrame(() => {
      onScrollFrame.current = null;
      if (scroller.current) setTop(scroller.current.scrollTop);
    });
  };

  const openDest = async (dest: unknown) => {
    if (!doc) return;
    const target = typeof dest === 'string' ? await doc.getDestination(dest) : (dest as unknown[] | null);
    const ref = target?.[0];
    if (ref === undefined || ref === null) return;
    const page = typeof ref === 'number' ? ref : await doc.getPageIndex(ref as never);
    goTo(page);
  };

  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if ((event.ctrlKey || event.metaKey) && event.code === 'KeyF') {
      event.preventDefault();
      setSearchOn(true);
      requestAnimationFrame(() => searchInput.current?.select());
      return;
    }
    if (event.target instanceof HTMLInputElement) return;
    if (event.key === '+' || event.key === '=') setZoom(zoomIn);
    if (event.key === '-') setZoom(zoomOut);
    if (event.key === '0') setZoom(1);
  };

  if (failed !== null)
    return (
      <div className="hx-media__failed" role="status">
        <Icon name="file-text" />
        <span className="hx-media__failed-title">
          {t(failed === 'locked' ? 'next.pdf.locked' : 'next.pdf.broken')}
        </span>
        <a className="hx-media__failed-save" href={saveHref} download>
          {t('next.delivered.save')}
        </a>
      </div>
    );

  const near = (page: number) => Math.abs(page - now) <= NEAR_PAGES;
  // The thumbnails nearest the current page are drawn, as in the pages' own column.
  const thumbHeight = (page: number) => Math.round((proportions[page] ?? 1.4) * THUMB_WIDTH);
  const found = search.query.trim() !== '';

  return (
    <div
      className="hx-pdf"
      tabIndex={-1}
      aria-label={name}
      onKeyDown={onKey}
      data-ready={doc ? 'true' : undefined}
    >
      <div className="hx-pdf__bar">
        <ToolButton
          icon="panel-left"
          label={t('next.pdf.pages')}
          pressed={railOn}
          onClick={() => setRailOn((on) => !on)}
        />
        <span className="hx-pdf__page">
          <input
            className="hx-pdf__page-input"
            inputMode="numeric"
            aria-label={t('next.pdf.pageNumber')}
            value={typed ?? (count ? String(now + 1) : '')}
            onChange={(event) => setTyped(event.target.value)}
            onFocus={(event) => event.target.select()}
            onBlur={() => setTyped(null)}
            onKeyDown={(event) => {
              if (event.key !== 'Enter') return;
              const page = typedPage(typed ?? '', count);
              if (page !== null) goTo(page);
              setTyped(null);
              event.currentTarget.blur();
            }}
          />
          <span className="hx-pdf__page-of">/ {count || '–'}</span>
        </span>
        <span className="hx-pdf__grow" />
        <ToolButton icon="minus" label={t('next.pdf.zoomOut')} onClick={() => setZoom(zoomOut)} />
        <button
          type="button"
          className="hx-pdf__zoom"
          aria-label={t('next.pdf.fit')}
          onClick={() => setZoom(1)}
        >
          {Math.round(zoom * 100)}%
        </button>
        <ToolButton icon="plus" label={t('next.pdf.zoomIn')} onClick={() => setZoom(zoomIn)} />
        <ToolButton
          icon="search"
          label={t('next.pdf.search')}
          pressed={searchOn}
          onClick={() => {
            if (searchOn) search.clear();
            setSearchOn(!searchOn);
            requestAnimationFrame(() => searchInput.current?.focus());
          }}
        />
      </div>
      <div className="hx-pdf__find" data-on={searchOn ? 'true' : undefined} inert={!searchOn}>
        <input
          ref={searchInput}
          className="hx-pdf__find-input"
          placeholder={t('next.pdf.searchHint')}
          aria-label={t('next.pdf.search')}
          value={search.query}
          onChange={(event) => search.setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') search.step(event.shiftKey ? -1 : 1);
            if (event.key === 'Escape') {
              search.clear();
              setSearchOn(false);
            }
          }}
        />
        {found && (
          <span className="hx-pdf__find-count" role="status">
            {search.hits.length > 0 && `${search.index + 1} / ${search.hits.length}`}
            {search.hits.length === 0 && search.done && t('next.pdf.nothing')}
          </span>
        )}
        <ToolButton icon="chevron-up" label={t('next.pdf.prev')} onClick={() => search.step(-1)} />
        <ToolButton icon="chevron-down" label={t('next.pdf.next')} onClick={() => search.step(1)} />
      </div>
      <div className="hx-pdf__body">
        <div className="hx-pdf__rail" data-on={railOn ? 'true' : undefined} ref={railRef} inert={!railOn}>
          {outline.length > 0 && (
            <div className="hx-pdf__rail-tabs">
              <Segmented
                label={t('next.pdf.pages')}
                value={railTab}
                options={[
                  { value: RAIL.pages, label: t('next.pdf.pages') },
                  { value: RAIL.contents, label: t('next.pdf.contents') },
                ]}
                onChange={setRailTab}
              />
            </div>
          )}
          {railTab === RAIL.contents && outline.length > 0 ? (
            <Outline items={outline} onPick={(dest) => void openDest(dest)} />
          ) : (
            doc &&
            railOn &&
            proportions.map((_, page) => (
              <button
                key={page}
                type="button"
                className="hx-pdf__thumb"
                data-now={page === now ? 'true' : undefined}
                aria-label={`${t('next.pdf.pageNumber')} ${page + 1}`}
                onClick={() => goTo(page)}
              >
                <span
                  className="hx-pdf__thumb-sheet"
                  ref={(element) => element?.style.setProperty('--h', `${thumbHeight(page)}px`)}
                >
                  {Math.abs(page - now) <= NEAR_PAGES * 2 && (
                    <PdfCanvas doc={doc} page={page} width={THUMB_WIDTH} />
                  )}
                </span>
                <span className="hx-pdf__thumb-num">{page + 1}</span>
              </button>
            ))
          )}
        </div>
        <div className="hx-pdf__scroll" ref={scroller} onScroll={onScroll}>
          {!doc && (
            <div role="status" aria-label={t('next.pdf.opening')} className="hx-pdf__wait">
              <span className="hx-sk hx-pdf__wait-sheet" />
            </div>
          )}
          {doc &&
            heights.map((height, page) => (
              <div
                key={page}
                className="hx-pdf__sheet"
                ref={(element) => {
                  element?.style.setProperty('--w', `${width}px`);
                  element?.style.setProperty('--h', `${height}px`);
                }}
                data-page={page + 1}
              >
                {near(page) && width > 0 && (
                  <PdfCanvas doc={doc} page={page} width={width} text onText={search.onText} />
                )}
              </div>
            ))}
        </div>
      </div>
    </div>
  );
};

export default PdfView;
