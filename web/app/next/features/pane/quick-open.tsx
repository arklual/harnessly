// «Перейти к файлу» (⌘P) of «Файлы»: a field over the list, the files of the folder whose paths match what
// is typed (letters in order, as the «@» of the chat field finds them), arrows to move, Enter to open, Esc to leave.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface IQuickOpenProps {
  /** The paths that match the query, best first, relative to the folder searched. */
  find: (query: string, signal: AbortSignal) => Promise<readonly string[]>;
  onOpen: (path: string) => void;
  onClose: () => void;
}

/** A pause in typing before the machine is asked. */
const TYPE_MS = 120;

const nameOf = (path: string) => path.slice(path.lastIndexOf('/') + 1);
const dirOf = (path: string) => (path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '');

const QuickOpen = ({ find, onOpen, onClose }: IQuickOpenProps) => {
  const [query, setQuery] = useState('');
  // What the machine answered for which query: null while the first answer is on its way.
  const [found, setFound] = useState<{ query: string; paths: readonly string[] } | null>(null);
  const [failed, setFailed] = useState(false);
  const [at, setAt] = useState(0);
  const list = useRef<HTMLUListElement>(null);
  // The first answer comes at once, the next after a pause in typing.
  const asked = useRef(false);
  useEffect(() => {
    const abort = new AbortController();
    const pause = asked.current ? TYPE_MS : 0;
    asked.current = true;
    const timer = window.setTimeout(() => {
      find(query, abort.signal).then(
        (paths) => {
          setFound({ query, paths });
          setFailed(false);
          setAt(0);
        },
        () => {
          if (!abort.signal.aborted) setFailed(true);
        },
      );
    }, pause);
    return () => {
      abort.abort();
      window.clearTimeout(timer);
    };
  }, [query, find]);
  useEffect(() => {
    list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [at]);
  const paths = found?.paths ?? [];
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (paths.length > 0)
        setAt((now) => (now + (event.key === 'ArrowDown' ? 1 : paths.length - 1)) % paths.length);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const path = paths[at];
      if (path !== undefined) onOpen(path);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onClose();
    }
  };
  return (
    <div className="hx-qo" role="dialog" aria-label={t('next.files.quickOpen')}>
      <div className="hx-qo__field">
        <Icon name="search" size="sm" />
        <input
          className="hx-qo__input"
          autoFocus
          value={query}
          placeholder={t('next.files.quickOpenHint')}
          aria-label={t('next.files.quickOpen')}
          role="combobox"
          aria-expanded={paths.length > 0}
          aria-controls="hx-qo-list"
          aria-activedescendant={paths.length > 0 ? `hx-qo-${at}` : undefined}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKey}
          onBlur={(e) => {
            if (!e.currentTarget.closest('.hx-qo')?.contains(e.relatedTarget as Node | null)) onClose();
          }}
        />
      </div>
      <ul className="hx-qo__list" id="hx-qo-list" role="listbox" ref={list}>
        {paths.map((path, i) => (
          <li
            key={path}
            id={`hx-qo-${i}`}
            role="option"
            aria-selected={i === at}
            className="hx-qo__row"
            onMouseDown={(e) => e.preventDefault()}
            onMouseMove={() => setAt(i)}
            onClick={() => onOpen(path)}
          >
            <Icon name="file-text" size="sm" />
            <span className="hx-qo__name">{nameOf(path)}</span>
            <span className="hx-qo__dir">{dirOf(path)}</span>
          </li>
        ))}
        {found !== null && paths.length === 0 && !failed && (
          <li className="hx-qo__note">{t('next.files.quickOpenNone')}</li>
        )}
        {failed && <li className="hx-qo__note">{t('next.files.quickOpenFailed')}</li>}
      </ul>
    </div>
  );
};

export default QuickOpen;
