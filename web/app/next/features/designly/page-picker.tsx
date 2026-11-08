// The list of a design's pages that opens from the pane's pager: a search where there are many pages, a row
// per page with its icon, name and path as a muted word, the current page ticked, the arrows moving the highlight
// and Enter going there. On the phone it is a sheet like the bar's menus. It opens from the pager under the page
// (above it) and from the address bar (below it); either way it is no taller than the window leaves it.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ISitePage } from '@/next/features/designly/site-view';
import { pagesFound, rowAfter, SEARCH_FROM } from '@/next/features/designly/page-pick';
import Icon from '@/next/ui/icon';
import { SheetGrip } from '@/next/ui/sheet';
import { t } from '@/next/i18n';

interface IPagePickerProps {
  pages: readonly ISitePage[];
  /** The page on screen. */
  path: string | null;
  state: string;
  onGo: (path: string) => void;
  onClose: () => void;
  /** Where the list stands by its button: above it (the pager) or below it, centred or from its start. */
  place?: TPickerPlace;
  /** A page's own download button at the end of its row, given where it was pressed. */
  onSave?: (path: string, at: DOMRect) => void;
  /** The download button's words for a page. */
  saveLabel?: (page: ISitePage) => string;
}

export const PICKER_PLACE = { up: 'up', center: 'center', start: 'start' } as const;
export type TPickerPlace = (typeof PICKER_PLACE)[keyof typeof PICKER_PLACE];

const PLACE_CLASS: Record<TPickerPlace, string> = {
  up: 'hx-pdock__menu',
  center: 'hx-site-menu--center',
  start: 'hx-site-menu--start',
};

/** What the list keeps clear of the window's edge. */
const EDGE = 12;

const PagePicker = ({
  pages,
  path,
  state,
  onGo,
  onClose,
  place = PICKER_PLACE.up,
  onSave,
  saveLabel,
}: IPagePickerProps) => {
  const [query, setQuery] = useState('');
  const found = pagesFound(pages, query);
  const [at, setAt] = useState(() =>
    Math.max(
      0,
      pages.findIndex((p) => p.path === path),
    ),
  );
  const row = found.length === 0 ? -1 : Math.min(at, found.length - 1);
  const searchable = pages.length > SEARCH_FROM;
  const box = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);

  // The room from the list's anchored edge to the window's: it scrolls inside that, never past the window.
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const fit = () => {
      const rect = el.getBoundingClientRect();
      const room = place === PICKER_PLACE.up ? rect.bottom - EDGE : window.innerHeight - rect.top - EDGE;
      el.style.setProperty('--hx-pgpick-room', `${Math.max(160, Math.round(room))}px`);
    };
    fit();
    window.addEventListener('resize', fit);
    return () => window.removeEventListener('resize', fit);
  }, [place]);

  useEffect(() => {
    if (searchable) input.current?.focus({ preventScroll: true });
    else box.current?.focus({ preventScroll: true });
  }, [searchable]);

  // The highlighted row stays in view as the arrows move it; on opening, the page on screen stands mid-list.
  const opened = useRef(false);
  useEffect(() => {
    const block = opened.current ? 'nearest' : 'center';
    opened.current = true;
    box.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block });
  }, [row]);

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter') {
      const page = found[row];
      if (!page) return;
      event.preventDefault();
      onGo(page.path);
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    if ((event.key === 'Home' || event.key === 'End') && event.target === input.current) return;
    event.preventDefault();
    setAt(rowAfter(event.key, row, found.length));
  };

  return (
    <div
      ref={box}
      className={`hx-site-menu ${PLACE_CLASS[place]} hx-pgpick hx-sheet-phone`}
      data-state={state}
      role="dialog"
      aria-label={t('next.site.pages')}
      tabIndex={-1}
      onKeyDown={onKeyDown}
    >
      <SheetGrip />
      {searchable && (
        <label className="hx-pgpick__search">
          <Icon name="search" size="sm" />
          <input
            ref={input}
            type="text"
            value={query}
            placeholder={t('next.site.findPage')}
            aria-label={t('next.site.findPage')}
            onChange={(event) => {
              setQuery(event.target.value);
              setAt(0);
            }}
          />
        </label>
      )}
      <div className="hx-pgpick__list" role="listbox" aria-label={t('next.site.pages')}>
        {found.map((p, i) => {
          const option = (
            <button
              key={p.path}
              type="button"
              role="option"
              tabIndex={-1}
              className="hx-pgpick__row"
              aria-selected={p.path === path}
              data-active={i === row}
              onMouseMove={() => i !== row && setAt(i)}
              onClick={() => onGo(p.path)}
            >
              <span className="hx-pgpick__ic">
                <Icon name="file-text" size="sm" />
              </span>
              <span className="hx-pgpick__name">{p.name}</span>
              <span className="hx-pgpick__meta">{p.path}</span>
              <Icon name="check" size="sm" className="hx-pgpick__check" />
            </button>
          );
          if (!onSave) return option;
          return (
            <div key={p.path} className="hx-pgpick__item">
              {option}
              <button
                type="button"
                tabIndex={-1}
                className="hx-head-btn hx-pgpick__dl"
                aria-label={saveLabel?.(p)}
                title={saveLabel?.(p)}
                onClick={(event) => onSave(p.path, event.currentTarget.getBoundingClientRect())}
              >
                <Icon name="download" size="sm" />
              </button>
            </div>
          );
        })}
        {found.length === 0 && <div className="hx-pgpick__empty">{t('next.site.pagesNone')}</div>}
      </div>
    </div>
  );
};

export default PagePicker;
