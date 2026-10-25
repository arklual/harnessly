// The list of a design's pages that opens from the pane's pager: a search where there are many pages, a row
// per page with its icon, name and path as a muted word, the current page ticked, the arrows moving the highlight
// and Enter going there. On the phone it is a sheet like the bar's menus.
import { useEffect, useRef, useState } from 'react';
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
}

const PagePicker = ({ pages, path, state, onGo, onClose }: IPagePickerProps) => {
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

  useEffect(() => {
    if (searchable) input.current?.focus({ preventScroll: true });
    else box.current?.focus({ preventScroll: true });
  }, [searchable]);

  // The highlighted row stays in view as the arrows move it.
  useEffect(() => {
    box.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' });
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
      className="hx-site-menu hx-pdock__menu hx-pgpick hx-sheet-phone"
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
        {found.map((p, i) => (
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
        ))}
        {found.length === 0 && <div className="hx-pgpick__empty">{t('next.site.pagesNone')}</div>}
      </div>
    </div>
  );
};

export default PagePicker;
