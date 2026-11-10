// «Найти в файлах» (⇧⌘F) of «Файлы» (HRN-679): a piece of text across the folder's files, the lines that hold it grouped
// by file, the line opened in the editor with the text selected. What was searched stays while the tab is open, so the
// panel comes back with it after a line is looked at.
import { Fragment, useEffect, useRef, useState, type KeyboardEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface ITextHit {
  /** As the files tab opens it: relative to the chat's folder, or absolute. */
  path: string;
  line: number;
  col: number;
  text: string;
}

export interface IFoundText {
  hits: readonly ITextHit[];
  truncated: boolean;
}

/** What was searched and found; kept by the view while the tab is open. */
export interface IFindState {
  query: string;
  /** The text `found` is for. */
  searched: string;
  found: IFoundText | null;
  failure: string | null;
}

export const NO_FIND: IFindState = { query: '', searched: '', found: null, failure: null };

export interface IFindInFilesProps {
  find: (query: string, signal: AbortSignal) => Promise<IFoundText>;
  state: IFindState;
  onState: (state: IFindState) => void;
  onOpen: (hit: ITextHit, size: number) => void;
  onClose: () => void;
}

/** A pause in typing before the machine is asked; a shorter text is not looked for until Enter. */
const TYPE_MS = 300;
const MIN_CHARS = 2;

const nameOf = (path: string) => path.slice(path.lastIndexOf('/') + 1);
const dirOf = (path: string) => (path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '');

/** The line with the first place that holds the query marked; the case counts only with a capital in the query. */
const marked = (text: string, query: string) => {
  const fold = query === query.toLowerCase();
  const at = (fold ? text.toLowerCase() : text).indexOf(fold ? query.toLowerCase() : query);
  if (at < 0) return text;
  return (
    <>
      {text.slice(0, at)}
      <mark>{text.slice(at, at + query.length)}</mark>
      {text.slice(at + query.length)}
    </>
  );
};

const FindInFiles = ({ find, state, onState, onOpen, onClose }: IFindInFilesProps) => {
  const [busy, setBusy] = useState(false);
  const [at, setAt] = useState(0);
  const asking = useRef<AbortController | null>(null);
  const list = useRef<HTMLDivElement>(null);
  const run = (query: string) => {
    asking.current?.abort();
    const text = query.trim();
    if (text === '') {
      onState({ query, searched: '', found: null, failure: null });
      return;
    }
    const abort = new AbortController();
    asking.current = abort;
    setBusy(true);
    find(text, abort.signal).then(
      (found) => {
        if (abort.signal.aborted) return;
        onState({ query, searched: query, found, failure: null });
        setAt(0);
        setBusy(false);
      },
      (failure: unknown) => {
        if (abort.signal.aborted) return;
        onState({
          query,
          searched: query,
          found: null,
          failure: failure instanceof Error ? failure.message : t('next.files.findFailed'),
        });
        setBusy(false);
      },
    );
  };
  const timer = useRef(0);
  useEffect(
    () => () => {
      window.clearTimeout(timer.current);
      asking.current?.abort();
    },
    [],
  );
  const type = (query: string) => {
    onState({ ...state, query });
    window.clearTimeout(timer.current);
    if (query.trim().length >= MIN_CHARS) timer.current = window.setTimeout(() => run(query), TYPE_MS);
  };
  const hits = state.found?.hits ?? [];
  useEffect(() => {
    list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [at]);
  const open = (hit: ITextHit) => onOpen(hit, state.searched.trim().length);
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (hits.length > 0)
        setAt((now) => (now + (event.key === 'ArrowDown' ? 1 : hits.length - 1)) % hits.length);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const hit = hits[at];
      // Enter opens the line picked once the results are those of the text typed; otherwise it searches now.
      if (hit && !busy && state.searched === state.query) open(hit);
      else {
        window.clearTimeout(timer.current);
        run(state.query);
      }
    } else if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onClose();
    }
  };
  const files = new Set(hits.map((h) => h.path)).size;
  return (
    <div className="hx-fif" role="dialog" aria-label={t('next.files.find')}>
      <div className="hx-qo__field">
        <Icon name="text-search" size="sm" />
        <input
          className="hx-qo__input"
          autoFocus
          value={state.query}
          placeholder={t('next.files.findHint')}
          aria-label={t('next.files.find')}
          onChange={(e) => type(e.target.value)}
          onKeyDown={onKey}
        />
        <button
          type="button"
          className="hx-head-btn"
          aria-label={t('next.files.findClose')}
          onClick={onClose}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      <div className="hx-fif__sum" role="status">
        {busy && t('next.files.finding')}
        {!busy && state.failure}
        {!busy &&
          state.failure === null &&
          state.found !== null &&
          hits.length === 0 &&
          t('next.files.findNone')}
        {!busy &&
          state.failure === null &&
          hits.length > 0 &&
          t(state.found?.truncated ? 'next.files.foundMore' : 'next.files.found', { n: hits.length, files })}
      </div>
      <div className="hx-fif__list" role="listbox" ref={list} aria-label={t('next.files.find')}>
        {hits.map((hit, i) => {
          const head = i === 0 || hits[i - 1].path !== hit.path;
          return (
            <Fragment key={`${hit.path}:${hit.line}:${hit.col}`}>
              {head && (
                <div className="hx-fif__file" title={hit.path}>
                  <Icon name="file-text" size="sm" />
                  <span className="hx-qo__name">{nameOf(hit.path)}</span>
                  <span className="hx-qo__dir">{dirOf(hit.path)}</span>
                </div>
              )}
              <div
                role="option"
                aria-selected={i === at}
                className="hx-fif__hit"
                onMouseDown={(e) => e.preventDefault()}
                onMouseMove={() => setAt(i)}
                onClick={() => open(hit)}
              >
                <span className="hx-fif__line">{hit.line}</span>
                <span className="hx-fif__text">{marked(hit.text, state.searched.trim())}</span>
              </div>
            </Fragment>
          );
        })}
      </div>
    </div>
  );
};

export default FindInFiles;
