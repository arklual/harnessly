import { Dialog } from '@base-ui/react/dialog';
import { useRef, useState, type KeyboardEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { usePortalContainer } from '@/next/ui/portal';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { useDelayed as useDelayedFlag } from '@/next/ui/skeleton';
import { partsOf } from '@/next/features/search/highlight';

export interface ISearchScope {
  id: string;
  label: string;
}

export interface ISearchRow {
  id: string;
  /** chat, message, file, link, design: the icon's look. */
  kind: string;
  icon: string;
  title: string;
  /** The words around the match, for a message. */
  snippet?: string;
  /** Where it is: a chat's project, a file's chat, a date. */
  meta: string;
}

export interface ISearchGroup {
  id: string;
  label: string;
  rows: readonly ISearchRow[];
}

export interface ISearchDialogProps {
  open: boolean;
  onClose: () => void;
  query: string;
  onQuery: (query: string) => void;
  scopes: readonly ISearchScope[];
  scope: string;
  onScope: (scope: string) => void;
  /** The answer for the query; null while it is on its way. */
  results: readonly ISearchGroup[] | null;
  /** What shows before anything is typed. */
  recent: readonly ISearchRow[];
  onPick: (row: ISearchRow) => void;
  /** The chat list did not come: why, and how to ask again. null while it is on its way or here. */
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
}

const groupsShown = (
  typed: boolean,
  results: readonly ISearchGroup[] | null,
  recent: readonly ISearchRow[],
): readonly ISearchGroup[] => {
  if (typed) return results ?? [];
  if (recent.length === 0) return [];
  return [{ id: 'recent', label: t('next.search.recent'), rows: recent }];
};

const Highlighted = ({ text, query }: { text: string; query: string }) => (
  <>
    {partsOf(text, query).map((part, i) => (
      <span key={i} data-hit={part.hit || undefined}>
        {part.text}
      </span>
    ))}
  </>
);

/**
 * Search, ⌘K: one field, «where» scopes (Tab moves between them), results by group with the match lit, ↑↓ and Enter.
 * An empty field shows the recent; nothing found says so with a hint.
 */
const SearchDialog = ({
  open,
  onClose,
  query,
  onQuery,
  scopes,
  scope,
  onScope,
  results,
  recent,
  onPick,
  failed = null,
}: ISearchDialogProps) => {
  const container = usePortalContainer();
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState(0);
  const typed = query.trim() !== '';
  const groups = groupsShown(typed, results, recent);
  const starts = groups.map((_, i) => groups.slice(0, i).reduce((sum, group) => sum + group.rows.length, 0));
  const rows = groups.flatMap((group) => group.rows);
  const at = Math.min(selected, Math.max(0, rows.length - 1));
  const unavailable = failed !== null && results === null;
  const searching = useDelayedFlag(typed && results === null && !unavailable);
  const nothing = typed && results !== null && rows.length === 0;

  const move = (delta: number) => {
    if (rows.length === 0) return;
    const next = (at + delta + rows.length) % rows.length;
    setSelected(next);
    listRef.current?.querySelector(`[data-row="${next}"]`)?.scrollIntoView({ block: 'nearest' });
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      move(1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      move(-1);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const row = rows[at];
      if (row) onPick(row);
    } else if (event.key === 'Tab' && scopes.length > 0) {
      event.preventDefault();
      const current = scopes.findIndex((item) => item.id === scope);
      const next = scopes[(current + (event.shiftKey ? -1 : 1) + scopes.length) % scopes.length];
      if (next) onScope(next.id);
    }
  };

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Portal container={container}>
        <div className="hx-srch">
          <Dialog.Backdrop className="hx-srch__scrim" />
          <Dialog.Popup className="hx-srch__box" aria-label={t('next.search.title')} initialFocus={inputRef}>
            <label className="hx-srch__field">
              <Icon name="search" />
              <input
                ref={inputRef}
                type="search"
                placeholder={t('next.search.placeholder')}
                autoComplete="off"
                enterKeyHint="search"
                value={query}
                onChange={(event) => {
                  setSelected(0);
                  onQuery(event.target.value);
                }}
                onKeyDown={onKeyDown}
              />
              {typed && (
                <button
                  type="button"
                  className="hx-srch__clear"
                  aria-label={t('next.search.clear')}
                  onClick={() => {
                    onQuery('');
                    inputRef.current?.focus();
                  }}
                >
                  <Icon name="circle-x" size="sm" />
                </button>
              )}
              <Dialog.Close className="hx-srch__cancel">{t('next.search.cancel')}</Dialog.Close>
            </label>
            {scopes.length > 0 && (
              <div className="hx-srch__scopes" role="tablist" aria-label={t('next.search.where')}>
                {scopes.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    role="tab"
                    className="hx-srch__scope"
                    aria-selected={item.id === scope}
                    onClick={() => {
                      setSelected(0);
                      onScope(item.id);
                    }}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            )}
            <div className="hx-srch__list" ref={listRef} role="listbox" aria-label={t('next.search.results')}>
              {searching && (
                <div className="hx-srch__wait" role="status">
                  <Icon name="loader-circle" size="sm" />
                  {t('next.search.searching')}
                </div>
              )}
              {groups.map((group, groupIndex) => (
                <div key={group.id} className="hx-srch__group" role="group" aria-label={group.label}>
                  <div className="hx-srch__label">{group.label}</div>
                  {group.rows.map((row, rowIndex) => {
                    const mine = (starts[groupIndex] ?? 0) + rowIndex;
                    return (
                      <button
                        key={row.id}
                        type="button"
                        role="option"
                        aria-selected={mine === at}
                        className="hx-srch__row"
                        data-row={mine}
                        data-sel={mine === at}
                        data-kind={row.kind}
                        onClick={() => onPick(row)}
                        onPointerMove={() => {
                          if (mine !== at) setSelected(mine);
                        }}
                      >
                        <span className="hx-srch__ic">
                          <Icon name={row.icon} size="sm" />
                        </span>
                        <span className="hx-srch__text">
                          <span className="hx-srch__title">
                            <Highlighted text={row.title} query={query} />
                          </span>
                          {row.snippet && (
                            <span className="hx-srch__snip">
                              <Highlighted text={row.snippet} query={query} />
                            </span>
                          )}
                        </span>
                        <span className="hx-srch__meta">{row.meta}</span>
                        <Icon name="corner-down-left" size="sm" className="hx-srch__go" />
                      </button>
                    );
                  })}
                </div>
              ))}
              {nothing && (
                <div className="hx-srch__none">
                  <b>{t('next.search.nothing')}</b>
                  <span>{t('next.search.nothingHint')}</span>
                </div>
              )}
              {unavailable && (
                <LoadFailed
                  size={LOAD_FAILED_SIZE.pane}
                  title={t('next.load.search')}
                  reason={failed.reason}
                  onRetry={failed.onRetry}
                />
              )}
              {!typed && recent.length === 0 && !unavailable && (
                <div className="hx-srch__none">
                  <span>{t('next.search.start')}</span>
                </div>
              )}
            </div>
            <div className="hx-srch__foot">
              <span>
                <kbd>↑</kbd>
                <kbd>↓</kbd>
                {t('next.search.keySelect')}
              </span>
              <span>
                <kbd>↵</kbd>
                {t('next.search.keyOpen')}
              </span>
              {scopes.length > 0 && (
                <span>
                  <kbd>Tab</kbd>
                  {t('next.search.where')}
                </span>
              )}
              <span className="hx-spacer" />
              <span>
                <kbd>⌘K</kbd>
              </span>
            </div>
          </Dialog.Popup>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
};

export default SearchDialog;
