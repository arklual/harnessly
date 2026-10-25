// The prompts over the field (the design's .hx-ph): ↑ in an empty field shows what was sent before, «/» at the start
// of the field looks for a quick prompt by its name. ↑↓ choose, Enter or Tab put the text into the field, Esc closes.
// With text in the field the list is narrowed by it and the text can be saved as a quick prompt under a /name.
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react';
import {
  addQuick,
  addRecent,
  clampSelection,
  filterRows,
  loadQuick,
  loadRecent,
  moveSelection,
  quickName,
  ROW_KIND,
  rowText,
  saveQuick,
  saveRecent,
  slashQuery,
  type IQuickPrompt,
  type TPromptRow,
} from '@/next/features/composer/prompts';
import Icon from '@/next/ui/icon';
import { usePresence, type TPresenceState } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

/** The panel's exit, as long as hx-anns-out in composer.css. */
const EXIT_MS = 180;

interface IOpenState {
  slash: boolean;
  query: string;
  selected: number;
  naming: boolean;
}

const OPENED: IOpenState = { slash: false, query: '', selected: 0, naming: false };

interface IPromptHistoryOptions {
  /** The field's text and its setter. */
  value: string;
  onChange: (value: string) => void;
  focusField: () => void;
  /** Off while the field is not the thing to type into (the recording bar). */
  enabled: boolean;
  /** The slash palette owns «/» (it lists the quick prompts among the rest): the list here opens by ↑ and «+» only. */
  slashPalette?: boolean;
}

export interface IPromptHistory {
  /** The panel to draw over the field; null while it is closed. */
  panel: ReactNode;
  /** Gives the key to the list when it takes it; true: the key is spent. */
  onKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => boolean;
  /** The text of the field changed by typing. */
  onInput: (text: string) => void;
  /** The quick prompts, as the field keeps them. */
  quick: readonly IQuickPrompt[];
  /** «Prompts» of the «+» menu. */
  show: () => void;
  /** A text goes out: it joins the history and the list closes. */
  remember: (text: string) => void;
  close: () => void;
}

export const usePromptHistory = ({
  value,
  onChange,
  focusField,
  enabled,
  slashPalette = false,
}: IPromptHistoryOptions): IPromptHistory => {
  const [recent, setRecent] = useState(loadRecent);
  const [quick, setQuick] = useState(loadQuick);
  const [open, setOpen] = useState<IOpenState | null>(null);
  const [name, setName] = useState('');
  const { value: shown, state } = usePresence(enabled ? open : null, EXIT_MS);

  const rows = shown ? filterRows(recent, quick, shown.query, shown.slash) : [];
  const selected = shown ? clampSelection(shown.selected, rows.length) : 0;

  const update = (patch: Partial<IOpenState>) =>
    setOpen((current) => (current ? { ...current, ...patch } : current));
  const close = () => setOpen(null);

  const fill = (row: TPromptRow) => {
    onChange(rowText(row));
    close();
    focusField();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    if (!enabled || event.nativeEvent.isComposing) return false;
    const plain = !event.shiftKey && !event.ctrlKey && !event.metaKey && !event.altKey;
    if (!open) {
      if (event.key !== 'ArrowUp' || value !== '' || !plain) return false;
      event.preventDefault();
      setName('');
      setOpen(OPENED);
      return true;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      if (open.slash) onChange('');
      close();
      return true;
    }
    if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && rows.length > 0) {
      event.preventDefault();
      update({ selected: moveSelection(selected, event.key === 'ArrowDown' ? 1 : -1, rows.length) });
      return true;
    }
    if ((event.key === 'Enter' || event.key === 'Tab') && plain && rows.length > 0) {
      event.preventDefault();
      fill(rows[selected]);
      return true;
    }
    return false;
  };

  const onInput = (text: string) => {
    if (!enabled) return;
    const command = slashQuery(text);
    if (command !== null && slashPalette) {
      if (open) close();
      return;
    }
    if (command !== null) {
      // «/» opens the quick prompts only where there are some: otherwise it is a path or a word that starts with it.
      if (open?.slash) update({ query: command, selected: 0 });
      else if (quick.length > 0) setOpen({ ...OPENED, slash: true, query: command });
      return;
    }
    if (open?.slash) close();
    else if (open) update({ query: text, selected: 0 });
  };

  const show = () => {
    setName('');
    setOpen(OPENED);
    focusField();
  };

  const remember = (text: string) => {
    close();
    if (!text.trim()) return;
    const next = addRecent(recent, text);
    setRecent(next);
    saveRecent(next);
  };

  const drop = (item: IQuickPrompt) => {
    const next = quick.filter((existing) => existing.name !== item.name);
    setQuick(next);
    saveQuick(next);
  };

  // «Save as quick»: the first press asks for the name, the second (or Enter in the name) saves the field's text.
  const save = () => {
    if (!shown) return;
    if (!shown.naming) {
      update({ naming: true });
      return;
    }
    const clean = quickName(name);
    if (!clean) {
      toast(t('next.prompts.badName'), { tone: 'error' });
      return;
    }
    const next = addQuick(quick, { name: clean, text: value.trim() });
    setQuick(next);
    saveQuick(next);
    update({ naming: false });
    setName('');
    toast(t('next.prompts.saved', { name: clean }));
  };

  const cancelNaming = () => {
    update({ naming: false });
    focusField();
  };

  const panel = shown ? (
    <PromptPanel
      state={state}
      rows={rows}
      selected={selected}
      empty={recent.length === 0 && quick.length === 0 && shown.query === ''}
      canSave={value.trim() !== '' && !shown.slash}
      naming={shown.naming}
      name={name}
      onName={setName}
      onPick={fill}
      onDrop={drop}
      onSave={save}
      onCancelNaming={cancelNaming}
    />
  ) : null;

  return { panel, onKeyDown, onInput, quick, show, remember, close };
};

