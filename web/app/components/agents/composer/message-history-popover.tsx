import { useEffect, useId, useMemo, useRef, useState, type RefObject } from 'react';
import { X } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  LAYER_ALIGN,
  LISTBOX_ACTION,
  POPOVER_SIDE,
  TwButton,
  TwListbox,
  TwPopover,
  useTwListboxNav,
  type ITwOption,
} from '@/components/agents/ui';
import { relativeWhen } from '@/lib/hl-design-library/home-model';
import type { IHistoryEntry } from '@/lib/hl-message-history';
import { t } from '@/lib/i18n';

interface IMessageHistoryPopoverProps {
  open: boolean;
  onClose: () => void;
  anchorRef: RefObject<HTMLElement | null>;
  entries: readonly IHistoryEntry[];
  isLoading: boolean;
  isError: boolean;
  onFetch: () => void;
  /** The text goes back into the composer. */
  onSelect: (message: string) => void;
  onDelete: (id: string) => void;
}

const firstLine = (message: string) => message.split('\n', 1)[0];

/** The messages sent from this workspace, newest first, with a search field: picking one puts its text back into the composer. */
const MessageHistoryPopover = ({
  open,
  onClose,
  anchorRef,
  entries,
  isLoading,
  isError,
  onFetch,
  onSelect,
  onDelete,
}: IMessageHistoryPopoverProps) => {
  const listId = useId();
  const [query, setQuery] = useState('');
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- every opening starts with an empty search
    setQuery('');
    onFetch();
  }, [open, onFetch]);

  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle ? entries.filter((e) => e.message.toLowerCase().includes(needle)) : entries;
  }, [entries, query]);

  const options = useMemo<ITwOption[] | null>(() => {
    if (isLoading && entries.length === 0) return null;
    return shown.map((entry) => ({
      value: entry.id,
      label: firstLine(entry.message),
      meta: (
        <span className="tw-history-meta">
          <time className="tw-history-when" dateTime={entry.sentAt}>
            {relativeWhen(Date.parse(entry.sentAt))}
          </time>
          <button
            type="button"
            className="tw-history-remove"
            aria-label={t('workspace.messageHistory.remove')}
            title={t('workspace.messageHistory.remove')}
            onMouseDown={(event) => event.preventDefault()}
            onClick={(event) => {
              event.stopPropagation();
              onDelete(entry.id);
            }}
          >
            <X aria-hidden="true" />
          </button>
        </span>
      ),
    }));
  }, [entries.length, isLoading, onDelete, shown]);

  const nav = useTwListboxNav({ listId, options });
  const pick = (id: string) => {
    const entry = entries.find((e) => e.id === id);
    if (!entry) return;
    onSelect(entry.message);
    onClose();
  };

  return (
    <TwPopover
      open={open}
      onClose={onClose}
      anchorRef={anchorRef}
      side={POPOVER_SIDE.top}
      align={LAYER_ALIGN.start}
      aria-label={t('workspace.messageHistory.title')}
      initialFocusRef={searchRef}
      className="tw-history-popover"
    >
      <input
        ref={searchRef}
        type="search"
        className="tw-history-search"
        value={query}
        placeholder={t('workspace.messageHistory.search')}
        aria-label={t('workspace.messageHistory.search')}
        role="combobox"
        aria-expanded="true"
        aria-controls={listId}
        aria-activedescendant={nav.activeDescendant}
        autoComplete="off"
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => {
          const result = nav.handleKeyDown(event);
          if (result?.type === LISTBOX_ACTION.select) pick(result.value);
        }}
      />
      {isError && entries.length === 0 ? (
        <div className="tw-history-error" role="alert">
          <span>{t('workspace.messageHistory.loadFailed')}</span>
          <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.secondary} onClick={onFetch}>
            {t('workspace.messageHistory.retry')}
          </TwButton>
        </div>
      ) : (
        <TwListbox
          id={listId}
          options={options}
          value={null}
          aria-label={t('workspace.messageHistory.title')}
          activeValue={nav.activeValue}
          onActiveValueChange={nav.setActiveValue}
          focusable={false}
          emptyText={
            query.trim() ? t('workspace.messageHistory.noMatch') : t('workspace.messageHistory.empty')
          }
          loadingText={t('workspace.messageHistory.loading')}
          className="tw-history-list"
          onChange={pick}
        />
      )}
    </TwPopover>
  );
};

export default MessageHistoryPopover;
