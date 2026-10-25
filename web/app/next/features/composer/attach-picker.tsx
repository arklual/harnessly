// One picker of «+» (a design, a document, a board, a file of the project, a merge request or a task): a search over
// its source, rows with a picture or the kind's icon, ↑/↓ and Enter, a loader while the machine answers, words when
// nothing is there, «Повторить» when the ask failed. It sits in the «+» panel in place of the menu, so it is a popover
// on a computer and the same sheet on a phone (one implementation).
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import {
  ATTACH_KIND,
  attachSource,
  type IAttachRow,
  type IAttachScope,
  type TAttachKind,
} from '@/next/features/composer/attach-sources';
import { REF_ICON } from '@/next/features/chat/attached';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { t } from '@/next/i18n';

/** The search waits this long after the last key before it asks the machine. */
export const PICK_DEBOUNCE_MS = 160;

const TITLE_KEYS: Record<TAttachKind, string> = {
  project: 'next.attach.project',
  design: 'next.attach.design',
  doc: 'next.attach.doc',
  board: 'next.attach.board',
  link: 'next.attach.link',
} as const;

const SEARCH_KEYS: Record<TAttachKind, string> = {
  project: 'next.attach.searchProject',
  design: 'next.attach.searchDesign',
  doc: 'next.attach.searchDoc',
  board: 'next.attach.searchBoard',
  link: 'next.attach.searchLink',
} as const;

const EMPTY_KEYS: Record<TAttachKind, string> = {
  project: 'next.attach.emptyProject',
  design: 'next.attach.emptyDesign',
  doc: 'next.attach.emptyDoc',
  board: 'next.attach.emptyBoard',
  link: 'next.attach.emptyLink',
} as const;

type TLoad = { k: 'loading' } | { k: 'ready'; rows: IAttachRow[] } | { k: 'failed'; reason: string };

export interface IAttachPickerProps {
  kind: TAttachKind;
  scope: IAttachScope;
  /** The rows already in the field (their keys): they show a check. */
  attached: ReadonlySet<string>;
  onPick: (row: IAttachRow) => void;
  onBack: () => void;
}

const reasonOf = (failure: unknown): string => (failure instanceof Error ? failure.message : String(failure));

const Thumb = ({ row }: { row: IAttachRow }) => {
  const [broken, setBroken] = useState(false);
  if (row.thumb && !broken) {
    return (
      <span className="hx-pick__thumb" data-pic="true">
        <img src={row.thumb} alt="" loading="lazy" onError={() => setBroken(true)} />
      </span>
    );
  }
  return (
    <span className="hx-pick__thumb">
      <Icon name={REF_ICON[row.ref.kind]} size="sm" />
    </span>
  );
};

const AttachPicker = ({ kind, scope, attached, onPick, onBack }: IAttachPickerProps) => {
  const [query, setQuery] = useState('');
  const [load, setLoad] = useState<TLoad>({ k: 'loading' });
  const [active, setActive] = useState(0);
  const [again, setAgain] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);
  const source = useMemo(() => attachSource(kind, scope), [kind, scope]);
  const typing = kind === ATTACH_KIND.link;

  useEffect(() => {
    const abort = new AbortController();
    // A link is read as it is typed; a source asks the machine a moment after the last key.
    const timer = setTimeout(
      () => {
        source(query, abort.signal).then(
          (rows) => {
            if (abort.signal.aborted) return;
            setLoad({ k: 'ready', rows });
            setActive(0);
          },
          (failure: unknown) => {
            if (!abort.signal.aborted) setLoad({ k: 'failed', reason: reasonOf(failure) });
          },
        );
      },
      typing ? 0 : PICK_DEBOUNCE_MS,
    );
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
  }, [source, query, typing, again]);

  // The row under the keys stays in sight.
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-i="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active]);

  const rows = load.k === 'ready' ? load.rows : [];
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (rows.length === 0) return;
      const step = event.key === 'ArrowDown' ? 1 : -1;
      setActive((now) => (now + step + rows.length) % rows.length);
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      const row = rows[active];
      if (row) onPick(row);
      return;
    }
    if (event.key === 'Backspace' && query === '') {
      event.preventDefault();
      onBack();
    }
  };

  let body = null;
  if (load.k === 'loading') {
    body = (
      <div className="hx-pick__state" data-busy="true" role="status">
        <span className="hx-spin" aria-hidden="true" />
        {t('next.attach.loading')}
      </div>
    );
  } else if (load.k === 'failed') {
    body = (
      <LoadFailed
        size={LOAD_FAILED_SIZE.inline}
        title={t('next.attach.failed')}
        reason={load.reason}
        onRetry={() => {
          setLoad({ k: 'loading' });
          setAgain((n) => n + 1);
        }}
      />
    );
  } else if (rows.length === 0) {
    body = <div className="hx-pick__state">{t(EMPTY_KEYS[kind])}</div>;
  } else {
    body = rows.map((row, i) => (
      <button
        key={row.key}
        type="button"
        className="hx-pick__row"
        role="option"
        id={`hx-pick-${kind}-${i}`}
        data-i={i}
        aria-selected={i === active}
        data-picked={attached.has(row.key) || undefined}
        onMouseMove={() => setActive(i)}
        onClick={() => onPick(row)}
      >
        <Thumb row={row} />
        <span className="hx-pick__text">
          <span className="hx-pick__name">{row.title}</span>
          {row.hint && <span className="hx-pick__hint">{row.hint}</span>}
        </span>
        {attached.has(row.key) && <Icon name="check" size="sm" className="hx-pick__check" />}
      </button>
    ));
  }

  return (
    <div className="hx-plus__pane hx-pick" data-sub="true" data-kind={kind}>
      <div className="hx-pick__head">
        <button type="button" className="hx-pick__back" aria-label={t('next.attach.back')} onClick={onBack}>
          <Icon name="chevron-left" size="sm" />
        </button>
        <span className="hx-pick__title">{t(TITLE_KEYS[kind])}</span>
      </div>
      <label className="hx-pick__search">
        <Icon name={typing ? 'link' : 'search'} size="sm" />
        <input
          type="search"
          autoFocus
          autoComplete="off"
          spellCheck={false}
          placeholder={t(SEARCH_KEYS[kind])}
          aria-label={t(SEARCH_KEYS[kind])}
          aria-controls={`hx-pick-${kind}`}
          aria-activedescendant={rows.length > 0 ? `hx-pick-${kind}-${active}` : undefined}
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            if (!typing) setLoad((now) => (now.k === 'failed' ? { k: 'loading' } : now));
          }}
          onKeyDown={onKeyDown}
        />
      </label>
      <div
        ref={listRef}
        id={`hx-pick-${kind}`}
        className="hx-pick__list"
        role="listbox"
        aria-label={t(TITLE_KEYS[kind])}
      >
        {body}
      </div>
    </div>
  );
};

export default AttachPicker;