interface IPromptPanelProps {
  state: TPresenceState;
  rows: TPromptRow[];
  selected: number;
  /** Nothing was ever sent or saved, so nothing is missing from the list: it just has nothing yet. */
  empty: boolean;
  canSave: boolean;
  naming: boolean;
  name: string;
  onName: (name: string) => void;
  onPick: (row: TPromptRow) => void;
  onDrop: (item: IQuickPrompt) => void;
  onSave: () => void;
  onCancelNaming: () => void;
}

const PromptPanel = ({
  state,
  rows,
  selected,
  empty,
  canSave,
  naming,
  name,
  onName,
  onPick,
  onDrop,
  onSave,
  onCancelNaming,
}: IPromptPanelProps) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);

  // The chosen row stays in sight when the list scrolls.
  useEffect(() => {
    rootRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' });
  }, [selected]);
  useEffect(() => {
    if (naming) nameRef.current?.focus();
  }, [naming]);

  // The field keeps the focus while the list is clicked (the name is the one thing in it to type into).
  const keepFocus = (event: MouseEvent) => {
    if (!(event.target instanceof HTMLInputElement)) event.preventDefault();
  };

  const onNameKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      onSave();
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onCancelNaming();
    }
  };

  // The rows come in one after another by their place (--j of the design).
  const row = (item: TPromptRow, index: number) => {
    const on = index === selected;
    const setPlace = (element: HTMLDivElement | null) => element?.style.setProperty('--j', String(index));
    if (item.kind === ROW_KIND.quick) {
      return (
        <div
          key={`q-${item.quick.name}`}
          ref={setPlace}
          className="hx-ph__row"
          role="option"
          aria-selected={on}
          tabIndex={-1}
          onClick={() => onPick(item)}
        >
          <span className="hx-ph__name">/{item.quick.name}</span>
          <span className="hx-ph__text">{item.quick.text}</span>
          <button
            type="button"
            className="hx-ph__x"
            aria-label={t('next.prompts.drop', { name: item.quick.name })}
            onClick={(event) => {
              event.stopPropagation();
              onDrop(item.quick);
            }}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
      );
    }
    return (
      <div
        key={`r-${item.text}`}
        ref={setPlace}
        className="hx-ph__row"
        role="option"
        aria-selected={on}
        tabIndex={-1}
        onClick={() => onPick(item)}
      >
        <Icon name="history" size="sm" />
        <span className="hx-ph__text">{item.text}</span>
      </div>
    );
  };

  const quickRows = rows.filter((item) => item.kind === ROW_KIND.quick);
  const recentRows = rows.filter((item) => item.kind === ROW_KIND.recent);

  return (
    <div
      ref={rootRef}
      className="hx-ph"
      data-state={state}
      role="listbox"
      aria-label={t('next.prompts.label')}
      onMouseDown={keepFocus}
    >
      {quickRows.length > 0 && <div className="hx-ph__label">{t('next.prompts.quick')}</div>}
      {quickRows.map((item) => row(item, rows.indexOf(item)))}
      {recentRows.length > 0 && <div className="hx-ph__label">{t('next.prompts.recent')}</div>}
      {recentRows.map((item) => row(item, rows.indexOf(item)))}
      {rows.length === 0 && (
        <div className="hx-ph__empty">{empty ? t('next.prompts.none') : t('next.prompts.notFound')}</div>
      )}
      <div className="hx-ph__foot">
        <span>
          <kbd>↑</kbd>
          <kbd>↓</kbd>
          {t('next.prompts.choose')}
        </span>
        <span>
          <kbd>↵</kbd>
          {t('next.prompts.insert')}
        </span>
        <span className="hx-spacer" />
        {naming && (
          <span className="hx-ph__nm">
            /
            <input
              ref={nameRef}
              type="text"
              autoComplete="off"
              value={name}
              placeholder={t('next.prompts.namePlaceholder')}
              aria-label={t('next.prompts.nameLabel')}
              onChange={(event) => onName(event.target.value)}
              onKeyDown={onNameKey}
            />
          </span>
        )}
        {canSave && (
          <button type="button" className="hx-ph__save" onClick={onSave}>
            <Icon name="bookmark-plus" size="sm" />
            {t('next.prompts.save')}
          </button>
        )}
      </div>
    </div>
  );
};
